# Forge Webhook 接收（低延迟事件加速层）设计方案

日期：2026-09-12
状态：**设计待评审**（本文档为设计稿，尚未实施）
前置文档：`docs/plans/2026-09-11-github-gitlab-integration.md`（P1–P4 已实施）

## 概述

为现有 forge 集成增加 **webhook 接收端点**，让 GitHub/GitLab 在事件发生时主动推送，替代/补充当前的轮询检测。

**定位：webhook 是轮询的加速层，不是替代品。**

当前轮询（P2 已实施）的固有代价：

| 维度 | 现状（轮询） |
|---|---|
| 状态事件延迟 | 60s |
| 评论事件延迟 | ~5min（独立降频） |
| API 配额 | 72 调用/hr/repo（状态 60 + 评论 12）；GitHub 认证限额 5000/hr → 仅支撑 ~69 个 repo |
| 规模瓶颈 | 与 repo 数量线性相关 |

Webhook 把延迟降到秒级、把配额消耗降到近零，但**必须保留轮询作为兜底**：webhook 会丢投递（网络抖动、实例重启、部署窗口），而轮询的「本地快照 diff + 水位线」天然自愈。二者叠加后：

- **有 webhook** → 秒级延迟，轮询降频做对账
- **无 webhook** → 行为与现在完全一致（纯轮询）

## 与现有架构的契合点（已核实）

**注意：以下第 1 条经评审修正。** 原始设计称「sink 层零改动」，实际不成立 —— 见「评审修正记录」F3。

1. **事件派发点已收敛（但需要新增注入点）。** `forge_syncer.go:276-293` 是唯一派发处：`InsertForgeEvent`（去重入库）→ 若 `fresh` 则 `sink.HandleChange`。webhook 复用这条路径可让**通知、未读计数、事件触发任务全部自动生效**，sink 的**语义**零改动。
   但 `sink` 字段是**未导出**的（`forge_syncer.go:31`），`ForgeSyncer` 现有导出方法只有 `SyncRepo`/`SyncRepoWithOptions`。handler 无法直接触达派发链路，**必须新增一个导出入口**（见 F3 修正）。

2. **去重已就位。** `forge_events` 表有 `UNIQUE(dedupe_key)` + `ON CONFLICT DO NOTHING`（`forge_sync.go:276-290`）。**webhook 与轮询同时开启不会重复通知** —— 前提是两者对同一事件算出同一个 `DedupeKey`。这个前提比原设计预想的脆弱，见 F1/F2 修正。

3. **无鉴权端点有成熟先例。** `/api/share/{token}` 已是公开无鉴权（`handler.go:297-298`），`parseSharePublicPath` 的路径解析 + capability token 模式可直接借鉴。路由表通过 `handler.go:212-222` 的 `register` 闭包注册。

4. **签名校验无新依赖。** `crypto/hmac` + `crypto/sha256` + `crypto/subtle`（常量时间比较，项目已在 `middleware/auth.go:4` 使用）均为标准库。

## 决策记录

| 维度 | 决策 | 理由 |
|---|---|---|
| 定位 | **加速层，非替代** | webhook 会丢投递，轮询自愈不可去 |
| 端点路径 | `POST /api/forge/webhook/{token}` | 复用 share 的 capability-token 思路；token 即凭证 |
| 鉴权 | **不做 cookie 鉴权**，走独立校验链 | forge 平台无法携带 cookie |
| 签名算法 | GitHub: HMAC-SHA256；GitLab: token 比对 | 平台协议差异，见下 |
| secret 存储 | 复用 `ForgeConfig`，**按 binding 作用域** | webhook secret 是 per-repo 的；按 host 会互相覆盖（B5） |
| secret 回传 | **write-only**，仅回传「已配置」布尔 | 沿用现有凭据遮蔽模式（R5 决策） |
| 轮询策略 | webhook 活跃的 repo 自动降频 | 见「轮询降频策略」 |
| 重放防护 | `X-GitHub-Delivery` 幂等（按 binding 隔离） | 平台无签名时间戳，无法做时间窗 |
| 前置条件 | **需公网可达** | 见「部署前置条件」 |

## 评审修正记录

本设计经可行性评审后修正。评审发现的问题及处置：

| # | 评审发现 | 严重度 | 处置 |
|---|---|---|---|
| F1 | **评论事件的 `itemType` 无法对齐**：GitHub 的 `issue_comment` 对 PR 评论也投递，载荷默认按 `ItemTypeIssue` 归类，而轮询归类为 `ItemTypeChangeRequest`。`DedupeKey` 含 `itemType` → 两个不同 key → **同一评论通知两次** | Blocker | 载荷解析必须检测 `issue.pull_request != nil` 时归为 CR；`pull_request_review_comment` 的 id 空间与 issue comment 不同，单独处理 |
| F2 | **CI 事件无法去重**：非评论事件的 revision 只有 `state:<NewState>`；`pipeline` 载荷无 item number（push 触发为 0），两次成功的 CI 会算出相同 key → 第二次被静默丢弃 | Should-fix | `DedupeKey` 对 `EventPipeline` 增加 run-id revision；定义 CI 事件的 item 身份 |
| F3 | **无导出注入点**：`sink` 未导出，`ForgeSyncer` 只有 `SyncRepo*` 导出方法，handler 无法触达派发链路。「管道零改动」不成立 | Should-fix | 新增导出方法 `ForgeSyncer.DispatchChange(ctx, repo, item, change) bool`（内部走 `InsertForgeEvent` + `HandleChange`），在 `main.go` 注入给 webhook handler |
| F4 | **`last_webhook_at` 作用域错误**：字段在 `project_forges`（per-project_path），但降频 tier 是 per-repo。`ListProjectForges` 按 `updated_at DESC` 排序且 `syncAll` 每个 repo 只取第一行 → 同一 repo 绑定两个 project 时，webhook 更新 A 行而 poller 读 B 行，**repo 永远进不了 ACTIVE** | Should-fix | 健康度按 `(platform,host,owner,repo)` 记录，存到已有的 `forge_sync_state`（该表已是 repo 维度） |
| F5 | **同步派发拖慢响应**：sink 内含 IM 网络推送（`forge_notify.go:83-87`）。轮询在自有 goroutine 里调用无妨，但 webhook 若内联调用后才响应，慢的 IM 调用会导致 GitHub 标记投递失败并重试 | Should-fix | 顺序改为：校验签名 → 幂等入库 → **立即 2xx** → 异步派发 |
| F6 | **重绑会轮换 token**：`UpsertProjectForge` 在每次绑定/remote 刷新时都执行（`forge_endpoints.go:403`），若在其中无条件生成 token，会在无关刷新时静默破坏用户已在平台侧配置的 webhook | Should-fix | token 仅在**不存在**或**显式轮换**时生成；`Upsert` 保留已有值 |
| F7 | 迁移机制描述不具体 | Nit | 明确挂到 `database.go:619-706` 的 `pragma_table_info` 守卫式 ALTER 列表；新表 DDL 加入 `:603-607` 的 DDL slice |

### 评审确认成立的判断

- 「webhook 作加速层、轮询作兜底」的架构方向正确；内容寻址的去重键天然处理重复投递
- 去重键一致性是**正确性所依赖的唯一支柱**，且比原设计预想的脆弱（F1/F2）—— webhook 不写 `forge_items`、不推进水位线，无法像轮询那样靠快照自愈
- 24h/15min 分层在配置缺失时安全（NULL → FALLBACK），但 webhook 静默失效时最长 24h 才发现，需补状态信号
- `pipeline_done` 的显示链路可用（`forge_event_context.go` 确实读 `PipelineStatus`/`PipelineURL`），但**sink 契约是 item 中心的**（`HandleChange(ctx, repo, item, change)`），且 `DedupeKey` 需要 number —— CI 事件两者都不提供。原设计「附带收益」一节**过于乐观**，需 F2 的改造才能成立

Webhook 要求 forge 平台能**主动访问** ClawBench 实例。当前项目的现实约束：

## 评审修正记录（第二轮 —— Superpowers 评审）

第一轮修正（F1–F7）之后又经一轮 Superpowers 评审，发现以下问题并修正：

| # | 评审发现 | 严重度 | 处置 |
|---|---|---|---|
| B1 | **GitLab `note` 事件缺少 `noteable_type` 判定** —— 与 F1 完全同构，但发生在 GitLab 侧。GitLab 对 issue / MR / commit / snippet 都投递 `object_kind:"note"`，靠 `object_attributes.noteable_type` 区分。不判定则 MR 评论被归为 issue → key 与轮询不一致 → 重复通知 | Blocker | 映射必须要求 `noteable_type`，非 `Issue`/`MergeRequest` 一律丢弃（见 5.3） |
| B2 | **GitLab system notes 污染** —— 轮询侧过滤 `n.System`（`gitlab.go:163`），但 GitLab **仍会投递**这些事件（"changed the description"、"added label"）。不过滤则产生轮询永远无法推导的 `commented` 事件，造成单向噪声 | Blocker | 过滤 `object_attributes.system == true` |
| B3 | **§7 的 15min 低频 ticker 不存在** —— 实际只有 `stateEvery=60s`、`commentEvery=5min`（`forge_poller.go:46-47`），且启动时还有第三次 `syncAll` 调用（`:102`）。在 60s ticker 上跳过 ACTIVE repo 得到的是 **5min**，不是 15min | Blocker | 要么新增 ticker，要么改为 per-repo due-time 调度；并明确启动 pass 的归类（见 7） |
| B4 | **ACTIVE repo 的评论同步会静默停止** —— `IncludeComments=true` 的 pass 是**唯一**拉评论的 pass，也是**唯一**执行剪枝的 pass（`forge_poller.go:170-172`）。若 ACTIVE repo 在 5min 评论 ticker 上被跳过，评论永久停更 | Blocker | 明确低频 pass **必须** `IncludeComments=true`（见 7） |
| B5 | **secret 作用域自相矛盾** —— §4 按 `(platform,host)` 存 `WebhookSecrets`，类比 `Credentials`；但这个类比**不成立**：PAT 天然跨 repo，而 GitHub webhook secret 是 **per-webhook/per-repo**。同 host 下两个 repo 用不同 secret 会互相覆盖。与 §2 自己的「平台侧配置天然是 per-repo」矛盾 | Blocker | 作用域改为与 token 一致（见 4） |
| B6 | **异步派发未指定 context** —— §3 步骤⑧起 goroutine 但未说明用哪个 ctx。`r.Context()` 在 handler 返回瞬间取消，goroutine 会与取消竞争 | Blocker | 强制使用 `context.Background()` + 独立超时 |
| S7 | **配额算术错误** —— 引用的「~130 调用/hr」来自父 spec 的诚实预算，其中含 ~60/hr 的**假设 CI 轮询**（spec:271），而本文档自己说 CI 轮询不存在。真实基线 = 60 + 12 = **72/hr** → 降幅约 12x（非 20x），可支撑 ~69 repo（非 38） | Should-fix | 重算（见 7） |
| S8 | **路径 token 经日志泄露** —— `RequestLogger` 记录 `r.URL.Path`（`middleware/logger.go:53`）。由于 capability token **就是路径**，每次投递都会把凭证写进服务端日志。§3「必须记日志」未察觉此矛盾 | Should-fix | 日志中脱敏 token，或把 token 移到请求头 |
| S9 | **delivery id 主键全局，破坏多绑定** —— `forge_webhook_deliveries(delivery_id PRIMARY KEY)` 下，两个 token 绑定同一 repo 会共享 delivery id，只有第一个 binding 能处理 | Should-fix | 主键改为 `(delivery_id, binding_id)`；同时把开放问题 #3 定论 |
| S10 | **去重键对重复状态转移非单射** —— `state:<NewState>` 下 close→reopen→close 会产出两次相同的 `closed|state:closed`，第二次被丢弃。这是**轮询侧既有行为**，但实时 webhook 会让它更可见 | Should-fix | 文档明确记录该取舍，不夸大为「天然处理重复投递」 |
| S11 | **评论编辑（`edited`）不对称** —— 轮询在 `updated_at` 前移时发 `commented`（`events.go:161-163`），webhook 只映射 `created`。无重复，但延迟不对称 | Should-fix | 文档显式说明 |
| S12 | **错误可观测性不足** —— 401 统一化后，用户 secret 配错**只**表现为一行日志。运营侧无信号 | Should-fix | 状态端点增加 per-binding `last_webhook_error`（见 7） |

### 评审确认成立的关键判断

- **架构方向正确**：webhook 作加速层、轮询作兜底的判断成立；复用 persist→dispatch（`forge_syncer.go:276-293`）是正确切入点
- **去重键一致性经追溯成立**（GitHub 侧）：webhook 报 `closed` 后 `forge_items` 仍为 `open`，下次轮询会重新推导出 `EventClosed{NewState:"closed"}` → **相同 key** → `ON CONFLICT DO NOTHING` → `fresh=false` → **不派发**。即「webhook 不写快照」**不会**造成虚假重放。**该结论应写入文档而非仅断言**（原文档缺此论证）
- `0o600` 已核实（`settings.go:1762`）；write-only 回传与 `CredentialHosts` 模式一致（`settings.go:500-521`）
- F1 的 `issue.pull_request` 判定、F4 的 repo 维度健康度、F5 的异步决策、GitLab 明文→强制 HTTPS 的推理、401 统一化选择，均正确

### 评审指出的「设计正确但文档需补论证」项

原文档 §6.1 称「内容寻址的去重键天然处理重复投递」—— 该表述**过强**（见 S10）。已改为准确表述。

## 部署前置条件（阻塞项）

Webhook 要求 forge 平台能**主动访问** ClawBench 实例。当前项目的现实约束：

- 无 `public_url` 配置项
- `internal/frp/` 仅做 **TCP 端口转发**，无 HTTP 子域/自定义域名隧道
- `internal/proxy/` 是**出站** CORS 代理，方向相反

因此有三种部署形态：

| 形态 | 可行性 | 说明 |
|---|---|---|
| 公网域名 + 反代 | ✅ 直接可用 | 用户自行配置 nginx/caddy 转发到 ClawBench |
| FRP HTTP 隧道 | ⚠️ 需先扩展 | 当前 frp 只转 TCP，需补 `type=http` + 子域/自定义域 |
| 仅局域网/localhost | ❌ 不可用 | **webhook 收不到请求**，只能继续纯轮询 |

**设计决策**：UI 必须显式提示这一约束，并在未配置公网入口时**隐藏或禁用 webhook 配置项**，避免用户配置后困惑于「为什么没反应」。

## 技术方案

### 1. 端点与路由

```
POST /api/forge/webhook/{token}
```

路由注册（`internal/handler/handler.go`，紧邻 share 的公开端点）：

```go
// forge webhook 是公开端点：forge 平台无法携带 cookie，鉴权靠路径 token +
// 平台签名。与 /api/share/{token} 同属「capability token 即凭证」模式。
register("/api/forge/webhook/", ServeForgeWebhook)
```

**不做** `middleware.Auth` 包装。

### 2. Token 生成与作用域

每个 **binding**（`project_forges` 行）一个独立 webhook token：

- 生成：复用 `service.GenerateShareToken()`（`file_shares.go:27`）—— `crypto/rand` 16 字节 → 32 位 hex，128 bit 熵
- 存储：新增 `project_forges.webhook_token TEXT NOT NULL DEFAULT ''`
- 唯一索引：`CREATE UNIQUE INDEX ... ON project_forges(webhook_token) WHERE webhook_token != ''`
- 轮换：提供「重新生成」操作；旧 token 立即失效（无宽限期，避免悬挂凭证）

**F6 修正 —— token 生成时机**：`UpsertProjectForge` 在**每次绑定/remote 刷新**时都会执行（`forge_endpoints.go:403`）。若在其中无条件生成 token，用户每次切换项目或刷新 remote 都会静默换掉 token，破坏平台侧已配置的 webhook。

因此：

- `UpsertProjectForge` **保留已有 token**（仅在为空时生成）
- 轮换必须走独立端点 `POST /api/forge/webhook-token/rotate`，显式触发

**为什么按 binding 而非全局**：token 泄露时影响面限于单个 repo；且平台侧配置天然是 per-repo 的（GitHub 每个 repo 配一个 webhook）。

**已知不一致（F4）**：token 是 per-project_path，而降频 tier 是 per-repo。同一 repo 被两个 project 绑定时会各持一个 token（平台侧只能配一个 webhook，因此实际只有一个生效）。降频健康度因此**不放在这张表**，而按 repo 维度记录（见「轮询降频策略」）。

### 3. 签名校验链

校验与响应顺序（**F5 修正**：派发必须异步，不能内联在响应前）：

```
① 路径 token 匹配某 binding？        → 否则 401（不解析 body）
② 读取完整 body（限长 ≤1MB）          → 超限 413
③ 平台签名校验                       → 否则 401
④ 投递 ID 幂等入库                    → 重复则直接 200
⑤ 解析事件类型                       → 不关心的事件直接 200 丢弃
⑥ 事件去重入库（forge_events）        → 重复则直接 200
⑦ 立即返回 200                        ← 响应到此为止
⑧ 异步派发 sink.HandleChange（goroutine，独立 ctx）
```

**为什么 ⑦⑧ 必须分离**：sink 内含 IM 网络推送（`forge_notify.go:83-87`）。GitHub 要求 **10 秒内响应**，超时会标记投递失败并重试（导致重复投递）；GitLab 同样期望快速 2xx。轮询路径在自有 goroutine 中调用 sink 无此约束，但 webhook 是同步 HTTP 上下文，必须解耦。

**B6 修正 —— 异步派发的 context**：⑧ 的 goroutine **绝不能**用 `r.Context()` —— 它在 handler 返回的瞬间被取消，goroutine 会与取消竞争（`ForgeEventDispatcher.HandleChange` 虽忽略 ctx（`forge_notify.go:45`），但 `ForgeTaskTrigger.HandleChange` 及下游任务执行不保证）。

必须使用独立 context：

```go
// 派发必须在 handler 返回后仍能完成，因此不能用 r.Context()。
// 带超时避免 goroutine 泄漏（IM 推送 + 任务触发都可能有网络 I/O）。
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()
    syncer.DispatchChange(ctx, repo, item, change)
}()
```

**S8 修正 —— 路径 token 会经日志泄露**：`RequestLogger` 记录 `r.URL.Path`（`middleware/logger.go:53`）。由于 capability token **就是路径的一部分**，每次投递都会把凭证明文写进服务端日志。

两种缓解（择一）：
- **(a) 日志脱敏**：`RequestLogger` 对 `/api/forge/webhook/` 前缀的路径做脱敏（只留前缀）
- **(b) token 移到请求头**：端点改为 `POST /api/forge/webhook`，token 走 `X-Clawbench-Webhook-Token` 头

**建议 (a)** —— 平台侧 webhook URL 通常只允许配置一个 URL，路径带 token 是最自然的形态（与 `/api/share/{token}` 一致）；(b) 会要求平台额外配置自定义 header，GitHub 支持但 GitLab 也支持，只是多一步配置。

**任一校验失败统一返回 401**，不区分「token 错」/「签名错」，避免给攻击者信息。但**必须记日志**（含来源 IP、事件类型、**binding 标识但绝不含 token 或 secret**），便于排障；同时写入 `last_webhook_error` 供状态端点展示（S12）。

**④ 与 ⑥ 是两级去重**：④ 防平台重试同一投递，⑥ 防 webhook 与轮询对同一事件重复通知。详见「重放与幂等」。

#### 3.1 GitHub：HMAC-SHA256

请求头：
- `X-Hub-Signature-256: sha256=<hex>` — HMAC-SHA256，key 为 webhook secret
- `X-GitHub-Event: issues | pull_request | issue_comment | ...`
- `X-GitHub-Delivery: <uuid>` — 幂等键
- `Content-Type: application/json`

校验实现：

```go
// verifyGitHubSignature 校验 X-Hub-Signature-256。
// 注意：必须用 hmac.Equal（常量时间）而非 bytes.Equal，避免时序侧信道。
func verifyGitHubSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}
```

**关键实现约束**：
- 必须先 `io.ReadAll` 完整 body，再校验，再解析。**不能用 `json.NewDecoder(r.Body)` 直接解码** —— 那样 body 被消费，无法计算 HMAC。
- 读取需限长：`io.LimitReader(r.Body, maxWebhookBody+1)`，超限返回 413。
- `hmac.Equal` 而非 `bytes.Equal`：虽然 HMAC 比对对时序攻击的敏感性低于裸 token，但仍应用常量时间比较。

#### 3.2 GitLab：token 比对（**非 HMAC**）

GitLab 不使用 HMAC，而是把配置的 secret **明文**放在 `X-Gitlab-Token` 头里直接比对。这是平台设计，不是我们可以选择的。

```go
// verifyGitLabToken 校验 X-Gitlab-Token。
// GitLab 不做 HMAC，secret 以明文头传输 —— 因此必须：
//   1. 强制 HTTPS（明文 HTTP 下 token 可被中间人读取）
//   2. 用 subtle.ConstantTimeCompare 防时序攻击
func verifyGitLabToken(secret string, header string) bool {
	if secret == "" || header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(secret)) == 1
}
```

**安全含义（必须在文档和 UI 中显式告知）**：GitLab 的 token 明文传输意味着 **webhook 端点必须强制 HTTPS**。若用户通过明文 HTTP 暴露实例，GitLab token 会被中间人窃取。实现上：

- 检测 `r.TLS == nil` 且非 localhost → 拒绝 GitLab webhook 并返回明确错误
- UI 在 GitLab 配置区显示 HTTPS 强制提示

### 4. Secret 存储

**B5 修正 —— 作用域必须按 binding，不能按 host。**

原设计按 `(platform,host)` 存 secret，理由是「类比 `Credentials`」。这个类比**不成立**：

- 一个 PAT 天然跨该 host 的所有 repo → per-host 合理
- 一个 GitHub webhook secret 是**每个 webhook 一份**（即 per-repo）→ per-host 会让同 host 下第二个 repo 的 secret **静默覆盖**第一个

这与 §2 自己的论证「平台侧配置天然是 per-repo」直接矛盾。

修正后：**secret 与 token 同作用域**（即同一个 binding 标识），二者一一对应 —— 一个 webhook 配置 = 一个 URL（含 token）+ 一个 secret。

```go
// ForgeConfig 新增。key 是 binding key（platform|host|owner/repo），
// 与 project_forges.webhook_token 同一维度。
//
// 为什么不按 host：webhook secret 是 per-webhook（per-repo）的，
// 按 host 存会让同 host 下多个 repo 的 secret 互相覆盖。
type ForgeConfig struct {
    // ... 现有字段 ...
    WebhookSecrets map[string]string `yaml:"webhook_secrets"`
}
```

- 回传：`buildConfigForge` 只回传 `webhook_secret_bindings []string`（**沿用 `CredentialHosts` 的 write-only 模式**，`settings.go:500-521`）
- 权限：config.yaml 已是 `0o600`（`settings.go:1762`，已核实）
- 写入端点：`POST /api/forge/webhook-secret {binding, secret}` / `DELETE ?binding=`
- **secret 不入日志**：写入路径不得 `slog` secret 值；校验失败只记 binding 与来源 IP
- **secret 比较**：GitLab 用 `subtle.ConstantTimeCompare`；GitHub 用 `hmac.Equal`（见 3）

**secret 轮换语义**：用户在平台侧改 secret 后，ClawBench 侧必须同步改，否则校验持续失败（表现为 401 + 状态端点的 `last_webhook_error`）。UI 应提供「测试」按钮（发一个自签名请求到自己的端点验证链路）。

### 5. 事件映射：webhook 载荷 → `forge.Change`

这是**最需要仔细实现的部分**。目标是让 webhook 事件与轮询推导出的事件**在语义上完全等价**，从而复用同一条派发链路。

#### 5.1 GitHub 事件映射表

| Webhook 事件 | `action` | 映射为 `forge.EventType` | 备注 |
|---|---|---|---|
| `issues` | `opened` | `EventOpened` | |
| `issues` | `closed` | `EventClosed` | |
| `issues` | `reopened` | `EventReopened` | |
| `pull_request` | `opened` | `EventOpened` | |
| `pull_request` | `closed` + `merged=true` | `EventMerged` | **必须查 `merged` 字段** |
| `pull_request` | `closed` + `merged=false` | `EventClosed` | |
| `pull_request` | `reopened` | `EventReopened` | |
| `issue_comment` | `created` | `EventCommented` | `CommentBody` 取 `comment.body`；**itemType 见下** |
| `issue_comment` | `edited` | — | **不支持**：轮询会在 `updated_at` 前移时发 `commented`（`events.go:161-163`），webhook 侧映射 `edited` 会与之 key 相同（同 comment id）故**不会重复**，但延迟不对称。首版丢弃（S11） |
| `pull_request_review_comment` | `created` | — | **不支持**：id 空间与 issue comment 不同，且轮询不拉取，无法对齐去重键（见 5.1.1） |
| `workflow_run` | `completed` | `EventPipeline` | 需 F2 的 run-id revision 改造 |

#### 5.1.1 评论事件的 `itemType` 判定（F1 修正）

**这是去重正确性的关键**。GitHub 的 `issue_comment` 事件对 **PR 上的评论同样投递**，载荷结构与 issue 评论完全一致。若一律归类为 `ItemTypeIssue`，则：

- webhook 算出 key `...|issue|...|commented|comment:<id>`
- 轮询对 PR 评论算出 key `...|pr|...|commented|comment:<id>`
- 两者不同 → **同一评论通知两次**

判定规则：

```go
// GitHub: issue_comment 载荷的 issue.pull_request 字段非空即表示这是 PR 评论。
// 注意 issue.pull_request 是一个对象（含 url/html_url），不是布尔值。
itemType := forge.ItemTypeIssue
if payload.Issue != nil && payload.Issue.PullRequest != nil {
    itemType = forge.ItemTypeChangeRequest
}
```

对 `pull_request_review_comment`（PR diff 行内评论）：其 `comment.id` 与 issue comment **不属于同一 id 空间**，且轮询路径的 `ListComments` 只取 issue-level comments（GitHub 的 `Issues.ListComments` 不含 review comment）。因此：

- **首版不支持行内评论**（直接 200 丢弃），避免产生轮询永远无法对齐、因而永远无法去重的事件
- 若后续要支持，需同时扩展轮询路径的评论来源，否则去重不成立

#### 5.2 关键字段提取

```go
// 从 GitHub 载荷提取归一化字段。
// repository.full_name = "owner/repo"；sender.login = 施动者（防递归用）。
type githubWebhookPayload struct {
    Action     string `json:"action"`
    Repository struct {
        FullName string `json:"full_name"`
        HTMLURL  string `json:"html_url"`
    } `json:"repository"`
    Sender struct {
        Login string `json:"login"`
    } `json:"sender"`
    Issue *struct {
        Number  int    `json:"number"`
        Title   string `json:"title"`
        State   string `json:"state"`
        HTMLURL string `json:"html_url"`
        User    struct{ Login string `json:"login"` } `json:"user"`
        // PullRequest 非空表示这个 "issue" 其实是 PR —— issue_comment 事件
        // 对 PR 评论同样投递，必须据此判定 itemType（见 5.1.1）。
        // 注意它是对象而非布尔值。
        PullRequest *struct {
            URL     string `json:"url"`
            HTMLURL string `json:"html_url"`
        } `json:"pull_request"`
    } `json:"issue"`
    PullRequest *struct {
        Number   int    `json:"number"`
        Title    string `json:"title"`
        State    string `json:"state"`
        Merged   bool   `json:"merged"`
        HTMLURL  string `json:"html_url"`
        User     struct{ Login string `json:"login"` } `json:"user"`
    } `json:"pull_request"`
    Comment *struct {
        ID   int64  `json:"id"`
        Body string `json:"body"`
        User struct{ Login string `json:"login"` } `json:"user"`
    } `json:"comment"`
    // WorkflowRun 用于 CI 事件（F2 需要 run id 做 revision）。
    WorkflowRun *struct {
        ID         int64  `json:"id"`
        Status     string `json:"status"`
        Conclusion string `json:"conclusion"`
        HTMLURL    string `json:"html_url"`
    } `json:"workflow_run"`
}
```

**`sender.login` 必须映射到 `Change.Actor`** —— 这正是上一轮修复的防递归字段（`5673bd8cb`）。webhook 天然携带准确的施动者，比轮询（只能从评论列表推断）更可靠。

#### 5.3 GitLab 事件映射表

GitLab 用 `object_kind` + `object_attributes.action`：

| `object_kind` | `action` | 映射 | 备注 |
|---|---|---|---|
| `issue` | `open` | `EventOpened` | |
| `issue` | `close` | `EventClosed` | |
| `issue` | `reopen` | `EventReopened` | |
| `merge_request` | `open` | `EventOpened` | |
| `merge_request` | `merge` | `EventMerged` | |
| `merge_request` | `close` | `EventClosed` | |
| `merge_request` | `reopen` | `EventReopened` | |
| `note` | `create` | `EventCommented` | **必须判 `noteable_type` 且过滤 system**（见下） |
| `pipeline` | `success`/`failed` | `EventPipeline` | 需 F2 改造 |

GitLab 的 `project.path_with_namespace` = `owner/repo`（**注意多级 namespace**，如 `group/sub/proj`，解析需用 `SplitN(..., 2)` 而非 `Split("/")`）。

##### B1 修正 —— `note` 事件必须判 `noteable_type`

**这是 F1 在 GitLab 侧的同构问题**，第一轮评审只修了 GitHub 侧。

GitLab 对 **issue、MR、commit、snippet** 都投递 `object_kind:"note"`，靠 `object_attributes.noteable_type` 区分。若不判定：

- MR 评论被归为 `ItemTypeIssue` → key `...|issue|...|commented|comment:N`
- 轮询侧按资源分别拉取（`gitlab.go:142-147`），MR 评论归为 `ItemTypeChangeRequest` → key `...|pr|...`
- **两者不同 → 同一评论通知两次**

判定规则：

```go
// GitLab note 事件：noteable_type 决定它属于 issue 还是 MR。
// 其余值（Commit、Snippet 等）没有对应的轮询路径，直接丢弃。
switch payload.ObjectAttributes.NoteableType {
case "Issue":
    itemType = forge.ItemTypeIssue
case "MergeRequest":
    itemType = forge.ItemTypeChangeRequest
default:
    return nil, errIgnoreEvent
}
```

##### B2 修正 —— 必须过滤 system notes

轮询侧**明确过滤**平台自动生成的 system notes（`gitlab.go:161-167`，如 "changed the description"、"added label X"）。但 GitLab **仍会把这些作为 `note` 事件投递**。

若不过滤：

- webhook 产生 `commented` 事件（`system:false` 之外的所有 note）
- 轮询永远**不会**推导出对应事件（它被过滤了）
- 结果：**单向永久噪声** —— 每次改标签/改描述都触发一次通知，且与轮询去重键无法对齐

因此：

```go
// 平台自动生成的 system note 必须丢弃：轮询侧已过滤它们
// （gitlab.go:161-167），不过滤会产生轮询永远无法推导的事件。
if payload.ObjectAttributes.System {
    return nil, errIgnoreEvent
}
```

**这是对「去重键一致性是唯一支柱」的直接呼应**：凡是轮询不产生的事件，webhook 也不能产生，否则该事件永久无法去重。

#### 5.4 去重键一致性（**正确性的唯一支柱**）

webhook 与轮询必须对同一事件算出**相同** `DedupeKey`，否则会重复通知。这是本方案**正确性所依赖的唯一机制** —— webhook 不写 `forge_items`、不推进水位线，无法像轮询那样靠快照自愈。

`forge.DedupeKey(repo, itemType, number, change)` 的四个输入，逐项核对：

| 输入 | webhook 能否与轮询对齐 | 风险 |
|---|---|---|
| `repo` | ✅ 载荷 `repository.full_name` | 多级 namespace 需 `SplitN(2)` |
| `itemType` | ⚠️ **需显式判定** | **F1**：`issue_comment` 对 PR 评论也投递，须查 `issue.pull_request` |
| `number` | ⚠️ CI 事件无 number | **F2**：见下 |
| `change.Type` | ✅ 映射表 | |
| `change.NewState` | ⚠️ **必须复用同一归一化** | GitHub merged PR：载荷 `state=closed`+`merged=true`，轮询归一化为 `merged` |

**F1 修正**（已并入 5.1.1）：评论事件按 `issue.pull_request` 判定 itemType，否则同一评论会产生 `...|issue|...` 与 `...|pr|...` 两个 key。

**F2 修正 —— CI 事件的 revision 冲突**：`DedupeKey` 对非评论事件的 revision 是 `state:<NewState>`（`events.go:179-180`）。CI 事件的 `NewState` 若定义为 `success`，则**同一 repo 上两次成功的 CI 会算出完全相同的 key**，第二次被 `ON CONFLICT DO NOTHING` 静默丢弃。

因此 `DedupeKey` 需为 `EventPipeline` 增加 run-id revision：

```go
switch ev.Type {
case EventCommented:
    revision = fmt.Sprintf("comment:%d", ev.CommentID)
case EventPipeline:
    // CI 事件没有 item number；用 run id 区分不同次执行，否则第二次
    // 成功的 CI 会与第一次撞 key 而被静默丢弃。
    revision = fmt.Sprintf("run:%s", ev.PipelineRunID)
default:
    revision = fmt.Sprintf("state:%s", ev.NewState)
}
```

同时 `Change` 需新增 `PipelineRunID string`，`number` 对 CI 事件传 0（并接受 key 中含 `|0|`）。这是对 `internal/forge/events.go` 的改动，**属于本方案范围**。

**实现约束（评审指出此处不可执行，需精确化）**：两个平台的归一化函数**签名不同**，不能合并成一份：

```go
// internal/forge/github/github.go（现为包私有，需导出）
// 现签名：func normalizeState(state string, merged bool) forge.State
func NormalizeState(state string, merged bool) forge.State

// internal/forge/gitlab/gitlab.go（现为包私有，需导出）
// 现签名：func normalizeState(state string) forge.State
// GitLab 把 merged 直接放在 state 里，所以不需要第二个参数
func NormalizeState(state string) forge.State
```

两者**同名不同包**（`github.NormalizeState` / `gitlab.NormalizeState`），webhook 映射层按平台调用对应包，**不要**在 `internal/forge` 里再造第三份。若强行统一成一个签名，就必须引入「merged 是参数还是 state 的一部分」的分支判断，反而更容易漂移。

#### 5.5 派发注入点（F3 修正）

原设计称「sink 层零改动」，但 `sink` 字段未导出（`forge_syncer.go:31`），`ForgeSyncer` 的导出方法只有 `SyncRepo`/`SyncRepoWithOptions`。handler **无法直接触达派发链路**。

**步骤 1 —— 抽出共用的 persist→dispatch**（避免两份实现漂移）。`processItem` 现有逻辑（`forge_syncer.go:274-293`）抽成私有方法：

```go
// persistAndDispatch 是唯一的 persist→dispatch 路径，轮询与 webhook 共用。
// 返回 (fresh, error)：fresh=false 表示事件已存在（被去重）。
func (s *ForgeSyncer) persistAndDispatch(
    ctx context.Context, repoRef ForgeRepoRef, remote forge.Remote,
    typ forge.ItemType, item forge.Item, change forge.Change,
) (bool, error)
```

`processItem` 改为调用它；新增的导出方法也调用它。

**步骤 2 —— 新增导出方法**：

```go
// DispatchChange persists an externally-sourced event (e.g. from a webhook)
// and dispatches it to the sink if fresh. It reuses the same persist→dispatch
// path as the poller, so notification/unread/task semantics are identical.
//
// Returns false when the event was already seen (deduped), which the webhook
// handler treats as success.
func (s *ForgeSyncer) DispatchChange(
    ctx context.Context, repo ForgeRepoRef, item forge.Item, change forge.Change,
) (bool, error) {
    remote := forge.Remote{  // 与 SyncRepoWithOptions:73-78 相同构造
        Platform: forge.Platform(repo.Platform),
        Host:     repo.Host,
        Owner:    repo.Owner,
        Repo:     repo.Repo,
    }
    return s.persistAndDispatch(ctx, repo, remote, item.Type, item, change)
}
```

*（原文档此处调用了不存在的 `remoteFromRef` 辅助函数 —— 已改为内联构造，与 `SyncRepoWithOptions` 保持一致。）*

**步骤 3 —— 注入到 handler**。`forge_endpoints.go` 目前没有 DI 变量可参照，需新建。最小方案是在 service 包暴露一个访问器（与 `GlobalScheduler` 同模式）：

```go
// internal/service/forge_syncer.go
var globalForgeSyncer *ForgeSyncer

// SetGlobalForgeSyncer records the syncer so the webhook handler can dispatch
// through the same instance the poller uses (same sink, same dedupe).
func SetGlobalForgeSyncer(s *ForgeSyncer) { globalForgeSyncer = s }

func GlobalForgeSyncer() *ForgeSyncer { return globalForgeSyncer }
```

`main.go` 在构造 poller 的同一处（`:972-973` 附近）调用 `SetGlobalForgeSyncer(syncer)`；handler 通过 `service.GlobalForgeSyncer()` 取用。**必须与 poller 是同一实例**，否则 sink 不一致（webhook 派发的事件不会触发通知/任务）。

*（备选：把 syncer 作为参数显式传给 handler 构造函数。但当前 handler 全是包级函数 + `register` 闭包，没有构造函数，改造面更大。故取全局访问器。）*
        s.sink.HandleChange(ctx, repo, item, change)
    }
    return fresh, nil
}
```

### 6. 重放与幂等

- **幂等键**：`X-GitHub-Delivery`（GitHub）/ `X-Gitlab-Event-UUID`（GitLab）。webhook 的 delivery id 与轮询的语义 key 不同，需**两级去重**：
  - 一级：delivery id 表（防平台重试同一投递）→ 新增 `forge_webhook_deliveries`
  - 二级：`forge_events.dedupe_key`（防 webhook 与轮询对同一事件重复通知）
- **时间戳窗口**：GitHub 载荷无签名时间戳，无法做严格时间窗；依赖 delivery id 幂等 + HMAC 已足够。GitLab 同理。
- **清理**：delivery 表按 `received_at` 定期剪枝（复用 poller 的剪枝周期，保留 7 天）。

**S9 修正 —— delivery 主键不能只用 delivery_id**：

```sql
-- 错误：delivery_id 全局唯一会让「两个 binding 共享同一 repo」时只有一个能处理
-- CREATE TABLE forge_webhook_deliveries (delivery_id TEXT PRIMARY KEY, ...);

-- 正确：按 binding 隔离，两个 binding 各自处理自己的投递
CREATE TABLE IF NOT EXISTS forge_webhook_deliveries (
    delivery_id TEXT NOT NULL,
    binding_key TEXT NOT NULL,   -- platform|host|owner/repo，与 webhook_token 同维度
    received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (delivery_id, binding_key)
);
CREATE INDEX IF NOT EXISTS idx_forge_webhook_deliveries_age
    ON forge_webhook_deliveries(received_at);
```

这同时**解决了开放问题 #3**：token/secret/delivery 全部统一到 binding 维度，语义一致。

### 6.1 乱序与陈旧投递（评审指出的未处理项）

去重键是**内容寻址**的，不含时间。因此：

- 延迟到达的 `opened` 若晚于 `closed`，两个通知都会发出且顺序颠倒 —— 轮询的快照 diff 会把这段区间**折叠为终态**（`merged > reopened > closed`），webhook 不会
- 这是 webhook 与轮询的**语义差异**，不是 bug，但会造成「先收到关闭、后收到新建」的困惑

缓解（**首版接受，不实现**）：事件 payload 携带平台时间戳，前端按时间排序展示。若后续要严格对齐，需引入投递时间窗 + 丢弃过旧事件，但那会牺牲「最终一致」的保证。

**S10 修正 —— 去重键对「重复的相同状态转移」非单射**：`DedupeKey` 用 `state:<NewState>`（`events.go:179-180`），因此 close → reopen → close 会产出**两次相同的 `...|closed|state:closed`**，第二次被 `ON CONFLICT DO NOTHING` 丢弃。

这是**轮询侧既有行为**（非本方案引入），但实时 webhook 会让它更明显。原文档「内容寻址的去重键天然处理重复投递」的表述**过强**，准确说法是：

> 去重键能正确处理**重复投递同一事件**，但**不能区分同一 item 上重复出现的相同终态**（如第二次 close）。后者需要时间戳或序号参与，本方案不引入。

**已知取舍**：宁可漏掉「重复的相同状态转移」，也不引入时间戳导致跨路径去重失效 —— 保持 webhook/轮询 key 严格一致优先。

### 7. 轮询降频策略

**核心问题**：webhook 生效后，轮询继续以原频率运行会浪费配额（webhook 已覆盖的事件不必再拉）。但**不能停**（会丢投递时无法自愈）。

方案：**按 repo 记录 webhook 健康度，动态调整轮询间隔**。

```
状态机（每个 repo 独立）：
  ACTIVE   —— 最近 24h 内收到过有效 webhook
              → 轮询降为低频对账（状态 15min / 评论 30min）
  FALLBACK —— 24h 内无 webhook
              → 恢复默认频率（状态 60s / 评论 5min）
```

实现要点（**F4 修正**：健康度必须按 repo 记录，不能放 `project_forges`）：

1. **存到已有的 `forge_sync_state`**（`forge_sync.go:222-233`，主键 `(platform,host,owner,repo)`）—— 该表已是 repo 维度，天然正确。新增列 `last_webhook_at DATETIME`。
   *为什么不放 `project_forges`*：那张表是 per-project_path，而 `ListProjectForges` 按 `updated_at DESC` 排序、`syncAll` 每个 repo 只取第一行（`forge_poller.go:135-141`）。同一 repo 绑定两个 project 时，webhook 更新 A 行而 poller 读 B 行 → **该 repo 永远进不了 ACTIVE，降频永不生效**。
2. **B3 修正 —— 需要新增 ticker 或改用 due-time 调度。** 现有结构只有 `stateEvery=60s` / `commentEvery=5min` 两个 ticker，且启动时还有第三次 `syncAll` 调用（`forge_poller.go:102`）。**不存在 15min ticker**，所以原方案「低频 ticker 处理所有 repo」无法直接实现（在 60s ticker 上跳过 ACTIVE repo 得到的是 5min，不是 15min）。

   两条可选路径：
   - **(a) 新增对账 ticker**：加 `reconcileEvery = 15min`，只在该 ticker 上处理 ACTIVE repo。改动小，但多一个 ticker。
   - **(b) per-repo due-time 调度**：在 `forge_sync_state` 存 `next_due_at`，每个 tick 只处理到期的 repo。更灵活（可做自适应降频），但需要重构 `syncAll` 的遍历逻辑。

   **建议 (a)** —— 与本方案的其他部分正交，且不触碰 `syncAll` 的核心遍历。

   **启动 pass 的归类**（`forge_poller.go:102`）：必须归为**全量对账**（处理所有 repo，`IncludeComments=true`），保证进程重启后 ACTIVE repo 也做一次完整对账。

3. **B4 修正 —— 低频对账 pass 必须 `IncludeComments=true`。** `IncludeComments=true` 的 pass 是**唯一**拉评论的 pass，也是**唯一**执行快照剪枝的 pass（`forge_poller.go:170-172`）。若 ACTIVE repo 只被 state-only pass 覆盖，其评论将**永久停更**、快照也永不剪枝。

   因此对账 pass 的 `SyncOptions` 必须是 `{IncludeComments: true}`。

4. 未配置 webhook 的 repo（`webhook_token == ''`）永远走 FALLBACK，**行为与现在完全一致**。

**为什么用「降频」而非「暂停」**：webhook 投递失败是静默的（平台侧会标记为 failed delivery，但 ClawBench 无从得知）。保留低频对账可在 webhook 静默失效时自动恢复。

**S7 修正 —— 配额算术**：原引用的「~130 调用/hr/repo」来自父 spec 的诚实预算，其中**含 ~60/hr 的假设 CI 轮询**（spec:271），而本文档自己说 CI 轮询不存在（见下文）。真实基线：

```
状态 60s  → 60/hr
评论 5min → 12/hr
合计        72/hr/repo
```

降频后（状态 15min → 4/hr；评论 30min → 2/hr）= **6/hr**，降幅约 **12x**（非 20x）。

单实例可支撑 repo 数：GitHub 认证限额 5000/hr ÷ 72 ≈ **69 个 repo**（非 38）。降频后 5000 ÷ 6 ≈ **830 个**。

**24h/15min/30min 的依据**：24h 是「一天内没有 webhook 就认为链路失效」的保守阈值（覆盖夜间无人提交、周末等正常静默期）；15min 对账是「即使 webhook 全失效，事件最多延迟 15min」的可接受上限，同时把配额压到基线的 ~1/12。这三个数字是**工程取舍**，非推导结果，应可配置。

**静默失效的可观测性**：当前设计下，webhook 停止投递后最长 24h 才会回到 FALLBACK，期间用户无从知晓。需补（含 S12）：
- 状态端点返回每个 repo 的 tier + `last_webhook_at` + **`last_webhook_error`**（S12：401 统一化后，secret 配错只表现为一行日志，运营侧需要可见信号）
- UI 在 ACTIVE repo 上显示「最近 webhook：X 前」；超过 24h 自动转 FALLBACK 时记 `slog.Warn`
- 平台侧 failed delivery 无法感知，因此**不做**「webhook 已失效」的主动告警，只呈现事实

## 附带收益：`pipeline_done` 可达（**需先做 F2 改造**）

`pipeline_done` 是当前唯一「UI 可勾选但永不触发」的事件类型（已确认无 CI 拉取实现）。

**webhook 能补上这个缺口，但原设计过于乐观**：GitHub 的 `workflow_run` / GitLab 的 `pipeline` 原生推送 CI 完成状态，确实**无需实现 CI 轮询**（那需要扩展 `Provider` 接口 + 两个平台的 CI 端点 + 配额预算）。

但评审指出两处**必须先解决**：

1. **sink 契约是 item 中心的**：`HandleChange(ctx, repo, item, change)`（`forge_syncer.go:23`）需要一个 `forge.Item`。CI 事件没有对应的 issue/PR 条目 —— 需构造一个最小 item（仅填 repo/URL/状态）或调整契约。
2. **`DedupeKey` 需要 number，CI 事件没有**：push 触发的 workflow run 无 item number，传 0 会导致**同一 repo 上两次成功的 CI 撞 key**（见 F2 修正）。必须为 `EventPipeline` 加 run-id revision。

因此 `pipeline_done` 的可达性**依赖 F2 的 `DedupeKey` 改造**，不是白送的。

**范围建议**：把 CI 事件支持拆成独立阶段（W6），本方案先交付 issue/PR/评论事件。理由是 CI 事件需要额外的契约调整，且 `workflow_run` 载荷结构与 issue 事件差异较大，混在一起会拖慢主路径。

若不实施本方案 → 应把 `pipeline_done` 从 UI 和 `validForgeEventTypes` 移除，避免用户勾选无效项。

## 安全考量

| 风险 | 缓解 |
|---|---|
| token 泄露 | per-binding token（影响面限于单 repo）+ 一键轮换 |
| GitLab token 明文传输 | 强制 HTTPS（非 localhost 明文直接拒绝）+ UI 提示 |
| 时序攻击 | `hmac.Equal` / `subtle.ConstantTimeCompare` |
| 重放 | delivery id 幂等表 |
| DoS（伪造大量请求） | body 限长 1MB + 路径 token 前置校验（未匹配立即 401，不解析 body） |
| SSRF（载荷含 URL 诱导请求） | 载荷中的 URL **仅作为字符串存储/展示，绝不主动请求** |
| 错误信息泄露 | 所有校验失败统一返回 401，不区分「token 错」/「签名错」 |
| 事件伪造触发 AI 任务 | 签名校验失败即拒绝；`sender.login` 防递归照常生效 |

## 测试面

**单元测试**（`internal/forge/webhook_test.go` + `internal/forge/webhook_sign_test.go`）：

| 测试名 | 覆盖 |
|---|---|
| `TestVerifyGitHubSignature_Valid` | 正确签名通过 |
| `TestVerifyGitHubSignature_TamperedBody` | 篡改 body 失败 |
| `TestVerifyGitHubSignature_MalformedHeader` | 缺前缀 / 非法 hex 失败 |
| `TestVerifyGitLabToken_ValidAndInvalid` | 正确/错误/空 secret |
| `TestMapGitHubEvent_TableDriven` | 各事件类型 → 期望 `forge.Change` |
| `TestMapGitHubEvent_MergedPullRequest` | `closed`+`merged=true` → `EventMerged`，`NewState="merged"` |
| `TestMapGitHubEvent_IssueCommentOnPRIsChangeRequest` | **F1**：`issue.pull_request != nil` → `ItemTypeChangeRequest` |
| `TestMapGitLabEvent_NoteableType` | **B1**：`NoteableType` 决定 itemType；`Commit`/`Snippet` 被丢弃 |
| `TestMapGitLabEvent_SystemNoteIgnored` | **B2**：`system=true` 被丢弃 |
| `TestMapEvent_RejectsUnsupported` | 未映射事件返回忽略信号 |
| `TestDedupeKey_WebhookMatchesPolling` | **核心**：对每种事件，webhook 路径与轮询路径产出相同 key |
| `TestDedupeKey_PipelineRunID` | **F2**：不同 run id 不撞 key |
| `TestParseGitLabNamespace_MultiLevel` | `group/sub/proj` 正确拆分 |
| `TestPayloadSizeLimit` | 超 1MB 返回 413 |

**集成测试**（`internal/handler/forge_webhook_test.go`）：

| 测试名 | 覆盖 |
|---|---|
| `TestServeForgeWebhook_EndToEnd` | 签名正确 → 入库 + 派发 + 通知 |
| `TestServeForgeWebhook_BadSignatureNoPersist` | 401 且**不入库、不派发** |
| `TestServeForgeWebhook_UnknownToken` | 401 |
| `TestServeForgeWebhook_GitLabPlaintextRejected` | 明文 HTTP 拒绝 |
| `TestServeForgeWebhook_DuplicateDelivery` | 同 delivery id 二次投递被吞 |
| `TestServeForgeWebhook_RespondsBeforeDispatch` | **F5**：慢 sink 不阻塞响应（用阻塞 sink 断言 200 先返回） |
| `TestServeForgeWebhook_DispatchUsesBackgroundContext` | **B6**：handler 返回后派发仍完成 |
| `TestServeForgeWebhook_TokenNotInLog` | **S8**：日志中无 token |

**回归测试**（`internal/service/forge_poller_test.go`）：

| 测试名 | 覆盖 |
|---|---|
| `TestForgePoller_WebhookActiveRepoDowngraded` | ACTIVE repo 在 60s tick 被跳过 |
| `TestForgePoller_ActiveRepoStillSyncsComments` | **B4**：对账 pass 含 `IncludeComments=true` |
| `TestForgePoller_NoWebhookUnchanged` | 未配 webhook 的 repo 行为不变 |
| `TestForgePoller_TierExpiresAfterWindow` | 超 24h 无 webhook 回 FALLBACK |
| `TestForgeSyncer_WebhookThenPollNoDoubleNotify` | webhook 后轮询同一事件**不重复通知** |

## 实施细节（补评审指出的可执行性缺口）

### 迁移条目（`internal/service/database.go`）

挂到现有 `pragma_table_info` 守卫式 ALTER 列表（`:619-706`）：

```go
{"project_forges", "webhook_token", "ALTER TABLE project_forges ADD COLUMN webhook_token TEXT NOT NULL DEFAULT ''"},
{"forge_sync_state", "last_webhook_at", "ALTER TABLE forge_sync_state ADD COLUMN last_webhook_at DATETIME"},
{"forge_sync_state", "last_webhook_error", "ALTER TABLE forge_sync_state ADD COLUMN last_webhook_error TEXT NOT NULL DEFAULT ''"},
```

新表 DDL 常量加入 `:603-607` 的 slice：

```go
const ForgeWebhookDeliveriesDDL = `
CREATE TABLE IF NOT EXISTS forge_webhook_deliveries (
    delivery_id TEXT NOT NULL,
    binding_key TEXT NOT NULL,
    received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (delivery_id, binding_key)
);
CREATE INDEX IF NOT EXISTS idx_forge_webhook_deliveries_age
    ON forge_webhook_deliveries(received_at);
`
```

唯一索引（token 查 binding）：

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_forges_webhook_token
    ON project_forges(webhook_token) WHERE webhook_token != '';
```

### 需要新增的访问器

`GetForgeSyncWatermark` 只 SELECT `watermark`（`forge_sync.go:206`），需新增兄弟函数：

```go
// MarkForgeWebhookSeen records that a valid webhook was received for this repo,
// used by the poller to decide the repo's frequency tier.
func MarkForgeWebhookSeen(repo ForgeRepoKey, at time.Time) error

// SetForgeWebhookError records the last webhook failure for observability (S12).
func SetForgeWebhookError(repo ForgeRepoKey, msg string) error

// GetForgeWebhookHealth returns last_webhook_at + last_webhook_error.
func GetForgeWebhookHealth(repo ForgeRepoKey) (time.Time, string, error)
```

### 路由注册（`internal/handler/handler.go`）

紧邻 share 的公开路由（`:297-298`）：

```go
// forge webhook 是公开端点：forge 平台无法携带 cookie，鉴权靠路径 token +
// 平台签名。与 /api/share/{token} 同属「capability token 即凭证」模式。
// 日志中间件对该前缀脱敏（S8）。
register("/api/forge/webhook/", ServeForgeWebhook)
```

### 前端需要的状态字段

状态端点（`GET /api/forge/webhook/status`）每个 binding 返回：

```json
{
  "binding": "github|github.com|acme/widgets",
  "hasToken": true,
  "url": "https://<host>/api/forge/webhook/<token>",
  "secretConfigured": true,
  "lastWebhookAt": "2026-09-12T10:00:00Z",
  "lastWebhookError": "",
  "tier": "active"
}
```

`url` 直接给出可复制到平台侧的完整地址（用户无需手工拼接）。**注意**：该端点需鉴权（会回传 token）。

## 影响文件

| 层 | 文件 | 变更 |
|---|---|---|
| 新 | `internal/forge/webhook.go` | 载荷类型 + 事件映射（纯函数，可测） |
| 新 | `internal/forge/webhook_sign.go` | 签名校验（GitHub HMAC / GitLab token） |
| 新 | `internal/handler/forge_webhook.go` | HTTP 端点、校验链、异步派发 |
| 新 | `internal/handler/forge_webhook_test.go` | 上述测试 |
| 改 | `internal/forge/events.go` | **F2**：`Change` 加 `PipelineRunID`；`DedupeKey` 加 `EventPipeline` 分支 |
| 改 | `internal/forge/github/github.go` | 导出平台状态归一化供 webhook 复用（注意与 gitlab 签名不同） |
| 改 | `internal/forge/gitlab/gitlab.go` | 同上 |
| 改 | `internal/service/forge_syncer.go` | **F3**：新增导出 `DispatchChange`；抽出共用的 persist→dispatch |
| 改 | `internal/service/project_forges.go` | 表加 `webhook_token`；**F6** 保留已有 token |
| 改 | `internal/service/forge_sync.go` | **F4**：`forge_sync_state` 加 `last_webhook_at`；delivery 表剪枝 |
| 改 | `internal/service/database.go` | 迁移：挂到 `pragma_table_info` 守卫列表（`:619-706`）；新表 DDL 加入 `:603-607` 的 slice |
| 改 | `internal/service/forge_poller.go` | **F4**：按 repo 维度 `last_webhook_at` 分级降频 |
| 改 | `internal/model/config.go` | `ForgeConfig.WebhookSecrets` |
| 改 | `internal/handler/settings.go` | `buildConfigForge` 回传 `webhook_secret_bindings`（write-only，按 binding） |
| 改 | `internal/handler/handler.go` | 注册公开路由（紧邻 `/api/share/`） |
| 改 | `internal/handler/forge_endpoints.go` | 暴露 token / 轮换 / 状态端点 |
| 改 | `cmd/server/main.go` | 把 `*ForgeSyncer` 注入 webhook handler |
| 改 | `web/src/components/forge/*` | webhook 配置 UI（token 展示、轮换、HTTPS 提示、tier 状态） |
| 改 | i18n | 新增文案 |

## 阶段划分

| 阶段 | 内容 | 可独立验证 |
|---|---|---|
| W0 | **F2 前置改造**：`Change.PipelineRunID` + `DedupeKey` 分支（若不做 CI 事件可跳过） | ✅ 单测 |
| W1 | 签名校验 + 载荷映射（纯函数 + 单测，无 HTTP） | ✅ 单测 |
| W2 | HTTP 端点 + token/secret 存储 + 校验链 + 异步派发（含 F3 注入点） | ✅ 集成测试 |
| W3 | 两级去重（delivery 表，按 binding 隔离）+ 重放防护 | ✅ 回归测试 |
| W4 | **B3/B4**：新增对账 ticker（或 due-time 调度）+ repo 维度健康度 + 状态端点 | ✅ 回归测试 |
| W5 | 前端配置 UI + 公网可达性提示 + tier 状态展示 | ✅ 手测 |
| W6 | （可选）CI 事件支持：`workflow_run`/`pipeline` 映射 + 最小 item 构造 | ✅ 单测 |

**W1 可与 W2 并行**；W4 依赖 W2（需 `last_webhook_at` 被写入）；W6 依赖 W0 且需先解决 sink 契约的 item 中心性。

## 已接受的限制

1. **需公网可达** —— 局域网/纯 localhost 部署无法使用，只能继续轮询。
2. **不做 webhook 自动注册** —— 用户需在 GitHub/GitLab 侧手动配置 webhook URL 和 secret（自动注册需 OAuth App / admin 权限，超出当前只读定位）。
3. **GitLab token 明文传输** —— 平台设计所限，只能靠强制 HTTPS 缓解。
4. **降频不等于停轮询** —— 保留 1/12 频率对账（约 6 次/hr vs 基线 72 次/hr），以应对 webhook 静默失效。
5. **不做 webhook 事件的全类型覆盖** —— 仅映射与现有 `EventType` 对应的子集，其余事件直接 200 丢弃（避免无意义入库）。
6. **不处理 `push` 事件** —— 与现有事件模型无关，明确忽略。
7. **不支持 PR 行内评论**（`pull_request_review_comment`）—— 其 id 空间与 issue comment 不同，且轮询路径不拉取它们，无法对齐去重键。首版直接丢弃（见 5.1.1）。
8. **不支持评论编辑**（`edited`）—— 轮询会在 `updated_at` 前移时发 `commented`，webhook 只映射 `created`，延迟不对称但不会重复（S11）。
9. **乱序投递不修正** —— 去重键内容寻址、不含时间，延迟到达的旧事件仍会通知。宁可乱序/重复也不丢事件（见 6.1）。
10. **重复的相同状态转移会漏** —— close→reopen→close 的第二次 close 与第一次撞 key 被丢弃，这是轮询侧既有行为（S10）。

## 待评审确认的开放问题

1. **公网可达性**（**最高优先，决定是否实施**）：用户的实际部署形态是哪种？决定本方案是否立即实施，还是先补 FRP HTTP 隧道。
2. **降频参数**：24h 健康窗口 + 15min 对账频率是否合理？已按 S7 重算为 12x 降幅，数值本身是工程取舍，建议做成可配置。
3. **W4 实现路径**：新增对账 ticker（建议，改动小）vs per-repo due-time 调度（更灵活，需重构 `syncAll`）。评审指出原方案在当前 ticker 结构下不可实现（B3）。
4. **是否纳入 CI 事件（W6）**：CI 事件能补上 `pipeline_done` 缺口，但需 sink 契约调整 + W0 改造。建议拆为独立阶段。
