# 06 · 测试：新增插件要补什么

> **已过时（2026-11-21 契约收口后）**：顺序（`order`）、设置字段（`settings.ts`）、插件文案（`i18n.ts`）现在都写在 `web/src/plugins/<id>/` 里，本文描述的接线位置不再适用。操作步骤一律照 [`README.md`](README.md)；本文件只保留仍然成立的概念解释。

## 1. 纯函数测试

`web/src/composables/__tests__/dockTabs.test.ts` 覆盖：

- 默认顺序（= “开发”预设的顺序）；
- 完整用户顺序原样生效（含把 `settings` 拖到第一位）；
- 隐藏过滤（常驻四项永远不被隐藏）；
- 分享可隐藏；
- `mergeDockOrder`：老数据只含可拖项时**保留常驻页签的槽位**、丢弃未知/重复、缺项留在默认槽位；
- `featureDisabled` 过滤（六个服务端开关）；
- `unavailable` 过滤（终端 / 端口映射等能力）。

新增插件如果改变顺序/可见性规则，在这里加用例。

## 2. 状态测试

`web/src/composables/__tests__/useDockPlugins.test.ts` 覆盖：

- 默认 `developer`；
- 三个预设各自的隐藏集合、顺序、服务端开关；
- 预设的 `commit` 只调用一次（一次提交）；
- `commit` 失败时本地状态一格不动（整体回滚）；
- 手动显示/隐藏 → `custom`；
- 常驻页签不能隐藏；
- 拖动排序（全量列表、`settings` 可拖到首位）；
- 持久化与重读；
- `reset`（走同一条 commit 路径）；
- `usePluginProfile`。

`web/src/composables/__tests__/useDockLifecycle.test.ts` 覆盖：

- 可见 + 可用时启动 `startBackground`；
- 不可用 / 隐藏时停；
- 恢复可见/可用时重启；
- 组件卸载时清理。

`web/src/composables/__tests__/dockPluginRuntime.test.ts` 覆盖：

- 各插件的角标来源；
- Forge / 邮件 / 终端 / 端口映射的 `available()`；
- Forge 的动态标题；
- 邮件后台资源的启停；
- `browse` / `chat` / `tasks` 的 `onEnter`。

每个用例前：

```ts
beforeEach(() => {
  localStorage.clear()
  _resetDockPluginsForTest()
})
```

`useDockPlugins` 是模块级单例，不 reset 会把上一个用例的状态带到下一个。

## 3. 组件测试

`web/src/components/settings/__tests__/DockPluginSetting.test.ts` 覆盖：

- 三个预设 + 全部 14 行；
- 每一行都有拖动手柄（含常驻与 `settings`）；
- 常驻四行的开关灰显、文案“常驻”；
- 开关文案三态：启用 / 在 Dock 中显示 / 常驻；
- 只控显示的页签：切换只写本地并变 `custom`；
- 服务端功能页签：切换写 `setServerValue`；
- 破坏性开关（Forge / 终端 / 任务 / 分享）关闭前确认，取消则不写；
- 保存失败：toast + 开关回到原状态；
- 能力缺失（终端无 PTY / 端口映射无通道）时开关灰显并写原因；
- 预设：确认、取消不生效、一次 PATCH、失败回滚；
- 齿轮 emit `navigate`，且每个 `settingsCategory` 都存在于 `categoryItems`。

`web/src/components/dock/__tests__/DockPanelHost.test.ts` 覆盖：

- 渲染 `panel.component` / `panel.load` 的组件；
- 传 `active`；
- `panel.props(host)` 的额外 props；
- `panel.on(host)` 的事件。

新增管理器 UI 时在这里加用例。

## 4. 守卫测试

现有守卫：

| 文件 | 守住什么 |
|---|---|
| `dockTabs.test.ts` | 清单完整性、顺序合并（槽位保留）、三种门控、常驻四项 |
| `useWideScreenLayout.test.ts` | 渲染列表 ⊆ 切换白名单；每种门控组合下都仍可切换 |
| `dockSingleListWiring.test.ts` | 宽屏只有一条 `v-for visibleDockTabs`；窄屏从同一列表取溢出；指示器按渲染顺序；固定头标识符不得复活 |
| `panelState.test.ts` | `PanelId` 与 `PANEL_ENTRIES` 一致；哪些状态是全局/按项目 |
| `forgeDockTabTitleWiring.test.ts` | Forge 的 title/icon/badge/available 在 `forge/dock.ts`；App.vue 走 runtime 解析 |
| `mailDockTabWiring.test.ts` | 邮件的 available/startBackground 在 `mail/dock.ts`；App.vue 不再自己启停轮询 |
| `dockPanelCoverage.test.ts` | 每个非核心页签都有 `panel`；App.vue 不直接 import 可选面板 |
| `SettingsIndex.test.ts` | 分类数量、分组分布、每个分类都有中英标签、每个分类在首页只出现一次 |
| `settingsFieldMap.test.ts` | 每个设置项的 label/description/sectionHeader 都能解析 |
| `settingsRowTypography.css.test.ts` | 设置行文字统一字号（含管理器拖动手柄用 2xl 而不是行标签的 xl） |
| `settingsControls.css.test.ts` | 开关/滑块的尺寸只在全局样式里定义 |

新增插件后至少跑：

```bash
npx vitest run \
  web/src/composables/__tests__/dockTabs.test.ts \
  web/src/composables/__tests__/useDockPlugins.test.ts \
  web/src/composables/__tests__/useDockLifecycle.test.ts \
  web/src/composables/__tests__/dockPluginRuntime.test.ts \
  web/src/composables/__tests__/dockPanelCoverage.test.ts \
  web/src/components/dock/__tests__/DockPanelHost.test.ts \
  web/src/components/settings/__tests__/DockPluginSetting.test.ts \
  web/src/components/settings/__tests__/SettingsIndex.test.ts \
  web/src/__tests__/dockSingleListWiring.test.ts \
  web/src/utils/__tests__/panelState.test.ts
```

## 5. i18n

每个新 key 必须同时存在于 `zh.ts` 与 `en.ts`。漏一个会在界面显示原始 key，不会构建失败。

至少检查：

- `nav.<id>`
- `settings.dockPlugins.desc.<id>`
- 设置分类的 `settings.categories.<id>`（如果有）
- 设置项的 `labelKey` / `descriptionKey` / `sectionHeader`（如果有）
- 有服务端开关的：`settings.dockPlugins.consequences.<section>`（关闭确认文案）

`SettingsIndex.test.ts` 会读真实 locale 文件检查分类标签；`settingsFieldMap.test.ts` 会检查设置项文案。

## 6. 类型检查

```bash
npm run typecheck
```

如果报错，先确认错误不在你改的文件里——可能是另一个 agent 的并行改动。不要为了让 typecheck 变绿去改无关文件。

## 7. 构建

纯前端改动：

```bash
npm run build
```

产物在 `.clawbench-web/`，服务端 disk 模式会立即读取，不需要重启。构建前确认没有其他 agent 正在跑全量测试或构建（会争抢内存，导致假失败）。

## 8. E2E（可选）

涉及“功能编排改 Dock → 刷新后保持”的跨组件流程，建议补一条 Playwright spec：

1. 打开 设置 → 功能编排；
2. 关掉“邮件”；
3. 关闭设置，确认左栏没有邮件入口；
4. 刷新页面，确认仍然没有；
5. 重新打开，确认入口和未读角标回来。

再补一条预设：应用“极简”，确认只剩常驻四项、且确认框列出会被停掉的东西；取消则一项都不变。

E2E 只跑新增/受影响的 spec：

```bash
npx playwright test --config e2e/playwright.config.ts \
  --project=chromium-coverage e2e/specs/<your>.spec.ts
```

## 9. 变异验证

对“隐藏/顺序/预设”这种状态逻辑，改完实现后至少做一次变异：

- 把 `hidden.has(id)` 改成 `!hidden.has(id)`，测试应该红；
- 把 `mergeDockOrder` 的槽位保留改成“追加到末尾”，测试应该红；
- 把 `preset = 'custom'` 删掉，测试应该红；
- 把预设的 `commit` 挪到本地状态之后，回滚用例应该红。

如果变异后测试仍然绿，说明测试是同义反复，需要重写。
