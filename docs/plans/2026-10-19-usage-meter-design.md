# 用量提醒（Usage Meter）设计

日期：2026-10-19
状态：已实现（2026-10-19）

## 背景与问题

ClawBench 已经有完整的用量账本（`chat_metadata`）和统计面板（`/api/usage/stats`），但两者都是**事后查看**：用户必须先想到去翻统计，才知道自己这个周期花了多少。

用户需求：设一个目标用量，系统按时间进度算出「此刻最多该用多少」，与实际用量对比，超出时提醒。

关键约束（用户明确）：

- 一条规则：周期 + 指标 + 额度
- 指标三选一：token / credit / 费用
- **只做通知**，不做硬拦截
- header 上可视化，移动端不展示
- 滚动周期：系统算出「到目前为止最多应该用多少」，与实际对比并图形化

## 决策汇总

| 维度 | 决策 |
|---|---|
| 规则条数 | **只支持一条**（全局唯一） |
| 统计范围 | **全局合计**（所有项目），与当前停在哪个项目无关 |
| 周期 | 用户指定起始日期 + 周期天数，按天滚动 |
| 基准线 | **线性匀速**，按整天向下取整 |
| 触发 | 越过基准线一次 + 触顶一次，每周期最多两条 |
| 同时成立 | 越线与触顶同时成立时**只发触顶** |
| 规则变更 | 视为新规则，清除已提醒状态，允许再提醒 |
| 开关 | **额度为空即关闭**，无独立开关 |
| 存储 | config.yaml 的 `usage_meter` 段 |
| 计算方 | 后端定时 worker（5 分钟）+ 回合结束事件触发 |
| 推送 | 通知中心 + 原生通知 + IM 三条都走 |
| 前台判断 | 服务端**不判断**，一律记录 + 推送；客户端沿用既有的「可见且有焦点则不弹系统通知」规则 |
| 时区 | **服务端本地时区**（与 cron 一致） |
| 接口 | 新增 `GET /api/usage/meter`；规则走 `GET/PATCH /api/config` |
| header 显隐 | 仅 `isWideScreen`（≥960 CSS px） |
| 数字格式 | 紧凑缩写（K/M/B）+ `tabular-nums` + 固定宽度 |
| 设置入口 | 通知与安全 → 用量提醒 |
| 通知类型 | 新增 `usage` kind，点击打开用量浮层 |

## 明确不做

### 不做硬拦截

额度是**提醒线**，不是闸门。超出后 AI 照常运行，系统只在越线/触顶各提醒一次，之后本周期沉默。

理由：用户原话是「只做通知」。一旦引入拦截，就要回答「拦截时怎么给用户放行」「任务/群聊/排队消息怎么算」等一串问题，而这些问题与「让用户知道花快了」这个目标无关。

### 不做多规则

一条规则同时覆盖短期和长期需求是做不到的（7 天 500 万 token + 30 天 20 美元 需要两条）。但：

- header 只放得下一个胶囊，多条规则必须点开看列表
- 通知必须说明「是哪条超了」，文案复杂度翻倍
- 绝大多数用户只有一种货币、一种主要消耗方式

若未来确有多规则需求，`usage_meter` 从 object 改成 array 是一次向后兼容的 YAML 变更（旧单对象按一条规则读取）。

### 不做按项目统计

预算的本质是「我这个月总共花多少」，与停在哪个项目无关。按项目统计会导致切项目换一套额度，用户很难建立稳定预期。

### 不做汇率换算

`chat_metadata.cost_usd` 只有一列，但 ACP 的 `usage_update.cost` 带 `currency` 字段。非 USD 的上报值**按原值累加**，UI 明确标注「按上报值合计，未做汇率换算」。

理由：汇率需要外部数据源和缓存策略，且历史数据的汇率无法回溯（当时的汇率与今天的汇率不同，重算会改变已发生的事实）。宁可标注不确定，也不做会变的换算。

### 不做每日明细表

不新增「每日用量」表。累计曲线由 `chat_metadata` 按 `date(created_at)` 聚合得出（已有 `idx_chat_metadata_created` 索引）。

## 核心公式

### 周期定位

所有边界按**服务端本地时区**计算，起始日期解释为该时区的 00:00。

```
anchor      = anchor_date 在当地时区的 00:00（绝对时刻）
elapsedDays = floor((now - anchor) / 24h)          // 已完整过去的天数
dayIndex    = elapsedDays + 1                       // 第 1 天起
periodIndex = floor(elapsedDays / period_days)      // 第几轮（0 起）
cycleStart  = anchor + periodIndex * period_days * 24h
dayInCycle  = elapsedDays - periodIndex * period_days + 1   // 1..period_days
```

**注意**：用 `floor(时长/24h)` 而不是 `date()` 差值。DST 切换日只有 23 或 25 小时，用日历日期差会在切换日跳变；用固定 24h 时长则周期长度恒定，代价是 DST 切换后当天的边界相对当地零点偏移 1 小时。选择后者：**周期长度必须恒定**，否则「第 10 天应花 1/3」这句话在某些日子会不成立。

### 基准线与实际

```
expected = quota * dayInCycle / period_days
actual   = SUM(metric) over [cycleStart, now)
```

- 第 1 天：`expected = quota / period_days`（**不是 0**，避免开局误报）
- 最后一天：`expected = quota`
- 用量取 `[cycleStart, now)`，上界为 `now` 而非 `cycleEnd`，所以周期内实时增长

### 三个指标的口径

| metric | SQL | 单位 |
|---|---|---|
| `tokens` | `COALESCE(SUM(CASE WHEN total_tokens > 0 THEN total_tokens ELSE input_tokens + output_tokens END), 0)` | token |
| `credit` | `COALESCE(SUM(credit), 0)` | credit |
| `cost` | `COALESCE(SUM(cost_usd), 0)` | USD |

`tokens` 的表达式**必须与 `usage_stats.go` 的 `usageTotalSumExpr` 完全一致**（含 `total_tokens = 0` 时回退到 `input + output` 的兜底），否则 header 的数字会和用量面板的「总计」列对不上。

## 数据模型

### config.yaml

```yaml
usage_meter:
  anchor_date: "2026-10-05"   # 周期起始日期（当地时区）
  period_days: 30             # 周期天数，1..366
  metric: "cost"              # tokens | credit | cost
  quota: 20.0                 # 额度；0 或缺失 = 关闭
  # 内部状态，不暴露给前端设置页
  notified_cycle_start: ""    # 上次提醒对应的 cycleStart（RFC3339），用于「新一轮重新武装」
  notified_stage: ""          # "" | "over" | "full" —— 本周期已发到哪一级
```

**额度为空即关闭**：`quota <= 0` 时整个功能停用，worker 不做查询，header 不渲染胶囊，`GET /api/usage/meter` 返回 `enabled: false`。

`notified_cycle_start` + `notified_stage` 是两个内部字段：

- `cycleStart` 变了（周期滚动）→ 自然重新武装，不需要清理任务
- 改规则时 handler 主动清空这两个字段 → 允许再提醒一次
- `stage` 单调递增（`"" → "over" → "full"`），不会回退

### 为什么存 config.yaml 而不是 SQLite

- 全局只有一行，没有查询需求
- 规则变更走 `PATCH /api/config` 已经是成熟的路径（白名单 + 校验 + 热更新 + YAML 原子写 + 回滚）
- `GET /api/config` 天然带出，设置页不用多打一个接口
- 新增一张单行表要走 DDL 迁移、新接口、新测试，收益不成比例

代价：每次提醒要写一次 config.yaml（两次 PATCH 级别的写盘）。频率极低（每周期最多 2 次），可接受。

### 写盘语义

`notified_cycle_start` / `notified_stage` 的更新**不走** `PATCH /api/config`（那是用户操作路径），而是由 service 层直接调用一个内部写函数：读 `model.ConfigInstance` → 改两个字段 → 写 YAML。必须持有 `configMutex`，否则会和用户的 PATCH 互相覆盖。

## 后端实现

### 新文件：`internal/service/usage_meter.go`

```go
// UsageMeterRule 是规则的可读形态（从 config 派生，不含内部状态）
type UsageMeterRule struct {
    AnchorDate string   `json:"anchorDate"`
    PeriodDays int      `json:"periodDays"`
    Metric     string   `json:"metric"`
    Quota      float64  `json:"quota"`
}

// UsageMeterStatus 是 GET /api/usage/meter 的完整响应
type UsageMeterStatus struct {
    Enabled       bool            `json:"enabled"`
    Rule          *UsageMeterRule `json:"rule,omitempty"`
    CycleStart    string          `json:"cycleStart,omitempty"`   // RFC3339，当地时区
    CycleEnd      string          `json:"cycleEnd,omitempty"`
    DayInCycle    int             `json:"dayInCycle,omitempty"`
    PeriodDays    int             `json:"periodDays,omitempty"`
    Actual        float64         `json:"actual"`
    Expected      float64         `json:"expected"`
    Quota         float64         `json:"quota"`
    RemainingDays int             `json:"remainingDays"`
    // ProjectedExhaustDay 是「按当前速度第几天用尽」，0 表示不会用尽
    ProjectedExhaustDay int `json:"projectedExhaustDay"`
    // Series 是逐日累计（含基准线值），供浮层画图
    Series []UsageMeterPoint `json:"series,omitempty"`
}

type UsageMeterPoint struct {
    Day      string  `json:"day"`      // "2026-10-05"
    Actual   float64 `json:"actual"`   // 截至当天的累计
    Expected float64 `json:"expected"` // 截至当天的基准
}
```

关键函数：

```go
// ComputeUsageMeter 是唯一的计算实现。所有调用方（handler / worker /
// 事件触发）都必须走它，否则数字会漂移。
func ComputeUsageMeter(ctx context.Context, now time.Time) (*UsageMeterStatus, error)

// CheckUsageMeter 计算 + 判断是否该提醒 + 发通知。返回本次发出的 stage
// （"" 表示没发）。worker 与回合结束都调它。
func CheckUsageMeter(ctx context.Context) string
```

`ComputeUsageMeter` 内部：

1. 读 `model.ConfigInstance.UsageMeter`，`quota <= 0` → `Enabled: false`
2. 算 `cycleStart` / `dayInCycle`
3. 一次 SQL 拿 `actual`（全局，`WHERE created_at >= ?`，无 project 过滤）
4. 一次 SQL 拿逐日累计（`GROUP BY date(created_at)`，再在 Go 里累加）
5. 算 `expected` / `remainingDays` / `projectedExhaustDay`

**逐日累计的时区陷阱**：SQL 的 `date(created_at)` 按 UTC 分桶，而周期边界按当地时区。两者不一致时曲线会整体偏移。解法：SQL 只按 UTC 日期聚合出原始桶，Go 层再按当地时区的日期重新归属（把每个 UTC 桶的时间点转成当地日期）。周期最多 366 天，桶数不超过 ~370，Go 层重归属的开销可忽略。

### 提醒判定

```go
func shouldNotify(stage string, actual, expected, quota float64) string {
    if actual >= quota && stage != "full" {
        return "full"
    }
    if actual > expected && stage == "" {
        return "over"
    }
    return ""
}
```

- `actual >= quota` 优先于越线：同时成立时只发触顶（用户决策）
- `stage` 已到 `full` 后不再发任何提醒
- `actual > expected` 用严格大于：正好等于基准线不算超（`expected` 本身就是「允许花到这里」）

### 通知写入

新增 `NotificationKindUsage = "usage"`，`EventType` 为 `"over"` / `"full"`。

```go
func RecordUsageNotification(eventID, stage string, st *UsageMeterStatus) {
    StoreNotification(NotificationRecord{
        EventID:   eventID,
        Kind:      NotificationKindUsage,
        EventType: stage,
        Title:     ...,  // 本地化：见下
        Body:      ...,  // 实际/应花/额度/剩余天数
    })
}
```

**标题与正文的本地化**：`notifications.title` 存的是渲染后的文本，不是 i18n key。现有三个 kind 的 title 都是数据（会话名/任务名/仓库名），没有本地化问题；`usage` 是第一个「文案由服务端生成」的 kind。

处理方式：复用已有的 `internal/i18n` 包（`i18n.T(i18n.LocalizerForLocale(model.Language), "UsageMeterOver", data)`，与群聊 `GroupCruiseSatisfied` 等消息同一机制），在 `internal/i18n/locales/active.{zh,en}.yaml` 里新增三条消息（`UsageMeterOverTitle` / `UsageMeterFullTitle` / `UsageMeterBody`）。不引入新的 i18n 机制。

### 推送通道

三条都走，但**原生通知这条链路需要三处联动**（这是本设计最容易漏的地方）：

1. **通知中心**：`StoreNotification`（无条件，不受 `push_mode` 门控）
2. **IM**：`dingtalk` / `feishu` 各新增 `PushUsageMeterEvent(title, body)`，与 `PushForgeEvent` 同构（无会话/任务归属的纯文本推送，正好匹配）
3. **原生通知**：广播 `usage_meter_update` WS 事件，但 Android 侧**只认 `session_update` / `task_update`**，必须同步改三处：
   - `service.IsNotifiableEvent`（`pending_events.go:61`）加 `usage_meter_update` 分支
   - `NativeNotificationPolicy.isNotifiableEvent` + `advancesCursor`（Java）加同样分支
   - `BackgroundService.postEventNotificationFromWorker` 加渲染分支

   **不做到这三处，Android 后台完全收不到用量提醒**（前台还能靠 `useGlobalEvents` → `showBrowserNotification` 收到，所以只在 Android 上测「前台」会假绿）。

   三处的具体形状：

   - `service.IsNotifiableEvent`：`case "usage_meter_update": return true`（该事件没有 status 概念，`Data` 用 `map[string]any`，需在 `default` 前先按 event 名短路）
   - `NativeNotificationPolicy.isNotifiableEvent`：加 `usage_meter_update` → true；`advancesCursor` **也要**返回 true（服务端落了 `pending_events`，不推进游标会永远重读）
   - `BackgroundService.postEventNotificationFromWorker`：`title`/`alert` 从 payload 直接取（服务端已本地化好文案，见下），`intent` 不带 `session_id`/`task_id`，改为 `intent.putExtra("event_type", "usage_meter")`；`EventNotificationIds.forEvent(taskId, sessionId)` 在两者皆空时返回 `BASE`(3)，**所有用量通知共用同一个 id**——每周期最多两条，后一条替换前一条是期望行为（不新增 band，避免改动既有 id 公式）

**文案在服务端本地化**：`usage_meter_update` 的 payload 直接带 `title` / `body` 两个已渲染字符串（按 `model.Language`），Android / 前端 / IM 都直接展示，不在三个客户端各写一套文案。这也是现有 `session_update` / `task_update` 的做法差异所在——那两个用 status 枚举在客户端查 `R.string.*`，而 `usage` 的文案带数值（「已用 $9.20 / 应花 $6.67」），不适合枚举化。

**不复用 `EmitSessionPushNotification`**：它带 once-per-run 的终端守卫，且会写 `session_update` 终态（前端会拆流）。用量提醒与会话生命周期无关。

**游标语义**：`usage_meter_update` 必须**推进游标**（服务端确实落了 `pending_events`），否则客户端会永远重读同一条。这与 `task_update running`（通知但不落库、不推进游标）相反，`NativeNotificationPolicy` 的注释已经把这条区分写清楚了。

### worker

`internal/service/usage_meter_worker.go`：

- 默认间隔 5 分钟
- 每次 tick 调 `CheckUsageMeter`
- 启动时跑一次（覆盖「服务重启后周期已越过基准线但没提醒」的情况）
- 配置热更新后重建（`ReconfigureUsageMeterWorker`，与 `ReconfigureSessionCleanup` 同构）

### 事件触发

挂在 `pushSessionTerminal`（`session_runtime.go:237`）末尾：

```go
// 异步，不阻塞推送路径
go CheckUsageMeter(context.Background())
```

用 goroutine 是因为 `CheckUsageMeter` 要跑聚合查询，不能让终态推送等它。同时它内部有并发保护（见下）。

**并发保护**：worker tick 和回合结束可能同时触发。用一个包级 `sync.Mutex` + `TryLock`（或带 `lastCheck` 时间戳的节流）保证同一时刻只有一个检查在跑，且两次检查间隔不小于 1 秒。否则同一秒可能写两条通知。

### 接口

`GET /api/usage/meter` → `UsageMeterStatus`。需要鉴权（与 `/api/usage/stats` 的 `scope=all` 一致：已登录即可，因为是全局数据）。

**不做 `scope` 参数**：只有全局一种口径。

## 前端实现

### header 胶囊

位置：`.badge-capsule` 之后、`.header-tips` 之前（`AppHeader.vue`）。

```vue
<button v-if="isWideScreen && usageMeter.enabled" class="usage-pill" ...>
  <span class="usage-bar">
    <span class="usage-fill" :style="{ width: fillPct }"></span>
    <span class="usage-mark" :style="{ left: expectedPct }"></span>
    <span v-if="state === 'full'" class="usage-cap"></span>
  </span>
  <span class="usage-text">
    <span class="used">{{ fmt(actual) }}</span>
    <span class="quota">/ {{ fmt(quota) }}</span>
  </span>
</button>
```

- 进度条 40×4，`border-radius: full`
- 基准刻度：1px 竖线 + `box-shadow: 0 0 0 1px var(--bg-secondary)` 描边（压在填充上仍可读）
- 三态配色：`--color-green` / `--color-orange` / `--color-red`
- 数字：`font-variant-numeric: tabular-nums`，紧凑缩写，最小宽度固定

**数字格式化必须新写一个叶子模块**：现有的 `formatTokenCount`（`statsChart.ts:245`）是私有的，而 `statsChart.ts` 顶层 `import type { EChartsCoreOption } from 'echarts/core'` —— 从 header 引用它会把 ECharts 拖进首屏依赖图（即使只是 `import type`，一旦将来有人改成值导入就会静默破防）。

新增 `web/src/utils/usageFormat.ts`（零依赖叶子模块）：`formatUsageValue(metric, value)`，K/M/B 分级与 `formatTokenCount` 保持同一套阈值（999,500 / 999,950 的进位边界），`cost` 用 `$` + 两位小数，`credit` 保留最多 4 位。`statsChart.ts` 与 header 都从这里取，消除第二份实现。

**宽度预算**：header 总宽减去 logo(22) + capsule(最多 220) + server 按钮(28) + 窗口控制(~90)，960px 下余量充足；胶囊本身 `flex-shrink: 0`。

**必须 `-webkit-app-region: no-drag`**：它是 `<button>`，已在 `.header button` 白名单里（`web/css/layout.css:59`），无需额外处理。

### 浮层

用现有 `PopupMenu`（`anchor="right"`），与 server 按钮的资源面板同构。

内容：

1. 标题行：「用量提醒」+ 状态徽章（「超出计划 38%」/「已用尽」）
2. 周期行：第 10 / 30 天 · 2026-10-05 → 2026-11-03
3. 三条对比横条：实际 / 应花 / 额度（同一根轴，宽度 = 值 / 额度）
4. 累计曲线（内联 SVG，不引 ECharts）：
   - 实际累计（实线，`--color-orange` 或绿）
   - 匀速基准（虚线，`--text-muted`）
   - 预测外推（虚线，按当前速度）
   - 额度顶线（`--color-red`，低透明度）
   - 今天竖线
5. 四个统计格：剩余额度 / 剩余天数 / 按当前速度用尽日 / 剩余每日可用
6. 链接：调整额度与周期 → 跳设置

**为什么用内联 SVG 而不是 ECharts**：ECharts 已经在 `UsageStatsPanel` 里按需加载（`defineAsyncComponent`），但它是重依赖（~300KB）。header 胶囊是首屏组件，把 ECharts 拖进首屏会破坏现有的「首屏不加载图表库」约束（见 `StatsTabHost.vue` 的注释）。浮层用 90×288 的 SVG 手绘，代码不到 60 行。

### 数据获取

`web/src/composables/useUsageMeter.ts`（模块级单例）：

- 初始 `GET /api/usage/meter`
- 监听 `useGlobalEvents` 的 `usage_meter_update` 事件 → 更新状态
- 设置变更（`clawbench-settings-change`）→ 重新拉一次

**`useGlobalEvents` 里要加两处**（都在 `useGlobalEvents.ts` 的 `msg.event` 分发块内）：

1. 状态更新：`if (msg.event === 'usage_meter_update') useUsageMeter().onServerUpdate(msg.data)`
2. 浏览器/桌面通知：`showEventBrowserNotification` 加 `usage_meter_update` 分支，直接取 payload 里的 `title` / `body`。**注意该函数开头的 `if (document.visibilityState === 'visible' && document.hasFocus()) return`** —— 这是「页面可见且有焦点就不弹」的既有规则，用量提醒**沿用**它（用户正盯着屏幕时不弹系统通知），但**通知中心与 IM 不受此影响**（它们不看焦点）。

**不做轮询**。用量只在回合结束变化，WS 事件已覆盖。

### 设置页

`settingsFieldMap.ts` 的 `notification` 分类下新增一个 `panel`：

```ts
{ type: 'panel', config: {
    panelId: 'usageMeter',
    titleKey: 'settings.items.usageMeter',
    entrySelector: {
      labelKey: 'settings.items.usageMeter',
      descriptionKey: 'settings.items.usageMeterDesc',
      key: 'usage_meter.metric',   // 用 metric 作为入口显示值
      type: 'select',
      source: 'server',
      options: [...],
      defaultValue: 'cost',
    },
    commonFields: [
      { labelKey: ..., key: 'usage_meter.anchor_date', type: 'text', source: 'server' },
      { labelKey: ..., key: 'usage_meter.period_days', type: 'number', source: 'server' },
      { labelKey: ..., key: 'usage_meter.quota', type: 'number', source: 'server' },
    ],
    ...
}}
```

**「切换指标清空额度」**：指标量纲不同（token vs USD），切换后旧额度无意义。前端在 metric 变更时把 quota 置 0 并提示；服务端也要做同样的校验（不能只靠前端）。

**日期输入**：`anchor_date` 用 `type: 'text'`（现有 field map 没有 date 类型），格式校验在服务端（`YYYY-MM-DD`，且必须是合法日期）。

### 通知中心

- `kindOptions` 增加 `{ value: 'usage', label: t('notifications.kind.usage') }`
- `kindIcon` 增加 `usage` → `Gauge`（lucide）
- `eventChip` 增加 `over` → 「超出计划」/ `full` → 「已用尽」
- 点击行为：宽屏打开用量浮层；窄屏跳设置页的用量提醒项（移动端没有 header 胶囊，也没有浮层）
- `MarkMissingSources` 不改（`usage` 不在 `session`/`task` 分支里，自然落入「never probed」）

### i18n

中英双语，新增：

- `settings.items.usageMeter` / `usageMeterDesc` / `usageMeterAnchor` / `usageMeterPeriod` / `usageMeterMetric` / `usageMeterQuota` 等
- `notifications.kind.usage` / `notifications.event.usageOver` / `notifications.event.usageFull`
- `usageMeter.*`（浮层与胶囊的文案、单位、状态）

## 测试计划

### 后端

`internal/service/usage_meter_test.go`：

- 周期定位：第 1 天 / 中间 / 最后一天 / 跨周期滚动
- `expected` 的整天取整：第 3 天下午仍是 3 天额度
- 三指标口径：token 的 `total_tokens = 0` 兜底
- 提醒判定：越线 / 触顶 / 同时成立只发触顶 / stage 不回退
- 周期滚动后重新武装
- 改规则后清空状态
- 并发：两次同时调用只发一条

`internal/service/usage_meter_worker_test.go`：tick 触发、配置热更新重建。

`internal/handler/usage_meter_test.go`：`enabled: false` 时返回什么、鉴权。

### 前端

- `useUsageMeter.test.ts`：初始加载、WS 事件更新、关闭态
- `AppHeader` 胶囊：三态渲染、`isWideScreen` 门控、宽度计算
- `UsageMeterPopover.test.ts`：数值格式、曲线点数、空序列
- `settingsFieldMap` 守卫：新增 panel 的 key 都在 `PatchableConfigPaths` 里

### 守卫（源码级）

- `usage_meter.go` 的 token 表达式必须与 `usage_stats.go` 的 `usageTotalSumExpr` 一致（两边都改或都不改）
- `AppHeader.vue` 不得（直接或间接）import `echarts` —— 首屏约束。守卫方式：从 `AppHeader.vue` 出发做一次静态 import 图遍历，断言不命中 `echarts`。仅 grep `AppHeader.vue` 抓不到「经由 useUsageMeter → statsChart」这种间接引用
- `formatTokenCount` 只能存在一份（`usageFormat.ts`）；`statsChart.ts` 改为从它导入，守卫断言 `statsChart.ts` 内不再定义该函数
- `NativeNotificationPolicy.isNotifiableEvent` 与 `advancesCursor` 对 `usage_meter_update` 必须**同时**为 true（只改一处会让通知重复或永久丢失）

## 实施顺序

1. `model.Config` 加 `UsageMeter` 段 + `ApplyDefaults` + `PatchableConfigPaths` + `configResponse` + `applyConfigPatch` + 校验
2. `internal/service/usage_meter.go`（计算 + 判定 + 通知）
3. `internal/service/usage_meter_worker.go` + main.go 接线
4. `pushSessionTerminal` 挂事件触发
5. `internal/handler/usage_meter.go` + 路由 + OpenAPI
6. `notifications` 的 `usage` kind
7. 前端 `useUsageMeter` + header 胶囊 + 浮层
8. 设置页 panel
9. 通知中心筛选与跳转
10. i18n 双语
11. 测试补齐

## 已知取舍与风险

1. **`chat_metadata.created_at` 不是消息发送时间**。`SaveMetadata` 用 `INSERT OR REPLACE` 且不写 `created_at`，SQLite 每次都重新打时间戳，所以它是**最后一次元数据写入**（≈ 回合完成）。用量归入「完成那一刻」的周期。跨周期边界的回合会被算到后一个周期——这是账本既有的语义，不改。

2. **DST 切换日的边界偏移**。周期长度用固定 24h 保证恒定，代价是 DST 切换后当天边界相对当地零点偏 1 小时。用户几乎不可能察觉（一年两次、影响 1 小时）。

3. **config.yaml 的写入竞争**。提醒状态与用户 PATCH 共用 `configMutex`，两者串行化。实际实现里 `PersistUsageMeterState` 在锁内只改 `notified_cycle_start` / `notified_stage` 两个字段并**只把这部分写进 YAML**（不整段回写规则），因此不会覆盖用户并发修改的额度。

4. **`usage` 是第一个服务端生成文案的 kind**。标题/正文在写入时本地化，之后切换语言不会更新历史行。与 `session`/`task` 的 title（数据本身）不同，这是有意为之：历史通知记录的是「当时告诉了你什么」。

5. **单条规则不支持「短期 + 长期」并存**。若用户同时想管 7 天和 30 天，只能选一个。未来可把 `usage_meter` 改成数组，向后兼容。

## 实现期发现并修复的三个缺陷

三个都是先写了测试、发现测试绿得可疑才挖出来的，已各留回归用例并做变异验证。

1. **`SELECT created_at, SUM(...)` 缺 GROUP BY**。原实现把「逐行值」和「汇总值」用同一个表达式：汇总没问题，但系列查询里 `SELECT m.created_at, SUM(...)` 没有 GROUP BY，SQLite 会把整个窗口折叠成一行任意时间戳，于是曲线恒为一条水平线（末点恒 0）。修法是把表达式拆成 `usageMeterRowExpr`（逐行）与 `SUM(...)`（合计）。

2. **按 UTC 日期分桶**。`created_at` 是 UTC，而周期边界是本地午夜。同一 UTC 日可以横跨两个本地日（反之亦然），所以无论 `date(created_at)` 还是 `date(created_at,'localtime')` 都会把用量挪到错误的一天。修法是 SQL 只返回原始时间戳，Go 层按本地日期归属。附带发现测试夹具写 RFC3339 而生产写空格分隔格式，解析器现在两种都接受（否则整行被静默丢弃）。

3. **重复 PATCH 相同规则会重复提醒**。设置面板每次保存都提交整个面板，若「任何 `usage_meter` patch 都清除已提醒状态」，用户不改任何值、只按保存也会再收一条通知。实测连续 3 次相同 PATCH 产生 4 条通知。修法是只在规则**实际变化**时清除。

## 补充决策（实现期）

- **worker 增加 `Wake()`**。设计里写「配置热更新后重建 worker」，但重建会与 PATCH 持有的 `configMutex` 死锁（进行中的 tick 需要同一把锁写状态）。改为 buffered channel 唤醒：非阻塞、可合并、不持锁。代价是新规则生效最多延迟到下一次 tick，但 PATCH 路径会立刻 wake，实测 4 秒内出通知。
- **`AppHeader` 不持有 popover 的组件 ref**。通知点击经 `useUsageMeter.requestOpen()` 的计数器传递，避免 App.vue 依赖 header 内部结构；窄屏没有胶囊，`handleOpenUsageMeter` 直接跳设置页。
