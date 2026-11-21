# 09 · 设置：给插件一个自己的配置页

> **已过时（2026-11-21 契约收口后）**：顺序（`order`）、设置字段（`settings.ts`）、插件文案（`i18n.ts`）现在都写在 `web/src/plugins/<id>/` 里，本文描述的接线位置不再适用。操作步骤一律照 [`README.md`](README.md)；本文件只保留仍然成立的概念解释。

## 1. 三种设置页形态

| 形态 | 适合 | 怎么接 |
|---|---|---|
| 扁平列表 | 几个 switch/select/number | `settingsFieldMap.ts` 的 `categoryItems` |
| 分组面板 | 一组字段 + 启用开关 + 测试连接 | `type: 'panel'` + `GroupPanelConfig` |
| 自定义页面 | 列表、预设、拖拽/排序、复杂交互 | `SettingsCategory.vue` 加分支 + 自己的 `.vue` |

功能编排（`dockPlugins`）是第三种：它有预设卡片、拖动排序和写服务端配置的开关，不适合用扁平字段描述。

## 2. 加一个扁平分类

1. `settingsFieldMap.ts` 的 `categoryItems`：

```ts
snippets: [
  {
    type: 'item',
    spec: {
      labelKey: 'settings.items.snippetsPageSize',
      descriptionKey: 'settings.items.snippetsPageSizeDesc',
      key: 'snippets.page_size',
      type: 'number',
      source: 'server',
      sectionHeader: 'settings.items.snippetsSection',
    },
  },
],
```

2. `SettingsIndex.vue` 的 `groupDefs` 加入口：

```ts
{ id: 'snippets', icon: Code2 },
```

3. 中英文案：

```ts
// zh
settings: {
  categories: { snippets: '代码片段' },
  items: {
    snippetsSection: '代码片段',
    snippetsPageSize: '每页条数',
    snippetsPageSizeDesc: '列表一页显示多少条。',
  },
}
```

4. 如果字段是 `source: 'server'`，确认服务端 `settings.go` 认识这个 key；否则保存会失败。

## 3. 加一个自定义页面

以功能编排为例：

1. 新建 `web/src/components/settings/SnippetsSetting.vue`。
2. `SettingsCategory.vue` 模板加分支：

```vue
<SnippetsSetting
  v-else-if="categoryId === 'snippets'"
  @navigate="(id: string) => $emit('navigate', id)"
/>
```

3. import 组件。
4. `categoryItems` 加空数组（保持“每个分类都登记”的约定；**空数组是合法状态**，专用面板不写扁平字段）：

```ts
snippets: [],
```

5. `SettingsIndex.vue` 加入口。
6. 中英文案。

自定义页面里需要跳到另一个设置分类时，**不要**直接调 `useSettingsNavigation().pushNav`：

- `useSettingsNavigation()` 的 `navStack` 是每次调用新建的，不是全局单例；
- 直接调只会改一份没人看的栈。

正确做法是 `emit('navigate', categoryId)`，由 `SettingsCategory` 转发给 `SettingsPage` 的 `pushNav`。

## 4. 从外部深链到设置分类

如果入口不在设置页内（例如顶部 logo、主题选择器、智能体抽屉），用：

```ts
import { setPendingSettingsCategory } from '@/composables/useSettingsNavigation'

setPendingSettingsCategory('snippets')
switchTab('settings')
```

`SettingsPage` 会在激活时消费它。不要自己维护一份 pending 状态。

## 5. 功能编排里的齿轮入口

`DOCK_TABS` 的 `settingsCategory` 字段就是给这个用的：

```ts
{
  id: 'mail',
  // ...
  settingsCategory: 'mail',
}
```

管理器会给这一行渲染一个齿轮图标（没有文字，`title` / `aria-label` 是“功能设置”），点击 `emit('navigate', 'mail')`，设置页跳到邮件分类。

### 5.1 一个页签一个配置页

配置页是**一对一**的：两个页签不共用一个分类，否则改一个页签的设置会顺带改另一个。文件管理与文件浏览就是这样拆开的：

| 页签 | `settingsCategory` | 说明 |
|---|---|---|
| 文件管理 `browse` | `files` | 搜索、列表/网格、排序、隐藏文件、上传 |
| 文件浏览 `view` | `fileView` | 最近文件数、自动换行、行号、粘性滚动、代码链接预览、预览模式、drawio |
| 历史 `history` | `gitHistory` | 每页提交数、默认隐藏合并提交、提交图、时间显示 |
| Forge `forge` | `forgeIntegration` | 凭据与通知项（总开关在功能编排） |
| 任务 `tasks` | `tasks` | 执行记录保留、通知、时区、默认筛选 |
| 终端 `terminal` | `terminal` | 终端外观与行为（总开关在功能编排） |
| 统计 `stats` | `stats` | 默认页签、默认时间范围、记住上次选择 |
| 邮件 `mail` | `mail` | 账号、同步与阅读（总开关在功能编排） |
| 知识库 `kb` | `kb` | 索引统计与重建（总开关在功能编排） |
| 会议 `live` | `meeting` | 纪要模型、中继等 |
| 分享 `share` | `share` | 允许分享的类型、下载、有效期（总开关在功能编排） |
| 端口映射 `proxy` | `portForward` | SSH 隧道端口 |
| 通知中心 `notifications` | `notifications` | 事件日志说明与清空 |

只有 `settings` 自己没有设置页。**不要为了凑齐而给 `settings` 建空配置页**：一个只有标题的设置页比没有更让人困惑。

插件被隐藏或被关闭时，管理器不渲染它的齿轮图标；面板已经挂载也不会丢状态，但入口跟着消失。

## 6. 总开关只在功能编排里

服务端总开关（`forge.enabled` / `mail.enabled` / `terminal.enabled` / `kb.enabled` / `tasks.enabled` / `share.enabled`）的**唯一**读写入口是功能编排：

- 管理器里的开关同时管显示与后台；
- 关闭前会确认（Forge 清空绑定、终端断开会话、任务取消运行、分享撤销链接）；
- 预设会把这些开关连同显示、顺序一起写，一次提交、失败整体回滚。

各功能自己的设置页只放**只读状态行**（`FeatureStatusRow.vue`）：显示「已启用 / 已停用」+「去功能编排」，点击 `navigate('dockPlugins')`。

不要在功能设置页里再放一个开关：

- 用户会以为两处各管一半；
- 关闭前的确认容易被绕过；
- 两处状态会在配置未刷新时短暂打架。

插件读开关状态用 `isFeatureEnabled(key)`（`useFeatureFlags.ts`），不要自己解析 `serverConfig`。

## 7. 设置页检查清单

- [ ] 分类 id 与 `DOCK_TABS.settingsCategory` 一致；
- [ ] `SettingsIndex` 有入口（且只出现一次）；
- [ ] `categoryItems` 有登记（自定义页面用空数组）；
- [ ] 中英 `settings.categories.<id>`；
- [ ] 中英 `settings.items.*`（如果有字段）；
- [ ] 自定义页面用 `emit('navigate')`，不用 `pushNav`；
- [ ] 服务端字段在 `settings.go` 有对应 key；
- [ ] 保存失败有 toast；
- [ ] 需要重启的字段标 `needsRestart`；
- [ ] 有服务端总开关的，页面里用只读状态行，不放开关。
