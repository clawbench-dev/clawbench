# Android 桌面悬浮状态窗设计

日期：2026-08-27
状态：已确认设计

## 目标

在 Android 端（原生 Java + WebView 壳）实现系统级桌面悬浮窗，实时展示会话状态。纯展示 + 点击回主界面，不做展开详情，不做悬浮窗内交互操作。

## 背景

- 复用 `BackgroundService`（前台服务）+ OkHttp 原生 WebSocket 事件通道（`connectNativeWs()`，BackgroundService.java L2099）
- 事件源已齐全：`session_update`（running/completed/cancelled/permission_pending）+ `chat_stream` 增量
- 现状：Android 端无悬浮窗能力，Manifest 未声明 `SYSTEM_ALERT_WINDOW`

## 技术选型

| 决策 | 选择 | 理由 |
|---|---|---|
| UI 渲染 | 原生 View（FrameLayout 胶囊） | 轻量省电，无额外依赖 |
| 数据源 | 复用 BackgroundService 原生 WS | 通道已存在，仅需扩展事件派发 |
| 窗口类型 | `TYPE_APPLICATION_OVERLAY` | Android 8.0+ 标准悬浮窗类型 |

## 架构

```
MainActivity (WebView 壳)
 ├─ 启动 BackgroundService（已有）
 │    └─ connectNativeWs() ──复用──> session_update / chat_stream 事件
 │         └─ 新增事件派发 → FloatingStatusController
 │              └─ 驱动 FloatingStatusView（原生 View，TYPE_APPLICATION_OVERLAY）
 │
 ├─ FloatingStatusController（新增）
 │    ├─ 事件 → UI 状态映射（running 绿 / permission_pending 黄呼吸 / error 红 / completed 灰）
 │    ├─ 自动显隐状态机（前台隐藏 / 后台有任务出现 / 完成淡出）
 │    ├─ 拖动 + 贴边 + 位置持久化
 │    └─ 点击 → startActivity(MainActivity, REORDER_TO_FRONT) + session_id 深链
 │
 └─ SettingsFragment 新增开关 + SYSTEM_ALERT_WINDOW 权限申请流程
```

## 数据流

```
WS (BackgroundService 原生通道，已有)
  → session_update / chat_stream
  → FloatingStatusController.handleEvent()
  → 状态机判断（前台？有任务？）→ 显示/隐藏/更新
  → FloatingStatusView.render()
```

## 交互逻辑

### 出现与隐藏（全自动）

| 场景 | 行为 |
|---|---|
| 会话进入 running/permission_pending，且主界面在后台 | 悬浮窗出现 |
| 主界面回到前台（onResume） | 立即隐藏 |
| 所有任务完成/取消 | 完成态 3s → 淡出隐藏 |
| 用户手动关闭 | 本次会话内不再出现，可在设置里重新开启 |
| APP 完全在前台 | 永不出现 |

### 形态

- 默认胶囊态：圆角胶囊，左侧状态点（绿=running / 黄=permission_pending / 灰=completed / 红=error），右侧单行文字：会话标题 + 最后一条消息/工具名（自动截断省略号）
- 不做展开详情视图

### 拖动与贴边

- 默认靠右边缘垂直居中，可自由拖动
- 松手自动吸附到左/右边缘（8dp margin）
- 拖动中半透明 0.85，松手恢复
- 位置存 SharedPreferences，重启恢复

### 状态更新反馈

- 状态切换：状态点变色 + 200ms 脉冲缩放
- 新消息：预览文字淡入替换
- permission_pending：黄点 + 「等待授权」+ 缓慢呼吸动画
- 完成：显示「✓ 完成」→ 自动淡出

### 点击与长按

- 点击：`FLAG_ACTIVITY_REORDER_TO_FRONT` 拉起 MainActivity，携带 session_id 深链定位
- 长按：迷你菜单（隐藏悬浮窗 / 打开设置）；与拖动用距离阈值区分

### 与通知协调

- 开启悬浮窗后，设置项：抑制 BackgroundService 重复弹通知

## 文件变更

| 文件 | 变更 |
|---|---|
| `android/app/src/main/AndroidManifest.xml` | + `SYSTEM_ALERT_WINDOW` 权限 |
| `android/.../BackgroundService.java` | WS 事件转发到 controller（扩展 connectNativeWs） |
| `android/.../FloatingStatusController.java`（新） | 显隐状态机、拖动贴边、点击回调、权限检查 |
| `android/.../FloatingStatusView.java`（新） | 胶囊 View：状态点 + 单行文字 + 动画 |
| `android/.../MainActivity.java` | 权限申请入口、onResume/onPause 通知显隐 |
| `android/.../SettingsFragment`（或等价设置页） | 悬浮窗开关、抑制重复通知开关 |

## 测试

- JUnit：状态机（事件序列 → 显示/隐藏决策）、事件 → UI 状态映射
- Robolectric：`FloatingStatusController` 生命周期 + 拖动贴边
- Instrumented：悬浮窗真实显示（需 overlay 权限）验证点击拉起主界面
