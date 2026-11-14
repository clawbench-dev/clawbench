# 08 · 坑：这些地方已经踩过

## 1. `dockTabs.ts` 必须零 import

**症状**：某个测试在模块加载时报 `No "FolderOpen" export is defined on the "lucide-vue-next" mock`。

**原因**：很多测试对 `lucide-vue-next` 做窄 mock，只导出几个图标。`dockTabs.ts` 被大量组件间接引用，一旦它 import 图标，所有走到的测试都会炸。

**规则**：

- `dockTabs.ts` 只放类型、字符串、布尔、纯函数；
- 图标放 `dockTabMeta.ts`；
- 面板放 `App.vue`（未来放各功能 `dock.ts`）；
- 运行时状态放 `useDockPlugins.ts`。

## 2. `primary` 不等于“不可关”

**症状**：用户隐藏了 Git 历史，宽屏 Dock 里它还在；或者点击它没反应。

**原因**：直接 `v-for="tab in WIDE_SCREEN_PRIMARY_TABS"`，没有按隐藏集合过滤。

**规则**：

```ts
const visiblePrimaryTabs = computed(() =>
  primaryDockTabs({ hidden: hiddenDockTabs.value }),
)
```

窄屏的历史按钮也要 `v-if="!isDockTabHiddenById('history')"`。

## 3. 设置页必须永远最后、永远不可关

**症状**：用户把设置隐藏后，再也打不开被关的插件。

**规则**：

- `settings` 的 `closable: false`；
- `REORDERABLE_DOCK_TABS` 排除 `settings`；
- `secondaryDockTabs` 永远把 `settings` 追加到最后；
- 管理器不给设置排序按钮。

## 4. 三种“关闭”不要混

**症状**：用户把任务“关掉”后以为定时任务也停了；或者终端本来就没有 PTY，开关打开也没用。

**规则**：

- 能力：灰掉 + 原因，不给开关；
- 功能：真开关，文案写清“会停服务端”；
- 偏好：只影响 Dock 入口；客户端角标轮询会停，服务端功能照常。

检查写在功能自己的 `dock.ts` 的 `available()` 里，不要写回 App.vue 的专用 computed。插件管理器里的显示开关是偏好。Forge/邮件的服务端开关在各自设置页，不要复制到管理器里（Forge 关闭会清空项目绑定，是破坏性操作）。

## 5. 隐藏的页签必须拒绝程序化导航

**症状**：通知深链跳到已隐藏的邮件页签，界面停在一个没有 Dock 入口的面板。

**规则**：`switchTab` 开头统一 `if (dockTabBlocked(tab)) return`。新增的深链入口（通知、任务、导航协调器）都走 `switchTab`，不要绕过它。

## 6. 启动恢复只校验 id 不够

**症状**：上次停在邮件，这次邮件被隐藏，刷新后落在一个没有入口的面板。

**规则**：

- `resolveLeftTabOnEnter` 之后再过 `tabBlocked`；
- `applyProjectPanel` 里 `resolvePanelTab` 之后再过 `isDockTabHiddenById`；
- 兜底是 `browse`（宽屏）或 `chat`（窄屏）。

## 7. 隐藏的插件不参与角标汇总

**症状**：溢出按钮上有一个数字，点开菜单却找不到对应的页签。

**规则**：`overflowBadgeCount` 只汇总 `popupOverflowTabs` 里的插件。隐藏/不可用的插件不会进这个列表，所以不贡献数字。

## 8. `custom` 不是一种“用户类型”

**症状**：插件看到 `custom` 就隐藏了高级功能，用户只是移动了一下顺序，功能却没了。

**规则**：`custom` 只表示“手动改过 Dock”，不是“非开发者”。插件应把它当默认，不要用它做权限或功能开关。

## 9. 响应式 Set 要替换，不要原地改

**症状**：隐藏/显示后 Dock 不更新。

**原因**：`ref(new Set())` 原地 `.add()` 后引用没变，依赖可能不通知。

**规则**：

```ts
const next = new Set(hidden.value)
next.add(id)
hidden.value = next // 替换引用
```

`useDockPlugins` 已经这样做。新增状态时照这个模式。

## 10. `dockHidden` / `dockOrder` / `dockPreset` 是全局的

**症状**：切换项目后插件配置变了。

**原因**：把这三个状态设成跟随“布局记忆”，用户选“按项目”时就变成每个项目一份。

**规则**：三者在 `panelState.ts` 里都是 `scope: 'global', followsLayoutMemory: false`。插件配置是设备级偏好，不随项目走。

## 11. 运行时资源不能带重依赖

**症状**：为了显示一个角标，把 WebRTC / DOMPurify / 编辑器拉进了主包，首屏变大。

**规则**：`dock.ts` 会被主包静态 import。重状态先抽到轻量模块，只暴露一个 ref。面板本身才用动态 import。

## 12. 可选面板不要裸 `defineAsyncComponent`

**症状**：网络抖动后页签永久转圈，重试也没用。

**原因**：裸 `defineAsyncComponent` 没有 `errorComponent`，失败后 Vue 会缓存 rejected promise。

**规则**：面板的 `panel.load` 由 `DockPanelHost` 用 `buildAsyncComponentOptions` 包装；不要自己写裸 `defineAsyncComponent`。

## 13. 新页签的文案两边都要加

**症状**：界面显示 `settings.dockPlugins.desc.snippets` 原文。

**规则**：`zh.ts` 和 `en.ts` 同时加。`SettingsIndex.test.ts` 与 `settingsFieldMap.test.ts` 会读真实 locale 文件检查部分 key，但不会覆盖所有运行时拼接的 key。

## 14. 守卫测试仍读源码，但已改成断言 runtime

**症状**：重构 `App.vue` 或 `dock.ts` 后 `forgeDockTabTitleWiring.test.ts` / `mailDockTabWiring.test.ts` 变红。

**规则**：这两个测试现在断言 `components/<feature>/dock.ts` 的内容 + App.vue 是否走 runtime 解析（`resolveDockTitle` / `isDockPluginAvailable` / `unavailableDockTabs`），不再钉旧的 `forgeTabBlocked` 字符串。改接线时要同步改断言。

## 15. 新页签在旧顺序里不存在

**症状**：用户存过顺序，新增页签没出现。

**原因**：`dockOrder` 里没有新 id。

**规则**：`mergeDockOrder` 会把缺失的 id 按默认顺序追加。不要手写“只渲染 order 里的 id”，否则新插件会消失。

## 16. `history` 在窄屏也要条件渲染

**症状**：宽屏隐藏了历史，窄屏底部 Dock 里还在。

**原因**：窄屏模板把 `history` 写死在固定按钮里，没有走 `visiblePrimaryTabs`。

**规则**：窄屏历史按钮加 `v-if="!isDockTabHiddenById('history')"`；`dockActiveIndex` 用 `visiblePrimaryTabs` 计算，不能用写死的 `['chat','browse','view','history']`。

## 17. `TabPanel` 会保活

**症状**：切走再切回，`onMounted` 不再跑，数据不刷新。

**规则**：需要每次进入都刷新的面板，监听 `active` prop；需要卸载清理的，挂 `onUnmounted`。

## 18. 构建和全量测试不要并发

**症状**：OOM、假失败、超时。

**规则**：跑 `npm run build` 前先 `ps` 看有没有别的 vitest/go test；有就等。纯前端改动只跑相关测试，不要顺手跑全量。

## 19. 类型检查可能被无关文件挡住

**症状**：`npm run typecheck` 报 `web/src/share/ShareView.vue` 的 `fitToScreen` 错误。

**规则**：这是与插件无关的既有错误（或别的 agent 的并行改动）。确认错误不在你改的文件里，不要为了让 typecheck 变绿去改无关文件。Vite build 不做类型检查，仍可正常构建。

## 20. `dock.ts` 不能 import 自己的面板

**症状**：首屏包突然变大，或者面板的懒加载失效。

**原因**：`dockPluginRuntime.ts` 在应用启动时静态 import 所有 `dock.ts`。如果 `dock.ts` 又 import 自己的面板，面板就被拉回主包了。

**规则**：`dock.ts` 只放轻资源——角标、可用性、`onEnter`、`startBackground`，以及 `panel.load` 的**动态 import 函数**。不要把 `import XxxPanel from './XxxPanel.vue'` 写在 `dock.ts` 顶部；那会把面板拉回主包。面板由 `DockPanelHost` 通过 `panel.load` 懒加载。

