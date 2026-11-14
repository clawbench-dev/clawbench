# 07 · 检查清单：新增一个可选插件

照着从上到下做。每一步都写了“改哪、为什么”。

## 0. 先决定

- [ ] **id**：短、稳定、英文小写，例如 `snippets`。
- [ ] **group**：`workspace` / `project` / `personal` / `system`。
- [ ] **primary**：要不要固定在宽屏头部？大多数插件不需要。
- [ ] **closable**：能不能被用户隐藏？只有核心四件填 `false`。
- [ ] **devRelated**：是不是开发相关？是的话“标准”预设会隐藏它。
- [ ] **feature / capability**：依赖服务端功能或设备能力吗？填标记，但记得实际门控要自己接线。
- [ ] **settingsCategory**：有没有自己的设置页？有的话填分类 id。

## 1. 加类型与清单

- [ ] `web/src/composables/dockTabs.ts` 的 `TabId` 联合类型加 `'snippets'`。
- [ ] `DOCK_TABS` 加一条：

```ts
{
  id: 'snippets',
  titleKey: 'nav.snippets',
  descriptionKey: 'settings.dockPlugins.desc.snippets',
  group: 'project',
  closable: true,
}
```

- [ ] 如果 `primary: true`，确认它在数组中的位置符合固定头顺序。
- [ ] 确认 `settings` 仍在数组最后。

## 2. 加图标

- [ ] `web/src/composables/dockTabMeta.ts`：

```ts
import { Code2 } from 'lucide-vue-next'

const DOCK_TAB_ICONS: Record<DockTabId, Component> = {
  // ...
  snippets: Code2,
}
```

- [ ] 图标只放这里，不要放进 `dockTabs.ts`（零 import 红线）。

## 3. 加文案

- [ ] `web/src/i18n/locales/zh.ts`：
  - `nav.snippets`
  - `settings.dockPlugins.desc.snippets`
- [ ] `web/src/i18n/locales/en.ts`：同上。
- [ ] 如果加了设置分类：`settings.categories.snippets` 中英都要有。
- [ ] 如果加了设置项：`labelKey` / `descriptionKey` / `sectionHeader` 中英都要有。

## 4. 加面板

- [ ] 新建 `web/src/components/snippets/SnippetsPanel.vue`。
- [ ] 在 `App.vue` import：

```ts
// 可选面板：懒加载
const SnippetsPanel = defineAsyncComponent(
  buildAsyncComponentOptions({
    loader: () => import('./components/snippets/SnippetsPanel.vue'),
  }),
)
```

- [ ] 在左栏加 `<TabPanel>`：

```vue
<TabPanel tabId="snippets" :activeTab="leftPanelActive" :noHeader="true">
  <SnippetsPanel :active="panelIsActive('snippets')" />
</TabPanel>
```

- [ ] 面板能单独打开（空状态）。
- [ ] 面板切走再切回状态还在（`TabPanel` 保活）。
- [ ] 面板需要轮询时看 `active`，不要用永不停的 `onMounted` 定时器。

## 5. 加角标（可选）

如果不需要角标，跳过。

- [ ] `dockTabBadgeCount(tab)` 加 `case`。
- [ ] `dockTabBadgeAnim(tab)` 加 `case`。
- [ ] `dockTabBadgeAnimEnd(tab)` 加 `case`。
- [ ] 窄屏 Dock 的 inline 模板加 `v-if` 条件。
- [ ] `overflowBadgeCount` 汇总加/减。
- [ ] 如果角标需要常驻轮询，在 `App.vue` 里成对启动/停止。
- [ ] 隐藏插件时角标不再参与汇总。

> 下一阶段的 `dock.ts` 会把这五处收成一处；当前先按现状做。

## 6. 加门控（可选）

如果插件不依赖功能/能力，跳过。

- [ ] 加一个 computed（例如 `isSnippetsDisabled`）。
- [ ] `overflowTabs` 传入对应过滤参数（或扩展 `secondaryDockTabs` 的选项）。
- [ ] `switchTab` 开头加 `if (tab === 'snippets' && ...) return`。
- [ ] 加 `watch`，开关关闭时把当前页签切到 `browse`/`chat`。
- [ ] 清单里填 `feature` / `capability` 标记，让管理器显示标签。
- [ ] 如果是服务端功能，确认服务端端点/worker 也检查开关，不能只藏 UI。

## 7. 加打开副作用（可选）

- [ ] 如果打开时要加载数据，优先放在面板的 `active` watcher；确实需要全局的才放 `switchTab`。
- [ ] 失败要有可见错误状态。
- [ ] 不要阻塞切换。

## 8. 加设置页（可选）

- [ ] `settingsFieldMap.ts` 的 `categoryItems` 加分类。
- [ ] `SettingsIndex.vue` 的 `groupDefs` 加入口。
- [ ] 如果分类是自定义页面，在 `SettingsCategory.vue` 加分支。
- [ ] 中英文案。
- [ ] 管理器里的 `settingsCategory` 填这个分类 id。

详见 [09-settings.md](09-settings.md)。

## 9. 测试

- [ ] `dockTabs.test.ts`：如果改了顺序/可见性规则，加用例。
- [ ] `useDockPlugins.test.ts`：如果改了预设/隐藏/排序逻辑，加用例。
- [ ] `DockPluginSetting.test.ts`：如果改了管理器 UI，加用例。
- [ ] 新面板至少一个 mount 测试。
- [ ] 变异验证：故意改错一行，测试应该红。

## 10. 跑检查

```bash
npm run typecheck
npx vitest run \
  web/src/composables/__tests__/dockTabs.test.ts \
  web/src/composables/__tests__/useDockPlugins.test.ts \
  web/src/components/settings/__tests__/DockPluginSetting.test.ts \
  web/src/components/settings/__tests__/SettingsIndex.test.ts \
  web/src/utils/__tests__/panelState.test.ts
npm run build
```

- [ ] 类型检查没有新增错误（忽略 `ShareView.vue` 的既有错误）。
- [ ] 相关测试全绿。
- [ ] 构建成功，产物在 `.clawbench-web/`。

## 11. 手动验收

- [ ] 设置 → 插件与 Dock 能看到新插件。
- [ ] 关掉再打开，Dock 入口跟着变。
- [ ] 上下移，Dock 顺序跟着变。
- [ ] 刷新页面，状态保持。
- [ ] 应用“标准”预设，如果 `devRelated` 为 true，它被隐藏。
- [ ] 当前停在该插件时关掉它，界面回到文件管理/聊天，不白屏。
- [ ] 通知深链跳到已隐藏的插件时被拒绝。
- [ ] 空状态正常。
