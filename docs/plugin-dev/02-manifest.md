# 02 · 清单：在 `DOCK_TABS` 里声明一个插件

> **已过时（2026-11-21 契约收口后）**：顺序（`order`）、设置字段（`settings.ts`）、插件文案（`i18n.ts`）现在都写在 `web/src/plugins/<id>/` 里，本文描述的接线位置不再适用。操作步骤一律照 [`README.md`](README.md)；本文件只保留仍然成立的概念解释。

清单是 `web/src/composables/dockTabs.ts` 里的 `DOCK_TABS` 数组。它是左栏的唯一事实源：渲染列表、切换白名单、功能编排、顺序、可见性都从它派生。

## 1. 字段

```ts
export interface DockTabDescriptor {
  id: DockTabId
  titleKey: string
  descriptionKey: string
  group: DockGroup
  closable: boolean
  feature?: DockFeature
  capability?: DockCapability
  settingsCategory?: string
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `id` | 是 | 页签 id，同时进 `TabId` / `DockTabId` 联合类型 |
| `titleKey` | 是 | 静态标题的 i18n key；动态标题在 `App.vue` 的 `dockTabTitle` 里覆盖（Forge） |
| `descriptionKey` | 是 | 功能编排里的一句话说明 |
| `group` | 是 | 管理器里的分组标签：`workspace` / `project` / `personal` / `system`（只是标签，不影响顺序） |
| `closable` | 是 | 用户能不能隐藏。常驻四项填 `false` |
| `feature` | 否 | 服务端总开关（见 §4）。填了它就由管理器写服务端配置，关闭时页签自动消失 |
| `capability` | 否 | 设备/宿主能力标记。管理器在能力缺失时灰显开关并写原因 |
| `settingsCategory` | 否 | 插件自己的设置分类 id，管理器会给“功能设置”齿轮 |

旧字段 `primary` 与「开发相关」标记已删除：

- 左栏是一条有序列表，没有固定头部，所以不需要 `primary`；
- 预设改成显式表（`DOCK_PRESET_PLANS`），不再靠「开发相关」标记推导要隐藏谁。

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

`DOCK_TAB_IDS`、`DOCK_TAB_BY_ID`、`DOCK_FEATURE_TABS` 都是从 `DOCK_TABS` 派生的，不需要手改。

## 3. 顺序语义

- 数组顺序 = 默认顺序 = “开发”预设的顺序。
- 所有页签都可以被用户拖动，包括 `closable: false` 的四项。
- 用户顺序存在 `dockOrder`，由 `mergeDockOrder` 与默认顺序合并：出现过的 id 占默认槽位、按已存相对顺序填充，没出现过的留在原位。
- 管理器拖动后写入的是完整 14 项，所以新顺序原样生效。

新增页签默认放在你想要的位置；如果用户已经存过顺序，新页签会留在默认槽位，不会消失，也不会被追加到末尾。

## 4. `feature`：服务端总开关

`feature` 的取值是 `DockFeature`（`dockTabs.ts`），目前六个：

| key | 页签 | 关闭后 |
|---|---|---|
| `forge.enabled` | Forge | 停轮询，清空所有项目的仓库绑定 |
| `mail.enabled` | 邮件 | 停后台收信与邮件触发任务，账号保留 |
| `terminal.enabled` | 终端 | 关闭所有已打开的终端会话 |
| `kb.enabled` | 知识库 | 停索引器、关库，数据保留 |
| `tasks.enabled` | 任务 | 停调度，取消运行中与排队中的任务 |
| `share.enabled` | 分享 | 撤销所有已发出的分享链接（旧链接 404） |

规则：

- **填了 `feature` 就自动生效**：`App.vue` 用 `isFeatureEnabled(tab.feature)` 过滤渲染，不需要在 `dock.ts` 里再写 `available()`。
- 读取只有一条路：`useFeatureFlags.ts` 的 `isFeatureEnabled` / `useFeatureFlag`（类型 `FeatureFlagKey` 就是 `DockFeature` 的别名）。不要自己解析 `serverConfig`。
- 新开关在 `web/src/composables/dockTabs.ts` 的 `DockFeature` 联合类型与 `DOCK_FEATURES` 数组各加一项（`FeatureFlagKey` 从它派生）。**前端没有默认值镜像**：默认值与实时值都来自 `GET /api/features`，未加载时读作「全开」。
- 服务端要有对应 key（GET 返回、PATCH 白名单、关闭时接口拒绝）。前端藏入口不算数。
- 管理器会自动给这一行渲染「启用」开关、关闭前确认（破坏性项）和一次提交的预设写入。

## 5. `capability`：环境能力

`capability` 目前两个值：

| 值 | 页签 | 判定 |
|---|---|---|
| `terminal` | 终端 | `/api/terminal/status` 的 `enabled` + `platform_supported` |
| `portForward` | 端口映射 | SSH 关闭且传输不允许 h2 时才算不可用 |

真正的检查在功能自己的 `dock.ts` 的 `available()`：

```ts
// components/terminal/dock.ts
available: () => terminalRuntimeEnabled.value === true,
```

`App.vue` 把所有 `available()` 汇总成 `unavailableDockTabs`，交给 `dockTabs()` 过滤；程序化导航统一过 `switchTab`（内部查 `isDockPluginAvailable`）。

管理器对带 `capability` 的行额外看两件事：

- `available()` 为 false → 灰显并写 `unavailableReasonKey` 指向的原因；
- 终端在“服务端说启用但 OS 没有 PTY”时，用 `platformSupported === false` 判定（`available()` 此时为 true，因为服务端确实启用了）。

新增插件如果依赖环境能力，清单与 `dock.ts` 两处都要写；只填清单不会自动门控。

## 6. `closable` 与常驻

| 组合 | 含义 | 例子 |
|---|---|---|
| `closable: false` | 常驻：永远可见，开关灰显 | 文件管理、文件浏览、设置 |
| `closable: true` + `feature` | 可隐藏，且开关同时控制服务端 | Forge、邮件、终端、知识库、任务、分享 |
| `closable: true`（无 `feature`） | 只控制显示 | 历史、统计、会议、端口映射、通知中心 |

`settings` 必须保持 `closable: false`：它是重新打开其他功能的唯一入口。

## 7. 零 import 红线

`dockTabs.ts` 必须保持零 import（类型除外）。原因：

- 很多测试对 `lucide-vue-next` 做了窄 mock；
- 一旦这个模块静态 import 图标，测试会在模块加载时直接炸；
- `useWideScreenLayout` 依赖它，而它被大量组件间接引用。

所以：

- 图标放 `dockTabMeta.ts`；
- 面板放各功能的 `dock.ts`（`panel.load`）；
- 纯函数（`dockTabs` / `mergeDockOrder`）放 `dockTabs.ts`；
- 运行时状态（隐藏集合、预设）放 `useDockPlugins.ts`；
- 服务端开关读取放 `useFeatureFlags.ts`。

## 8. 守卫

`web/src/composables/__tests__/dockTabs.test.ts`、`useWideScreenLayout.test.ts`、`web/src/__tests__/dockSingleListWiring.test.ts` 会检查：

- 每个页签的 `titleKey` 在中英 locale 都能解析；
- `DOCK_TAB_IDS` 与 `DOCK_TABS` 一致；
- 渲染列表是切换白名单的子集；
- 常驻四项不可隐藏、且没有固定位置；
- `mergeDockOrder` 对老数据的槽位保留；
- 宽屏只有一条 `v-for visibleDockTabs`，窄屏从同一列表取溢出项；
- 除 `settings` 外每个页签都声明了 `settingsCategory`，且该分类在 `categoryItems` 里真实存在。

新增页签后跑：

```bash
npx vitest run web/src/composables/__tests__/dockTabs.test.ts \
  web/src/composables/__tests__/useWideScreenLayout.test.ts \
  web/src/__tests__/dockSingleListWiring.test.ts \
  web/src/components/settings/__tests__/DockPluginSetting.test.ts
```
