# HTTP/2 流隧道实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 ClawBench 上新增一条 HTTP/2 流隧道传输通道，使用户在网络层只需放行 20000 一个端口即可使用 `-L` / `-R` 端口转发，SSH 通道原样保留为兜底。

**Architecture:** 一条 h2 连接承载多条被转发的 TCP 流（每个被转发的 TCP 连接 = 一个 HTTP 流；请求体承载 client→server 字节，响应体承载 server→client 字节，双向同时流式）。服务端复用现有 `http.Server` / `http.ServeMux`，不新增监听端口；`-L` 走 `POST /api/tunnel/stream`，`-R` 走长寿命控制流 `POST /api/tunnel/control`（NDJSON）+ 客户端凭单次 token 发起的 `claim` 流。客户端三端（Electron / Android / 前端）按优先级 h2-over-TLS → h2c → SSH 选择传输。

**Tech Stack:** Go 1.26（`net/http` 的 `http.Protocols`、`golang.org/x/net/http2` 仅测试用）；Electron `node:http2`（内建）；Android OkHttp 4.12.0（既有依赖）；Vue 3 + TypeScript。**零新增依赖。**

**设计来源（唯一事实来源，不要重新调研、不要推翻其决策）：** `docs/plans/2026-09-25-h2-tunnel-design.md`

---

## 0. 开工前必读

### 0.1 执行环境前置（不做完这步，后面的验证命令会假失败）

| 前置 | 命令 | 说明 |
|---|---|---|
| Go 工具链不在 PATH | 全程用 `/usr/local/go/bin/go` | 已确认 `go version go1.26.2 linux/amd64` |
| `cmd/server` 无法编译/测试 | `mkdir -p internal/frontend/dist && touch internal/frontend/dist/.gitkeep` | `internal/frontend/embed.go:10` 的 `//go:embed all:dist` 在 `dist/` 不存在或为空时报 `pattern all:dist: no matching files found`。**实测**：空目录仍报 `cannot embed directory dist: contains no embeddable files`，必须有至少一个文件。`.gitkeep` 被 `.gitignore:62` 的 `dist/` 规则忽略（`:38` 的 `!...gitkeep` 负向规则被后面的目录规则覆盖，实测 `git check-ignore` 命中 `.gitignore:62`），**不会污染 git status**。 |
| `desktop/node_modules` 为空 | `cd desktop && npm ci` | 实测 `desktop/node_modules/` 只有一个 `.vite/`，`npx vitest` 报 `Failed to resolve import "electron-store"`。**必须 `npm ci`（带 lock）**，不带 lock 的 `npm install` 会因 npm arborist bug 失败。 |
| Android 工具链 | `JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk ./gradlew ...` | JDK 17 已确认存在（JDK 21 的 jlink 会拒绝 AGP 的 ModuleTarget）；`/opt/android-sdk` 已确认存在。**必须用真 JDK 17，不能用 21。** |
| 全量测试是稀缺资源 | 跑前 `ps -eo pid,ppid,etime,cmd \| grep -E "vitest\|go test\|npm run build" \| grep -v grep` | 有则等待。优先隔离单跑；不重复跑；**绝不同时跑 `npm run build` 与全量 vitest**（会 OOM）。 |

### 0.2 设计文档与 HEAD 实际状态的两处漂移（**已实测，按本计划执行，不要按设计文档的旧数字**）

| 项 | 设计文档写的 | HEAD 实测 | 处理 |
|---|---|---|---|
| OpenAPI 计数 | 「150 路径 / 189 操作」→ +2 → 152/191 | **HEAD 实际已是 152 路径 / 191 操作**（`gopkg.in/yaml.v3` 解析确认：`paths=152 ops=191`）；而 `docs/spec/api/README.md:7` 与 `docs/spec/README.md:64` 的**文字仍是 150/189**（已陈旧 2） | T5 中：新增 2 条路径后实际为 **154 路径 / 193 操作**；文档文字要**改为 154/193**（同时修正已陈旧的 2 个差值）。**不要**写成 152/191——那会与 spec 实际内容不符。 |
| `desktop/src/main/tunnel.ts` 行数 | 876 行 | **572 行** | 行号锚点全部仍然准确，仅行数描述过时。 |

### 0.3 其他已实测的锚点修正（写代码时用这些）

- `web/src/composables/usePortForward.test.ts` 的**实际路径**是 `web/src/composables/__tests__/usePortForward.test.ts`（设计文档 §3.3/§9 写的路径不存在）。
- i18n `sshTunnel` 在 `web/src/i18n/locales/zh.ts:1565`（设计文档写 1566）、`en.ts:1562`。
- `desktop/src/main/tunnel.test.ts` 实测 **34** 个 `it(`（不是 63）。
- `cmd/server/main.go` 的 `reserveSSHPorts` 在 `:1739`、`hotReloadSSH` 在 `:1750`。
- **`cmd/server` 没有测试包**（`cmd/server/*_test.go` 不存在），`main()` 是 400+ 行单体函数 → T1 必须先抽一个可测的纯 helper（见 T1）。
- **stdlib `http.Transport.Protocols` 不做 h2c prior-knowledge**（实测：服务端 `SetHTTP1+SetUnencryptedHTTP2`，客户端也设了同样两项，实际协商到 `HTTP/1.1`）。**测试里的 h2c 客户端必须用 `golang.org/x/net/http2.Transport{AllowHTTP:true}`**（实测得到 `HTTP/2.0`；`golang.org/x/net v0.57.0` 已在 `go.mod:88` indirect，无需新增依赖）。

---

## 1. 任务总览与依赖图

```
阶段 1 服务端（最高风险优先）
  T1 三协议共存 ─────────────┐  （独立验收，阻塞 T2/T3 的端到端验证）
  T2 -L handler ─────────────┤
  T3 -R 控制流 + claim ──────┤  依赖 T1（h2c 才能跑起来）
  T4 ProxyRegistry 门控放宽 ─┘  依赖 T3（SetReverseBound 生命周期）
  T5 文档同步 ────────────────  依赖 T2+T3（路由与端点已定）

阶段 2 Electron
  T6 h2Transport.ts ──┐
  T7 tunnel.ts 接入 ──┴─ 依赖 T2/T3 协议冻结；T7 依赖 T6
  T8 传输选择配置 ────  依赖 T4（服务端 transport 配置）

阶段 3 Android（工作量最大）
  T9 TunnelStream + h2 实现 ──┐
  T10 本地 ServerSocket 监听 ─┤ 依赖 T9
  T11 -R 控制流 + claim ──────┤ 依赖 T9；与 T10 可并行（不同区域）
  T12 WifiLock / 息屏策略 ────┘ 依赖 T9/T11

阶段 4 前端
  T13 门控放宽 + 展示 + i18n ── 依赖 T8（桥契约）

阶段 5 集成验证
  T14 端到端 ────────────────── 依赖 T1-T13 全部
```

**可并行组：**
- 阶段 2 与阶段 3 **整体可并行**（不同语言/不同目录，互不碰文件）。
- 阶段 4 的 `usePortForward.ts` 改动与阶段 3 可并行；但 `clawbenchNative.ts` 桥契约必须等 T8 冻结。
- T5 文档同步只碰 `docs/` 与 `internal/api/openapi.yaml`，可在 T2/T3 完成后与阶段 2/3 并行。
- T10 与 T11 在 Android 内**不可并行**（都改 `BackgroundService.java` 的 `ensureConnection`/`disconnectInternal` 区域），但可与 T12 的 `maybeReleaseWifiLock`（:2388）区域并行——**注意同一文件多区域同时改易冲突，建议 T10→T11→T12 串行**。
- **T1 必须独立验收后再开 T2**（最高风险，失败会让现有 5 个 WS 端点全挂）。

**每个任务完成后立即 commit**（AGENTS.md：频繁提交）。

---

## 阶段 1 — 服务端

### Task 1: `http.Server.Protocols` 三协议共存（最高风险）

**为什么必须独立验收：** `http.Protocols` 一旦非 nil 就**完全取代**默认值。只调 `SetUnencryptedHTTP2(true)` 会**同时关掉 HTTP/1.1 与 h2-over-TLS**——实测 h1 客户端 `ECONNRESET`、`curl: (56) Recv failure: Connection reset by peer`。而现有 5 个 `coder/websocket` 端点硬依赖 `http.Hijacker`（`/go/pkg/mod/github.com/coder/websocket@v1.8.14/accept.go:128-133`），Go h2 明确不支持 hijack（`net/http/h2_bundle.go:4717`）。**漏掉 `SetHTTP1(true)` 会让 5 个 WS 端点全挂。** 因此这个任务必须自己跑通「h1 可用 + h2c 可用 + WS 可用」三件事，不可与后续合并验收。

**Files:**
- Create: `cmd/server/server_protocols.go`（抽出的纯 helper，`package main`）
- Create: `cmd/server/server_protocols_test.go`
- Modify: `cmd/server/main.go:1277`（`srv := &http.Server{Handler: mux}` → 加 `Protocols`）

**依赖：** 无（最先做）。**可并行：** 否，必须单独验收。
**前置：** `mkdir -p internal/frontend/dist && touch internal/frontend/dist/.gitkeep`（否则 `cmd/server` 无法编译/测试）。

**Step 1: 写失败测试**

新建 `cmd/server/server_protocols_test.go`。测试要覆盖三件事，且**h2c 客户端必须用 `golang.org/x/net/http2.Transport{AllowHTTP:true}`**（stdlib 不做 prior-knowledge，实测）：

```go
package main

import (
    "context"
    "crypto/tls"
    "fmt"
    "io"
    "net"
    "net/http"
    "testing"
    "time"

    "github.com/coder/websocket"
    "github.com/stretchr/testify/require"
    "golang.org/x/net/http2"
)

// 三协议共存：h1 仍可用（现有 WS 端点依赖它）、h2c 可用、WS 升级仍可用。
func TestServerProtocols_PlaintextCoexistence(t *testing.T) {
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    require.NoError(t, err)

    mux := http.NewServeMux()
    mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, r.Proto) // 回显协商到的协议，供断言
    })
    mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
        c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
        if err != nil {
            return
        }
        defer c.Close(websocket.StatusNormalClosure, "")
        _ = c.Write(r.Context(), websocket.MessageText, []byte("hello"))
    })

    srv := &http.Server{Handler: mux, Protocols: serverProtocols(false)}
    go srv.Serve(ln)
    defer srv.Close()
    time.Sleep(150 * time.Millisecond)

    base := "http://" + ln.Addr().String()

    // (a) h1 客户端仍可用 —— 这条守住 5 个 WS 端点。
    r1, err := http.Get(base + "/echo")
    require.NoError(t, err, "h1 must survive: Protocols non-nil replaces defaults")
    b1, _ := io.ReadAll(r1.Body)
    r1.Body.Close()
    require.Equal(t, "HTTP/1.1", string(b1))

    // (b) h2c prior-knowledge 客户端可用（stdlib 不做 prior-knowledge，必须 x/net）。
    tr := &http2.Transport{
        AllowHTTP: true,
        DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
            return net.Dial(network, addr)
        },
    }
    c2 := &http.Client{Transport: tr}
    r2, err := c2.Get(base + "/echo")
    require.NoError(t, err, "h2c prior-knowledge must work")
    b2, _ := io.ReadAll(r2.Body)
    r2.Body.Close()
    require.Equal(t, "HTTP/2.0", string(b2))

    // (c) WS 升级仍可用（coder/websocket 依赖 Hijacker，只在 h1 下成立）。
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    ws, _, err := websocket.Dial(ctx, "ws"+base[len("http"):]+"/ws", nil)
    require.NoError(t, err, "WS upgrade must survive (needs SetHTTP1(true))")
    defer ws.Close(websocket.StatusNormalClosure, "")
    _, msg, err := ws.Read(ctx)
    require.NoError(t, err)
    require.Equal(t, "hello", string(msg))
}
```

**Step 2: 跑测试确认失败**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/
mkdir -p internal/frontend/dist && touch internal/frontend/dist/.gitkeep
/usr/local/go/bin/go test ./cmd/server/ -run TestServerProtocols_PlaintextCoexistence -v
```

预期：**编译失败** `undefined: serverProtocols`。

**Step 3: 最小实现**

新建 `cmd/server/server_protocols.go`：

```go
package main

import "net/http"

// serverProtocols returns the HTTP protocols the main server accepts.
//
// http.Protocols is all-or-nothing: the moment it is non-nil it fully replaces
// the defaults, so every protocol we still serve must be enabled explicitly.
// HTTP/1.1 is not optional here — the five coder/websocket endpoints
// (/api/ai/events/ws, /api/file/watch/ws, /api/tts/audio/ws,
// /api/stt/transcribe/ws, /api/terminal/ws) upgrade via http.Hijacker, which
// HTTP/2 does not support. Dropping SetHTTP1 turns them all into ECONNRESET.
// h2-over-TLS is likewise opt-in: ServeTLS only advertises ALPN "h2" when the
// server is configured for HTTP/2.
func serverProtocols(tls bool) *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)          // required for the existing WebSocket endpoints
	p.SetUnencryptedHTTP2(true) // h2c prior-knowledge on the plaintext default
	if tls {
		p.SetHTTP2(true) // ALPN h2 when the instance is served over TLS
	}
	return p
}
```

修改 `cmd/server/main.go:1277`：

```go
srv := &http.Server{Handler: mux, Protocols: serverProtocols(scheme == "https")}
```

（`scheme` 变量在 `:1291` 附近已解析，`:1473` 用同一变量选 `ServeTLS`/`Serve`，语义一致。）

**Step 4: 跑测试确认通过**

```bash
/usr/local/go/bin/go test ./cmd/server/ -run TestServerProtocols_PlaintextCoexistence -v
```

预期输出：
```
=== RUN   TestServerProtocols_PlaintextCoexistence
--- PASS: TestServerProtocols_PlaintextCoexistence (0.15s)
PASS
ok  	clawbench/cmd/server
```

**Step 5: 现有 5 个 WS 端点回归（手动，用隔离实例）**

```bash
# 起一个隔离实例（端口 20100，不碰 20000 主服务器）
./build.sh --restart --restart-port=20100
# 拿到 cookie 后逐个探测 WS 升级（101）
for p in /api/ai/events/ws /api/file/watch/ws /api/tts/audio/ws /api/stt/transcribe/ws /api/terminal/ws; do
  curl -s -o /dev/null -w "$p -> %{http_code}\n" \
    -H "Connection: Upgrade" -H "Upgrade: websocket" \
    -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
    -b "<cookie>" "http://127.0.0.1:20100$p"
done
```

预期：5 个端点全部 `101`（升级成功）。**任何 `000` / `56` / `ECONNRESET` 都说明 `SetHTTP1(true)` 没生效——停止，不要继续后续任务。**

同时确认 h1 与 h2c 在真实实例上可用：

```bash
curl -s -o /dev/null -w "h1 -> %{http_code} %{http_version}\n" http://127.0.0.1:20100/api/health
curl -s -o /dev/null -w "h2c -> %{http_code} %{http_version}\n" --http2-prior-knowledge http://127.0.0.1:20100/api/health
```

预期：`h1 -> 200 1.1`、`h2c -> 200 2`（curl 8.14.1 已确认带 HTTP2 特性）。

**Step 6: Commit**

```bash
git add cmd/server/server_protocols.go cmd/server/server_protocols_test.go cmd/server/main.go
git commit -m "feat(tunnel): enable HTTP/1.1 + h2c + h2-over-TLS coexistence on main server

http.Protocols is all-or-nothing: once non-nil it replaces the defaults, so
SetHTTP1 must be explicit or the five coder/websocket endpoints (which
upgrade via http.Hijacker, unsupported on h2) all die with ECONNRESET."
```

**完成标准：**
- `go test ./cmd/server/ -run TestServerProtocols_PlaintextCoexistence` PASS。
- 隔离实例上 5 个 WS 端点全部返回 `101`。
- h1 与 h2c 在真实实例上都能 `200`。

---

### Task 2: `-L` 流 handler（`POST /api/tunnel/stream`）

**Files:**
- Create: `internal/tunnel/ndjson.go` + `internal/tunnel/ndjson_test.go`（本任务只建包骨架与守卫，T3 补 NDJSON）
- Create: `internal/tunnel/guard.go` + `internal/tunnel/guard_test.go`
- Create: `internal/handler/tunnel_stream.go`
- Create: `internal/handler/tunnel_stream_test.go`
- Modify: `internal/handler/handler.go:475` 之后（在 `register("/api/ssh/info/full", ...)` 后加一行 `register("/api/tunnel/stream", TunnelStream)`）

**依赖：** T1（h2c 才能跑）。**可并行：** 与 T3 同阶段，但两者都改 `handler.go` 的注册段 → **注册行建议 T2 一次加两行**（见 Step 3），避免同文件冲突。

**关键语义（必须逐条对齐 `internal/ssh/server.go`）：**
- 鉴权由 `middleware.Auth` 在 handler 之前完成（用 `register`，不是 `registerPublic`）。
- 校验**只** `service.ProxyService.IsPortAllowed(port)`，对齐 `internal/ssh/server.go:738`（正向路径**不查** `IsNonLocalhostTarget`、**不查** `IsPortRegistered`、**不启 HTTP 反代**）。
- **nil 守卫**：`service.ProxyService == nil` → `503`（先例 `internal/handler/ssh_info.go:117`）。
- **`net.Dial` 失败必须在写响应头之前返回 `502`**（写了头就改不了状态码）。
- 成功则 `WriteHeader(200)` + `Flush()` 后才双向 pump；每次写后 `Flush`。
- `http.NewResponseController(w).EnableFullDuplex()`（**h1 兜底必需**；不调时服务端 `read n=0 err=http: invalid Read on closed Body`，见 `net/http/server.go:1392`；h2 下是 no-op）。
- 半关闭：读 EOF → 结束请求体（`END_STREAM`），对端仍可回写。

**Step 1: 写守卫纯逻辑与测试（先做纯逻辑，TDD）**

`internal/tunnel/guard.go`：

```go
package tunnel

// PortGuard is the port-whitelist / reserved-port decision surface the tunnel
// handlers need. It mirrors internal/ssh/server.go's reverseBindAllowed and
// isReservedPort so the h2 transport enforces exactly the same policy as SSH.
type PortGuard struct {
    MainPort    int
    SSHPort     int
    IsAllowed   func(port int) bool
    IsReserved  func(port int) bool
}

// ForwardAllowed mirrors internal/ssh/server.go:738 (handleDirectTCPIP): the
// forward path only consults the allowed range. It deliberately does not check
// IsNonLocalhostTarget or IsPortRegistered — SSH operates at the transport
// layer and needs no URL-rewriting metadata.
func (g PortGuard) ForwardAllowed(port int) bool {
    if g.IsAllowed == nil {
        return false
    }
    return g.IsAllowed(port)
}

// ReverseBindAllowed mirrors internal/ssh/server.go:589 (reverseBindAllowed)
// composed with :616-624 (isReservedPort), with one deliberate difference:
// port == 0 is legal and means "let the OS pick". isReservedPort treats
// port <= 0 as reserved, so a naive port-through would reject the documented
// bind(0) semantics.
func (g PortGuard) ReverseBindAllowed(port int) bool {
    if port == 0 {
        return true // OS-assigned; the actual port is re-checked after Listen
    }
    if port < 0 {
        return false // isReservedPort's port<=0 branch, minus the port==0 case
    }
    if port == g.MainPort || port == g.SSHPort {
        return false
    }
    if g.IsReserved != nil && g.IsReserved(port) {
        return false
    }
    if g.IsAllowed == nil {
        return false
    }
    return g.IsAllowed(port)
}
```

`internal/tunnel/guard_test.go`（表驱动，逐条对齐 SSH 守卫）：

```go
package tunnel

import "testing"

func TestPortGuard_ReverseBindAllowed_MatchesSSHSemantics(t *testing.T) {
    reserved := map[int]bool{9000: true}
    g := PortGuard{
        MainPort:   20000,
        SSHPort:    20001,
        IsAllowed:  func(p int) bool { return p >= 1024 && p <= 65535 },
        IsReserved: func(p int) bool { return reserved[p] },
    }
    cases := []struct {
        name string
        port int
        want bool
    }{
        {"zero is OS-assigned and legal", 0, true},
        {"negative is rejected", -1, false},
        {"main port rejected", 20000, false},
        {"ssh port rejected", 20001, false},
        {"registry-reserved rejected", 9000, false},
        {"below allowed range rejected", 80, false},
        {"inside allowed range accepted", 5173, true},
        {"top of allowed range accepted", 65535, true},
        {"above allowed range rejected", 65536, false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if got := g.ReverseBindAllowed(tc.port); got != tc.want {
                t.Fatalf("ReverseBindAllowed(%d) = %v, want %v", tc.port, got, tc.want)
            }
        })
    }
}

func TestPortGuard_ForwardAllowed_OnlyChecksWhitelist(t *testing.T) {
    g := PortGuard{MainPort: 20000, SSHPort: 20001, IsAllowed: func(p int) bool { return p == 8080 }}
    if !g.ForwardAllowed(8080) {
        t.Fatal("allowed port must pass")
    }
    if g.ForwardAllowed(9090) {
        t.Fatal("non-whitelisted port must be rejected")
    }
    // Main port is not special on the forward path: SSH's handleDirectTCPIP
    // only consults IsPortAllowed. The whitelist decides.
    g2 := PortGuard{MainPort: 20000, IsAllowed: func(p int) bool { return p == 20000 }}
    if !g2.ForwardAllowed(20000) {
        t.Fatal("forward path must defer entirely to the whitelist (SSH parity)")
    }
}

func TestPortGuard_NilPredicatesAreClosed(t *testing.T) {
    var g PortGuard
    if g.ForwardAllowed(8080) {
        t.Fatal("nil IsAllowed must fail closed")
    }
    if g.ReverseBindAllowed(8080) {
        t.Fatal("nil IsAllowed must fail closed")
    }
}
```

**Step 2: 跑测试确认先失败再通过**

```bash
/usr/local/go/bin/go test ./internal/tunnel/ -v
```
预期：先 `undefined: PortGuard`（若文件还没建），补全后 `PASS`。

**Step 3: 实现 handler**

`internal/handler/tunnel_stream.go`（骨架，**不写完整实现，按上面语义写**）：

- `func TunnelStream(w http.ResponseWriter, r *http.Request)`
- `requireMethod` 为 `POST`（参考 `internal/handler/proxy_api.go` 的 `requireMethod` 用法）。
- 解析 query：`host`、`port`、`claim`。
- `service.ProxyService == nil` → `http.Error(w, ..., 503)`。
- 无 `claim` 时：缺 `host`/`port` → `400`；`IsPortAllowed(port)` 为假 → `403`；`net.DialTimeout("tcp", host:port, ...)` 失败 → `502`（**在写头之前**）。
- 有 `claim` 时：交给 T3 的 claim 表匹配（T2 先留一个 `claimStream(w, r, token)` 桩，T3 实现）。
- 成功：`http.NewResponseController(w).EnableFullDuplex()` → `w.WriteHeader(200)` → `Flush()` → 双泵（`r.Body`→conn 与 conn→`w`，每次写后 `Flush`）。

`internal/handler/handler.go`：在 `:475` 的 `register("/api/ssh/info/full", ServeSSHInfoFull)` 之后加**两行**（T2 一次加完，避免与 T3 抢同一文件）：

```go
register("/api/tunnel/stream", TunnelStream)
register("/api/tunnel/control", TunnelControl) // T3 提供；T2 可先加桩
```

> **T2 若单独交付：** 只加 `stream` 一行；`TunnelControl` 的桩函数需先存在（否则编译失败）。建议 T2+T3 的 handler 注册行合并为一次改动。

**Step 4: 写 handler 测试**

`internal/handler/tunnel_stream_test.go`，仿 `internal/handler/file_watch_test.go:29-69` 的 `httptest.NewServer` + Cookie 模式，鉴权用 `internal/handler/testutil_test.go:410 withAuthCookie`（`setupTestEnv`:39 会把 `model.SessionToken` 置空，注意用真实 token 或直接走 `middleware.Auth` 的测试约定）。

必须覆盖的用例：

| 用例 | 断言 |
|---|---|
| 未鉴权 | `401` |
| `ProxyService == nil` | `503`（先保存/恢复 `service.ProxyService`） |
| 白名单拒绝 | `403` |
| 允许但未注册端口可拨 | 200 + echo 往返成功 |
| echo 往返（**全双工**：先写后读交错） | 先收响应头再写 body，数据正确 |
| 64 KiB 大块 | 收全 64 KiB，字节相等 |
| 多流并发 | N 条并发流各自 echo 正确 |
| dial 失败 | `502`（**且响应头未被提前写出**：断言状态码为 502，不是 200） |
| 缺参数 | `400` |

参考 SSH 侧对应用例：`internal/ssh/server_test.go:242/275/291/322/386`。

**Step 5: 跑测试**

```bash
/usr/local/go/bin/go test ./internal/handler/ -run 'TestTunnelStream' -v
/usr/local/go/bin/go test ./internal/tunnel/ -v
```

预期：全部 PASS。**注意 h2c 客户端用 `golang.org/x/net/http2.Transport{AllowHTTP:true}`**；若用 `httptest.NewServer`（默认 h1），要显式调 `EnableFullDuplex` 才能测全双工——两种都要有（一个走 h1 兜底、一个走 h2c）。

**Step 6: Commit**

```bash
git add internal/tunnel/ internal/handler/tunnel_stream.go internal/handler/tunnel_stream_test.go internal/handler/handler.go
git commit -m "feat(tunnel): add POST /api/tunnel/stream full-duplex -L relay"
```

**完成标准：**
- `go test ./internal/tunnel/` 与 `go test ./internal/handler/ -run TestTunnelStream` 全绿。
- 有显式的「dial 失败返回 502 且未提前写头」用例。
- 有显式的「`ProxyService == nil` → 503」用例。

---

### Task 3: `-R` 控制流 + claim token（`POST /api/tunnel/control`）

**Files:**
- Create: `internal/tunnel/ndjson.go` + `internal/tunnel/ndjson_test.go`
- Create: `internal/tunnel/token.go` + `internal/tunnel/token_test.go`
- Create: `internal/handler/tunnel_control.go`
- Create: `internal/handler/tunnel_control_test.go`
- Modify: `internal/handler/handler.go:475` 之后（若 T2 未一并加，则加 `register("/api/tunnel/control", TunnelControl)`）

**依赖：** T1、T2（复用 `guard.go` 与 `tunnel_stream.go` 的 `claimStream` 挂点）。**可并行：** 与 T2 部分重叠（都碰 handler.go）→ 串行或合并注册行。

**NDJSON 消息（设计文档 §4.4，必须逐字对齐）：**

| 消息 | 方向 | 字段 |
|---|---|---|
| `bind` | C→S | `port`（0 = OS 分配） |
| `bound` | S→C | `port`（实际端口） |
| `bind_err` | S→C | `port`, `code`, `msg` |
| `incoming` | S→C | `port`（服务端监听口）, `token` |
| `unbind` | C→S | `port` |
| `unbound` | S→C | `port` |
| `ping` | C→S | — |
| `pong` | S→C | — |

`bind_err.code`：`2`=不允许 / `3`=保留或占用 / `4`=`net.Listen` 失败 / `6`=内部错误（对齐设计文档 §4.6）。

**Step 1: NDJSON 编解码 + 测试**

`internal/tunnel/ndjson.go`：`ControlMessage` 结构 + `DecodeControl(line []byte) (ControlMessage, error)` + `EncodeControl(ControlMessage) []byte`。

测试覆盖：正常编解码、**畸形行**（截断 JSON、非 JSON、未知 `type`）都返回 error 而非 panic。

**Step 2: claim token + 测试**

`internal/tunnel/token.go`：`TokenStore`，`Issue(binding Binding) (token string)` 用 `crypto/rand` 生成（建议 32 字节 → hex/base64url），`Claim(token string, binding Binding) bool` **单次使用**。

**绑定语义（设计文档 §4.5）：** token 绑定到「创建它的那条已认证控制流」。实现上用 `Binding` 表示控制流身份，至少包含：
- 该控制流实例的 ID（handler 内每条控制流一个唯一 ID）；
- `middleware.Auth` 已认证的会话标识（从请求 cookie 取，`model.ScopedCookieName(model.SessionCookie)`）。

`Claim` 必须同时校验 token 存在、未使用、`Binding` 相等。**跨连接 claim 必须拒绝。**

测试覆盖：
- 生成不可猜（长度/随机性抽样）。
- 单次使用：第二次 `Claim` 同 token 返回 false。
- 跨连接拒绝：用不同 `Binding` claim 返回 false。
- 过期拒绝（`TokenStore` 带 TTL；测试用短 TTL + 等待，或用可注入的时钟）。
- 挂起连接超时关闭（`TokenStore`/挂起表带认领超时）。

**Step 3: 实现控制流 handler**

`internal/handler/tunnel_control.go`：
- `func TunnelControl(w http.ResponseWriter, r *http.Request)`，`POST` + `EnableFullDuplex`。
- 长寿命：读 NDJSON 行循环；`bind` 走 `PortGuard.ReverseBindAllowed(port)`（`port==0` 走 OS 分配分支，**不要被 `isReservedPort` 的 `port<=0` 误拒**）。
- `net.Listen("127.0.0.1", port)` 成功后调 `service.ProxyService.SetReverseBound(实际端口, true)` 驱动 `Active`（对齐 `internal/service/proxy.go:504`）；回 `bound`。
- accept 循环：accept 到连接 → **挂起**（带认领超时，未认领则关闭）→ 发 `incoming` + `crypto/rand` 单次 token。
- `claim` 由 `TunnelStream` 转交 `claimStream`：凭 token 匹配挂起连接 → `WriteHeader(200)` + `Flush()` + 双泵。
- `unbind` 或控制流断开 → `listener.Close()` + 关闭所有挂起连接 + `SetReverseBound(port, false)` + 回 `unbound`。
- `ping` → `pong`。

**Step 4: 控制流测试**

必须覆盖（对应 SSH 侧用例在 `internal/ssh/server_test.go`）：

| 用例 | SSH 对照 |
|---|---|
| bind 保留端口拒绝（mainPort / sshPort） | `:1493` / `:1481` |
| allowed range 拒绝 | `:1524` |
| 重复绑定拒绝 | `:1629` |
| unbind 释放端口 | `:1536` |
| 控制流断开释放端口 | `:1554` |
| `SetReverseBound` 的 Active 生命周期 | `:1580` |
| incoming → claim 往返 | — |
| token 单次使用 | — |
| token 跨连接不可用 | — |
| 挂起连接超时关闭 | — |

`SetReverseBound` 的 Active 生命周期断言方式：先通过 `POST /api/proxy/ports`（`internal/handler/proxy_api.go:37-61`，direction=`reverse`）注册一个 reverse 端口，再 bind，然后查 `service.ProxyService.ListPorts()` 确认该端口 `Active==true`；unbind 后确认 `Active==false`。

**Step 5: 跑测试**

```bash
/usr/local/go/bin/go test ./internal/tunnel/ -v
/usr/local/go/bin/go test ./internal/handler/ -run 'TestTunnelControl|TestTunnelStream' -v
```

预期：全绿。

**Step 6: Commit**

```bash
git add internal/tunnel/ internal/handler/tunnel_control.go internal/handler/tunnel_control_test.go internal/handler/handler.go
git commit -m "feat(tunnel): add -R control stream with single-use claim tokens"
```

**完成标准：**
- `port=0` 走 OS 分配分支的用例存在且通过（防 `isReservedPort` 的 `port<=0` 误拒）。
- token 单次 + 跨连接 + 过期三类拒绝用例齐全。
- `SetReverseBound` Active 生命周期用例通过。

---

### Task 4: 放宽 `ProxyRegistry` 创建门控（使隧道不依赖 SSH）

**Files:**
- Modify: `cmd/server/main.go:1064-1088`（`if cfg.PortForward.Enabled { ... }`）
- Modify: `cmd/server/main.go:1739-1747`（`reserveSSHPorts`）/ `:1750`（`hotReloadSSH`）

**依赖：** T3（`SetReverseBound` 生命周期）。**可并行：** 与 T5 可并行。

**现状：** `ProxyRegistry` **只在** `cfg.PortForward.Enabled` 时创建（注释明说「没有 SSH 隧道它没有独立用途」）。不改这个门控，隧道 handler 永远拿到 `nil` 只能全拒（503）。**这是服务端最关键的生命周期改动。**

**Step 1: 改门控**

把创建条件从「仅 `cfg.PortForward.Enabled`」放宽为「SSH 或 h2 任一启用」。注意 T8 会引入 `cfg.PortForward.Transport`，本任务先按「`cfg.PortForward.Enabled` 为真 **或** `Transport` 含 `h2`」写；若 T8 尚未做，可先用 `cfg.PortForward.Enabled || true` 的过渡形式并在 T8 收敛——**推荐直接按 T8 的字段写，把 T4 放在 T8 之后**（见执行顺序总览的备选）。

关键约束（保持原语义不变）：
- `:1074` 的 `proxyService.SetReservedPorts(port, sshPort)` **必须仍然执行**（mainPort + sshPort 必须登记为 reserved，否则 `-R` 能绑到 20000/20001 把平台打挂）。
- `sshPort` 的默认推导 `if sshPort == 0 { sshPort = port + 1 }` 保持不变。
- SSH 服务器分支：`ssh.NewServer(...)` 只在 SSH 启用时创建；h2-only 模式下不要起 20001 监听。
- `defer proxyService.Stop()` 仍要挂上。

**Step 2: 确认 `hotReloadSSH` 行为**

`reserveSSHPorts`（`:1739`）由 `hotReloadSSH`（`:1750`）在三处调用（`:1763`/`:1777`/`:1785`）。放宽门控后要确认：**h2-only 模式下热重载仍会把 mainPort/sshPort 登记为 reserved**。若 `hotReloadSSH` 在 `!cfg.PortForward.Enabled` 时提前返回，需要改成「h2 启用时也走 reserveSSHPorts」。

**Step 3: 验证**

`cmd/server` 无测试包，用隔离实例做行为验证：

```bash
# 配一个「SSH 关、h2 开」的实例
./build.sh --restart --restart-port=20100
# 登录拿 cookie，然后确认 -L 不再 503
curl -s -o /dev/null -w "tunnel/stream -> %{http_code}\n" \
  -X POST -b "<cookie>" \
  --http2-prior-knowledge \
  "http://127.0.0.1:20100/api/tunnel/stream?host=127.0.0.1&port=8080"
```

预期：**不是 `503`**（`502` 说明 dial 失败但门控已放开——这是期望结果，因为 8080 上没有服务）。若仍 `503`，说明 `ProxyService` 仍是 nil。

同时确认 reserved 端口仍生效：

```bash
# -R bind 20000 必须被拒
# （用 T3 的控制流脚本或 curl 发 bind 消息，见 T14）
```

预期：bind 20000 回 `bind_err`（code=3）。

**Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "fix(tunnel): create ProxyRegistry for h2 tunnels without SSH enabled"
```

**完成标准：**
- SSH 关闭时 `POST /api/tunnel/stream` 不返回 503。
- `-R` bind 20000 / 20001 仍被拒。
- SSH 启用路径行为不变（`cfg.PortForward.Enabled` 为真时与改动前一致）。

---

### Task 5: 同步文档（OpenAPI + 计数 + 隧道 spec）

**Files:**
- Modify: `internal/api/openapi.yaml`（新增 `/api/tunnel/stream`、`/api/tunnel/control` 两个 `post`）
- Modify: `docs/spec/api/README.md:7`（计数）、`:26`（端点表）
- Modify: `docs/spec/README.md:64`（计数）
- Modify: `docs/spec/infra/ssh-tunnel.md:5`（现状声明重写 + 新增「传输方式」「h2 流隧道」章节）

**依赖：** T2 + T3（路由与端点已冻结）。**可并行：** 与阶段 2/3 可并行（只碰 `docs/` 与 openapi）。

**Step 1: OpenAPI 两个新条目**

`operationId` 必须**全局唯一**（`internal/api/render_test.go:296` 会拒绝重复）。参考既有 WS 收录写法（`:1619` / `:2331`）。字段名**必须从 handler 代码抄**（`r.URL.Query().Get(...)` 的参数名、`decodeJSON` 的 JSON tag），禁止望文生义。

`/api/tunnel/stream`：`post`，参数 `host` / `port` / `claim`（query），说明 duplex 请求体/响应体语义，响应 `200`（流）/ `400` / `401` / `403` / `502` / `503`。
`/api/tunnel/control`：`post`，说明 NDJSON 控制流，响应 `200`（流）/ `400` / `401` / `503`。

**Step 2: 修正计数（注意 §0.2 的漂移）**

改完后**重新数一遍**，以实际为准：

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/
python3 - <<'PY'
import yaml
d = yaml.safe_load(open('internal/api/openapi.yaml'))
p = d['paths']
ops = sum(1 for v in p.values() for k in v if k in ('get','post','put','delete','patch','head','options','trace'))
print('paths', len(p), 'ops', ops)
PY
```

预期：`paths 154 ops 193`（当前实测 152/191，+2/+2）。

把 `docs/spec/api/README.md:7` 与 `docs/spec/README.md:64` 的「150 个路径 / 189 个操作」**改为实测值**（`154 路径 / 193 操作`）。**注意这两处文字现在就已经陈旧了 2**（HEAD 实际 152/191，文字写 150/189）——本次一并修正，不要只做 +2 的算术。

把两个新端点加入 `docs/spec/api/README.md:26` 的不可建模端点表（该表当前列出 5 个 WS 端点；隧道是裸 HTTP 流，同样无法用 OpenAPI 的请求/响应 schema 完整建模）。

**Step 3: 重写 `docs/spec/infra/ssh-tunnel.md`**

`:5` 的现状声明需重写（原文只讲 SSH direct-tcpip）。新增：
- **「传输方式」章节**：优先级链 h2-over-TLS → h2c → SSH，`port_forward.transport` 配置（T8），「记住上次成功传输」策略。
- **「h2 流隧道」章节**：一条 h2 连接多路复用、`POST /api/tunnel/stream`（`-L`）、`POST /api/tunnel/control`（`-R` + claim token）、只放行 20000 一个端口。
- 保留现有 SSH 章节（SSH 未删除，是兜底）。

**Step 4: 验证漂移测试与文档**

```bash
/usr/local/go/bin/go test ./internal/handler/ -run TestOpenAPISpecMatchesRegisteredRoutes -v
/usr/local/go/bin/go test ./internal/api/ -run 'TestRender|TestOperationID|TestUnique' -v
```

预期：全绿。若 `TestOpenAPISpecMatchesRegisteredRoutes` 报 `registered /api/ routes are missing from the OpenAPI spec`，说明新路由没写进 spec。

**Step 5: Commit**

```bash
git add internal/api/openapi.yaml docs/spec/api/README.md docs/spec/README.md docs/spec/infra/ssh-tunnel.md
git commit -m "docs(api): document /api/tunnel/{stream,control} and refresh spec counts"
```

**完成标准：**
- 漂移测试与 operationId 唯一性测试全绿。
- 计数与实际 YAML 解析结果一致（154/193）。
- `ssh-tunnel.md` 同时描述 SSH 与 h2 两种传输，并保留 SSH 兜底说明。

---

## 阶段 2 — Electron

> **前置：** `cd desktop && npm ci`（实测 `desktop/node_modules/` 为空，只有 `.vite/`）。

### Task 6: 新增 `desktop/src/main/h2Transport.ts`

**Files:**
- Create: `desktop/src/main/h2Transport.ts`
- Create: `desktop/src/main/h2Transport.test.ts`

**依赖：** T2/T3（协议冻结）。**可并行：** 与阶段 3 整体并行。

**transport 接口（设计文档 §7.1）：** `connect()` / `openStream(host, port)` / `bind(port)` / `unbind(port)` / `close()` / `isConnected()`。

**实现要点：**
- 用 `node:http2`，**零新增 npm 依赖**。
- h2c prior-knowledge：`http2.connect('http://host:20000')`（**不做 Upgrade 协商**，实测可行）。
- h2-over-TLS：`https:` + `rejectUnauthorized:false`（自签），ALPN 协商为 `h2`。
- cookie 复用 `desktop/src/main/clientLog.ts:88-96 getSessionCookie()`（按 `clawbench_session` / `*_clawbench_session` 后缀匹配），作为普通 `cookie` header 传给 `http2.connect`。
- 背压：写侧 `write()` 返回值 + `'drain'`；读侧 `stream.pause()/resume()`。**不要用 `session.socket.pause()`**（抛 `ERR_HTTP2_NO_SOCKET_MANIPULATION`，Node 20/24 一致）。
- **坑**：`http2.ClientHttp2Stream` **不是运行时导出**（只是 TS 类型），判 Duplex 要用 `node:stream` 的 `Duplex`；`stream.end()` 后再 `write()` 会异步 emit `ERR_STREAM_WRITE_AFTER_END`。

**Step 1: 写失败测试**

`desktop/src/main/h2Transport.test.ts`，用 `vi.mock('node:http2')` 注入 fake session/stream（设计文档 §7.3 已实测 `node:http2` 在 jsdom 下可 import）。覆盖：
- `connect()` 走 h2c 时调用 `http2.connect('http://...')`。
- `openStream` 发出的 `POST` path 含 `?host=&port=`。
- cookie header 被带上。
- `close()` 调 `session.close()`。
- 背压：`write()` 返回 false 时等 `'drain'` 再继续。

**Step 2: 跑测试确认失败**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/desktop
npx vitest run src/main/h2Transport.test.ts
```
预期：FAIL（模块不存在 / 函数未定义）。

**Step 3: 实现**

按上面要点实现 `h2Transport.ts`。

**Step 4: 跑测试确认通过**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/desktop
npx vitest run src/main/h2Transport.test.ts
npx tsc --noEmit -p tsconfig.json   # 类型检查
```
预期：全绿 + 类型检查无错。

**Step 5: Commit**

```bash
git add desktop/src/main/h2Transport.ts desktop/src/main/h2Transport.test.ts
git commit -m "feat(desktop): add node:http2 transport for the h2 tunnel"
```

**完成标准：** 测试全绿 + `tsc --noEmit` 通过 + 零新增 npm 依赖（`git diff desktop/package.json` 为空）。

---

### Task 7: `desktop/src/main/tunnel.ts` 接入 transport

**Files:**
- Modify: `desktop/src/main/tunnel.ts:165`（`classifyError`）、`:181-304`（`openClient`）、`:311`（`disconnectTunnel`）、`:350`（`listenForward` 的**唯一** `forwardOut` 调用）、`:398-425`（`listenReverse`）、`:144`（`unforwardReverse`）、`:541`（`ensureTunnel`）
- Create: `desktop/src/main/tunnel.h2.test.ts`

**依赖：** T6。**可并行：** 与阶段 3 并行。

**硬约束：现有 `desktop/src/main/tunnel.test.ts` 的 34 个用例在保留 SSH 为默认传输时必须继续全绿。**

**只改这 7 处，其余一行不改**（设计文档 §7.2 逐符号判定）：
- `:165 classifyError` — 加 h2 / `ERR_HTTP2_*` / `ECONNREFUSED` 映射。
- `:181-304 openClient` — ssh2 生命周期 → `transport.connect()`；`:237-257` 的 `'tcp connection'` 是 ssh2 专有。
- `:311 disconnectTunnel` — `client.end()` → `transport.close()`。
- `:350`（在 `listenForward` `:341-383` 内）— `c.forwardOut(...)` → `transport.openStream(host, port)`；其余 `net.createServer`/错误处理/单飞**全保留**。
- `:398-425 listenReverse` — `forwardIn` → `transport.bind()`。
- `:144 unforwardReverse` — → `transport.unbind()`。
- `:541 ensureTunnel` — 按传输分派。

**复用不动：** `state.forwarded`(:27)、`forwardServers`(:58)、`reverseForwards`(:69)、连接监视器(:84-122)、`pendingBinds`(:339)、`pendingReverseBinds`(:396)、`rebuildAllForwards`(:434)、`addForwardedPort`(:445)、`addReverseForwardedPort`(:462)、`removeForwardedPort`(:472)、`removeReverseForwardedPort`(:488)、`testPortReachable`(:492)、getter(:158-163)、`fetchSshInfo`(:510-531)、`DEFAULT_SSH_USER`(:501)、`reconnectTunnel`(:564)。

**Step 1: 记录基线（**必做**，这是本任务的硬约束）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/desktop
npx vitest run src/main/tunnel.test.ts 2>&1 | tail -8
```
预期：`Tests  34 passed (34)`。**记下这个数字**，改完后必须仍是 34 passed。

**Step 2: 写 h2 测试（先写）**

`desktop/src/main/tunnel.h2.test.ts`，用 `vi.mock('./h2Transport')` 注入 fake（设计文档 §7.3 首选方案）。覆盖：
- `ensureTunnel` 在 transport=h2 时调 `h2Transport.connect()`。
- `listenForward` 调 `transport.openStream(host, port)` 而非 `forwardOut`。
- `listenReverse` 调 `transport.bind()`。
- `unforwardReverse` 调 `transport.unbind()`。
- `classifyError` 对 `ERR_HTTP2_*` 返回预期类型。

**Step 3: 跑 h2 测试确认失败**

```bash
npx vitest run src/main/tunnel.h2.test.ts
```
预期：FAIL。

**Step 4: 实现接入**

按上面 7 处改。保持 SSH 为默认传输（`transport` 默认 `'ssh'`）。

**Step 5: 跑两个测试文件**

```bash
npx vitest run src/main/tunnel.test.ts src/main/tunnel.h2.test.ts
npx tsc --noEmit -p tsconfig.json
```

预期：**`tunnel.test.ts` 仍是 34 passed**（若变成失败，说明改动越界——检查是否碰了「复用不动」的符号）；`tunnel.h2.test.ts` 全绿；类型检查无错。

**Step 6: Commit**

```bash
git add desktop/src/main/tunnel.ts desktop/src/main/tunnel.h2.test.ts
git commit -m "feat(desktop): route tunnel.ts through a pluggable transport"
```

**完成标准：**
- `npx vitest run src/main/tunnel.test.ts` → **34 passed**（SSH 默认传输回归）。
- `npx vitest run src/main/tunnel.h2.test.ts` → 全绿。
- `git diff --stat desktop/src/main/tunnel.ts` 只显示上述 7 处所在行附近。

---

### Task 8: 传输选择配置链路（`port_forward.transport`）

**Files:**
- Modify: `internal/model/port_forward.go`（新增 `Transport` 字段）
- Modify: `internal/model/defaults.go:306-314`（默认值）
- Modify: `internal/handler/settings.go`：`:103-105`（`hotReloadFields`）、`:223`（`configPortForward` 结构体字段）、`:332-335`（`configPortForward` 填充）、`:620-622`（`PatchableConfigPaths`）、`:776-779`（填充）、`:964`（`validatePatchValues`）、`:1567-1577`（`applyConfigPatch`）
- Modify: `desktop/src/preload/index.ts:70-87`、`desktop/src/main/bridge.ts:84-93`、`web/src/utils/clawbenchNative.ts:62-72`（三处桥同步）

**依赖：** T4（服务端 transport 配置）。**可并行：** 桥同步部分与 T13 有耦合（见 T13）。

**配置语义（设计文档 §11.1）：** `port_forward.transport: ssh | h2 | both`。默认值需保兼容——**推荐 `both`**（已启用 SSH 的用户行为不变，同时提供 h2）；保守可 `ssh`（完全等价旧行为）。选定后在 `defaults.go` 用 presence 模式（注意 `bool`/`string` 零值陷阱，参考 `:306-314` 既有注释）。

**「记住上次成功传输」策略（设计文档 §2.3）：** 首次连接严格按 h2-over-TLS → h2c → SSH 探测；**明文部署首次会白付一次 TLS 失败**（握手即被拒，不是超时）。客户端记住上次成功的传输，重连优先复用。这条要在客户端（T6/T7/T9）落地一个持久化字段（Electron 可用 `electron-store`；Android 用 SharedPreferences；**注意不要改 `PortInfo` 持久化格式**）。

**Step 1: 改 Go 侧**

`internal/model/port_forward.go` 加 `Transport string \`yaml:"transport"\``；`defaults.go` 设默认；`settings.go` 六处接线（hotReload / 填充 / Patchable / 校验 / apply）。

**Step 2: 写测试**

Go：`internal/handler/settings_test.go` 加用例——patch `port_forward.transport` 为 `h2` 后 `cfg.PortForward.Transport == "h2"`；非法值（如 `"quic"`）被 `validatePatchValues` 拒绝。

```bash
/usr/local/go/bin/go test ./internal/handler/ -run 'TestSettings.*Transport|TestConfigPatch.*Transport' -v
/usr/local/go/bin/go test ./internal/model/ -run 'TestDefault' -v
```

**Step 3: 三处桥同步**

`preload/index.ts:70-87`、`bridge.ts:84-93`、`clawbenchNative.ts:62-72` 三处必须同步新增传输查询方法（如 `getTunnelTransport()`）。**三处缺一就会在另一端静默 undefined。**

**Step 4: 验证桥**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/desktop && npx tsc --noEmit -p tsconfig.json
cd /root/code/clawbench/.worktrees/ssh-ws-forward/ && npx vitest run web/src/utils/__tests__/clawbenchNative.test.ts 2>/dev/null || npx vitest run web/src/utils/clawbenchNative.test.ts
```

预期：类型检查无错；桥测试全绿。

**Step 5: Commit**

```bash
git add internal/model/port_forward.go internal/model/defaults.go internal/handler/settings.go \
        desktop/src/preload/index.ts desktop/src/main/bridge.ts web/src/utils/clawbenchNative.ts
git commit -m "feat(config): add port_forward.transport (ssh|h2|both)"
```

**完成标准：** 非法 transport 值被拒；三处桥同步（类型检查能证明）；SSH 默认行为不变。

---

## 阶段 3 — Android（工作量最大）

> **前置：** JDK 17 + `ANDROID_HOME=/opt/android-sdk`。
> **基线（已实测）：** `JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk ./gradlew :app:testDebugUnitTest` → **804 tests / 0 failures**。
> **硬约束：** 保留 SSH 为默认传输时现有 **804 个 @Test 必须继续全绿**；`PortInfo` 持久化格式 `"localPort:targetPort:host"`（`saveForwardedPorts`:1070 / `restoreForwardedPorts`:1151）**不可变**——改了会挂 `BackgroundServicePortHostTest` 的 save/restore 用例（实测该文件 27 个 @Test，其中 save/restore 命名用例 11 个）。

### Task 9: 抽 `TunnelStream` 接口 + h2 实现

**Files:**
- Create: `android/app/src/main/java/com/clawbench/app/TunnelStream.java`（接口）
- Create: `android/app/src/main/java/com/clawbench/app/H2TunnelStream.java`（h2 实现）
- Create: `android/app/src/test/java/com/clawbench/app/H2TunnelStreamTest.java`

**依赖：** T2/T3（协议冻结）。**可并行：** 与阶段 2 整体并行。

**四条必须遵守（设计文档 §8.1，每条都有实测证据）：**
1. **`RequestBody.isDuplex()` 必须返回 `true`** — 默认 `false` 时 `onResponse` 被推迟到 `writeTo` 返回之后（实测 2010ms vs 2009ms），无法全双工。
2. **每个流用阻塞 `Call.execute()`，不要用 `enqueue()`/`Callback`** — 默认 `maxRequestsPerHost=5` 会让第 6 条流永久 queued（实测 20 个 async 只有 5 个 running、15 个 queued）；`execute()` 完全绕过 Dispatcher 计数（字节码 `Dispatcher.executed$okhttp` 无计数检查；实测 20 线程 → `maxConcurrentSync=20`，200 条流 329ms 全开）。每个流需一个专用线程。
3. **`readTimeout(0)` + `writeTimeout(0)`** — 默认 10s 会让空闲流超时（实测 idle 10076ms 失败），`Http2Stream$StreamTimeout.newTimeoutException` 会 `closeLater(ErrorCode.CANCEL)` **RST 整个流**——隧道对端 TCP 静默时必被误杀。实测双 0 下 idle 16s 仍存活。
4. **控制流与数据流共用同一个 `OkHttpClient` 实例** — 不同实例不共享连接池（实测 2 client × 10 流 → `distinctConnections=3`）；同实例 1 控制 + 20 数据 → `distinctConnections=1`。

**其他要点：**
- h2c：`Protocol.H2_PRIOR_KNOWLEDGE`（`OkHttpClient.Builder.protocols(listOf(...))`）。
- h2-over-TLS：复用 `initTrustAllSSL()`（`BackgroundService.java:2482`）+ `getTrustAllSSLContext()`（`:328`）。
- 半关闭：`RequestBody.writeTo` 里 `sink.close()` 发 `END_STREAM` **不关整条流**；`Call.cancel()` = `RST_STREAM` **只影响本流**。
- 背压无需额外 API：写侧 h2 窗口阻塞；读侧不调 `source().read()` 就不发 `WINDOW_UPDATE`。

**Step 1: 定义接口 + 写 fake 测试**

接口方法建议：`openStream(host, port)` / `openControl()` / `isConnected()` / `close()`，返回可读写的流句柄。

测试用 **fake `TunnelStream`**（**不要**沿用 `BackgroundServiceFloatingTest.java:663-674` 的 `mock(okhttp3.WebSocket.class)` + 反射模式——h2 隧道不是 WebSocket）。覆盖：`execute()` 被调用（非 `enqueue()`）、`readTimeout==0`、`writeTimeout==0`、`isDuplex()==true`、`H2_PRIOR_KNOWLEDGE` 在 h2c 模式下被设置。

**Step 2: 跑测试确认失败**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*H2TunnelStream*'
```
预期：编译失败（类不存在）。

**Step 3: 实现**

按上面四条 + 其他要点实现。

**Step 4: 跑测试**

```bash
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*H2TunnelStream*'
```
预期：全绿。

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/TunnelStream.java \
        android/app/src/main/java/com/clawbench/app/H2TunnelStream.java \
        android/app/src/test/java/com/clawbench/app/H2TunnelStreamTest.java
git commit -m "feat(android): add TunnelStream abstraction and OkHttp h2 implementation"
```

**完成标准：** 四条硬约束各有对应断言（`isDuplex`/`execute`/双 0 超时/共享 client）。

---

### Task 10: 本地 `ServerSocket` 监听循环 + 专用线程池

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`
  - `:194`（`networkExecutor`）— **新增独立线程池**，不复用单线程
  - `:1536`（`addPortForward`，`setPortForwardingL` 在 `:1601`）
  - `:1741`（`removePortForward`）
  - `:1455-1483`（`ensureConnection` 的 `-L` 重放）
  - `:2011`（`disconnectInternal`，`:2015-2024` 循环 `delPortForwardingL/R`）
- Create/Modify: 对应测试

**依赖：** T9。**可并行：** 与 T11 **不可并行**（同改 `ensureConnection`/`disconnectInternal`）；与 T12 的 `maybeReleaseWifiLock`(:2388) 区域理论可并行，但同文件建议串行。

**要点：**
- **全文件当前无 `ServerSocket`**（仅注释 `:1658`）→ 每个 `localPort` 一个 accept 循环。
- **不能占用单线程 `networkExecutor`(:194)**（29 处调用）→ 另起专用线程池，否则会与 `ensureConnection` 等任务互相饿死。
- accepted → h2 流桥接 + 背压（T9 的 `TunnelStream`）。
- `PortInfo`（:141-168）**结构不变**；listener / 流引用另开 `Map` 存放（**不落盘**）。
- 保留 SSH 为默认传输：SSH 路径行为不变。

**Step 1: 记录基线**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*PortHost*' --tests '*ReversePort*'
```
预期：`BackgroundServicePortHostTest`(27) + `BackgroundServiceReversePortTest`(19) 全绿。记下数字。

**Step 2: 写测试**

测试用 fake `TunnelStream`，断言：`addPortForward` 后 `ServerSocket` 在监听 `localPort`；`removePortForward` 后监听关闭；`ensureConnection` 重放会重建 listener；`disconnectInternal` 关闭所有 listener。

> **注意设计文档 §10.3 的警告**：替换 `addPortForward` 内部会让 `BackgroundServicePortHostTest` 里约 10+ 个 mock `com.jcraft.jsch.Session` 的 verify 用例失败（`:337`/`:367`/`:403`/`:428` 等）。修法：抽 `TunnelTransport` 接口注入 fake，或改为断言 `ServerSocket` 在监听。**纯持久化/解析用例不受影响**（`testSaveForwardedPorts_*`/`testRestoreForwardedPorts_*`/`BackgroundServicePortParsingTest` 19 个/`BackgroundServiceStalePortsTest` 11 个）。

**Step 3: 实现**

**Step 4: 跑测试**

```bash
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*PortHost*' --tests '*ReversePort*' --tests '*Tunnel*'
```
预期：全绿。

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/BackgroundService.java android/app/src/test/
git commit -m "feat(android): local ServerSocket listener loop for -L over h2"
```

**完成标准：** `PortInfo` 持久化格式未变；save/restore 用例全绿；新 listener 生命周期用例全绿。

---

### Task 11: `-R` 控制流 + claim（Android 侧）

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`
  - `:1784`（`addReversePortForward`，`setPortForwardingR` 在 `:1823`）
  - `:1852`（`removeReversePortForward`，`delPortForwardingR` 在 `:1862`）
  - `:1489-1513`（`ensureConnection` 的 `-R` 重放）
  - `:2011`（`disconnectInternal` 的 reverse 清理）
- Create/Modify: 对应测试

**依赖：** T9。**可并行：** 与 T10 不可并行（同改 `ensureConnection`/`disconnectInternal`）。

**要点：**
- 改走控制流（T3 的 NDJSON）+ claim。
- **控制流与数据流必须共用同一个 `OkHttpClient` 实例**（实测：不同实例不共享连接池，各开一条连接）。
- `isNonLocalhost()`（`:161-164`）对 reverse 返回 false — **保持**（reverse 无本地监听）。
- `PortInfo.reverse`（`:145`）**结构不变**。

**Step 1: 写测试**

用 fake `TunnelStream`：`addReversePortForward` 发 `bind`；`removeReversePortForward` 发 `unbind`；收到 `incoming` 后用 token 发 `claim`；控制流与数据流用同一 client 实例（断言 client 引用相等）。

**Step 2-4: 实现并跑测试**

```bash
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*ReversePort*' --tests '*Tunnel*'
```
预期：全绿（`BackgroundServiceReversePortTest` 19 个基线不回归）。

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/BackgroundService.java android/app/src/test/
git commit -m "feat(android): -R over h2 control stream with claim tokens"
```

**完成标准：** 共享 `OkHttpClient` 有断言；`BackgroundServiceReversePortTest` 19 个不回归。

---

### Task 12: `WifiLock` 判据扩展 + 息屏策略按传输区分

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`
  - `:2388-2389`（`maybeReleaseWifiLock`，`sshActive` 判据）
  - `:806-830`（息屏 suspend / 亮屏重连）
  - `:790`（`sshScreenSuspended`）

**依赖：** T9/T11（需要 h2 活跃态可判定）。**可并行：** 与 T10/T11 同文件但不同区域；**建议串行**。

**要点：**
- `maybeReleaseWifiLock`(:2388) 的判据从 `!sshActive && !nativeWsActive` 扩展为再 `&& !h2TunnelActive`。**否则 h2 隧道在跑时 WifiLock 会被误释放**（h2 下 `sshActive` 恒 false）。
- 息屏 suspend（`:806-830`）按传输区分：**h2 隧道不应被 suspend**（`sshScreenSuspended` 逻辑只对 SSH 生效）。

**Step 1: 写测试**

- h2 隧道活跃时 `maybeReleaseWifiLock` **不**释放锁。
- 息屏时 SSH 被 suspend、h2 不被 suspend。
- 亮屏重连行为按传输区分。

**Step 2-4: 实现并跑测试**

```bash
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*WifiLock*' --tests '*Screen*' --tests '*Tunnel*'
```

**Step 5: 全量 Android 回归（本阶段收尾必做）**

```bash
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest
```
预期：**804 tests / 0 failures / 0 errors**（保留 SSH 为默认传输时不回归）。若数量变化，先确认是自己新增的用例（新增会 > 804），**不要**把新增算作回归。

**Step 6: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/BackgroundService.java android/app/src/test/
git commit -m "fix(android): keep WifiLock and screen-on policy correct under h2 tunnel"
```

**完成标准：** h2 活跃时 WifiLock 不误释放；息屏策略按传输区分；804 基线不回归。

---

## 阶段 4 — 前端

### Task 13: 放宽 SSH 门控 + 展示传输方式 + i18n

**Files:**
- Modify: `web/src/composables/usePortForward.ts:471`（`loadSSHInfo`）、`:492`（`checkTunnelHealth` 的 `if (!info?.enabled) return`）
- Modify: `web/src/components/proxy/ProxyPanelContent.vue`（展示传输方式；锚点 `:19-44` banner、`:55-106` 手动指南）
- Modify: `web/src/i18n/locales/zh.ts:1556-1605`（`sshTunnel` 在 `:1565`）、`web/src/i18n/locales/en.ts`（`sshTunnel` 在 `:1562`）
- Modify: `web/src/composables/__tests__/usePortForward.test.ts:70-88`（自复制的 `portForwardUtils` mock，新增函数要同步）

**依赖：** T8（桥契约 `getTunnelTransport`）。**可并行：** 与阶段 3 并行（不同目录），但桥契约部分依赖 T8。

**要点：**
- `:492` 的 `if (!info?.enabled) return` **必须放宽为「SSH 或 h2 任一可用」**，否则服务器未开 SSH 时整个健康检查被跳过。
- `:471 loadSSHInfo` 同步放宽（同时获取 h2 可用性）。
- **web 端零客户端隧道代码**（浏览器靠用户自己跑 `ssh -L`；`fetch()` 规范层面只有 `duplex:"half"`）。**不要**写浏览器隧道客户端。
- `portForwardUtils.ts` 的 `tunnelStatusFromPorts`(:47-52) 与 `buildPortUrl`(:59-66) **零改动**。

**Step 1: 写失败测试**

在 `web/src/composables/__tests__/usePortForward.test.ts` 加用例：**SSH 关、h2 开时 `checkTunnelHealth` 仍继续检查**（这是本次门控放宽的核心行为）。

注意 `:70-88` 自复制了一份 `portForwardUtils` mock——若新增了工具函数，要在这里同步补上，否则测试会因 mock 缺函数而失败。

**Step 2: 跑测试确认失败**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/
npx vitest run web/src/composables/__tests__/usePortForward.test.ts
```
预期：新用例 FAIL（门控仍短路）。

**Step 3: 实现**

放宽两处门控；`ProxyPanelContent.vue` 展示当前传输方式；i18n 加文案（zh + en 同步，`en.ts` 的 key 必须与 `zh.ts` 对齐）。

**Step 4: 跑测试 + 编译**

```bash
npx vitest run web/src/composables/__tests__/usePortForward.test.ts
# AGENTS.md：纯前端改动完成后必须自觉编译
npm run build
```
预期：测试全绿；`npm run build` 成功，产物写入 `.clawbench-web/`（disk 模式立即生效，无需重启）。

**Step 5: Commit**

```bash
git add web/src/composables/usePortForward.ts web/src/composables/__tests__/usePortForward.test.ts \
        web/src/components/proxy/ProxyPanelContent.vue web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(web): allow h2-only tunnels in the health check and show transport"
```

**完成标准：** 「SSH 关、h2 开仍检查」用例通过；`npm run build` 成功；i18n zh/en 对齐。

---

## 阶段 5 — 集成验证

### Task 14: 端到端（Electron + Android，`-L` 与 `-R`）

**依赖：** T1-T13 全部。**可并行：** 否（收尾）。

**前置：**
```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/
mkdir -p internal/frontend/dist && touch internal/frontend/dist/.gitkeep   # 见 §0.1
./build.sh --restart --restart-port=20100                                  # 隔离实例，不碰 20000
# 起一个本地 echo/HTTP 目标服务用于 -L 验证
python3 -m http.server 18080 --bind 127.0.0.1 &
```

**A. 只需映射 20000 一个端口（核心目标）**

```bash
# 只允许访问 20100（模拟「只放行 20000」的网络）
curl -s -o /dev/null -w "h1 %{http_code} %{http_version}\n"   http://127.0.0.1:20100/api/health
curl -s -o /dev/null -w "h2c %{http_code} %{http_version}\n" --http2-prior-knowledge http://127.0.0.1:20100/api/health
```
预期：`h1 200 1.1`、`h2c 200 2`。**不需要任何其他端口。**

**B. Electron `-L`**

在 Electron 中配置一个 forward 映射（`localPort=15080` → `host=127.0.0.1&port=18080`，传输选 h2），然后：

```bash
curl -s -o /dev/null -w "electron -L -> %{http_code}\n" http://127.0.0.1:15080/
```
预期：`200`（本地 15080 经 h2 隧道到达服务端 18080）。

覆盖：64 KiB 大块、多流并发、半关闭。

```bash
# 64 KiB 大块
head -c 65536 /dev/urandom > /tmp/big.bin
curl -s --data-binary @/tmp/big.bin http://127.0.0.1:15080/ -o /tmp/big.out
cmp /tmp/big.bin /tmp/big.out && echo "64KiB OK"
# 多流并发
for i in $(seq 1 20); do curl -s -o /dev/null -w "%{http_code} " http://127.0.0.1:15080/ & done; wait; echo "concurrency OK"
```
预期：`64KiB OK`；20 个请求全部 `200`。

**C. Electron `-R`**

配置一个 reverse 映射（`serverPort=17080` → 客户端 `host=127.0.0.1&port=18080`），然后从**服务端**访问：

```bash
curl -s -o /dev/null -w "electron -R -> %{http_code}\n" http://127.0.0.1:17080/
```
预期：`200`（服务端 17080 经 h2 隧道回到客户端 18080）。

同时验证 token 认领与保留端口拒绝：

```bash
# bind 20000 必须被拒（code=3）
# 通过控制流发 {"type":"bind","port":20000}，预期回 {"type":"bind_err","code":3,...}
# 单次 token：同一 token 第二次 claim 必须 403
```
预期：bind 20000 回 `bind_err`；重复 claim 回 `403`。

**D. Android `-L` / `-R`**

用真机/模拟器 + 上述同样的目标服务：
```bash
# Android -L：本机 15080 → 服务端 18080
curl -s -o /dev/null -w "android -L -> %{http_code}\n" http://127.0.0.1:15080/
# Android -R：服务端 17080 → 客户端 18080
curl -s -o /dev/null -w "android -R -> %{http_code}\n" http://127.0.0.1:17080/
```
预期：均 `200`。

> **全双工集成测试必须起真实 Go 服务端**——**MockWebServer 是半双工的**（字节码 `Http2SocketHandler.onStream` 先 `readRequest` 消费完整个 body 再 `writeResponse`；实测全双工用例**直接 hang 到 timeout，EXIT=124**）。`MockWebServer` 支持 h2c（`server.setProtocols(Arrays.asList(Protocol.H2_PRIOR_KNOWLEDGE))` 实测通过）但**不能测全双工**。单元测试用 fake `TunnelStream`。

**E. SSH 路径回归（必须确认仍工作）**

```bash
# 关掉 h2（transport=ssh），或直接用 SSH 客户端验证既有 -L/-R 不受影响
ssh -p 20001 -L 15081:127.0.0.1:18080 clawbench@127.0.0.1
curl -s -o /dev/null -w "ssh -L -> %{http_code}\n" http://127.0.0.1:15081/
```
预期：`200`（SSH 兜底未回归）。

**F. 收尾：全量检查**

```bash
# 先确认没有并发 agent 在跑
ps -eo pid,ppid,etime,cmd | grep -E "vitest|go test|npm run build" | grep -v grep
# 再串行执行（绝不同时跑 build 与全量 vitest）
./scripts/pre-push-checks.sh --skip-android
```
预期：lint + Go test + 前端 test + build + typecheck 全绿（Tier 1-only 失败属 non-blocking，需先确认归属）。

**Commit（若有测试脚本/文档补充）：**

```bash
git add -A
git commit -m "test(tunnel): end-to-end h2 -L/-R verification across Electron and Android"
```

**完成标准：**
- 只放行 20000 一个端口即可完成 `-L` 与 `-R`（Electron + Android 各一条）。
- 64 KiB 大块、多流并发、半关闭、token 认领、保留端口拒绝全部通过。
- SSH 路径回归通过。

---

## 2. 执行顺序总览

| 顺序 | 任务 | 可并行组 | 备注 |
|---|---|---|---|
| 1 | **T1** 三协议共存 | 独占 | **必须独立验收**；5 个 WS 端点全挂 = 停 |
| 2 | **T2** `-L` handler | A | 与 T3 建议合并注册行 |
| 3 | **T3** `-R` 控制流 + claim | A | 依赖 T1 |
| 4 | **T4** ProxyRegistry 门控 | B | 依赖 T3；若 T8 先做则用真实 `transport` 字段 |
| 5 | **T5** 文档同步 | B | 依赖 T2+T3；只碰 docs/openapi |
| 6 | **T6** `h2Transport.ts` | C（与阶段 3 并行） | 前置 `cd desktop && npm ci` |
| 7 | **T7** `tunnel.ts` 接入 | C | 依赖 T6；**34 用例必须仍全绿** |
| 8 | **T8** transport 配置链路 | C | 依赖 T4；三处桥同步 |
| 9 | **T9** Android `TunnelStream` | D（与阶段 2 并行） | 四条硬约束 |
| 10 | **T10** Android 本地监听 | D | 依赖 T9 |
| 11 | **T11** Android `-R` | D | 依赖 T9；与 T10 同文件不同区域，建议串行 |
| 12 | **T12** Android WifiLock/息屏 | D | 依赖 T9/T11；收尾跑 804 全量 |
| 13 | **T13** 前端门控 + i18n | E | 依赖 T8；`npm run build` |
| 14 | **T14** 端到端 | 收尾 | 依赖全部 |

**可并行矩阵（互不碰文件，可同时开工）：**
- **A** = {T2, T3} 内部建议串行（同改 `handler.go` 注册段）；T2 完成后的纯逻辑（`internal/tunnel/guard.go`）与 T3 的 `ndjson.go`/`token.go` 可并行。
- **C** = 阶段 2（Electron）与 **D** = 阶段 3（Android）**整体可并行**（不同语言、不同目录）。
- **B** = {T4, T5} 可与 C/D 并行。
- **E** = T13 可与 D 并行；与 C 的桥契约部分需等 T8。

**并行禁令（AGENTS.md）：**
- 绝不同时跑 `npm run build` 与全量 vitest（OOM）。
- 全量测试前先 `ps` 检查他人是否在跑；不重复跑同一个全量。
- 判「是否我引入的回归」一律先隔离单跑（`npx vitest run <file>` / `go test ./internal/<pkg>/ -run <TestName>`）。

**提交纪律：** 每个任务完成后立即 commit（AGENTS.md：频繁提交）。

---

## 3. 风险与缓解（对应设计文档 §12，只列执行相关）

| # | 风险 | 缓解（落到本计划的哪个任务） |
|---|---|---|
| 1 | `Protocols` 非 nil 覆盖默认 → 5 个 WS 端点全挂 | T1 独立验收；`serverProtocols` helper + 三合一测试 |
| 2 | OkHttp 默认 10s 超时 RST 流 | T9 断言 `readTimeout==0` + `writeTimeout==0` |
| 3 | `execute()` 线程模型与线程数 | T9 每流专用线程；T10 独立线程池 |
| 4 | 250 流上限 | 已知边界，本计划不提升（YAGNI）；T14 多流并发用 20 条验证 |
| 5 | claim token 安全 | T3 `crypto/rand` + 单次 + 绑定控制流 + 挂起超时 |
| 6 | Android ALPN 未真机验证 | h2c 是默认形态不涉 ALPN，不阻塞；T14 D 项验证 |
| 7 | `ProxyRegistry` 门控 | T4 |
| 8 | 前端 SSH 门控残留 | T13 |
| 9 | OpenAPI 漂移测试 | T5 + 漂移测试命令 |
| 10 | Android `PortInfo` 被持久化 | T10/T11 **不改结构**；T12 收尾跑 804 全量 |
| 11 | MockWebServer 半双工 | T14 全双工必须真实 Go 服务端 |
| 12 | 反代 WS 升级透传无测试 | 本方案不经过；不补（设计文档 §13.8） |

---

## 4. 明确不做（设计文档 §13，执行时不要擅自扩范围）

1. 不做 HTTP/3。
2. 不做浏览器隧道客户端（`fetch()` 只有 `duplex:"half"`）。
3. 不做流优先级。
4. 不重写 `PortInfo`。
5. 不改 `buildPortUrl`；不新增 `buildPortWsUrl`。
6. 不做压缩。
7. 不做多连接池。
8. 不补 HTTP 反代 WS 升级透传测试。
9. 不复用 `SetSSHServer` 全局注入（隧道 handler 无状态，只依赖 `service.ProxyService` + nil 守卫）。
10. 不删除 SSH 传输。
