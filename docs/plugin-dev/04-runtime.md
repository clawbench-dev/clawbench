# 04 · 运行时：角标、门控、打开副作用

这一章讲“插件在运行时需要什么”。当前版本这些仍在 `App.vue` 里接线；下一阶段会收进每个功能自己的 `dock.ts`。

## 1. 当前：角标要改五处

以邮件未读为例，现在要改：

1. `dockTabBadgeCount(tab)` 的 `switch` 加一个 `case 'mail'`；
2. `dockTabBadgeAnim(tab)` 加一个 `case`；
3. `dockTabBadgeAnimEnd(tab)` 加一个 `case`；
4. 窄屏 Dock 的 inline 按钮模板里加 `v-if="tab === 'mail' && ..."`；
5. `overflowBadgeCount` 汇总里加/减这个计数。

如果这个角标还需要常驻轮询（邮件未读），还要在 `App.vue` 里启动/停止它：

```ts
if (hidden) stopMailUnreadPolling()
else startMailUnreadPolling()
```

这就是“新增一个角标要改五处”的来源。设计目标是让插件只写：

```ts
// 目标形态（尚未落地）
export const mailDock: DockPluginModule = {
  icon: Mail,
  loadPanel: () => import('./MailPanel.vue'),
  badge: () => ({ count: mailUnread.value }),
  startBackground: () => {
    startMailUnreadPolling()
    return stopMailUnreadPolling
  },
}
```

## 2. 当前：门控要改四处

以 Forge 为例，现在要改：

1. `isForgeTabHidden` computed（读 `serverConfig.forge.enabled`）；
2. `overflowTabs` 的 `forgeHidden` 参数；
3. `switchTab` 的 `if (tab === 'forge' && forgeTabBlocked()) return`；
4. 一个 `watch(isForgeTabHidden, ...)`，在开关关闭时把当前页签切走。

终端/端口映射/邮件同理。新增插件如果依赖功能或能力，要照这个模式接线。

清单里的 `feature` / `capability` 字段目前只是管理器标签，不会自动门控。

## 3. 当前：打开副作用在 `switchTab`

```ts
if (tab === 'browse') store.loadFiles(...)
if (tab === 'chat') loadSessionsOnce()
if (tab === 'tasks') loadTasks()
```

新增插件如果打开时要加载数据，在这里加一个 `if`。注意：

- 不要阻塞切换（用 `void load()` 或异步函数）；
- 失败要有可见的错误状态，不要静默；
- 如果数据只需要在面板打开时加载，优先放在面板的 `active` watcher 里，不要堆在 `switchTab`。

## 4. 目标：每个插件一个 `dock.ts`

下一阶段的目标结构（设计文档里有完整说明）：

```ts
// web/src/components/snippets/dock.ts
import { Code2 } from 'lucide-vue-next'

export const snippetsDock: DockPluginModule = {
  icon: Code2,
  loadPanel: () => import('./SnippetsPanel.vue'),
  // 动态标题（可选）
  title: () => t('nav.snippets'),
  // 角标（可选）
  badge: () => ({ count: snippets.count.value }),
  // 进入/离开（可选）
  onEnter: () => snippets.load(),
  onLeave: () => snippets.cancel(),
  // 页签关闭时也要跑的常驻资源（可选）；返回清理函数
  startBackground: () => {
    const stop = snippets.startPolling()
    return stop
  },
}
```

宿主会负责：

| 时刻 | 宿主动作 |
|---|---|
| 应用启动 | 对每个可见插件调 `startBackground()`，保存清理函数 |
| 插件隐藏/功能关闭/能力不可用 | 调清理函数 |
| 插件重新可见 | 重新 `startBackground()` |
| 进入页签 | `onEnter()` |
| 离开页签 | `onLeave()` |
| 应用卸载 | 停掉所有后台资源 |

**在它落地之前，新增插件仍按第 1–3 节手工接线。** 不要提前 import 不存在的 `dock.ts` 类型。

## 5. 角标契约

角标只表达“有多少东西值得看”，不是通知系统：

- 数字角标：`count > 0` 时显示；
- 进行中红点：`{ count: 0, live: true }`（会议用）；
- 隐藏的插件不参与溢出按钮的汇总；
- 角标变化动画由按钮自己处理，不要再加全局 `*BadgeAnim` ref。

角标数据来源可以是：

- store 里的实时状态（终端会话数、端口映射数）；
- WS 事件驱动的 composable（Forge 未读）；
- 轮询（邮件未读）。

只有轮询需要 `startBackground`；事件驱动的角标不需要常驻定时器。

## 6. 重依赖红线

`dock.ts` 会被主包静态 import，所以不能把面板的重依赖带进来：

- WebRTC（会议）
- DOMPurify（邮件正文）
- 编辑器 / 终端 / 图表

如果某个状态来源本身很重，先把它抽到一个轻量模块，只暴露一个 ref。例如会议只暴露“是否在会中”的一个布尔，不要把整个 `useLiveShare` 拖进主包。

## 7. 生命周期与 `active` 的分工

| 资源 | 放哪 |
|---|---|
| 角标轮询（页签关闭也要跑） | `startBackground` |
| 面板打开才需要的轮询/订阅 | 面板的 `active` watcher |
| 打开时加载一次的数据 | `onEnter` 或面板 `active` watcher |
| 离开时取消请求 | `onLeave` / `active` watcher |
| 全局 WS 订阅 | `useGlobalEvents`，不要每个插件自己开 socket |

不要为了“统一”给每个插件加一个空 `startBackground`。只有真正需要常驻资源的插件才写。

## 8. 当前版本的清理责任

因为 `dock.ts` 还没落地，清理逻辑现在由插件自己负责：

- 轮询：在 `active` 变 false、`onUnmounted` 时停；
- 请求：用 `AbortController` 或取消标志；
- WS：用 `useGlobalEvents` 的 `onEvent`，并在卸载时取消订阅；
- 定时器：保存 handle，成对清理。

隐藏插件时，Dock 只是不显示入口；面板可能仍然挂载。不要假设“用户看不见 = 我的定时器已经停了”。
