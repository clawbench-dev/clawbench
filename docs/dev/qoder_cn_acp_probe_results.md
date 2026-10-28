# Qoder 国内版（qoderclicn）ACP 探针结论

> 对应 issue #589。探针：`internal/ai/qoder_cn_acp_probe_integration_test.go`
> （`go test -tags integration -run TestQoderCNProbe ./internal/ai/`，6 例全 PASS，约 57s）。

## 一句话结论

国际版与国内版是**同一份代码、不同品牌常量的两个独立发行包**，不是运行期开关。
CLI 参数面逐字相同，可以共用一份 argv 构造器；但**配置根、凭据、模型目录各自独立**，
所以必须注册成两个后端。

## 两个发行包

| | 国际版 | 国内版 |
|---|---|---|
| npm 包 | `@qoder-ai/qodercli` | `@qodercn-ai/qoderclicn` |
| bin | `qodercli` / `qoder` | `qoderclicn` / `qodercn` |
| 用户配置根 | `~/.qoder` | `~/.qoder-cn` |
| 项目级配置 | `.qoderignore` / `.qoder.json` | `.qoder-cnignore` / `.qoder-cn.json` |
| 用户 skills 根 | `~/.qoder/skills` | `~/.qoder-cn/skills` |
| 项目 skills 根 | `<project>/.qoder/skills` | `<project>/.qoder/skills`（**共用**） |
| 共享 skills | `~/.agents/skills` | `~/.agents/skills`（相同） |
| ACP `agentInfo.name` | `qoder-cli` | `qoder-cli-cn` |
| 鉴权端点 | `api.qoder.com` / `openapi.qoder.sh` | `openapi.qoder.com.cn` / `center.qoder.sh` |
| 登录命令 | `qodercli login` | `qoderclicn login` |

品牌分叉在 bundle 里是一处常量：`Gs="cn"==(Z4a="cn")`（CN） vs `Gs="cn"==(Z4a="global")`（国际）。
同一处常量派生出上面所有路径与端点差异。

## ⚠️ 安全红线：CN 二进制指向国际版配置根会**删除**该凭据

实测事故（2026-10-27）：用

```
node <cn-bundle> --config-dir ~/.qoder --acp
```

启动国内版，让它读国际版的真实登录态。CN 二进制**加载了**该凭据、判定鉴权失败，
然后自动清除：

```
[auth-storage] event=credential.load  outcome=loaded
[auth-storage] event=credential.clear reason=automatic_auth_rejection outcome=deleted
[auth]         event=auth.logout    source=automatic auth_source=local
```

结果：国际版被登出，`~/.qoder/.auth/user` 被删除且无副本。

这不是「读不到」，而是「读到了会被销毁」。因此：

- 两个后端**绝不能**共享 `--config-dir`，也绝不能靠 env（`QODER_SITE=CN` / `QODERCN_CONFIG_DIR`）互相切换
  —— 实测 `QODER_SITE=CN` 对国际版无效（它仍写 `~/.qoder`），`QODERCN_CONFIG_DIR` 对国际版同样无效。
- 探针里所有 spawn 都跑在 `t.TempDir()` 造的隔离 HOME，`qcnIsolatedHome()` 有硬断言；
  新增用例不要绕过它。

## 能力面（两版逐字段相同）

`initialize` 返回：

```json
{"agentInfo":{"name":"qoder-cli-cn","title":"Qoder CLI CN","version":"1.1.79"},
 "agentCapabilities":{
   "loadSession":true,
   "promptCapabilities":{"image":true,"embeddedContext":true},
   "mcpCapabilities":{"http":true,"sse":true},
   "sessionCapabilities":{"additionalDirectories":{},"close":{},"delete":{},"fork":{},"list":{},"resume":{}},
   "_meta":{"qoder":{"promptQueueing":true}}}}
```

`session/new` 广告 5 档 mode（`default` / `acceptEdits` / `auto` / `dontAsk` / `yolo`），
`models.availableModels` 为空（模型目录来自本地缓存，不走 ACP 广告）。

**事件处理可完全复用 qoder 的路径**，只需把 `qodercn` 加进三处按 backend 字符串分派的地方：

- `internal/ai/acp_tool.go`（tool_call / tool_call_update）
- `internal/ai/acp_meta.go`（`_meta` usage 解析，走 claude 编码）
- `internal/ai/acp_parent_link.go`（子代理父工具调用归属，走 `_meta.claudeCode.parentToolUseId`）

## 鉴权闸

未登录时 `session/new` 返回 `-32000 AUTH_REQUIRED`（ACP 保留区间）。
`initialize` 本身不需要登录，所以能力面探针在未登录机器上也能跑。

`authMethods[0].id` = `qoderclicn-login`，对应命令 `qoderclicn login`。
**`BackendSpec.LoginCmd` 必须是 CN 的那个**：两个包凭据独立，写错等于把用户引到另一个产品登录。

## 「能不能不登录直接用三方模型」——不能

实测把所有看起来能免登录的开关都试过，全部仍是 `-32000`：

| 尝试 | 结果 |
|---|---|
| `settings.json` 的 `providers`（BYOK custom provider） | 仍 `-32000` |
| `QODER_PERSONAL_ACCESS_TOKEN` | 仍 `-32000` |
| `QODER_SDK_ACCESS_TOKEN` + `QODER_SDK_AUTH_PAYLOAD_FILE` | 仍 `-32000` |
| `QODER_REMOTE_WORKER=1`（sdk 模式）+ `QODER_SDK_CUSTOM_BASE_URL_BYOK=1` | 仍 `-32000` |
| `--print` 模式 + BYOK + `-m <provider>/<model>` | `Not logged in · Please run /login` |

代码里的原因很明确：鉴权闸在 provider 解析之前，唯一短路条件是隐藏参数 `--fake-responses`：

```js
async ensureAuthReady(){ if(await this.ensureQoderAuthBootstrapped(), this.argv.fakeResponses) return;
    ... if(!CA().isAuthenticated()) throw Ss.authRequired() }
requireAuth(){ if(!this.argv.fakeResponses && !this.acpAuthenticatedType) throw Ss.authRequired() }
```

所以 **BYOK 是「登录后走自己的上游」，不是「免登录」**。

### BYOK 配置形状（登录后可用）

```json
// ~/.qoder-cn/settings.json
{"providers":{"minimax":{
  "type":"openai-compatible",
  "displayName":"MiniMax BYOK",
  "baseUrl":"https://api.minimaxi.com/anthropic",
  "apiKey":"...",
  "model":"MiniMax-M2.7",
  "models":[{"model":"MiniMax-M2.7","name":"MiniMax M2.7"}]
}}}
```

两个坑（都会静默忽略整个 provider，日志里只有一行 WARN）：

- `models` 必须是**数组**，不是对象：`[custom-provider] x.models: models must be an array`
- 数组每项的模型 id 字段名是 **`model`**，不是 `modelId`：`[custom-provider] x.models[0].model: model is required`

### `--fake-responses` 是唯一的离线旁路

隐藏参数（`--help` 里不显示）可以完全绕过鉴权，让 ACP 协议层在无凭据机器上跑通：

```
{"method":"generateContentStream","response":[{"candidates":[
  {"content":{"parts":[{"text":"..."}]},"finishReason":"STOP"}]}]}
```

但**它不发 `agent_message_chunk`**：协议跑完了、`stopReason=end_turn` 也回来了，却没有一个文本块。
所以它能用来测协议面（session/new、configOptions、命令列表），**不能**用来验证流式文本链路。
探针 L3 把这一点写成了硬断言，避免以后有人拿它做假绿的流式测试。

## 模型发现：修了一个真实缺陷

`~/.qoder/.auth/dynamic-texts.json` 现在（`schemaVersion: 3`）的形状是：

```json
{"schemaVersion":3,"version":"1.0.320","namespace":"qoder-ide",
 "locales":{"en":{...},"zh-CN":{...}}}
```

**顶层没有 `texts`**。原解析器只读 `texts`，于是对每一个真实安装都返回
`contains no texts` —— 模型列表从未被发现过（本机实测：修复前 0 个，修复后 15 个）。

修复后同时接受两种形状（旧 `texts` 优先，其次 `locales.en`，再退化为按名字排序的第一个非空 locale），
并且补上了对嵌套元数据键的过滤（`kmodel.functionSwitch.highspeed.description`、
`experts-ultimate.teams.markdownDescription`）。

## 接入实现

| 项 | 取值 |
|---|---|
| backend / ID | `qodercn` |
| `DefaultCmd` / `AltCmd` | `qoderclicn` / `qodercn` |
| `AcpCommand` | `qoderclicn --acp` |
| `InstallCmd` | `npm install -g @qodercn-ai/qoderclicn` |
| `LoginCmd` | `qoderclicn login` |
| `SortOrder` | 20（紧跟 OpenClaw，不与 qoder 相邻） |
| `NativeSkillsDirs` | `.qoder-cn/skills`, `.agents/skills` |
| `ACPLoadSession` | true |
| CLI argv | 复用 `qoder.BuildStreamArgs`（探针 L0 断言 59 个 flag 全等） |

`SortOrder` 放在末尾而不是紧挨 `qoder`(5)：选择器按发现顺序分组，把两个 Qoder 排在一起
会暗示它们可互换，而它们是独立安装、独立账号。追加也不会打乱既有 agent 的顺序。
