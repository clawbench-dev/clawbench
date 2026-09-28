# Android h2 隧道本地开关 实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让 Android 端的 h2 隧道由**本地 SharedPreferences 功能开关**控制（默认关 = SSH），同时把 Electron clamp 到 `ssh`、移除前端向原生推送 `transport` 的链路、修复 Android「当前传输」展示 bug，并同步文档。**服务端零改动。**

**Architecture:** 删除 Android `BackgroundService` 的进程级静态字段 `transportPreference`，改为「使用时直读 SharedPreferences」——`isTunnelTransportH2Enabled(Context)` 是唯一真相源。Electron 在 **bridge（IPC 边界）** 写死 `ssh`，`tunnel.ts` 的三值能力与 `tunnel.h2.test.ts` 保持不动。前端删除 `syncTunnelTransportToNative` 推送，改用 Android 专用设置行直接读写偏好。服务端 `port_forward.transport` 保持三值 / 默认 `both`（理由见设计文档 §2.2 的 registry 门控陷阱）。

**Tech Stack:** Java 17（Android，JSch + OkHttp 4.12）、Kotlin/Java + Gradle、Vue 3 + TypeScript + Vitest、Electron `node:http2`、Go（仅文档改动）。

**设计来源（唯一事实来源，不要重新调研、不要推翻其决策）：** `docs/plans/2026-09-28-android-h2-toggle-design.md`（756 行，12 章）

---

## 0. 开工前必读

### 0.1 执行环境前置

| 前置 | 命令 / 值 | 说明 |
|---|---|---|
| Go 工具链不在 PATH | 全程用 `/usr/local/go/bin/go` | 见 `AGENTS.md` |
| Android JDK | `JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64` | **必须 JDK 17**，JDK 21 的 jlink 会拒绝 AGP 的 ModuleTarget |
| Android SDK | `ANDROID_HOME=/opt/android-sdk` | 已确认存在 |
| 前端 vitest | **必须从 worktree 根跑** | `web` 的 vitest 配置排除了 `**/.worktrees/**`；在子目录或主仓路径跑会静默找不到测试 |
| 桌面 vitest | 从 `desktop/` 跑 | 前置若 `desktop/node_modules` 为空：`cd desktop && npm ci`（带 lock） |

### 0.2 并发纪律（AGENTS.md 强制）

另一个 agent 正在 `android-e2e/` 搭模拟器 / Appium 环境，**可能随时跑 gradle / emulator**。跑任何测试前先检查：

```bash
ps -eo pid,ppid,etime,cmd | grep -E "gradlew|emulator|appium|vitest|go test|npm run build" | grep -v grep
```

- 有输出 → **等待**其结束，不要并发起跑（AGENTS.md：全量测试是共享稀缺资源）。
- **绝不同时跑 `npm run build` 与全量 vitest**（会 OOM）。
- **不要动 `android-e2e/` 目录**。
- 判「是否我引入的回归」一律**先隔离单跑**（`./gradlew :app:testDebugUnitTest --tests '*Xxx*'` / `npx vitest run <file>`）。
- 不重复跑同一个全量；能复用结果就复用。

### 0.3 基线（实测，记录到本地即可，不要写进代码注释）

| 套件 | 基线 | 命令 |
|---|---|---|
| Android 全量 | **1032 tests / 0 failures** | `cd android && JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk ./gradlew :app:testDebugUnitTest` |
| `BackgroundServicePortHostTest` | 27 个 `@Test`，mock JSch `Session` + `verify(setPortForwardingL/delPortForwardingL)` | `--tests '*PortHost*'` |
| `BackgroundServiceReversePortTest` | 19 个 `@Test`，mock JSch `Session` + `verify(setPortForwardingR/delPortForwardingR)` | `--tests '*ReversePort*'` |
| 桌面 `tunnel.test.ts` | **34 passed**（实测；以运行输出为准，不要照抄数字） | `cd desktop && npx vitest run src/main/tunnel.test.ts` |
| 桌面 `tunnel.h2.test.ts` | 全绿（21+ 用例） | `cd desktop && npx vitest run src/main/tunnel.h2.test.ts` |

> **不要照抄用例数**：`grep -c 'it('` 会把注释里的 `it(` 也算进去（实测 `tunnel.test.ts` grep 得 36，实际 34）。一律以 `vitest run` / gradle 的**实际输出**为准，改完前后对比同一命令的输出。

### 0.4 设计文档行号漂移（**已实测，按本计划执行**）

设计文档 §7.1 的「用例:行号」表，绝大多数行号指向 `@Test` **装饰器行**，而不是 `setField(...)` 调用行。实测权威清单如下（以符号定位，不要照搬数字）：

- `BackgroundServiceTransportTest.java` 共 **20 处** `setField("transportPreference", ...)`：
  `:152`、`:212`、`:251`、`:279`、`:290`、`:307`、`:322`、`:334`、`:353`、`:367`、`:405`、`:426`、`:441`、`:469`、`:488`、`:500`、`:527`、`:558`、`:583`、`:732`
- 直接调用**已删静态方法**的用例共 4 个：
  - `BackgroundServiceTransportTest.java`：`defaultPreferenceIsSsh`（`:223`）、`setTransportPreference_parsesTheWireValue`（`:230`，内含 `:233-234` 的 `"both"`）、`setTransportPreference_unknownValueFallsBackToSsh`（`:238`）、`setTransportPreference_whileConnected_doesNotTearDownTheLiveTransport`（`:551`，`:562` 调 `setTransportPreference("ssh")`）
  - `MainActivityTunnelBridgeTest.java`：`:167/:171/:173/:179/:184/:186/:192/:196/:198/:204/:208`
- 需删的 both 回退用例：`ensureConnection_both_fallsBackToSshWhenH2Fails`（`:499`，其 `setField(...BOTH)` 在 `:500`）。
- 展示断言：`getActiveTunnelTransport_reportsTheH2WireKind`（`:701`，断言在 `:704` = `"h2c"`）。
- `setField` 助手（`:802-812`）：对静态字段走 `field.set(null, ...)`（`:807-808`）。**删 `transportPreference` 后所有相关 `setField` 抛 `NoSuchFieldException`**，必须全部替换。

设计文档写的 `PortForwardTransportKind.java` 路径在 `app/`，**实测实际在 `app/tunnel/`**（`android/app/src/main/java/com/clawbench/app/tunnel/PortForwardTransportKind.java`）。以实测为准。

### 0.5 一处**刻意的任务边界偏离**（编译耦合，必读）

设计文档把「删 `setTransportPreference`/`getTransportPreference`」放在 T1、把「桥改造」放在 T4。但 Java 主源码与测试同批编译：`MainActivity.WebAppInterface.setTunnelTransport(String)` 调用 `BackgroundService.setTransportPreference(String)`（`MainActivity.java:2719-2722`），**删掉该静态方法后 `MainActivity.java` 与 `MainActivityTunnelBridgeTest.java` 都无法编译**。而 `setTransportPreference(String)` 没有 `Context` 参数，**无法**改成写 SharedPreferences 的过渡 shim（写了也是不持久化的假生效）。

因此本计划的边界是：

- **T1 包含 `MainActivity` 桥的两处源码最小改动**（`setTunnelTransport(String)` → `setTunnelTransportH2Enabled(boolean)`；`getTunnelTransport()` → 由本地布尔派生 `'h2'|'ssh'`），使 T1 提交后整个 `:app` 可编译、可独立验收（验收标准只看 **SSH 默认行为**）。
- **T4 承接桥的完整契约测试**（改写 `MainActivityTunnelBridgeTest` 的 5 个用例 + `invoke()` 助手）与语义注释。
- 若实施者希望严格把 `MainActivity` 源码留给 T4，则 **T1 与 T4 必须同一提交**（不得留下不可编译的中间态）。二选一，本计划默认前者。

---

## 1. 任务总览与依赖图

```
阶段 1 Android 权威化（最高风险优先）
  T1 删静态字段 + 偏好真相源 ────┐  （独立验收：默认关 = SSH，PortHost/ReversePort 未改断言全绿）
  T2 删 BOTH ────────────────────┤  依赖 T1（枚举删除后 fromWire 回退 SSH）
  T3 修 tls/h2c → h2 展示映射 ───┤  依赖 T1（同文件不同区域，建议紧随）
  T4 桥测试 + 语义收尾 ──────────┘  依赖 T1（源码已在 T1 换签名）
  T5 改造 TransportTest 测试助手 ──  依赖 T1（字段已删）
  T6 新增 Android 测试 ───────────  依赖 T1/T3/T5

阶段 2 前端
  T7 移除服务端推送 ────┐
  T8 新增桥契约 ────────┤  依赖 T1（新桥方法名冻结）
  T9 设置页专用开关行 ──┘  依赖 T8；T9 最后 `npm run build`

阶段 3 Electron
  T10 bridge 层 clamp ─────  独立（不碰 tunnel.ts）；可与阶段 2 并行

阶段 4 文档
  T11 同步文档 ────────────  依赖 T1/T7/T10 的最终行为（可与阶段 2/3 并行收尾）

阶段 5 集成验证
  T12 端到端 ──────────────  依赖全部
```

**可并行组（互不碰文件区域）：**

- **A** = {T2, T3}：都改 `BackgroundService.java`/`PortForwardTransportKind.java` 的**不同区域**，可与 T4 的 `MainActivity` 区域并行；但同文件多区域同时改易冲突，**建议 T1 → T2 → T3 串行**。
- **B** = {T5, T6}：T5 改 `BackgroundServiceTransportTest.java` 助手，T6 新增独立测试文件；T6 依赖 T5 的 `mockPrefs` stub 模式。
- **C** = 阶段 2（前端）与 **D** = T10（Electron）**整体可并行**（不同目录）。T11 文档可与 C/D 并行。
- **T1 必须独立验收后再开 T2/T3/T5/T6**（最高风险）。

**每个任务完成后立即 commit**（AGENTS.md：频繁提交）。

---

## 阶段 1 — Android 权威化

### Task 1: 删除 `transportPreference` 静态字段，改用 SharedPreferences 单一真相源（最高风险）

**为什么必须独立验收：** 这是唯一会让「默认行为」改变的任务。默认关必须**逐字节等价**于改造前的 SSH 路径。验收锚点是 `BackgroundServicePortHostTest`(27) 与 `BackgroundServiceReversePortTest`(19) 里 mock JSch `Session` 并 `verify(setPortForwardingL/delPortForwardingL/setPortForwardingR/delPortForwardingR)` 的用例——它们**必须全绿且未改断言**。任何为让测试通过而改这些断言的行为都等于「把回归藏起来」。

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`
  - `:212` — ❌ 删 `private static volatile PortForwardTransportKind transportPreference = ...SSH;`
  - `:100-110` — ➕ 在 `KEY_*` 常量区加 `KEY_TUNNEL_TRANSPORT_H2 = "tunnel_transport_h2_enabled"`
  - `:412-415` / `:423-433` — ♻️ 照抄 `isFloatingWindowEnabled` / `setFloatingWindowEnabled` 形态
  - `:1453-1470` — ✏️ `ensureConnection()` 改读布尔偏好（both 分支被结构性移除，见 T2）
  - `:1788-1797` — ✏️ `transportForPortOps()`
  - `:1804-1812` — ❌ 删 `setTransportPreference(String)` / `getTransportPreference()`
  - `:1821-1831` — ✏️ `getActiveTunnelTransport()`（本任务**只**改读法，映射留 T3）
  - `:1842-1850` — ✏️ `isSelectedTransportConnected()`
- Modify: `android/app/src/main/java/com/clawbench/app/MainActivity.java:2719-2728`（**编译必需**，见 §0.5）
- Modify（最小编译修复）: `android/app/src/test/java/com/clawbench/app/MainActivityTunnelBridgeTest.java`（调用已删静态方法的用例先注释/改写到能编译，完整契约测试留给 T4）

**依赖：** 无（最先做）。**可并行：** 否，必须单独验收。

**Step 1: 记录基线（必做）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*PortHost*' --tests '*ReversePort*'
```

预期：`BackgroundServicePortHostTest`(27) + `BackgroundServiceReversePortTest`(19) 全绿。**记下输出**，改完后必须仍全绿且断言未动。

**Step 2: 加偏好键 + getter/setter（照抄先例）**

`BackgroundService.java` `:100-110` 加键；在 `:412-433` 附近照抄浮动窗口先例：

```java
// 签名片段（非完整实现）
private static final String KEY_TUNNEL_TRANSPORT_H2 = "tunnel_transport_h2_enabled";

/** 默认 false = SSH：从未开启过的安装行为与 h2 隧道存在前完全一致。 */
public static boolean isTunnelTransportH2Enabled(Context context) {
    return context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getBoolean(KEY_TUNNEL_TRANSPORT_H2, false);
}

public static void setTunnelTransportH2Enabled(Context context, boolean enabled) {
    context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit().putBoolean(KEY_TUNNEL_TRANSPORT_H2, enabled).apply();
    AppLog.i(TAG, "Tunnel: h2 transport enabled=" + enabled);
}
```

（`isFloatingWindowEnabled` 先例在 `:412-415`；`setFloatingWindowEnabled` 在 `:423-433`，含「运行中同步」逻辑——h2 开关**不需要**运行中同步，切换靠重连，不要照搬那部分。）

**Step 3: 三个使用点改读偏好**

```java
// ensureConnection() :1453
boolean h2 = isTunnelTransportH2Enabled(this);
if (h2) { ensureH2Connection(); return; }
ensureSshConnection();   // 原 :1469 不变

// transportForPortOps() :1796
return isTunnelTransportH2Enabled(this) ? h2Transport() : sshTransport();

// isSelectedTransportConnected() :1843
if (isTunnelTransportH2Enabled(this)) return tunnelStream().isConnected();
```

同步更新 `:1445-1452`、`:1833-1841` 的 javadoc（原文写「server's `port_forward.transport` setting」已失真，改为「本地偏好 `tunnel_transport_h2_enabled`」）。

**Step 4: 删静态字段与旧静态方法**

- 删 `:212` 字段。
- 删 `:1799-1812` 的 `setTransportPreference(String)` / `getTransportPreference()` 及其 javadoc。

**Step 5: 编译必需的 `MainActivity` 桥改动（§0.5）**

```java
// MainActivity.java:2719-2722
@JavascriptInterface
public void setTunnelTransportH2Enabled(boolean enabled) {
    AppLog.i(TAG, "JSBridge: setTunnelTransportH2Enabled=" + enabled);
    BackgroundService.setTunnelTransportH2Enabled(activity, enabled);
}

// MainActivity.java:2726-2728
@JavascriptInterface
public String getTunnelTransport() {
    return BackgroundService.isTunnelTransportH2Enabled(activity) ? "h2" : "ssh";
}
```

同步把 `:2705-2716` 的注释改掉（不再是「服务端下发」语义）；`:2730-2736` 的 `getActiveTunnelTransport()` javadoc 里 `"both"` 相关措辞本任务不动（T3 处理映射）。

**Step 6: 让测试能编译（完整改造留给 T4/T5）**

- `MainActivityTunnelBridgeTest.java`：把调用已删静态方法的用例临时改为调用新桥方法（`invoke("setTunnelTransportH2Enabled", true)` / `getTunnelTransport()`），或先 `@Ignore` 并留 TODO——**只要保证编译通过**。完整断言 T4 补。
- `BackgroundServiceTransportTest.java`：本步只需保证编译（`setField("transportPreference", ...)` 会在运行期抛异常，但编译通过）。运行期修复在 T5。

**Step 7: 跑 SSH 默认行为回归（第一验收标准）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*PortHost*' --tests '*ReversePort*'
```

预期：`PortHost`(27) + `ReversePort`(19) **全绿**，且 `git diff` 中这两个文件的断言**一行未改**。

```bash
# 断言未改的硬证据：
git diff --stat android/app/src/test/java/com/clawbench/app/BackgroundServicePortHostTest.java \
                 android/app/src/test/java/com/clawbench/app/BackgroundServiceReversePortTest.java
```

预期：**无输出**（两文件未被改动）。

**Step 8: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/BackgroundService.java \
        android/app/src/main/java/com/clawbench/app/MainActivity.java \
        android/app/src/test/java/com/clawbench/app/MainActivityTunnelBridgeTest.java \
        android/app/src/test/java/com/clawbench/app/BackgroundServiceTransportTest.java
git commit -m "refactor(android): make SharedPreferences the single source of truth for the h2 tunnel toggle

Deletes the process-wide static transportPreference field. The default (false)
routes through SSH exactly as before; PortHost/ReversePort JSch verifications are
untouched, which is the acceptance anchor for this change."
```

**完成标准：**
- `PortHost`(27) + `ReversePort`(19) 全绿，且两文件 `git diff` 为空。
- `BackgroundService` 中不再有 `transportPreference` 字段与 `setTransportPreference`/`getTransportPreference`。
- 整个 `:app` 主源码可编译（`./gradlew :app:compileDebugJavaWithJavac` 成功）。
- **不得**与 T2/T3 合并提交。

---

### Task 2: 删除 Android 侧 `BOTH` 枚举

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/tunnel/PortForwardTransportKind.java`
  - `:7-10` — ✏️ javadoc 删掉 `BOTH` 的说明
  - `:17-18` — ❌ 删 `BOTH("both")` 及其注释
- Modify: `android/app/src/test/java/com/clawbench/app/BackgroundServiceTransportTest.java`
  - ❌ 删 `ensureConnection_both_fallsBackToSshWhenH2Fails`（`:499`，含 `:500` 的 `setField(...BOTH)`）
- （`BackgroundService.java:1459-1468` 的 both 回退分支已在 T1 结构性移除，本任务确认其不存在即可。）

**依赖：** T1（布尔开关无法表达 both；`fromWire("both")` 之后落到 `SSH`，`:35-42`，无害）。**可并行：** 与 T3 同文件不同区域，建议串行。

**Step 1: 删枚举值**

`PortForwardTransportKind.java` 只留 `SSH("ssh")` / `H2("h2")`。`fromWire` 保持不变（未知值回退 `SSH`，现在 `"both"` 也落入该分支）。

**Step 2: 删 both 回退用例**

删 `BackgroundServiceTransportTest.java` 的 `ensureConnection_both_fallsBackToSshWhenH2Fails`。同时确认 T1 已把 `setTransportPreference_parsesTheWireValue`（`:230`，内含 `:233-234` 的 `"both"` 断言）删除或改写——若 T1 只注释，本任务删除。

**Step 3: 验证**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
# 确认全仓再无 BOTH 引用
grep -rn "BOTH\|PortForwardTransportKind.BOTH\|\"both\"" app/src/main/java/ || echo "NO BOTH IN MAIN"
grep -rn "BOTH" app/src/test/java/ || echo "NO BOTH IN TESTS"

JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*Transport*'
```

预期：`NO BOTH IN MAIN` / `NO BOTH IN TESTS`；`*Transport*` 全绿（`H2TunnelStreamTest` 也含 `Transport`，一并跑）。

> **不对称须注明**：桌面端**保留** `'both'`，Android 没有。两端 `TransportPreference` 集合不一致，这是刻意的（设计文档 D5）。

**Step 4: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/tunnel/PortForwardTransportKind.java \
        android/app/src/test/java/com/clawbench/app/BackgroundServiceTransportTest.java
git commit -m "refactor(android): drop BOTH from PortForwardTransportKind

A boolean toggle has only two states, so 'both' cannot be expressed. fromWire
still falls back to SSH for unknown wire names, so a stale 'both' is harmless."
```

**完成标准：** 主源码与测试均无 `BOTH` 引用；`--tests '*Transport*'` 全绿。

---

### Task 3: 修复 `getActiveTunnelTransport()` 的 `tls`/`h2c` 展示 bug

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java:1821-1831`（`getActiveTunnelTransport()`）
- Modify: `android/app/src/test/java/com/clawbench/app/BackgroundServiceTransportTest.java:701-705`（断言 `"h2c"` → `"h2"`）

**依赖：** T1。**可并行：** 与 T2 同文件不同区域，建议串行。

**现状与理由：** `getActiveTunnelTransport()` 返回 `kind.wireName()`，即 `"tls" | "h2c"`。但前端白名单 `web/src/composables/usePortForward.ts:62` 是 `TRANSPORTS = ['ssh', 'h2', 'both']`，`read()`（`:551`）判 `TRANSPORTS.includes(value)` 为 false → 回退到偏好。结果：**Android 上 `h2c` 永远显示不出来**，「当前传输」永远显示偏好值。修法：在 **Android 边界**把 `tls|h2c → h2` 归一化，与桌面直接返回 `'ssh'|'h2'` 对齐（`TransportKind` 定义见 `tunnel/TransportKind.java:17/19`：`TLS("tls")` / `H2C("h2c")`）。

**Step 1: 写/改失败测试**

把 `:704` 的断言改为：

```java
assertEquals("h2", BackgroundService.getActiveTunnelTransport());
```

**Step 2: 跑测试确认失败**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*BackgroundServiceTransportTest*'
```

预期：`getActiveTunnelTransport_reportsTheH2WireKind` FAIL（期望 `h2`，实得 `h2c`）。

**Step 3: 实现映射**

```java
// BackgroundService.java:1821-1831，getActiveTunnelTransport()
TransportKind kind = tunnel.getKind();
if (kind == null) return "";
// Android 边界归一化：前端白名单只有 'ssh'|'h2'|'both'（usePortForward.ts:62），
// 直接回 'tls'|'h2c' 会被 read() 拒绝并回退到偏好，导致 h2 永远显示不出来。
// TransportKind 只有 TLS("tls") / H2C("h2c")（tunnel/TransportKind.java:17/19），
// 两者都是 h2 线缆，故统一回 "h2"。
return "h2";
```

> 注意：`TransportKind` **没有** `SSH` 值——非 null 必为 `TLS` 或 `H2C`。不要凭空引入 `TransportKind.SSH`；本函数只在有 live h2 会话时才会走到这里（`:1828` 已对 `!isConnected()` 返回 `""`）。

**Step 4: 跑测试确认通过 + 证明前端能显示 h2**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*TransportTest*'
```

预期：全绿。

前端白名单证明（**只读命令，不改代码**）：

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
grep -n "TRANSPORTS" web/src/composables/usePortForward.ts
# 预期 :62 => const TRANSPORTS: readonly string[] = ['ssh', 'h2', 'both']
# 'h2' 在列 => 归一化后的 'h2' 会被 read() 接受（:551）
```

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/BackgroundService.java \
        android/app/src/test/java/com/clawbench/app/BackgroundServiceTransportTest.java
git commit -m "fix(android): normalize tls/h2c to h2 at the bridge boundary

The frontend whitelist is ['ssh','h2','both'] (usePortForward.ts:62), so
reporting the raw 'tls'/'h2c' wire name made read() reject it and fall back to
the preference — Android's 'current transport' could never show h2."
```

**完成标准：** `getActiveTunnelTransport()` 返回 `"h2"`（有 live h2 会话时）或 `""`；`--tests '*TransportTest*'` 全绿。

---

### Task 4: 桥契约测试改造 + 语义收尾

**Files:**
- Modify: `android/app/src/test/java/com/clawbench/app/MainActivityTunnelBridgeTest.java`
  - `:156-162` `setTunnelTransport_methodExists` → `setTunnelTransportH2Enabled(boolean)`
  - `:164-175` `setTunnelTransport_appliesThePreference` → 改写为写偏好断言
  - `:177-188` `setTunnelTransport_unknownValueKeepsTheDefault` → **删除**（布尔无 unknown）
  - `:190-200` `setTunnelTransport_acceptsNullWithoutThrowing` → **删除**（布尔无 null）
  - `:202-210` `getTunnelTransport_reportsThePreference` → 改写（不再设 `"both"`；断言 `"ssh"` 默认 / `"h2"` 开启后）
  - `:338-349` `invoke()` 助手 → `String.class` 分支改 `boolean.class`
- Modify: `android/app/src/main/java/com/clawbench/app/MainActivity.java:2705-2716`（注释）

**依赖：** T1（源码签名已在 T1 换成 `setTunnelTransportH2Enabled(boolean)`）。**可并行：** 与 T2/T3 并行（不同文件）。

**Step 1: 改写桥测试**

```java
// 形态片段（非完整实现）
@Test
public void setTunnelTransportH2Enabled_methodExists() throws Exception {
    Method method = webAppInterface.getClass().getDeclaredMethod(
            "setTunnelTransportH2Enabled", boolean.class);
    assertNotNull(method);
    assertNotNull(method.getAnnotation(android.webkit.JavascriptInterface.class));
}

@Test
public void setTunnelTransportH2Enabled_writesThePreference() throws Exception {
    invoke("setTunnelTransportH2Enabled", true);
    assertTrue(BackgroundService.isTunnelTransportH2Enabled(activity));
    invoke("setTunnelTransportH2Enabled", false);
    assertFalse(BackgroundService.isTunnelTransportH2Enabled(activity));
}

@Test
public void getTunnelTransport_derivesFromTheLocalToggle() throws Exception {
    invoke("setTunnelTransportH2Enabled", false);
    assertEquals("ssh", invoke("getTunnelTransport"));
    invoke("setTunnelTransportH2Enabled", true);
    assertEquals("h2", invoke("getTunnelTransport"));
}
```

> **测试需 Robolectric Context**：`MainActivityTunnelBridgeTest` 当前用 `Unsafe.allocateInstance`（无 Robolectric），而 `isTunnelTransportH2Enabled(activity)` 需要 `activity.getSharedPreferences(...)`。**若 `activity` 不是真 Context**，此测试会 NPE。两种解法，选其一：
> 1. 给该类加 `@RunWith(RobolectricTestRunner.class) @Config(sdk = 28)` 并用 `Robolectric.buildActivity(MainActivity.class).get()`（参考 `MainActivityOverlayPermissionTest:44-60` 的真 Activity 先例）；
> 2. 若保持无 Robolectric，把「写偏好」的断言改为对 `mockPrefs`/Robolectric 应用 Context 的断言（参考 `MainActivityOverlayPermissionTest:108-120` 用 `RuntimeEnvironment.getApplication()`）。
>
> **推荐 (1)**，因为它同时覆盖了 `@JavascriptInterface` 注解 + 真持久化，正是 T6 要复用的先例。

**Step 2: `invoke()` 助手**

```java
// :338-346
method.equals("setTunnelTransportH2Enabled") ? new Class<?>[]{boolean.class}
        : method.equals("removeForwardedPort") ...
```

**Step 3: 注释收尾**

`MainActivity.java:2705-2716`：删除「server's `port_forward.transport` / syncTunnelTransportToNative」措辞，改为「本地 SharedPreferences 开关；切换在下次重连生效（与桌面一致）」。

**Step 4: 验证**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*TunnelBridge*'
```

预期：全绿（4 个改写/新增用例 + 既有 `reconnectTunnel` / `getActiveTunnelTransport_methodExists` 等不受影响）。

**Step 5: Commit**

```bash
git add android/app/src/test/java/com/clawbench/app/MainActivityTunnelBridgeTest.java \
        android/app/src/main/java/com/clawbench/app/MainActivity.java
git commit -m "test(android): pin the boolean tunnel-toggle bridge contract

The bridge now takes a boolean (setTunnelTransportH2Enabled) and derives
getTunnelTransport from the local preference; the string wire-value and null
cases no longer exist."
```

**完成标准：** `--tests '*TunnelBridge*'` 全绿；`MainActivity` 注释不再提「服务端下发」。

---

### Task 5: 改造 `BackgroundServiceTransportTest.java` 的测试助手

**Files:**
- Modify: `android/app/src/test/java/com/clawbench/app/BackgroundServiceTransportTest.java`
  - `:152`（`setUp`）、`:212`（`tearDown`）— ❌ 删静态复位
  - 20 处 `setField("transportPreference", ...)` — 全部替换为 `mockPrefs` stub
  - `:802-812` `setField` 助手 — 静态分支（`:807-808`）可保留但不再被 `transportPreference` 使用

**依赖：** T1（字段已删）。**可并行：** 与 T3/T4 并行（不同文件）。**⚠️ 必须在 T6 之前完成**（T6 复用本任务的 stub 模式）。

**硬坑（R8）：** `setField` 对静态字段走 `field.set(null, ...)`（`:807-808`）。删字段后**所有** `setField("transportPreference", ...)` 抛 `NoSuchFieldException`，必须全部替换。逐条清单见 §0.4。

**Step 1: 建立 stub 模式**

`:200` 已有 `doReturn(mockPrefs).when(service).getSharedPreferences(anyString(), anyInt());`。在 `setUp` 中补一条默认 stub（默认 mock 的 `getBoolean` 返回 false = SSH，**天然满足所有 SSH 用例**）：

```java
// setUp 内，mockPrefs 已 doReturn 到 service 之后
when(mockPrefs.getBoolean(eq("tunnel_transport_h2_enabled"), anyBoolean())).thenReturn(false);
```

新增一个助手替代 `setField("transportPreference", H2)`：

```java
/** 让 isTunnelTransportH2Enabled(...) 读到 true，等价于旧的 setField("transportPreference", H2)。 */
private void enableH2Preference() {
    when(mockPrefs.getBoolean(eq("tunnel_transport_h2_enabled"), anyBoolean())).thenReturn(true);
}
```

**Step 2: 逐条替换**

- 20 处 `setField("transportPreference", PortForwardTransportKind.H2)` → `enableH2Preference()`。
- 3 处 `setField("transportPreference", PortForwardTransportKind.SSH)`（`:152`/`:212`/`:279`/`:441` 中是 SSH）→ 删除；SSH 是默认值，`mockPrefs` 默认返回 false 即满足。
  - `:152` 是 `setUp` 的复位 → 删。
  - `:212` 是 `tearDown` 的复位 → 删（prefs 由 Mockito 每例重建）。
  - `:279`（`addPortForward_underSsh_stillCallsSetPortForwardingL`）、`:441`（`addReversePortForward_underSsh_stillCallsSetPortForwardingR`）→ 直接删该行，依赖默认 false。
- 1 处 `setField("transportPreference", PortForwardTransportKind.BOTH)`（`:500`，在 `:499` 的 both 用例内）→ 随 T2 删除该用例。

**Step 3: 改写两个价值高的用例**

| 用例 | 处置 |
|---|---|
| `defaultPreferenceIsSsh`（`:223`） | 断言 `assertFalse(BackgroundService.isTunnelTransportH2Enabled(service))`（或经 `mockPrefs` 读 false） |
| `setTransportPreference_parsesTheWireValue`（`:230`） | **删除**（`setTransportPreference` 不再存在） |
| `setTransportPreference_unknownValueFallsBackToSsh`（`:238`） | **删除**（语义消失） |
| `setTransportPreference_whileConnected_doesNotTearDownTheLiveTransport`（`:551`） | **改写（价值最高）**：先建立 live h2 transport，再 `setTunnelTransportH2Enabled(service, false)` 只写偏好，断言 `activeTransport` / live transport **不变**（未即时切换）；随后调用 `forceReconnectAsync` 才换到 SSH |

**Step 4: 验证**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*BackgroundServiceTransportTest*'
```

预期：全绿，且**无** `NoSuchFieldException`。若出现 `NoSuchFieldException: transportPreference`，说明还有 `setField` 未替换——回到 Step 2 用 `grep -n 'setField("transportPreference"'` 逐条核对。

```bash
grep -n 'setField("transportPreference"' android/app/src/test/java/com/clawbench/app/BackgroundServiceTransportTest.java || echo "ALL REPLACED"
```

预期：`ALL REPLACED`。

**Step 5: Commit**

```bash
git add android/app/src/test/java/com/clawbench/app/BackgroundServiceTransportTest.java
git commit -m "test(android): drive transport selection through mockPrefs instead of a static field

setField() reaches static fields via field.set(null, ...), so every
transportPreference call site would throw NoSuchFieldException once the field
is gone. The mockPrefs stub (default false) also covers all SSH cases."
```

**完成标准：** `--tests '*BackgroundServiceTransportTest*'` 全绿；无 `setField("transportPreference"` 残留。

---

### Task 6: 新增 Android 测试（默认关 / setter 持久化 / 桥写入偏好 / 切换不碰 live transport）

**Files:**
- Create: `android/app/src/test/java/com/clawbench/app/BackgroundServiceTunnelTransportPrefsTest.java`（Robolectric 真偏好模式）
- （桥 + 持久化 + `@JavascriptInterface` 注解断言已在 T4 落到 `MainActivityTunnelBridgeTest`；本任务补 **Robolectric 真偏好** 侧，与 T4 的 mock 侧互补。）

**依赖：** T1（新方法）、T3（展示映射）、T5（stub 模式）。**可并行：** 与 T2/T3 并行（新文件）。

**照抄先例：**
- `BackgroundServiceFloatingTest.java:60`（`Robolectric.buildService`）、`:64`（清偏好）、`:97-101`（默认值）、`:103-112`（持久化）
- `MainActivityOverlayPermissionTest.java:108-127`（桥 + 持久化 + `@JavascriptInterface` 注解）

**必须覆盖：**

| # | 用例 | 断言 |
|---|---|---|
| 1 | 默认 false = SSH | `isTunnelTransportH2Enabled(appContext)` 为 false；`getTunnelTransport()`（桥）回 `"ssh"` |
| 2 | setter 持久化 | `setTunnelTransportH2Enabled(ctx, true)` 后新读为 true；再 false 后为 false |
| 3 | 桥写入偏好 | `bridge.setTunnelTransportH2Enabled(true)` 后 `isTunnelTransportH2Enabled(ctx)` 为 true（并断言 `@JavascriptInterface` 注解存在） |
| 4 | 冷启动重读 | 用**同一** SharedPreferences 名重建 service（`Robolectric.buildService`）后仍读到 true（证明不是内存态） |
| 5 | 切换**不**触碰 live transport | 建立 live h2 transport → `setTunnelTransportH2Enabled(ctx, false)` → 断言 live transport 引用/`activeTransport` **不变**；仅 `forceReconnectAsync` 后才换（这是设计文档 §7.4 第 1 条的集成等价物） |

**Step 1: 写测试**

```java
// 形态片段（非完整实现）
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class BackgroundServiceTunnelTransportPrefsTest {
    private Application appContext;
    @Before public void setUp() {
        appContext = RuntimeEnvironment.getApplication();
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().clear().commit();   // 照 BackgroundServiceFloatingTest:64
    }
    @Test public void defaultsToSsh() { assertFalse(BackgroundService.isTunnelTransportH2Enabled(appContext)); }
    // ... setter 持久化 / 桥 / 冷启动重读 / 不碰 live transport
}
```

**Step 2: 跑测试确认先失败后通过**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*TunnelTransportPrefs*'
```

预期：先编译失败（类不存在）→ 实现后全绿。

**Step 3: 全量 Android 回归（阶段 1 收尾必做）**

**先查并发**（§0.2），无冲突后：

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest
```

预期：**0 failures / 0 errors**。总数 **≥ 1032**（新增用例会让数字上升；**不要**把新增算作回归）。若出现失败，先隔离单跑定位归属（是否自己改过的包），再决定。

**Step 4: Commit**

```bash
git add android/app/src/test/java/com/clawbench/app/BackgroundServiceTunnelTransportPrefsTest.java
git commit -m "test(android): cover the SharedPreferences h2 toggle end to end

Robolectric real-preference mode (per BackgroundServiceFloatingTest) proves the
default is SSH, the setter persists, the bridge writes the preference, a
recreated service still reads it, and flipping the toggle leaves the live
transport untouched until a reconnect."
```

**完成标准：** 5 类用例全绿；全量 Android 0 failures 且总数 ≥ 1032。

---

## 阶段 2 — 前端

### Task 7: 移除向原生推送 `port_forward.transport` 的链路

**Files:**
- Modify: `web/src/composables/useSettingsConfig.ts:813-820` — ❌ 删 `syncTunnelTransportToNative`
- Modify: `web/src/composables/useSettingsConfig.ts:852-853` — ❌ 删调用点（含 `:852` 注释）
- Modify: `web/src/utils/clawbenchNative.ts:84` — ❌ 删 `setTunnelTransport?(pref: string)` 声明
- Modify: `web/src/composables/__tests__/useSettingsConfig.test.ts:1409-1500` — ❌ 删整块 `describe('useSettingsConfig: tunnel transport sync')`
- Modify: `web/src/utils/__tests__/clawbenchNative.test.ts:88-120` — ❌ 删 4 条 `setTunnelTransport` 用例，**保留** `:122-145`

**依赖：** T1（Android 不再消费推送值）。**可并行：** 与 T8/T9 部分重叠（同文件 `clawbenchNative.ts`），**建议 T7 → T8 → T9 串行**；与 T10 可并行。

**理由（写进 commit）：** 两个原生客户端都不再消费服务端的 `port_forward.transport`（Electron clamp 到 `ssh`，Android 用本地开关）。保留推送会把「服务端配置值」伪装成原生实际状态，反而更不诚实。展示链路读的是两个 **getter**（`usePortForward.ts:558-559`），不依赖推送。

**Step 1: 删源码**

```bash
grep -rn "syncTunnelTransportToNative" web/src/   # 确认只有 :813 定义 + :853 调用
grep -rn "setTunnelTransport" web/src/            # 确认只有 clawbenchNative.ts:84 + 测试
```

删 `useSettingsConfig.ts:805-820` 的函数（含其上方 javadoc）与 `:853` 的调用 + `:852` 注释。

**Step 2: 删测试**

- `useSettingsConfig.test.ts`：删 `:1409-1500` 整块（文件到 `:1500` 结束，即删到文件末尾）。其中 `:1428-1437` 会硬失败（`setTunnelTransport` 不再被调用），其余变空断言。
- `clawbenchNative.test.ts`：删 `:88-95`、`:97-104`、`:106-108`、`:110-120`（4 条）。**保留** `:122-133`（`getTunnelTransport`）与 `:135-145`（`getActiveTunnelTransport`）。

**Step 3: 验证**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
# 先查并发（§0.2）
npx vitest run web/src/utils/__tests__/clawbenchNative.test.ts \
               web/src/composables/__tests__/useSettingsConfig.test.ts
```

预期：两文件全绿；`clawbenchNative.test.ts` 用例数比改前少 4，`useSettingsConfig.test.ts` 少 6。

**Step 4: Commit**

```bash
git add web/src/composables/useSettingsConfig.ts \
        web/src/utils/clawbenchNative.ts \
        web/src/composables/__tests__/useSettingsConfig.test.ts \
        web/src/utils/__tests__/clawbenchNative.test.ts
git commit -m "refactor(web): stop pushing port_forward.transport to the native tunnel

Neither native client consumes the server value any more (Electron clamps to
ssh, Android has a local toggle), and the display path reads the two getters,
so the push only misrepresented server config as native state."
```

**完成标准：** `syncTunnelTransportToNative` 与 `setTunnelTransport` 在 `web/src` 全部消失；两测试文件全绿。

---

### Task 8: 新增桥契约 `getTunnelTransportH2Enabled?` / `setTunnelTransportH2Enabled?`

**Files:**
- Modify: `web/src/utils/clawbenchNative.ts`（`ClawBenchNative` 接口，`:84-92` 附近）
- Modify: `web/src/utils/__tests__/clawbenchNative.test.ts`（照搬 `:110-133` 的 sync/async/缺失降级三形态）

**依赖：** T1（Android 桥方法名冻结：`setTunnelTransportH2Enabled(boolean)` / `getTunnelTransport()`）。**可并行：** 与 T10 并行；与 T7 同文件，串行。

**方法契约（设计文档 §4.E）：**

| 方法 | 处置 | 说明 |
|---|---|---|
| `setTunnelTransport?(pref)` | 已在 T7 删除 | — |
| `getTunnelTransport?()` | **保留** | 展示回退（`usePortForward.ts:559`） |
| `getActiveTunnelTransport?()` | **保留** | 展示首选（`usePortForward.ts:558`） |
| `getTunnelTransportH2Enabled?()` | ➕ **新增（可选 `?`）** | Android 本地开关初值 |
| `setTunnelTransportH2Enabled?(enabled)` | ➕ **新增（可选 `?`）** | 写 SharedPreferences |

**Step 1: 加接口声明**

```ts
// clawbenchNative.ts，紧邻 getTunnelTransport 声明
/** Android 本地 h2 开关的初值。旧宿主缺失该方法 => 设置页隐藏开关。 */
getTunnelTransportH2Enabled?(): Promise<boolean> | boolean
/** 写 Android 本地 h2 开关（SharedPreferences）。 */
setTunnelTransportH2Enabled?(enabled: boolean): Promise<void> | void
```

**Step 2: 新增测试（三形态）**

照搬 `:110-133` 形态：

```ts
// 形态片段（非完整实现）
it('setTunnelTransportH2Enabled forwards the boolean on an Android sync host', () => {
  const setTunnelTransportH2Enabled = vi.fn()
  setNative({ setTunnelTransportH2Enabled })
  const result = getNative()?.setTunnelTransportH2Enabled?.(true)
  expect(setTunnelTransportH2Enabled).toHaveBeenCalledWith(true)
  expect(result).toBeUndefined()          // Android 同步 void
})
it('setTunnelTransportH2Enabled degrades to a no-op on a legacy host', () => {
  setNative({ isNativeApp: () => true })
  expect(() => getNative()?.setTunnelTransportH2Enabled?.(true)).not.toThrow()
})
it('setTunnelTransportH2Enabled degrades to a no-op with no bridge (web mode)', () => {
  expect(() => getNative()?.setTunnelTransportH2Enabled?.(true)).not.toThrow()
})
it('getTunnelTransportH2Enabled resolves an Electron Promise and an Android sync value alike', async () => {
  setNative({ getTunnelTransportH2Enabled: () => Promise.resolve(true) })
  expect(await getNative()?.getTunnelTransportH2Enabled?.()).toBe(true)
  setNative({ getTunnelTransportH2Enabled: () => false })
  expect(await getNative()?.getTunnelTransportH2Enabled?.()).toBe(false)
})
it('getTunnelTransportH2Enabled resolves undefined on a legacy host', async () => {
  setNative({ isNativeApp: () => true })
  expect(await getNative()?.getTunnelTransportH2Enabled?.()).toBeUndefined()
})
```

**Step 3: 验证**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
npx vitest run web/src/utils/__tests__/clawbenchNative.test.ts
```

预期：全绿。

**Step 4: Commit**

```bash
git add web/src/utils/clawbenchNative.ts web/src/utils/__tests__/clawbenchNative.test.ts
git commit -m "feat(web): add optional bridge methods for the Android h2 toggle

Optional (?) so an older host hides the settings row instead of pretending the
toggle works; the caller must never fall back to the old non-persisting
setTunnelTransport."
```

**完成标准：** 三形态（sync/async/缺失降级）均有断言且全绿。

---

### Task 9: 设置页「端口映射」专用开关行

**Files:**
- Modify: `web/src/components/settings/SettingsGroupPanel.vue`
  - 模板：`panelId === 'portForward'` 时渲染专用 switch 行（**照抄 FRP `auto_port` 注入先例 `:70-87`**）
  - 脚本：`import { useAppMode }` + `isAndroidApp`（照 `SettingsCategory.vue:205-210`）；`onMounted` 异步取初值（照 `SettingsCategory.vue:145-153`）；`v-if` 隐藏；`@change` 调 setter + 提示 + 「立即重连」按钮
- Modify: `web/src/i18n/locales/zh.ts` + `web/src/i18n/locales/en.ts`（新增键）
- Test: `web/src/components/settings/__tests__/SettingsGroupPanel.tunnelToggle.test.ts`（新建）

**依赖：** T8（桥契约）。**可并行：** 与 T10 并行；与 T7/T8 同文件串行。**T9 完成必须 `npm run build`。**

**硬约束（选专用行的理由）：**
- `web/src/components/settings/__tests__/settingsFieldMap.test.ts:422`（`portForward` 的 `commonFields.length === 1`）**必须不破**；
- `settingsFieldMap.test.ts:289`（`isPanelOnlyCategory('portForward') === true`）**必须不破**。

> 专用行方案一次性规避三处破坏：不碰 `commonFields.length`、不碰 `isPanelOnlyCategory`、不需要给 `usePanelSnapshot` 加 `source: 'native'` 第三种来源。**不要**做成 `commonFields` 条目，**不要**复用 `source: 'local'`（会写进 localStorage）或 `source: 'server'`（`validatePatchFields` 对未知 key 拒绝 → 整个保存 400，`internal/handler/settings.go:959-961`）。

**平台门控（R2）：** 必须用 `isAppMode && !isDesktopApp`（Electron 也报 `isAppMode === true`）。**不能用 `appOnly`**（其语义是「Android + Electron」，`SettingsCategory.vue:219` 判 `!isAppMode`）。`SettingsGroupPanel.vue:412-439` 的 `renderList` **目前完全没有平台过滤**，专用行在组件内自行门控。

**UX（设计文档 §4.C / §8）：**
- 开关行下方显示「下次重连生效」提示 + 「立即重连」按钮；
- 成功后复用现有 toast `portForward.tunnelReconnected`（zh `web/src/i18n/locales/zh.ts:1613` = `'SSH 隧道已重连'`；en `:1610`）；
- **不要复用** `settings.panel.needsRestartHint`（那是「服务器重启」语义）；
- 重连走 `clawbenchNative.ts:227-263` 的 `reconnectTunnel()`（16s 超时）。

**Step 1: 写失败测试**

新建 `SettingsGroupPanel.tunnelToggle.test.ts`（`mount()` 组件，mock `getNative`）：

| 用例 | 断言 |
|---|---|
| 初值未到前隐藏 | `getTunnelTransportH2Enabled` 返回 pending Promise 时开关 `v-if` 不渲染 |
| onMounted 后按 getter 值渲染 | getter 回 true → 开关 checked；回 false → unchecked |
| 缺失 getter（旧宿主）隐藏 | `getNative()` 无该方法 → 开关不渲染 |
| `@change` 调 setter | 切换后 `setTunnelTransportH2Enabled` 被调用且参数正确 |
| 「立即重连」调 reconnect | 点击后 `reconnectTunnel` 被调用；成功 → toast `portForward.tunnelReconnected` |
| 平台门控 | `isDesktopApp === true` 时不渲染（即使 `isAppMode === true`） |

> **Vue 函数 `:ref` 陷阱**：若用函数 ref，unmount 时会用 null 调**旧闭包**（见既有教训）。本行不涉及函数 ref，但组件内若复用既有模式请注意。

**Step 2: 跑测试确认失败**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
npx vitest run web/src/components/settings/__tests__/SettingsGroupPanel.tunnelToggle.test.ts
```

预期：FAIL（专用行未实现）。

**Step 3: 实现**

模板（照 FRP 先例 `:70-87`，插在 `renderList` 循环内 `panelId === 'portForward'` 的分支）：

```vue
<!-- 形态片段（非完整实现） -->
<template v-if="isAndroidApp && panelId === 'portForward' && h2ToggleLoaded">
  <SettingsItem
    :label="t('settings.items.tunnelTransportH2')"
    :description="t('settings.items.tunnelTransportH2Desc')"
    type="boolean"
    :model-value="h2Enabled"
    @update:model-value="onH2Toggle"
  />
  <div class="group-panel__hint">
    {{ t('settings.items.tunnelTransportH2Hint') }}
    <button type="button" @click="onReconnectTunnel">{{ t('settings.items.tunnelTransportReconnect') }}</button>
  </div>
</template>
```

脚本要点：

```ts
import { useAppMode } from '@/composables/useAppMode'
const { isAppMode, isDesktopApp } = useAppMode()
const isAndroidApp = computed(() => isAppMode.value && !isDesktopApp.value)
const h2Enabled = ref(false)
const h2ToggleLoaded = ref(false)   // 初值未到前 v-if 隐藏，避免闪「假关」(R3)

onMounted(async () => {
  if (!isAndroidApp.value || props.config.panelId !== 'portForward') return
  const native = getNative()
  if (!native?.getTunnelTransportH2Enabled) return   // 旧宿主：保持隐藏 (R6)
  try { h2Enabled.value = !!(await native.getTunnelTransportH2Enabled()) } catch { return }
  h2ToggleLoaded.value = true
})

async function onH2Toggle(v: unknown) {
  h2Enabled.value = !!v
  getNative()?.setTunnelTransportH2Enabled?.(h2Enabled.value)
  // 显示「下次重连生效」提示；不即时切换
}
async function onReconnectTunnel() {
  const ok = await reconnectTunnel()
  if (ok) toast.success(t('portForward.tunnelReconnected'))
}
```

i18n 新增键（zh + en **必须对齐**）：`settings.items.tunnelTransportH2`、`tunnelTransportH2Desc`、`tunnelTransportH2Hint`、`tunnelTransportReconnect`。

**Step 4: 跑测试 + 编译**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
# 先查并发（§0.2），且绝不同时跑 build 与全量 vitest
npx vitest run web/src/components/settings/__tests__/SettingsGroupPanel.tunnelToggle.test.ts \
               web/src/components/settings/__tests__/settingsFieldMap.test.ts \
               web/src/composables/__tests__/usePortForward.test.ts
```

预期：全绿；`settingsFieldMap.test.ts:422`（`commonFields.length === 1`）与 `:289`（`isPanelOnlyCategory`）**未破**。

```bash
# AGENTS.md：纯前端改动完成后必须自觉编译
npm run build
```

预期：构建成功，产物写入 `.clawbench-web/`（disk 模式立即生效，无需重启）。

**Step 5: Commit**

```bash
git add web/src/components/settings/SettingsGroupPanel.vue \
        web/src/components/settings/__tests__/SettingsGroupPanel.tunnelToggle.test.ts \
        web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(web): Android-only h2 tunnel toggle in the port-forward panel

A dedicated row (not a commonFields entry) keeps commonFields.length === 1 and
isPanelOnlyCategory('portForward') intact. Platform gate is isAppMode &&
!isDesktopApp because Electron also reports isAppMode. The row is hidden until
the async initial value arrives and on hosts without the new bridge method."
```

**完成标准：** 6 类用例全绿；`settingsFieldMap.test.ts` 两条硬断言不破；`npm run build` 成功；zh/en 键对齐。

---

## 阶段 3 — Electron

### Task 10: `bridge.ts` 传输 clamp（只认 `ssh`）

**Files:**
- Modify: `desktop/src/main/bridge.ts:94-96` — ✏️ 只对 `'ssh'` 调 `setTransportPreference`
- Modify: `desktop/src/main/bridge.test.ts:100-115` — ✏️ `it.each(['ssh','h2','both'])` 改为只对 `'ssh'` 断言调用
- **🚫 不动** `desktop/src/main/tunnel.ts`

**依赖：** 无（独立）。**可并行：** 与阶段 2 并行（不同目录）。

**为什么 clamp 在 bridge 而不是 `tunnel.ts`：** `desktop/src/main/tunnel.h2.test.ts` **直接 import 并调用 `setTransportPreference`** 驱动 h2 测试（`:287`、`:359`、`:403`、`:626`、`:633`、`:642`）。改 `tunnel.ts` 会让整个桌面 h2 套件失败。clamp 放在 IPC 边界既完成产品约束，又不动测试驱动的代码路径；还能挡住「旧缓存前端仍推 `both`」（R7）。

**Step 1: 改 handler**

```ts
// bridge.ts:94-96
// 只认 'ssh'：Electron 的 h2 通道尚未在生产验证，本次产品层关闭。
// h2/both 一律忽略——即使旧缓存前端仍推 'both'，主进程也只认 ssh。
ipcMain.handle('native:set-tunnel-transport', (_e, pref: unknown) => {
  if (pref === 'ssh') setTransportPreference('ssh')
})
```

**Step 2: 改测试**

```ts
// bridge.test.ts:100-103
it('forwards ssh to setTransportPreference', () => {
  invoke('native:set-tunnel-transport', 'ssh')
  expect(tunnelMock.setTransportPreference).toHaveBeenCalledWith('ssh')
})

it.each(['h2', 'both'])('clamps %s to no-op (Electron stays on ssh)', (pref) => {
  invoke('native:set-tunnel-transport', pref)
  expect(tunnelMock.setTransportPreference).not.toHaveBeenCalled()
})
```

（`:105-115` 的「ignores unknown values」用例保留。）

**Step 3: 验证（必须证明 h2 套件未受影响）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/desktop
npx vitest run src/main/bridge.test.ts src/main/tunnel.h2.test.ts src/main/tunnel.test.ts
npx tsc --noEmit -p tsconfig.json
```

预期：三文件全绿；**`tunnel.h2.test.ts` 全绿且未改一行**（这是本任务的核心证明）；类型检查无错。

```bash
git diff --stat desktop/src/main/tunnel.ts desktop/src/main/tunnel.h2.test.ts
```

预期：**无输出**（两文件未被改动）。

**Step 4: Commit**

```bash
git add desktop/src/main/bridge.ts desktop/src/main/bridge.test.ts
git commit -m "fix(desktop): clamp the tunnel transport IPC to ssh

Electron's h2 path is not production-validated, so the bridge accepts only
'ssh' and ignores h2/both. Clamping at the IPC boundary leaves tunnel.ts and
its h2 test suite untouched, and blocks a stale renderer pushing 'both'."
```

**完成标准：** `bridge.test.ts` + `tunnel.h2.test.ts` + `tunnel.test.ts` 全绿；`tunnel.ts`/`tunnel.h2.test.ts` 的 `git diff` 为空；`tsc --noEmit` 通过。

---

## 阶段 4 — 文档

### Task 11: 同步文档（无 drift 测试守护，需人工改）

**Files:**
- Modify: `internal/api/openapi.yaml:386-390`（`/api/config` 的 `port_forward.transport` 描述）
- Modify: `docs/spec/infra/ssh-tunnel.md:101-140`（「传输方式」与「传输优先级链」章节）
- Modify: `docs/spec/README.md:47`（索引摘要）
- Modify: `docs/spec/infra/proxy.md:44`（transport 描述）
- Modify: `docs/plans/2026-09-28-android-h2-toggle-design.md` — ➕ 新增 **§12 措辞通用化与传输标注**（T13–T16 的设计来源；已随本计划一并落地）

**依赖：** T1/T7/T10 的最终行为已定。**可并行：** 与阶段 2/3 并行收尾。

**必须写清的两点：**
1. **服务端 `port_forward.transport` 仍保留三值、默认 `both`**，但**两个原生客户端都不再消费它**——Electron 在 bridge 层 clamp 到 `ssh`；Android 用本地 SharedPreferences 开关（`tunnel_transport_h2_enabled`，默认关）。
2. 它只剩两个用途：**registry 门控**（`cmd/server/proxy_registry_gate.go:43-47`）与 **web 端健康检查门控**（`web/src/composables/usePortForward.ts:515-523` 的 `tunnelTransportAllowsH2()`，读服务端配置，**仍要认 `'both'`**）。**标注为后续可清理项**（设计文档 §10），本次不动——动它会触碰 §2.2 的默认值陷阱（`cmd/server/proxy_registry_gate_test.go:94-111` 的回归守卫会拒绝改默认值）。

**Step 1: 改 OpenAPI 描述**

`internal/api/openapi.yaml:386-390` 现文「客户端（Electron / Android）读该值后下发到原生隧道层」**已失真**。改为：

```yaml
        `port_forward` 段包含 `enabled`、`port`、`transport`（`host_key` 与
        `allowed_ports` 不下发）。`transport` 是隧道传输方式：`ssh`（仅 SSH 通道）、
        `h2`（仅 HTTP/2 流隧道，走主端口）、`both`（默认）。**该值仅用于服务端
        registry 门控与 web 端健康检查门控；两个原生客户端（Electron / Android）
        都不再消费它**——Electron 在 IPC 边界固定为 `ssh`，Android 使用本地
        SharedPreferences 开关（默认关 = SSH）。
```

**Step 2: 改 `docs/spec/infra/ssh-tunnel.md:101-140`**

- `:103` 的「它随 `/api/config` 下发给客户端，由客户端决定探测哪条线」需补一句：**原生客户端已不再消费**（Electron clamp / Android 本地开关）。
- `:105-109` 的「客户端行为」表：加注「以下为**服务端配置语义**；原生客户端已改为固定 `ssh`（Electron）/ 本地开关（Android）」。
- `:138-140` 的「传输优先级链」：标注「**桌面与 Android 当前不按此链探测**；桌面固定 SSH，Android 由本地开关二选一」。

**Step 3: 改 `docs/spec/README.md:47` 与 `docs/spec/infra/proxy.md:44`**

- `README.md:47`：把「`port_forward.transport` ... **客户端传输提示**」补注「**原生客户端已不消费（Electron clamp ssh / Android 本地开关）**」。
- `proxy.md:44`：`transport` 的描述加同款补注。

**Step 4: 验证（无 drift 测试，人工核对）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
# 确认服务端 transport 默认值未被改动（本任务只改文档）
git diff --stat internal/model/ cmd/server/ internal/handler/
grep -n "客户端（Electron / Android）读该值后下发" internal/api/openapi.yaml || echo "STALE CLAIM REMOVED"
```

预期：`internal/model/`、`cmd/server/`、`internal/handler/` **无改动**；`STALE CLAIM REMOVED`。

```bash
# 若改了 openapi.yaml，跑漂移测试确认没写坏 YAML / 路由
/usr/local/go/bin/go test ./internal/handler/ -run TestOpenAPISpecMatchesRegisteredRoutes -v
```

预期：PASS（本任务不改路由，只改描述文字）。

**Step 5: Commit**

```bash
git add internal/api/openapi.yaml docs/spec/infra/ssh-tunnel.md docs/spec/README.md docs/spec/infra/proxy.md
git commit -m "docs(tunnel): note that native clients no longer consume port_forward.transport

Electron clamps to ssh at the IPC boundary and Android uses a local
SharedPreferences toggle, so the server value now only drives the registry gate
and the web health gate. Kept at three values / default 'both' deliberately:
changing the default would disable the registry for installs that never set it."
```

**完成标准：** 四处文档均写明「两个原生客户端不再消费该值」+ 后续可清理项；`internal/model`/`cmd/server`/`internal/handler` 无源码改动。

---

## 阶段 5 — 集成验证

### Task 12: 端到端（真实服务端）

**依赖：** T1-T11 全部。**可并行：** 否（收尾）。

**前置：** 一个隔离实例（端口 20100，**不碰 20000 主服务器**）+ 一个 h2 可用的真实服务端。Android 侧用真机/模拟器（`android-e2e/` 环境由**另一 agent** 负责，**不要动它**；若它在跑 gradle/emulator，按 §0.2 等待）。

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
./build.sh --restart --restart-port=20100
# 登录拿 cookie（见既有隔离实例配方）
```

**A. 默认（开关关）→ Android 走 SSH**

1. 确保 Android 本地开关为关（设置 → 端口映射，开关显示关）。
2. 让 Android 建立隧道。
3. 在服务端侧观察 SSH 连接数：

```bash
# 服务端 SSH 连接统计（clientCount > 0 即证明走 SSH）
curl -s -b "<cookie>" http://127.0.0.1:20100/api/ssh/info | python3 -c "import sys,json;d=json.load(sys.stdin);print('clientCount=',d.get('clientCount'))"
```

预期：`clientCount >= 1`；服务端日志中**没有** `POST /api/tunnel/stream`。

**B. 开关开 + 立即重连 → Android 走 h2**

1. 设置 → 端口映射 → 打开 h2 开关 → 点「立即重连」。
2. 观察：

```bash
# h2 数据面出现
grep -c "POST /api/tunnel/stream" <服务端日志>          # 预期 >= 1
# SSH 统计归零（h2 下 sshActive 恒 false）
curl -s -b "<cookie>" http://127.0.0.1:20100/api/ssh/info | python3 -c "import sys,json;d=json.load(sys.stdin);print('clientCount=',d.get('clientCount'))"
```

预期：`POST /api/tunnel/stream` 计数 ≥ 1；`clientCount = 0`。

**C. 确认「只需映射 20000 一个端口」**

```bash
# 客户端侧只连主端口（在 Android 设备 shell 内，或经 adb）
adb shell "cat /proc/net/tcp /proc/net/tcp6" | grep -i "<主端口 hex>" || true
# 或在服务端侧看 h2 连接的目的端口
ss -tnp | grep 20100
```

预期：Android 只与主端口（20100/20000）建立连接，**不连 `mainPort+1`（SSH 端口）**。这是「只放行一个端口」的核心证明。

**D. 服务端 `transport: ssh` 后，Android 本地开关**仍能开 h2**（核心目标）**

1. 把隔离实例配置改为 `port_forward: { enabled: true, transport: ssh }`（**不改默认值，只改本实例配置**）。
2. 重启隔离实例。
3. Android 本地开关打开 + 立即重连。
4. 验证：

```bash
grep -c "POST /api/tunnel/stream" <服务端日志>   # 预期 >= 1
```

预期：**h2 仍可用**——证明本地开关权威，**不再被服务端推送覆盖**。这是本次改动的核心目标。

> **对照（503 控制组）**：若实例配 `enabled: false && transport: ssh`（唯一 registry 为 nil 的组合，见 `android-e2e/server-config/config.no-h2.yaml:44-45`），h2 端点必返回 `503`——这是**预期**，不属本任务回归。

**E. 前端面板「当前传输」显示正确（T3 修复生效）**

1. Android 上打开 h2 且已连接。
2. 打开端口映射面板，观察「当前传输」。

预期：显示 **h2**（改动前永远显示偏好值/`ssh`）。若显示 `ssh`，说明 T3 的 `tls|h2c → h2` 映射未生效。

**F. 收尾：全量检查**

```bash
# 先确认没有并发 agent 在跑
ps -eo pid,ppid,etime,cmd | grep -E "gradlew|emulator|appium|vitest|go test|npm run build" | grep -v grep
# 再串行执行（绝不同时跑 build 与全量 vitest）
./scripts/pre-push-checks.sh --skip-android
```

预期：lint + Go test + 前端 test + build + typecheck 全绿（Tier 1-only 失败属 non-blocking，需先确认归属）。

**Step 2: Commit（若有验证脚本/记录补充）**

```bash
git add -A
git commit -m "test(android): end-to-end verification of the local h2 toggle"
```

**完成标准：**
- A：默认关 → SSH（`clientCount > 0`）。
- B：开关开 + 重连 → h2（`POST /api/tunnel/stream` 出现，SSH 归零）。
- C：只映射主端口一个端口。
- D：服务端 `transport: ssh` 时 Android 本地开关**仍能开 h2**。
- E：面板「当前传输」正确显示 h2。

---

## 阶段 6 — 措辞通用化与传输标注（设计文档 §12）

> **设计来源**：`docs/plans/2026-09-28-android-h2-toggle-design.md` **§12 措辞通用化与传输标注**。用户已拍板：措辞改动**并入本次 h2 开关任务**（同一批改动、同一次验收），**不单独出计划**；动态显示采用**追加括号标注**，`activeTransport` 为 `''` 或 `'both'` 时**退回纯通用措辞、不猜传输**。
>
> **行号基准**：本阶段所有 `文件:行号` 在本 worktree HEAD `32086bca` 上实测（源码与设计文档基准 `4c12d3b8` 逐字节相同）。若 T1–T12 已改动源码导致漂移，**以资源名 / i18n key / 符号名重新定位**，不要照搬数字。
>
> **阶段 6 可并行矩阵**：
> - **T13 ∥ T15**：T13 只碰前端 i18n 值，T15 只碰 Android 资源 + `BackgroundService.java`，**不同语言/目录，完全可并行**。
> - **T14 依赖 T13 与 T3**：T13 提供通用基线值，T3 提供 `tls|h2c → h2` 归一化（否则 Android 永远拿不到 `'h2'`，动态标注永不显示）。
> - **T16 与 T14 可并行**：T16 改 `v-if` 触发条件 + 删死 key + i18n 一个硬编码标签，与 T14 的插值改造不碰同一区域。

---

### Task 13: 前端措辞通用化（A 类 2 处 + B 类 9 处的通用基线）

**目标**：把与 SSH 协议无关的 11 处文案去掉「SSH」字样，改成通用「隧道 / 端口映射」措辞。**本任务只改 i18n 值，不加插值**（插值留给 T14），因此**不碰任何 `.vue` / `.ts` 调用点**。

**Files:**
- Modify: `web/src/i18n/locales/zh.ts`（11 个值）
- Modify: `web/src/i18n/locales/en.ts`（11 个值，**必须与 zh 对齐**）

**逐条清单**（zh 行 / en 行，设计文档 §12.3.2）：

| # | key | zh 行 / en 行 | 当前 zh | 改为（zh / en） | 分类 |
|---|---|---|---|---|---|
| 1 | `proxy.tunnelDisconnected` | 1530 / 1527 | `SSH 隧道未连接` | `隧道未连接` / `Tunnel disconnected` | B |
| 2 | `proxy.tunnelConnectedButNoResponse` | 1536 / 1533 | `SSH 隧道已连接，但所有端口的服务均未响应` | `隧道已连接，但所有端口的服务均未响应` / `Tunnel connected, but all port services are unresponsive` | B |
| 3 | `proxy.backgroundTip` | 1537 / 1534 | `…否则 APP 进入后台后 SSH 隧道会被系统终止` | `…否则 APP 进入后台后隧道会被系统终止` / `…otherwise the tunnel will be killed when app goes to background` | B |
| 4 | `proxy.toast.tunnelRecovered` | 1604 / 1601 | `SSH 隧道已恢复` | `隧道已恢复` / `Tunnel recovered` | B |
| 5 | `proxy.toast.tunnelConnectedNoResponse` | 1605 / 1602 | `SSH 隧道已连接，但端口服务未响应` | `隧道已连接，但端口服务未响应` / `Tunnel connected, but port services not responding` | B |
| 6 | `proxy.toast.tunnelStillDisconnected` | 1606 / 1603 | `SSH 隧道仍未连接` | `隧道仍未连接` / `Tunnel still disconnected` | B |
| 7 | `portForward.tunnelDegraded` | 1611 / 1608 | `SSH 隧道已连接，但所有转发端口均无服务响应` | `隧道已连接，但所有转发端口均无服务响应` / `Tunnel connected, but all mapped ports are unresponsive` | B |
| 8 | `portForward.tunnelDisconnected` | 1612 / 1609 | `SSH 隧道未连接，端口映射将无法使用` | `隧道未连接，端口映射将无法使用` / `Tunnel disconnected, port mapping unavailable` | B |
| 9 | `portForward.tunnelReconnected` | 1613 / 1610 | `SSH 隧道已重连` | `隧道已重连` / `Tunnel reconnected` | B |
| 10 | `proxy.appRecommendation` | 1572 / 1569 | `使用 ClawBench APP 可自动建立 SSH 隧道，无需手动配置` | `使用 ClawBench APP 可自动建立隧道，无需手动配置` / `Use the ClawBench app for automatic tunnel setup — no manual configuration needed` | **A** |
| 11 | `chat.localhost.sshDisabled` | 946 / 943 | `SSH 隧道已禁用，无法打开本地地址` | `隧道已禁用，无法打开本地地址` / `Tunnel is disabled, cannot open localhost URL` | **A** |

**不动的 C 类 15 处**（设计文档 §12.3.3）：`proxy.directionForwardHint`/`directionReverseHint`（`ssh -L`/`ssh -R`）、`tunnelGuideStep2/3`、`tunnelNeedSshHint`、`tunnelInstallWin/Mac/Linux`、`tunnelNoCommand`、`tunnelNoSsh`、`tunnelErrorAuth/Network/HostKey`、`settings.items.frpAssignedSSHPort`/`frpSSHRemotePort`。

**依赖：** 无（可与 T15 并行）。**可并行：** 与 T15 并行；与 T14 同文件，**串行**（T14 在 T13 的通用基线上加插值）。

**Step 1: 改 zh.ts**

按上表逐条改值。**不要改 key 名**（改 key 会破 `ProxyPanelContent.transport.test.ts` 等断言，见设计文档 §12.8）。

**Step 2: 改 en.ts**

同样 11 条，行号见上表。**zh/en 必须一一对应**（虽然无 parity 测试守护，但漏改会导致中英不一致）。

**Step 3: 验证（先查并发，§0.2）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
# 确认 11 处已无 "SSH" 字样（这些 key 的值）
grep -nE "(tunnelDisconnected|tunnelConnectedButNoResponse|backgroundTip|tunnelRecovered|tunnelConnectedNoResponse|tunnelStillDisconnected|tunnelDegraded|tunnelReconnected|appRecommendation|sshDisabled):" \
  web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts

npx vitest run web/src/composables/__tests__/usePortForward.test.ts \
               web/src/composables/__tests__/useLocalhostAnnotation.test.ts \
               web/src/components/proxy/__tests__/ProxyPanelContent.transport.test.ts
```

预期：grep 输出中这 11 行的值**不含 `SSH`**；三个测试文件**全绿**（断言的是 key，不是值，见设计文档 §12.8）。

**Step 4: 前端编译（AGENTS.md：纯前端改动必须自觉编译）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
npm run build     # 产物写入 .clawbench-web/，disk 模式立即生效
```

预期：构建成功。

**Step 5: Commit**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "i18n(web): make tunnel wording transport-neutral

The tunnel can be SSH or the HTTP/2 stream tunnel, so 11 user-visible strings
that only talk about the tunnel itself no longer say SSH. SSH-specific copy
(ssh -L/-R hints, the manual guide, SSH error classes, FRP SSH ports) is
deliberately untouched."
```

**完成标准：** 11 处（zh + en 共 22 个值）改完且不含「SSH」；三个相关测试文件全绿；`npm run build` 成功；C 类 15 处**一行未动**。

---

### Task 14: 动态传输标注（B 类 9 处加插值 + 调用点传参）

**目标**：把 B 类 9 处文案改成**带 `{transport}` 插值**，调用点按 `activeTransport` 传参；规则见设计文档 §12.4——`'ssh'→（SSH）`、`'h2'→（HTTP/2）`、`''`/`'both'` **不追加**。

**Files:**
- Modify: `web/src/i18n/locales/zh.ts` + `en.ts`（T13 改过的 9 个 B 类值再加插值占位）
- Modify: `web/src/components/proxy/ProxyPanelContent.vue`
  - 模板 `:30`（`tunnelDisconnected`）、`:39`（`tunnelConnectedButNoResponse`）、`:48`（`backgroundTip`）
  - toast `:520`（`toast.tunnelRecovered`）、`:522`（`toast.tunnelConnectedNoResponse`）、`:524`（`toast.tunnelStillDisconnected`）
  - **不改** `:22-24` 的 `.tunnel-transport` 行与 `:348-355` 的 `transportLabel`（见风险 R12.2）
- Modify: `web/src/composables/usePortForward.ts`
  - `tunnelMessage` 赋值点 `:592, :606, :633, :643, :720, :750`
  - toast `:830, :859, :870`
- Test: 新增/扩展 `web/src/components/proxy/__tests__/ProxyPanelContent.transport.test.ts` 或新建 `ProxyPanelContent.transportAnnotation.test.ts`

**依赖：** **T13**（通用基线值）与 **T3**（Android `tls|h2c → h2` 归一化；否则 Android 拿不到 `'h2'`，标注永不显示）。**可并行：** 与 T15/T16 并行；与 T13 同文件，串行。

**规则（硬性，设计文档 §12.4）：**

| `activeTransport` | 渲染 |
|---|---|
| `'ssh'` | 通用措辞 + `（SSH）` |
| `'h2'` | 通用措辞 + `（HTTP/2）` |
| `''` | **纯通用措辞，不追加** |
| `'both'` | **纯通用措辞，不追加** |

**标签来源**：插值值复用 `transportLabel`（`ProxyPanelContent.vue:348-355`）的映射结果（`'ssh'→'SSH'`、`'h2'→'HTTP/2'`）。**不要**在调用点另起字面量（三处会漂移）。若需要，把该映射抽成一个小 helper；**若抽到 `web/src/utils/portForwardUtils.ts`，必须同步 `usePortForward.test.ts:96-115` 的自复制 mock**（风险 R12.3）。

**Step 1: 写失败测试**

新增用例（覆盖规则四种取值）：

| 用例 | 断言 |
|---|---|
| `activeTransport='h2'` → banner/toast 含 `（HTTP/2）` | `t('proxy.tunnelDisconnected', { transport: 'HTTP/2' })` 形如 `隧道未连接（HTTP/2）` |
| `activeTransport='ssh'` → 含 `（SSH）` | 同上 |
| `activeTransport=''` → **不含**括号 | 文本 === 纯通用措辞 |
| `activeTransport='both'` → **不含**括号 | 同上 |

**Step 2: 跑测试确认失败**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
npx vitest run web/src/components/proxy/__tests__/ProxyPanelContent.transportAnnotation.test.ts
```

预期：FAIL（插值未实现）。

**Step 3: 改 i18n 为插值 + 改调用点**

zh 示例（en 对应）：`tunnelDisconnected: '隧道未连接{transport}'`，其中 `transport` 由调用点算成 `'（SSH）'` / `'（HTTP/2）'` / `''`（**空串时整段消失**，天然满足「不追加」）。

```ts
// 形态片段（非完整实现）——调用点
const transportSuffix = computed(() =>
  activeTransport.value === 'ssh' ? '（SSH）'
  : activeTransport.value === 'h2' ? '（HTTP/2）'
  : '')                                  // '' | 'both' → 不追加
```

- 模板 `:30/:39/:48`：`t('proxy.tunnelDisconnected', { transport: transportSuffix })`。
- toast `:520/:522/:524`：同样传参。
- `usePortForward.ts` 的 `tunnelMessage` 赋值点（`:592, :606, :633, :643, :720, :750`）与 toast（`:830, :859, :870`）：该文件已持有 `activeTransport`（`:79`）与 `gt`，按同规则算后缀传入。

> **⚠️ 关键选择（决定测试是否破）**：`usePortForward.test.ts:82` 把 `gt` mock 成 identity（`(key) => key`）。若 `tunnelMessage` 存的是 `gt(key)` 的返回（纯 key），**加插值实参不影响断言**（`:1695/:1742/:1897/:1976/:2003` 仍成立）；若改成**拼接**（`gt(key) + suffix`），`tunnelMessage` 不再是纯 key，**这些断言会破**。**推荐**：把后缀拼进 i18n 的插值（`gt(key, { transport })`），保持 `tunnelMessage` 的形态判断不变；若坚持拼接，则**必须同步更新那 5 条断言**。

**Step 4: 验证（先查并发，§0.2）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
npx vitest run web/src/components/proxy/__tests__/ProxyPanelContent.transportAnnotation.test.ts \
               web/src/components/proxy/__tests__/ProxyPanelContent.transport.test.ts \
               web/src/composables/__tests__/usePortForward.test.ts
```

预期：全绿；`ProxyPanelContent.transport.test.ts:149/:156/:165`（`.tunnel-transport-value` === `'SSH'`/`'HTTP/2'`/`'自动'`）**未破**（本任务不碰那一行）。

**Step 5: 前端编译**

```bash
npm run build
```

**Step 6: Commit**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts \
        web/src/components/proxy/ProxyPanelContent.vue \
        web/src/composables/usePortForward.ts \
        web/src/components/proxy/__tests__/ProxyPanelContent.transportAnnotation.test.ts
git commit -m "feat(web): annotate tunnel messages with the active transport

Appends （SSH）/（HTTP/2） only when activeTransport is a known single value;
'' and 'both' fall back to the neutral wording rather than guessing. Depends on
T3 normalizing Android's tls/h2c to h2, otherwise the annotation can never show."
```

**完成标准：** 四种 `activeTransport` 取值均有断言；`'ssh'`/`'h2'` 追加正确括号，`''`/`'both'` 不追加；`ProxyPanelContent.transport.test.ts` 三条 `.tunnel-transport-value` 断言未破；`npm run build` 成功。

---

### Task 15: Android 原生通知文案（N1–N4）

**目标**：改 4 个 Android 字符串资源（zh + en 各 4 条），其中 N2 按传输动态选词。

**Files:**
- Modify: `android/app/src/main/res/values/strings.xml`（`:93, :94, :95, :97`）
- Modify: `android/app/src/main/res/values-zh/strings.xml`（`:93, :94, :95, :97`）
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`
  - N1 调用点 `:1332-1333`、`:2455-2456`
  - N3 调用点 `:1343-1344`
  - N2 调用点 `:1361-1362`（**动态选词**）
  - N4 调用点 `:2366-2371`（`createNotificationChannel`）

**逐条清单**（行号两文件相同，设计文档 §12.3.1）：

| # | 资源名 | 行 | 当前 zh | 改为（zh / en） | 分类 |
|---|---|---|---|---|---|
| N1 | `ssh_notification_reconnecting` | `:93` | `SSH 隧道断开，正在重连…` | `隧道断开，正在重连…` / `Tunnel disconnected, reconnecting…` | A |
| N2 | `ssh_notification_recovering` | `:94` | `SSH 隧道已恢复` | **动态**：ssh → `SSH 隧道已恢复` / `SSH tunnel reconnected`；h2 → 新增资源 `tunnel_notification_recovering_h2` = `HTTP/2 隧道已恢复` / `HTTP/2 tunnel reconnected` | B |
| N3 | `notif_ssh_reconnecting_attempt` | `:95` | `SSH 隧道断开，第 %1$d 次重连…` | `隧道断开，第 %1$d 次重连…` / `Tunnel disconnected, reconnecting (attempt %1$d)…` | A |
| N4 | `notif_channel_bg_service_desc` | `:97` | `SSH 端口映射与后台事件监听` | `端口映射与后台事件监听` / `Port forwarding and background event listening` | A |

**依赖：** 无（可与 T13 并行）。**可并行：** 与 T13 并行；与 T14/T16 并行。

**Step 1: 先查现有测试覆盖（V3，必做）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
grep -rn "ssh_notification_reconnecting\|ssh_notification_recovering\|notif_ssh_reconnecting_attempt\|notif_channel_bg_service_desc\|tunnel_notification_recovering_h2" \
  android/app/src/test/ android/app/src/androidTest/ 2>/dev/null || echo "NO NOTIFICATION STRING TEST"
```

预期：大概率 `NO NOTIFICATION STRING TEST`。**若为空**：改 N1–N4 **零测试影响**，但**也意味着无回归守护**——本任务需**新增**一条最小断言（见 Step 4）。**若非空**：把断言的行号记下，改文案后同步。

**Step 2: 改两个 strings.xml**

按上表改 N1/N3/N4 的值；**N2 保留原值**（ssh 用），**新增** `tunnel_notification_recovering_h2`（zh + en 各一条，紧邻 N2）。

**Step 3: N2 动态选词（`BackgroundService.java:1361-1362`）**

```java
// 形态片段（非完整实现）
// 此刻传输已连接：getActiveTunnelTransport() 非空即 h2（T3 后返回 "h2"），空即 SSH。
boolean h2 = !getActiveTunnelTransport().isEmpty();
String text = getString(h2
        ? R.string.tunnel_notification_recovering_h2
        : R.string.ssh_notification_recovering);
```

> **不要**给 N2 塞 `%1$s` 插值：Android 资源插值在调用点拼装即可，且**只有 N2 能动态**（N1/N3 在断线态渲染时 `getActiveTunnelTransport()` 也返回 `""`，与 SSH 不可区分——设计文档 §12.3.1）。N4 是通道描述，首次 `createNotificationChannel` 后固化，**保持静态通用措辞**。

**Step 4: 验证**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward/android
JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ANDROID_HOME=/opt/android-sdk \
  ./gradlew :app:testDebugUnitTest --tests '*Transport*'
```

预期：`*Transport*` 全绿（`BackgroundServiceTransportTest` / `H2TunnelStreamTest` 等）。

```bash
# 资源引用完整性（R.java 生成失败即编译失败，但先静态确认无拼写错误）
grep -rn "tunnel_notification_recovering_h2" android/app/src/main/res/values/strings.xml android/app/src/main/res/values-zh/strings.xml
```

预期：两文件各命中 1 条。

> **若 Step 1 确认无测试**：在 `BackgroundServiceTunnelTransportPrefsTest`（T6 新建）或同类 Robolectric 测试中补一条最小断言——N2 的选词在 h2 / ssh 两态下分别取到 `tunnel_notification_recovering_h2` / `ssh_notification_recovering`（或直接断言 `getString` 结果文本）。**不要**只改文案而不留任何守护。

**Step 5: Commit**

```bash
git add android/app/src/main/res/values/strings.xml \
        android/app/src/main/res/values-zh/strings.xml \
        android/app/src/main/java/com/clawbench/app/BackgroundService.java
git commit -m "i18n(android): make tunnel notifications transport-neutral

N1/N3/N4 only talk about the tunnel/port mapping, so they drop the SSH prefix.
N2 ('tunnel recovered') fires while the transport is connected, so it picks the
HTTP/2 or SSH wording from getActiveTunnelTransport(); the reconnect-progress
strings stay generic because a disconnected h2 is indistinguishable from SSH."
```

**完成标准：** N1/N3/N4 通用化（zh + en）；N2 在 h2 / ssh 两态下选到正确资源；新增 `tunnel_notification_recovering_h2`；`--tests '*Transport*'` 全绿；N2 有最小测试守护。

---

### Task 16: `tunnelNoSsh` 触发条件修复 + 死 key 清理 + `Fingerprint:` i18n（低风险收尾）

**目标**：三件互相独立的小事，均属设计文档 §12.6 / §12.7 的记录项。**`tunnelNoSsh` 触发条件修复是行为改动**，其余为文案/清理。

**Files:**
- Modify: `web/src/components/proxy/ProxyPanelContent.vue`
  - `:116` — ✏️ `tunnelNoSsh` banner 的 `v-if` 触发条件（**行为改动**）
  - `:107` — ✏️ 硬编码 `<span class="fingerprint-label">Fingerprint:</span>` → i18n
- Modify: `web/src/i18n/locales/zh.ts` + `en.ts`
  - ➕ `proxy.fingerprintLabel`（`指纹：` / `Fingerprint:`）
  - ❌ 删死 key `proxy.sshTunnel`（1565 / 1562）、`proxy.copySSHCommand`（1570 / 1567）、`settings.items.portForwardEnabledDesc`（2434 / 2431）

**依赖：** 无。**可并行：** 与 T14 并行（不同区域）；与 T13 同文件 `zh.ts`/`en.ts`，**建议 T13 之后再动**（避免同文件冲突）。

**Step 1: `tunnelNoSsh` 触发条件（设计文档 §12.6）**

**现状**：`:116` 的 `v-if="!isAppMode && sshInfo && !sshInfo.enabled"` → 渲染 `:119` 的 `proxy.tunnelNoSsh`。根因是**把「没有 SSH 监听」等同于「没有端口转发」**——在 h2 可用的安装下，用户关掉 `port_forward.enabled` 靠 h2 工作，端口映射**其实正常**，banner 却叫用户去开 SSH（误导）。

**建议修法**：判定改为「端口转发是否可用」而非「SSH 监听是否启用」——需同时考虑 h2 通道可用性（web 端可读服务端 `port_forward.transport`；h2 可用性判定可复用 `usePortForward.ts:515-523` 的 `tunnelTransportAllowsH2()`）。

> **注意**：`sshInfo` 来自 `/api/ssh/info/full`，**只反映 SSH 状态**，不含 h2。因此不能只改文案，必须改判定输入。
> **文案同步**：若修了触发条件，`proxy.tunnelNoSsh`（`:1583`）的文案应同步改通用（「端口转发未启用…」），否则「端口转发正常」时还显示「SSH 隧道未启用」仍矛盾。**本任务同时改该值**（zh + en），但它属 C 类语义的 key（讲 `port_forward.enabled`），**不改 key 名**。

**Step 2: `Fingerprint:` i18n（设计文档 §12.7）**

`ProxyPanelContent.vue:107` 的 `<span class="fingerprint-label">Fingerprint:</span>` 是**唯一未 i18n 的硬编码英文**，属 C 类概念（host key 指纹标签，**保留 SSH 语义**）。新增 `proxy.fingerprintLabel`（zh `指纹：` / en `Fingerprint:`）并替换。

> 该标签在 web 模式手动指南整块内（`:62` 的 `!isAppMode && sshInfo.enabled` 门控），改的是**本地化**不是**措辞通用化**。

**Step 3: 死 key 清理（设计文档 §12.7）**

先确认无消费者，再删（zh + en 同时删）：

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
grep -rn "proxy.sshTunnel\|proxy.copySSHCommand\|settings.items.portForwardEnabledDesc" web/src --include=*.vue --include=*.ts | grep -v "i18n/locales" || echo "NO CONSUMERS"
```

预期：`NO CONSUMERS`。确认后删三个 key（zh + en）。

> **命名空间无 parity 测试**：`proxy.*` / `settings.items.*` 既无 zh/en parity 测试也无快照（设计文档 §12.8），所以删 key 不会被测试抓到——**正因如此必须先用 grep 证明无消费者**，且两个 locale 同时删。

**Step 4: 验证（先查并发，§0.2）**

```bash
cd /root/code/clawbench/.worktrees/ssh-ws-forward
npx vitest run web/src/components/proxy/__tests__/ProxyPanelContent.transport.test.ts \
               web/src/components/proxy/__tests__/ProxyPanelContent.direction.test.ts \
               web/src/components/proxy/__tests__/ProxyPanelContent.scan.test.ts
```

预期：全绿。若 `ProxyPanelContent.scan.test.ts` / `direction.test.ts` 的 mock 里含 `proxy.fingerprintLabel` 缺失导致渲染告警，按需补 mock（它们的 `:105/:89` 附近已有 locale mock 区）。

**Step 5: 前端编译**

```bash
npm run build
```

**Step 6: Commit**

```bash
git add web/src/components/proxy/ProxyPanelContent.vue web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "fix(web): stop telling h2-only installs to enable SSH; drop dead keys

The 'tunnelNoSsh' banner keyed off sshInfo.enabled, which conflates 'no SSH
listener' with 'no port forwarding' — an h2-only install forwards ports fine.
The banner now keys off port-forwarding availability. Also i18n the hardcoded
Fingerprint: label and remove three keys with no consumers."
```

**完成标准：** h2-only 安装（关 `port_forward.enabled`、靠 h2 工作）**不再**显示「SSH 隧道未启用」banner；`Fingerprint:` 走 i18n；三个死 key 从 zh + en 删除且 grep 证明无消费者；相关前端测试全绿；`npm run build` 成功。

---

## 2. 执行顺序总览

| 顺序 | 任务 | 并行组 | 依赖 | 备注 |
|---|---|---|---|---|
| 1 | **T1** 删静态字段 + 偏好真相源 | 独占 | — | **必须独立验收**；PortHost/ReversePort 断言未改全绿 = 停 |
| 2 | **T2** 删 `BOTH` | A | T1 | 枚举 + 删 both 用例 |
| 3 | **T3** 修 `tls/h2c → h2` 展示映射 | A | T1 | 同文件不同区域，建议紧随 T2 |
| 4 | **T4** 桥测试 + 语义收尾 | A′ | T1 | 不同文件，可与 T2/T3 并行 |
| 5 | **T5** 改造 `TransportTest` 助手 | B | T1 | 20 处 `setField` 全替换 |
| 6 | **T6** 新增 Android 测试 + 全量回归 | B | T1/T3/T5 | 收尾跑全量（0 failures，总数 ≥ 1032） |
| 7 | **T7** 移除前端推送 | C | T1 | `clawbenchNative.ts` 串行起点 |
| 8 | **T8** 新增桥契约 | C | T1 | 与 T7 同文件，串行 |
| 9 | **T9** 设置页专用开关行 | C | T8 | 最后 `npm run build` |
| 10 | **T10** Electron bridge clamp | D（与 C 并行） | — | **不动 `tunnel.ts`** |
| 11 | **T11** 文档同步 | E（与 C/D 并行） | T1/T7/T10 | 只碰 docs + openapi |
| 12 | **T12** 端到端 | 收尾 | 全部 | 真实服务端 + 真机 |
| 13 | **T13** 前端措辞通用化（A 2 + B 9 基线） | F | — | 只改 i18n 值；可与 T15 并行 |
| 14 | **T14** 动态传输标注（B 9 加插值） | F′ | **T13 + T3** | 无 T3 则 Android 永不显示（HTTP/2） |
| 15 | **T15** Android 通知文案 N1–N4 | G | — | 可与 T13 并行；N2 动态选词 |
| 16 | **T16** `tunnelNoSsh` 触发条件 + 死 key + `Fingerprint:` | H | — | 触发条件是**行为改动**；与 T14 可并行 |

**可并行矩阵（互不碰文件区域，可同时开工）：**

- **A** = {T2, T3} 同改 `BackgroundService.java`/`PortForwardTransportKind.java` 的不同区域；**A′** = {T4} 改 `MainActivity*`；A 与 A′ 可并行。同文件多区域同时改易冲突，**A 内部建议串行**。
- **B** = {T5, T6}：T5 改既有测试文件，T6 新增独立测试文件；T6 依赖 T5 的 stub 模式。
- **C** = 阶段 2（前端）与 **D** = T10（Electron）**整体可并行**（不同目录、不同语言）。
- **E** = T11 只碰 `docs/` 与 `internal/api/openapi.yaml`，可与 C/D 并行。
- **F** = {T13, T14} 与 **F′** 的关系：T13（只改 i18n 值）**必须先于 T14**（T14 在通用基线上加插值）；**G** = {T15} 只碰 Android 资源 + `BackgroundService.java`，与 **F 完全可并行**（不同语言/目录）。
- **H** = {T16} 与 T14 可并行（不同区域）；与 T13 同改 `zh.ts`/`en.ts`，**建议 T13 之后再动**（避免同文件冲突）。
- **阶段 6 内部**：T13 ∥ T15；T14 依赖 T13 + T3；T16 与 T14 可并行。
- **并行禁令（AGENTS.md）：** 绝不同时跑 `npm run build` 与全量 vitest（OOM）；全量测试前先 `ps` 检查；不重复跑同一个全量；判回归一律先隔离单跑。
- **与另一 agent 的隔离：** 不碰 `android-e2e/`；发现它在跑 gradle/emulator 时**等待**。

**提交纪律：** 每个任务完成后立即 commit（AGENTS.md：频繁提交）。

---

## 3. 风险与缓解（对应设计文档 §9，只列执行相关）

| # | 风险 | 缓解（落到本计划的哪个任务） |
|---|---|---|
| R1 | 冷启动时序：`ensureConnection` 必须晚于 `onCreate` | T1 只改读法不改调用时机；`restoreAndReconnect`(`:1264-1292`) 已保证晚于 `onCreate`(`:789-905`) |
| R2 | 平台过滤不能用 `appOnly` | T9 用 `isAppMode && !isDesktopApp`（照 `SettingsCategory.vue:210`） |
| R3 | 异步初值闪烁 | T9 用 `h2ToggleLoaded` + `v-if` 隐藏到初值到达 |
| R4 | `tls/h2c` 契约不一致 | T3 在 Android 边界归一化为 `h2` |
| R5 | 切换不即时生效 | T9 的提示 + 「立即重连」按钮；T5/T6 断言写偏好后 live transport 不变 |
| R6 | 旧宿主降级 | T8 新方法为可选 `?`；T9 缺失时隐藏；**不可**回退到不持久化的老 `setTunnelTransport` |
| R7 | Electron clamp 防旧缓存前端推 `both` | T10 clamp 在 bridge 层 |
| R8 | `setField` 静态字段助手抛异常 | T5 逐条替换 20 处；验证命令 `grep -n 'setField("transportPreference"'` 必须无输出 |
| — | 编译耦合（T1↔T4） | §0.5 已说明：T1 含 `MainActivity` 源码最小改动，T4 承接其测试 |
| R12.1 | 插值改造牵动 `gt` identity mock | T14：`usePortForward.test.ts:82` 的 `gt` 是 identity。**推荐**把后缀并入 i18n 插值（`gt(key, { transport })`）以保持 `tunnelMessage` 的 key 形态；若改为拼接则必须同步 `:1695/:1742/:1897/:1976/:2003` 断言（设计文档 §12.9 R12.1） |
| R12.2 | `transportLabel` 映射测试 | T14：`.tunnel-transport-value` 断言（`ProxyPanelContent.transport.test.ts:149/:156/:165`）不能动；标注落在 banner/toast，不落该行（设计文档 §12.9 R12.2） |
| R12.3 | `usePortForward.test.ts` 自复制 mock | T14：若为传输标签新增 `portForwardUtils` 导出，**必须**同步 `:96-115` 的 mock，否则套件抛 `No export named …`（设计文档 §12.9 R12.3） |
| R12.4 | `activeTransport` 依赖 T3 | T14 **必须等 T3**：否则 Android 拿不到 `'h2'`，动态标注永不显示（安全降级但不达验收） |
| R12.5 | `activeTransport` 可能为 `''` | T14：按规则退回纯通用措辞，**不得猜传输**（设计文档 §12.5） |
| R12.6 | `tunnelNoSsh` 触发条件是行为 bug | T16：改 `v-if` 判定输入（`sshInfo` 只反映 SSH）；**单独验收**（设计文档 §12.6） |
| R12.7 | 改 i18n key 名会破测试 | T13/T14/T16：只改**值**安全；改 key 名必须同步两 locale + 调用点 + 断言（设计文档 §12.8） |

---

## 4. 明确不做（设计文档 §1.2 / §10，执行时不要擅自扩范围）

1. ❌ 不改服务端 `port_forward.transport` 默认值（保持 `both`）——`proxy_registry_gate_test.go:94-111` 会拒绝。
2. ❌ 不给 Electron 启用 h2；不改 `tunnel.ts` 与 `tunnel.h2.test.ts`。
3. ❌ 不做 web 端隧道客户端。
4. ❌ 不给 Android 加 `BOTH`，也不做 h2 失败回退 SSH。
5. ❌ 不做 `source: 'native'` 第三种 panel 来源（专用行规避）。
6. ❌ 不给 `SettingsItem` 加异步受控能力。
7. ❌ 不扩前端 `TRANSPORTS` 白名单（在 Android 边界归一化即可）。
8. ❌ 不动 `android-e2e/`（另一 agent 负责）。
9. ❌ **不改 web 模式手动指南整块的 SSH 措辞**（`ProxyPanelContent.vue:55-106`，设计文档 §12.1）——它展示的是 `ssh -N -L …` 命令、host key 指纹与装 ssh 客户端指引，浏览器没有客户端 h2 路径。
10. ❌ **不给 SSH 错误分类改措辞**（`proxy.tunnelErrorAuth/Network/HostKey`，`zh.ts:1532-1534`）。
11. ❌ **不给 `tunnelNoSsh` 之外任何断线态文案加动态标注**（N1/N3/N4 与 `activeTransport=''`/`'both'` 场景一律退回纯通用措辞，不猜传输，设计文档 §12.3.1 / §12.4）。

### 后续可清理项（本次不做，仅记录）

服务端 `port_forward.transport` 对原生客户端已成为**事实上的死配置**，只剩 registry 门控与 web 健康门控两个用途。后续版本可考虑把它拆成两个独立配置项，或移除对客户端无意义的 `h2`/`both` 下发。

> **关联**：设计文档 §12 新增的「措辞通用化与传输标注」（T13–T16）与本项同源——`port_forward.transport` 语义混乱的**用户可见表现**就是「SSH 转发」措辞与 `tunnelNoSsh` 误报（§12.6）。T13–T16 已把**文案层**收口；配置项本身的清理仍留后续版本。
