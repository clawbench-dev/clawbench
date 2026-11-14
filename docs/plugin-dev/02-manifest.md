# 02 · 清单：在 `DOCK_TABS` 里声明一个插件

清单是 `web/src/composables/dockTabs.ts` 里的 `DOCK_TABS` 数组。它是 Dock 的唯一事实源：渲染列表、切换白名单、插件管理器、顺序、可见性都从它派生。

## 1. 字段

```ts
export interface DockTabDescriptor {
  id: DockTabId
  titleKey: string
  descriptionKey: string
  group: DockGroup
  primary?: boolean
  closable: boolean
  devRelated?: boolean
  feature?: DockFeature
  capability?: DockCapability
  settingsCategory?: string
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `id` | 是 | 页签 id，同时进 `TabId` / `DockTabId` 联合类型 |
| `titleKey` | 是 | 静态标题的 i18n key；动态标题在 `App.vue` 的 `dockTabTitle` 里覆盖（Forge） |
| `descriptionKey` | 是 | 插件管理器里的一句话说明 |
| `group` | 是 | 管理器分组：`workspace` / `project` / `personal` / `system` |
| `primary` | 否 | 是否在宽屏 Dock 固定头部；**不等于不可关** |
| `closable` | 是 | 用户能不能隐藏。核心页签填 `false` |
| `devRelated` | 否 | 开发相关；“标准”预设会隐藏它 |
| `feature` | 否 | 服务端功能标记（管理器显示“服务端功能”标签） |
| `capability` | 否 | 设备/宿主能力标记（管理器显示“环境能力”标签） |
| `settingsCategory` | 否 | 插件自己的设置分类 id，管理器会给“功能设置”深链 |

## 2. 加一条清单

以“代码片段”插件为例：

```ts
{
  id: 'snippets',
  titleKey: 'nav.snippets',
  descriptionKey: 'settings.dockPlugins.desc.snippets',
  group: 'project',
  closable: true,
  settingsCategory: 'snippets',
}
```

同时要改三处类型/派生：

1. `web/src/composables/dockTabs.ts` 的 `TabId` 联合类型加 `'snippets'`；
2. `web/src/i18n/locales/zh.ts` 与 `en.ts` 加 `nav.snippets` 与 `settings.dockPlugins.desc.snippets`；
3. `web/src/composables/dockTabMeta.ts` 的 `DOCK_TAB_ICONS` 加图标。

`DOCK_TAB_IDS`、`WIDE_SCREEN_PRIMARY_TABS`、`DOCK_TAB_BY_ID`、`REORDERABLE_DOCK_TABS` 都是从 `DOCK_TABS` 派生的，不需要手改。

## 3. 顺序语义

- 数组顺序 = 默认 Dock 顺序。
- `primary: true` 的页签在宽屏固定头部，位置不随用户排序变化。
- `settings` 必须留在数组最后，并且 `closable: false`。
- 其余 `!primary && closable && id !== 'settings'` 的页签进入 `REORDERABLE_DOCK_TABS`，用户可以上下移。

新增页签默认放在你想要的位置；如果用户已经存过顺序，新页签会被 `mergeDockOrder` 追加到可排序尾部，不会插到用户排好的前面。

## 4. `feature` / `capability` 只是元数据

当前版本的 `feature` / `capability` **不会自动门控**。它们的作用是：

- 插件管理器显示“服务端功能”/“环境能力”标签；
- 给后续统一可用性层提供声明；
- 让 review 时一眼看出这个插件依赖什么。

实际门控仍在 `App.vue`：

| 标记 | 实际读取位置 |
|---|---|
| `forge.enabled` | `isForgeTabHidden` + `forgeTabBlocked` |
| `mail.enabled` | `isMailTabHidden` + `mailTabBlocked` |
| `terminal` | `isTerminalDisabled` |
| `portForward` | `isPortForwardUnavailable` |

新增插件如果依赖某个功能/能力，要自己在 `App.vue` 接线，并在清单里填上对应标记。

## 5. `primary` vs `closable`

这两个字段经常被混：

| 组合 | 含义 | 例子 |
|---|---|---|
| `primary: true, closable: false` | 固定在头部，永远可见 | 文件管理、文件预览 |
| `primary: true, closable: true` | 固定在头部，但可以隐藏 | Git 历史 |
| `primary: false, closable: true` | 在尾部，可隐藏、可排序 | 邮件、任务 |
| `primary: false, closable: false` | 在尾部，永远可见、不可排序 | 设置 |

宽屏 Dock 渲染固定头部时，必须用 `primaryDockTabs({ hidden })` 过滤，不能直接渲染 `WIDE_SCREEN_PRIMARY_TABS`。

## 6. 零 import 红线

`dockTabs.ts` 必须保持零 import（类型除外）。原因：

- 很多测试对 `lucide-vue-next` 做了窄 mock；
- 一旦这个模块静态 import 图标，测试会在模块加载时直接炸；
- `useWideScreenLayout` 依赖它，而它被大量组件间接引用。

所以：

- 图标放 `dockTabMeta.ts`；
- 面板放 `App.vue`（未来放各功能的 `dock.ts`）；
- 纯函数（`primaryDockTabs` / `secondaryDockTabs` / `mergeDockOrder`）放 `dockTabs.ts`；
- 运行时状态（隐藏集合、预设）放 `useDockPlugins.ts`。

## 7. 守卫

`web/src/composables/__tests__/useWideScreenLayout.test.ts` 与 `dockTabs.test.ts` 会检查：

- 每个页签的 `titleKey` 在中英 locale 都能解析；
- `DOCK_TAB_IDS` 与 `DOCK_TABS` 一致；
- 渲染列表是切换白名单的子集；
- `settings` 永远最后；
- `history` 隐藏后不再出现在 `primaryDockTabs`。

新增页签后跑：

```bash
npx vitest run web/src/composables/__tests__/dockTabs.test.ts \
  web/src/composables/__tests__/useWideScreenLayout.test.ts
```
