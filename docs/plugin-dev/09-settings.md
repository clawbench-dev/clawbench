# 09 · 设置：给插件一个自己的配置页

## 1. 三种设置页形态

| 形态 | 适合 | 怎么接 |
|---|---|---|
| 扁平列表 | 几个 switch/select/number | `settingsFieldMap.ts` 的 `categoryItems` |
| 分组面板 | 一组字段 + 启用开关 + 测试连接 | `type: 'panel'` + `GroupPanelConfig` |
| 自定义页面 | 列表、预设、拖拽/排序、复杂交互 | `SettingsCategory.vue` 加分支 + 自己的 `.vue` |

插件管理器（`dockPlugins`）是第三种：它有预设卡片、开关、上下移，不适合用扁平字段描述。

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

## 5. 插件管理器里的“功能设置”链接

`DOCK_TABS` 的 `settingsCategory` 字段就是给这个用的：

```ts
{
  id: 'mail',
  // ...
  settingsCategory: 'mail',
}
```

管理器会给这一行渲染“功能设置”按钮，点击 `emit('navigate', 'mail')`，设置页跳到邮件分类。

## 6. 不要在插件管理器里复制服务端开关

插件管理器的显示开关是**偏好**，只影响 Dock 入口。

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
