# 仓内副本说明（fork of coder/acp-go-sdk）

## 为什么拷进来

上游是「一家公司一个人维护」的库，2026-06-02 发 v0.13.5 后停了 5 个多月，
2026-11-18 一天连发 7 个版本把协议从 0.13.5 追到 1.32.0。它的节奏是攒一批、
集中发一次，每次升级都是大改（v0.20.0 一次性破坏，本仓实测 148 处编译错）。

拷进仓里以后，升级只跟「官方协议契约」走，不再跟上游的 API 设计走。

## 来源

- 仓库：https://github.com/coder/acp-go-sdk
- 基线：v0.13.5（2026-06-02），对应 ACP schema 0.13.5
- 随后在仓内升级到 schema 1.32.0（对应上游 v0.20.0 的生成物）：
  上游文件**逐字节一致**，唯一新增是 `rawcall.go`（见下）
- 升级的破坏性变更见同目录 `MIGRATION_v0.20.md`
- 许可证：Apache-2.0，见同目录 `LICENSE`

## 目录里有什么

| 路径 | 说明 |
| --- | --- |
| `*.go` | 根包 `acp`，其中 `*_gen.go` 是生成代码 |
| `schema/` | 官方协议契约（JSON Schema）+ 版本号，生成器的唯一输入 |
| `testdata/` | 上游单测夹具 |
| `cmd/generate/` | 代码生成器（独立 module，不并入主 `go.mod`） |
| `rawcall.go` | **我们加的**：`ClientSideConnection.CallRaw`，发任意方法名（生成器不碰它） |

## 我们加的唯一一个文件：rawcall.go

`CallExtension` 只接受 `_` 开头的方法名，而 CodeBuddy 的 `session/steer`、Hermes 的
`session/set_model` 都没有下划线，typed 面也没有建模。`CallRaw` 就是 `SendRequest`
去掉名字校验，约 20 行，让这些调用复用 SDK 自己的连接、写锁、id 计数器和响应表。

有它之后，仓里不再需要那套自写的 JSON-RPC 通道（原 `internal/ai/acp_raw_rpc.go`
里的 id 前缀、pending 表、响应分发、共享写锁全部删除）。

## 改动规则

1. **只改非生成文件**：`connection.go`、`errors.go`、`extensions.go`、`helpers.go`、
   `agent.go`、`client.go`、`patch.go`、`meta.go`、`shape.go` 可以改；
   任何 `*_gen.go` 改了下次重新生成就会被冲掉。
2. 需要在生成代码上加东西时，新建一个同包文件（例如 `extra_*.go`），不要手改生成物。
3. 本仓的定制改动在文件头注释里标 `// ClawBench:`，方便重新生成后比对。

## 怎么跟进官方协议

官方契约升级时：

1. 用新版 `schema/schema.json`、`schema/schema.unstable.json`、`schema/meta.json`、
   `schema/version` 覆盖 `schema/` 下的同名文件；
2. 重新生成：

   ```bash
   cd third_party/acp-go-sdk/cmd/generate && go run .
   ```

   默认读 `<repo>/schema`、写到仓库根；`-schema` / `-out` 可改路径。
3. 回根目录 `go build ./...`，按编译错误改调用方。

生成器只依赖 `github.com/dave/jennifer`（代码生成库），离线可跑。

## 跑上游自带的测试

它是独立 module，根目录的 `go test ./...` 不会覆盖到，要单独跑：

```bash
cd third_party/acp-go-sdk && go test ./...
```
