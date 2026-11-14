# 07 · 检查清单：新增一个可选插件

照着从上到下做。每一步都写了“改哪、为什么”。

## 0. 先决定

- [ ] **id**：短、稳定、英文小写，例如 `snippets`。
- [ ] **group**：`workspace` / `project` / `personal` / `system`。
- [ ] **primary**：要不要固定在宽屏头部？大多数插件不需要。
- [ ] **closable**：能不能被用户隐藏？只有核心四件填 `false`。
- [ ] **devRelated**：是不是开发相关？是的话“标准”预设会隐藏它。
- [ ] **feature / capability**：依赖服务端功能或设备能力吗？填标记，并在 `dock.ts` 里实现 `available()`。
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

- [ ] 静态图标：`web/src/composables/dockTabMeta.ts`：

```ts
import { Code2 } from 'lucide-vue-next'

const DOCK_TAB_ICONS: Record<DockTabId, Component> = {
  // ...
  snippets: Code2,
}
```

- [ ] 图标随状态变（像 Forge 那样）：不写进 meta，在 `dock.ts` 里给 `icon: () => ...`。
- [ ] 图标不要放进 `dockTabs.ts`（零 import 红线）。

## 3. 加文案

- [ ] `web/src/i18n/locales/zh.ts`：
  - `nav.snippets`
  - `settings.dockPlugins.desc.snippets`
- [ ] `web/src/i18n/locales/en.ts`：同上。
- [ ] 如果加了设置分类：`settings.categories.snippets` 中英都要有。
- [ ] 如果加了设置项：`labelKey` / `descriptionKey` / `sectionHeader` 中英都要有。

## 4. 加 `dock.ts`

在功能目录新建 `dock.ts`，只放运行时资源。没有需要的字段就留空/不写。

- [ ] 有角标：

```ts
badge: () => ({ count: store.state.snippetUnreadCount }),
```

- [ ] 有门控：

```ts
available: () => (serverConfig.value?.snippets as Record<string, unknown> | undefined)?.enabled !== false,
```

- [ ] 打开时加载：

```ts
onEnter: () => { void loadSnippets() },
```

- [ ] 页签关闭也要跑的轮询：

```ts
startBackground: () => {
  startSnippetPolling()
  return stopSnippetPolling
},
```

- [ ] 终端这类“平台不支持时不要切走”的插件，加 `leaveWhenUnavailable`。
- [ ] 不 import 自己的面板。
- [ ] 不 import 别的插件的 `dock.ts`。
- [ ] 不在 `dock.ts` 里放 WebRTC / DOMPurify / 编辑器这类重依赖。

## 5. 加面板

- [ ] 新建 `web/src/components/snippets/SnippetsPanel.vue`。
- [ ] 在 `dock.ts` 的 `panel` 里指向它：

```ts
panel: {
  load: () => import('./SnippetsPanel.vue'),
  // 需要宿主 props/events 时才写：
  // props: (host) => ({ projectPath: host.projectPath }),
  // on: (host) => ({ 'open-file': host.openFile }),
}
```

- [ ] 不要在 `App.vue` 里写 `<TabPanel tabId="snippets">`；`DockPanelHost` 会统一渲染。
- [ ] 面板能单独打开（空状态）。
- [ ] 面板切走再切回状态还在（`TabPanel` 保活）。
- [ ] 面板需要轮询时看 `active`，不要用永不停的 `onMounted` 定时器。
- [ ] 面板不 import `App.vue` / 其他插件；需要的宿主能力从 `DockPanelHost` 的 host 取。

## 6. 加设置页（可选）

- [ ] `settingsFieldMap.ts` 的 `categoryItems` 加分类。
- [ ] `SettingsIndex.vue` 的 `groupDefs` 加入口。
- [ ] 如果分类是自定义页面，在 `SettingsCategory.vue` 加分支。
- [ ] 中英文案。
- [ ] 管理器里的 `settingsCategory` 填这个分类 id。

详见 [09-settings.md](09-settings.md)。

## 7. 测试

- [ ] `dockTabs.test.ts`：如果改了顺序/可见性规则，加用例。
- [ ] `useDockPlugins.test.ts`：如果改了预设/隐藏/排序逻辑，加用例。
- [ ] `useDockLifecycle.test.ts`：如果新增了 `startBackground`，确认启停覆盖。
- [ ] `DockPluginSetting.test.ts`：如果改了管理器 UI，加用例。
- [ ] 新面板至少一个 mount 测试。
- [ ] 变异验证：故意改错一行，测试应该红。

## 8. 跑检查

```bash
npm run typecheck
npx vitest run \
  web/src/composables/__tests__/dockTabs.test.ts \
  web/src/composables/__tests__/useDockPlugins.test.ts \
  web/src/composables/__tests__/useDockLifecycle.test.ts \
  web/src/components/settings/__tests__/DockPluginSetting.test.ts \
  web/src/components/settings/__tests__/SettingsIndex.test.ts \
  web/src/utils/__tests__/panelState.test.ts
npm run build
```

- [ ] 类型检查通过。
- [ ] 相关测试全绿。
- [ ] 构建成功，产物在 `.clawbench-web/`。

## 9. 手动验收

- [ ] 设置 → 插件与 Dock 能看到新插件。
- [ ] 关掉再打开，Dock 入口跟着变。
- [ ] 拖动排序，Dock 顺序跟着变。
- [ ] 刷新页面，状态保持。
- [ ] 应用“标准”预设，如果 `devRelated` 为 true，它被隐藏。
- [ ] 当前停在该插件时关掉它，界面回到文件管理/聊天，不白屏。
- [ ] 通知深链跳到已隐藏/不可用的插件时被拒绝。
- [ ] 空状态正常。
- [ ] 如果它有 `startBackground`：隐藏后轮询停止，重新显示后恢复。
