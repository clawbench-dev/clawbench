# 04 · 运行时：`dock.ts` 里放什么

> **已过时（2026-11-21 契约收口后）**：顺序（`order`）、设置字段（`settings.ts`）、插件文案（`i18n.ts`）现在都写在 `web/src/plugins/<id>/` 里，本文描述的接线位置不再适用。操作步骤一律照 [`README.md`](README.md)；本文件只保留仍然成立的概念解释。

每个插件的运行时资源放在功能目录的 `dock.ts` 里。App.vue 不再为某个插件写专门的角标 switch、门控 computed 或 `switchTab` 里的 `if`。

## 1. 契约

类型定义在 `web/src/composables/dockPluginTypes.ts`（只含类型，避免循环依赖）：

```ts
export interface DockPluginRuntime {
  icon?: () => Component
  title?: () => string
  badge?: () => DockBadge | null
  panel?: DockPanelDescriptor
  available?: () => boolean
  leaveWhenUnavailable?: () => boolean
  unavailableReasonKey?: string
  onEnter?: () => void
  onLeave?: () => void
  startBackground?: () => () => void
}
```

| 字段 | 什么时候用 | 宿主怎么消费 |
|---|---|---|
| `icon` | 图标随状态变（Forge 的 GitHub/GitLab） | `resolveDockIcon(tab)` |
| `title` | 标题随状态变（Forge 跟着绑定平台） | `resolveDockTitle(tab)` |
| `badge` | 有数字角标或进行中红点 | `resolveDockBadge(tab)` |
| `panel` | 提供面板组件/加载器、额外 props、事件 | `DockPanelHost` |
| `available` | 依赖**设备能力**（终端 PTY、端口映射传输） | `isDockPluginAvailable(tab)` |
| `leaveWhenUnavailable` | 不可用时是否把用户切走（终端在平台不支持时返回 false） | `shouldLeaveUnavailableDockTab(tab)` |
| `unavailableReasonKey` | 功能编排显示“为什么不可用” | 设置页 |
| `onEnter` | 打开页签时加载数据 | `useDockLifecycle().onEnter(tab)` |
| `onLeave` | 离开页签时取消/清理 | `useDockLifecycle().onLeave(tab)` |
| `startBackground` | 页签关闭也要跑的资源（角标轮询） | `useDockLifecycle` 启停 |

没有需要的字段就不写，不要为了“统一”加空实现。

### 1.1 面板

`panel` 有三种写法：

- `load: () => import('./XxxPanel.vue')`：正常，懒加载；
- `component: XxxPanel`：同步组件（核心面板才需要；目前文件管理/文件预览仍留在 App.vue，不用这个）；
- `props` / `on`：从宿主上下文取额外 props 和事件。

`DockPanelHost` 会把 `active` 传给每个面板，再 `v-bind` `panel.props(host)`、`v-on` `panel.on(host)`。

```ts
panel: {
  load: () => import('./ForgePanelContent.vue'),
  props: (host) => ({ projectPath: host.projectPath }),
  on: (host) => ({ 'request-project': host.requestProject }),
}
```

`host` 的字段见 `DockPanelHost` 类型（`dockPluginTypes.ts`）：项目路径、终端请求目录、平台是否支持终端、打开文件、打开任务文件、会议引用、打开邮件设置、请求项目、打开会议中继设置、清空终端目录。需要新能力时加到这个接口，不要在面板里 import App.vue。

## 2. 聚合层

`web/src/composables/dockPluginRuntime.ts` 是唯一把 id 映射到 `dock.ts` 的地方：

```ts
const RUNTIME: Partial<Record<TabId, DockPluginRuntime>> = {
  chat: chatDock,
  browse: browseDock,
  history: historyDock,
  forge: forgeDock,
  tasks: tasksDock,
  terminal: terminalDock,
  mail: mailDock,
  live: liveDock,
  proxy: proxyDock,
  kb: kbDock,
  share: shareDock,
  notifications: notificationsDock,
  stats: statsDock,
  settings: settingsDock,
}
```

服务端总开关**不走** `available()`：清单里填了 `feature` 的页签由 `App.vue` 用 `isFeatureEnabled(tab.feature)` 过滤，`dock.ts` 不需要再写一遍（Forge / 邮件历史上写过，保留即可，但新插件不要再加）。`available()` 只回答“这台机器/这个宿主能不能跑”。

宿主只读这些解析函数：

```ts
resolveDockBadge(id)
resolveDockTitle(id)
resolveDockIcon(id)
isDockPluginAvailable(id)
shouldLeaveUnavailableDockTab(id)
dockUnavailableReasonKey(id)
runDockOnEnter(id) / runDockOnLeave(id)
startDockBackground(id)
```

插件不 import 这个聚合层，也不 import 别的插件的 `dock.ts`。

## 3. 例子

### 邮件：角标 + 服务端功能 + 后台轮询

```ts
// web/src/components/mail/dock.ts
import { useMailUnread, startMailUnreadPolling, stopMailUnreadPolling } from '@/composables/useMailUnread'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

const { total } = useMailUnread()
const { serverConfig } = useSettingsConfig()

export const mailDock: DockPluginRuntime = {
  badge: () => ({ count: total.value }),
  available: () => (serverConfig.value?.mail as Record<string, unknown> | undefined)?.enabled !== false,
  startBackground: () => {
    startMailUnreadPolling()
    return stopMailUnreadPolling
  },
}
```

`startBackground` 返回清理函数。宿主只在“可见 + 可用”时启动它；隐藏邮件或关闭服务端功能都会停。

### Forge：动态图标/标题 + 角标 + 服务端功能

```ts
export const forgeDock: DockPluginRuntime = {
  icon: () => (forgeDockIconKind(platform.value) === 'gitlab' ? Gitlab : Github),
  title: () => i18n.global.t(forgeTabTitleKey(platform.value)),
  badge: () => ({ count: forgeUnreadCount.value }),
  available: () => (serverConfig.value?.forge as Record<string, unknown> | undefined)?.enabled !== false,
}
```

### 终端：能力门控 + 平台例外

```ts
export const terminalDock: DockPluginRuntime = {
  badge: () => ({ count: store.state.terminalSessionCount }),
  available: () => terminalRuntimeEnabled.value === true,
  // 平台不支持时不要切走：面板要显示“此平台不支持终端”的空状态
  leaveWhenUnavailable: () => platformSupported.value !== false,
}
```

### 文件管理 / 任务：打开时加载

```ts
export const browseDock: DockPluginRuntime = {
  onEnter: () => { void store.loadFiles(store.state.currentDir, false, 0, true) },
}

export const tasksDock: DockPluginRuntime = {
  badge: () => ({ count: store.state.taskUnreadCount }),
  onEnter: () => { void loadTasks() },
}
```

### 会议：进行中红点

```ts
export const liveDock: DockPluginRuntime = {
  badge: () => ({ count: 0, live: liveState().value !== 'idle' }),
}
```

`count: 0` + `live: true` 表示只显示点，不显示数字。

## 4. 生命周期

`web/src/composables/useDockLifecycle.ts` 在 App.vue setup 里调用一次：

| 时刻 | 宿主动作 |
|---|---|
| 应用启动 | 对每个“可见 + 可用 + 有 `startBackground`”的插件启动资源 |
| 插件被隐藏 / 功能关闭 / 能力不可用 | 调清理函数 |
| 插件重新可见 | 重新启动 |
| 进入页签 | `onEnter` |
| 离开页签 | `onLeave` |
| 应用卸载 | 停掉所有后台资源 |

`watchEffect` 会追踪隐藏集合、服务端配置、终端状态、端口能力等响应式来源，所以不需要为每个插件写 watch。

## 5. 角标

- 数字角标：`count > 0` 时显示；
- 进行中红点：`{ count: 0, live: true }`；
- 隐藏的插件不参与溢出按钮的汇总（汇总只遍历 `popupOverflowTabs`）；
- 角标变化动画由 App.vue 的 `badgeAnims` 统一处理，不需要每个插件加 ref。

只有轮询需要 `startBackground`；WS 事件或 store 状态驱动的角标不需要常驻定时器。

## 6. 仍然在 App.vue 的东西

面板加载已经搬走了；以下仍在宿主：

- 核心文件管理 / 文件预览的 `<TabPanel>` 与大量 props/events；
- 抽屉清理（`onTabSwitch`）、惰性路径选择器关闭；
- Dock 布局、顺序、可见性、角标展示；
- 全局 WS 事件（`useGlobalEvents`）。

不要因为 `dock.ts` 存在就把面板也静态 import 进去——那会破坏懒加载。面板一律走 `panel.load` 的动态 import，由 `DockPanelHost` 用 `buildAsyncComponentOptions` 包装。

## 7. 重依赖红线

`dock.ts` 会被聚合层在应用启动时静态 import，所以不能把面板的重依赖带进来：

- WebRTC（会议）
- DOMPurify（邮件正文）
- 编辑器 / 终端 / 图表

如果某个状态来源本身很重，先把它抽到一个轻量模块，只暴露一个 ref。例如会议只暴露“是否在会中”的一个 ref，不要把整个 `useLiveShare` 拖进来。

## 8. 清理责任

- 轮询：放在 `startBackground`，返回清理函数；
- 请求：用 `AbortController` 或取消标志；
- WS：用 `useGlobalEvents` 的 `onEvent`，卸载时取消订阅；
- 定时器：保存 handle，成对清理。

隐藏插件时，宿主会停 `startBackground`，但已经挂载的面板可能仍然存在。不要假设“用户看不见 = 我的所有定时器都停了”；面板自己的 `active` watcher 仍然要负责它自己的资源。
