# 07 · 检查清单：新增一个可选插件

照着从上到下做。每一步都写了“改哪、为什么”。

## 0. 先决定

- [ ] **id**：短、稳定、英文小写，例如 `snippets`。
- [ ] **group**：`workspace` / `project` / `personal` / `system`（只是管理器里的标签）。
- [ ] **closable**：能不能被用户隐藏？只有常驻三项（文件管理、文件浏览、设置）填 `false`。通知中心虽然也是“读入口”，但用户可以关（它没有 `feature`，关掉只写本机偏好）。
- [ ] **feature**：依赖服务端总开关吗？是的话在 `DockFeature` 加 key，并补 `FEATURE_FLAG_DEFAULTS`。
- [ ] **capability**：依赖设备能力吗？填标记，并在 `dock.ts` 里实现 `available()`。
- [ ] **settingsCategory**：有没有自己的设置页？有的话填分类 id（除 `settings` 外每个页签都应有）。

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

- [ ] 想清楚它默认排在哪：数组顺序就是默认顺序，也是“开发”预设的顺序。用户仍然可以把它拖到任何位置。
- [ ] 确认 `settings` 仍是 `closable: false`。

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

## 4. 服务端开关（可选）

只有需要“关掉就连后台一起停”的插件才做这一步。

- [ ] `dockTabs.ts` 的 `DockFeature` 加 key（例如 `snippets.enabled`），清单条目填 `feature: 'snippets.enabled'`。
- [ ] `useFeatureFlags.ts` 的 `FEATURE_FLAG_DEFAULTS` 加默认值（**缺键 = 开**）。
- [ ] 服务端：GET `/api/config` 返回、PATCH 白名单接受、关闭时接口拒绝、热更新。
- [ ] 不需要在 `dock.ts` 里写 `available()`——`feature` 字段已经驱动渲染过滤。

## 5. 加 `dock.ts`

在功能目录新建 `dock.ts`，只放运行时资源。没有需要的字段就留空/不写。

- [ ] 有角标：

```ts
badge: () => ({ count: store.state.snippetUnreadCount }),
```

- [ ] 有环境门控：

```ts
available: () => terminalRuntimeEnabled.value === true,
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
- [ ] 不要在模块顶层调用 `useSettingsConfig()` / 任何读配置的 composable：改配置的测试 mock 会在模块加载时撞 TDZ。要读就在函数体里读，或直接用 `isFeatureEnabled()`。
- [ ] 不 import 自己的面板。
- [ ] 不 import 别的插件的 `dock.ts`。
- [ ] 不在 `dock.ts` 里放 WebRTC / DOMPurify / 编辑器这类重依赖。

## 6. 加面板

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

## 7. 加设置页（可选）

- [ ] `settingsFieldMap.ts` 的 `categoryItems` 加分类。
- [ ] `SettingsIndex.vue` 的 `groupDefs` 加入口。左栏功能的配置页一律放进**功能模块**组（管理器 `dockPlugins` 打头，按 Dock 顺序排）；不属于任何页签的设置才进 项目与外观 / AI 与对话 / 模型能力 / 系统。
- [ ] 如果分类是自定义页面，在 `SettingsCategory.vue` 加分支。
- [ ] 中英文案。
- [ ] 管理器里的 `settingsCategory` 填这个分类 id。
- [ ] 有服务端总开关的功能，设置页里**不要**再放开关：用只读状态行 + 「去功能编排」入口。

详见 [09-settings.md](09-settings.md)。

## 8. 测试

- [ ] `dockTabs.test.ts`：如果改了顺序/可见性规则，加用例。
- [ ] `useDockPlugins.test.ts`：如果改了预设/隐藏/排序逻辑，加用例。
- [ ] `useDockLifecycle.test.ts`：如果新增了 `startBackground`，确认启停覆盖。
- [ ] `DockPluginSetting.test.ts`：如果改了管理器 UI，加用例。
- [ ] `dockSingleListWiring.test.ts`：如果动了 App.vue 的 Dock 渲染结构，确认它仍通过。
- [ ] 新面板至少一个 mount 测试。
- [ ] 变异验证：故意改错一行，测试应该红。

## 9. 跑检查

```bash
npm run typecheck
npx vitest run \
  web/src/composables/__tests__/dockTabs.test.ts \
  web/src/composables/__tests__/useDockPlugins.test.ts \
  web/src/composables/__tests__/useDockLifecycle.test.ts \
  web/src/components/settings/__tests__/DockPluginSetting.test.ts \
  web/src/components/settings/__tests__/SettingsIndex.test.ts \
  web/src/__tests__/dockSingleListWiring.test.ts \
  web/src/utils/__tests__/panelState.test.ts
npm run build
```

- [ ] 类型检查通过。
- [ ] 相关测试全绿。
- [ ] 构建成功，产物在 `.clawbench-web/`。

## 10. 手动验收

- [ ] 设置 → 功能编排能看到新插件。
- [ ] 关掉再打开，左栏入口跟着变。
- [ ] 拖动排序（任意行都能拖，包括常驻行），左栏顺序跟着变。
- [ ] 刷新页面，状态保持。
- [ ] 应用“办公”预设：开发相关项被隐藏/关闭，其余保持。
- [ ] 应用“极简”预设：只剩常驻四项，且确认框列出了会被停掉的东西。
- [ ] 当前停在该插件时关掉它，界面回到文件管理/聊天，不白屏。
- [ ] 通知深链跳到已隐藏/不可用的插件时被拒绝。
- [ ] 空状态正常。
- [ ] 如果它有 `startBackground`：隐藏后轮询停止，重新显示后恢复。
- [ ] 如果它有服务端开关：关掉后服务端确实停了（例如任务不再调度），重新打开不会补跑。
