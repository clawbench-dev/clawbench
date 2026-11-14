# 09 · 设置：给插件一个自己的配置页

## 1. 三种设置页形态

| 形态 | 适合 | 怎么接 |
|---|---|---|
| 扁平列表 | 几个 switch/select/number | `settingsFieldMap.ts` 的 `categoryItems` |
| 分组面板 | 一组字段 + 启用开关 + 测试连接 | `type: 'panel'` + `GroupPanelConfig` |
| 自定义页面 | 列表、预设、拖拽/排序、复杂交互 | `SettingsCategory.vue` 加分支 + 自己的 `.vue` |

插件管理器（`dockPlugins`）是第三种：它有预设卡片、开关、拖动排序，不适合用扁平字段描述。

## 2. 加一个扁平分类

1. `settingsFieldMap.ts` 的 `categoryItems`：

```ts
snippets: [
  {
    type: 'item',
    spec: {
      labelKey: 'settings.items.snippetsEnabled',
      descriptionKey: 'settings.items.snippetsEnabledDesc',
      key: 'snippets.enabled',
      type: 'switch',
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
    snippetsEnabled: '启用代码片段',
    snippetsEnabledDesc: '关闭后不再出现在 Dock。',
  },
}
```

4. 如果字段是 `source: 'server'`，确认服务端 `settings.go` 认识这个 key；否则保存会失败。

## 3. 加一个自定义页面

以插件管理器为例：

1. 新建 `web/src/components/settings/SnippetsSetting.vue`。
2. `SettingsCategory.vue` 模板加分支：

```vue
<SnippetsSetting
  v-else-if="categoryId === 'snippets'"
  @navigate="(id: string) => $emit('navigate', id)"
/>
```

3. import 组件。
4. `categoryItems` 加空数组（保持“每个分类都登记”的约定）：

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

## 5. 插件管理器里的齿轮入口

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

配置页是**一对一**的：两个页签不共用一个分类，否则改一个页签的设置会顺带改另一个。文件管理与文件预览就是这样拆开的：

| 页签 | `settingsCategory` | 说明 |
|---|---|---|
| 文件管理 `browse` | `files` | 搜索、列表/网格、排序、隐藏文件、上传 |
| 文件预览 `view` | `fileView` | 最近文件数、自动换行、行号、粘性滚动、代码链接预览、预览模式、drawio |
| Forge `forge` | `forgeIntegration` | 服务端开关与凭据 |
| 终端 `terminal` | `terminal` | 终端外观与行为 |
| 邮件 `mail` | `mail` | 账号与服务端开关 |
| 知识库 `kb` | `kb` | 索引统计与重建 |
| 会议 `live` | `meeting` | 纪要模型、中继等 |
| 端口映射 `proxy` | `portForward` | SSH 隧道端口 |
| 通知中心 `notifications` | `notifications` | 事件日志说明与清空 |

Git 历史、任务、统计、分享没有可配的项，因此不填 `settingsCategory`，管理器里也不显示齿轮图标。**不要为了凑齐而给它们建空配置页**：一个只有标题的设置页比没有更让人困惑。

插件被隐藏时，管理器不渲染它的齿轮图标（`v-if="plugin.settingsCategory && !isHidden(plugin.id)"`）；面板已经挂载也不会丢状态，但入口跟着消失。

## 6. 不要在插件管理器里复制服务端开关

插件管理器的显示开关是**偏好**，只影响 Dock 入口。常驻页签也保留开关，但它是灰显且不可点的，只为让每一行的右侧形状一致。

Forge / 邮件的服务端开关在它们自己的设置页：

- Forge 关闭会停轮询并清空所有项目的仓库绑定，是破坏性操作，需要二次确认；
- 邮件关闭会停 IMAP 同步和邮件触发任务，但保留账号。

在管理器里再放一个“启用 Forge”的开关，会让用户以为两处是同一个东西，且容易绕过确认。管理器的职责是编排 Dock，不是管理服务端功能。

## 7. 设置页检查清单

- [ ] 分类 id 与 `DOCK_TABS.settingsCategory` 一致；
- [ ] `SettingsIndex` 有入口；
- [ ] `categoryItems` 有登记（自定义页面用空数组）；
- [ ] 中英 `settings.categories.<id>`；
- [ ] 中英 `settings.items.*`（如果有字段）；
- [ ] 自定义页面用 `emit('navigate')`，不用 `pushNav`；
- [ ] 服务端字段在 `settings.go` 有对应 key；
- [ ] 保存失败有 toast；
- [ ] 需要重启的字段标 `needsRestart`。
