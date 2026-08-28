# Android 悬浮窗多会话列表面板设计

日期：2026-08-28
状态：已确认设计（含 2026-08-28 第二次迭代：统计胶囊）

## 2026-08-28 迭代二：统计胶囊（当前目标）

用户反馈：胶囊应展示统计信息而非会话标题，且 logo 未显示。

### 胶囊布局（收起态）
`logo | 绿点呼吸 + "执行中 N" | 黄点 + "待审批 N" | 蓝点 + "未读 N"`

- 为 0 的计数不显示
- **不显示会话标题**
- 执行中 = 纯 running 会话数（不含待审批）
- 待审批 = permission_pending 会话数
- 未读 = 有未读消息的**已完成**会话数（running=false && pending=false && unreadCount>0）
- 三组不重叠
- logo：圆形 app 图标（ic_launcher），**当前未显示，需修复**
- 数据：原生从已拉取的 overview JSON 本地计算，无需后端改动

### 点击行为
- 统一点击胶囊 → 展开分组列表面板（不再单会话直接进）

### 展开面板
- 宽 280dp 固定，高随内容（最多一屏，超出滚动）
- 会话项状态点：绿=执行中（**呼吸动效**）、黄=待审批、蓝=有未读
- 优先级：黄 > 绿 > 蓝
- 保留：分组、点击项打开会话（跨 project 深链）、头部/空白收起

## 目标

将现有单胶囊悬浮状态窗扩展为"迷你胶囊 ↔ 展开列表面板"双形态。面板展示**所有项目**的运行中 / 待审批 / 未读会话，按项目名分组，点击会话项打开对应会话。

## 关键决策

| 项 | 决策 |
|---|---|
| 形态 | 迷你胶囊 ↔ 展开面板切换 |
| 显隐 | 有"值得关注"会话（运行中/待审批/未读）才出现 |
| 数据源 | 后端新增 `/api/ai/sessions/overview` 跨项目端点 |
| 返回结构 | `{ projects: [{name, sessions: [...]}], total }`，session 含 running/pendingApproval/unreadCount |
| 会话范围 | 运行中 + 待审批 + 未读，不区分项目 |
| 刷新 | WS 事件驱动 + 展开面板时拉取一次 |
| 未读清除 | 点击会话项打开时前端清除（复用现有逻辑） |
| 鉴权 | session cookie，登录用户全量 |
| 切换交互 | 点胶囊展开；点面板头部/空白收起 |
| 胶囊点击 | 单会话直接进会话；多会话展开列表 |

## 后端

### 新端点 `/api/ai/sessions/overview`（GET, Auth）

- 返回所有项目中"值得关注"的会话，按项目名分组
- 复用 `GetRecentSessions("")` 获取全部会话（含 project_path），或新增跨 project 查询
- 批查 `GetRunningSessionIDs()` + `GetPendingApprovalSessionIDs()` 标注 running/pendingApproval
- unread_count 子查询需支持跨 project（按 `s.project_path` 关联，不能按固定 project）
- 过滤：`running || pendingApproval || unreadCount>0`
- 响应：
```json
{
  "projects": [
    {
      "name": "/home/user/proj-a",
      "sessions": [
        { "id": "...", "title": "...", "running": true, "pendingApproval": false, "unreadCount": 3, "updatedAt": "..." }
      ]
    }
  ],
  "total": 5
}
```

### 路由注册
`handler.go`：`register("/api/ai/sessions/overview", middleware.Auth(ServeSessionsOverview))`

## Android 原生

### FloatingStatusController 扩展

- 状态机新增 `expanded` 状态：胶囊 ↔ 面板
- `setExpanded(boolean)`：展开时拉取 overview + 渲染列表；收起时回胶囊
- 点击处理变化：
  - 胶囊点击：单会话 → `launchFromFloatingWindow(sessionId)`；多会话 → 展开
  - 面板会话项点击 → `launchFromFloatingWindow(sessionId)`
  - 面板头部/空白点击 → 收起
- 维护 `runningSessionIds` / 会话状态集合（事件驱动），事件更新后刷新胶囊计数

### 新列表面板 View

- 原生 View 列表（RecyclerView 或 LinearLayout）
- 按项目分组：每组 header（项目名）+ 会话项（状态点/图标 + 标题 + 未读数）
- 状态展示：运行中（绿点/旋转）、待审批（黄点）、未读（数字角标）
- 面板大小：内容自适应，最大高度限制
- 头部可拖动贴边（复用现有拖动逻辑）

### 事件驱动刷新

- `handleEvent` 更新本地会话状态集合（running 加入、completed/cancelled 移出）
- 收到事件后若面板展开 → 重新拉取 overview 刷新列表

## 主题配色

悬浮窗必须匹配 app 当前主题（20+ 种主题）。方案：**前端把颜色传给原生**。

- 前端 `setTheme(themeId)` 桥扩展为附带颜色：`setTheme(themeId, bg, text, textSecondary, accent)`
- 来源：`themeMeta.ts` 的 `preview`（bg/text/accent）+ `getComputedStyle` 读 `--text-secondary`
- 原生持久化到 SharedPreferences，悬浮窗读取；主题切换即时生效，重启保留
- 状态色（绿/黄/红）保持固定语义色，不随主题

## 测试

- 后端：overview handler 单测（分组、过滤、running/pending/unread 标注、跨 project unread）
- Android：状态机（单/多会话点击决策）、overview JSON 解析、列表渲染数据映射
- 前端：setTheme 传色桥单测

## 文件变更

| 文件 | 变更 |
|---|---|
| `internal/handler/chat_session.go` | 新增 `ServeSessionsOverview` |
| `internal/handler/handler.go` | 注册 `/api/ai/sessions/overview` |
| `internal/service/chat.go` | unread 子查询跨 project 支持（或新增 overview 查询） |
| `android/.../FloatingStatusController.java` | 展开/收起状态机、点击决策、overview 拉取 |
| `android/.../FloatingStatusPanelView.java`（新） | 分组成列表 |
| `android/.../BackgroundService.java` | 事件驱动刷新、overview 拉取入口 |
