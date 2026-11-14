# 06 · 测试：新增插件要补什么

## 1. 纯函数测试

`web/src/composables/__tests__/dockTabs.test.ts` 覆盖：

- 默认顺序；
- 用户顺序；
- 隐藏过滤；
- `settings` 永远最后；
- `primaryDockTabs` 过滤 `history`；
- `mergeDockOrder` 丢弃未知/重复、追加缺失；
- 终端/端口映射/Forge/邮件的运行时门控。

新增插件如果改变顺序/可见性规则，在这里加用例。

## 2. 状态测试

`web/src/composables/__tests__/useDockPlugins.test.ts` 覆盖：

- 默认 `developer`；
- `standard` 只隐藏 `devRelated`；
- `minimal` 只留核心；
- 手动显示/隐藏 → `custom`；
- 核心页签不能隐藏；
- 上下移与边界；
- 持久化与重读；
- `reset`；
- `usePluginProfile`。

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

- 三个预设 + 所有插件行；
- 点“标准”后 `terminal`/`forge` 被隐藏；
- 开关隐藏邮件 → `custom`；
- 上下移；
- “功能设置” emit `navigate`；
- 核心页签没有开关。

新增管理器 UI 时在这里加用例。

## 4. 守卫测试

现有守卫：

| 文件 | 守住什么 |
|---|---|
| `useWideScreenLayout.test.ts` | 渲染列表 ⊆ 切换白名单；主次顺序；门控过滤 |
| `panelState.test.ts` | `PanelId` 与 `PANEL_ENTRIES` 一致；哪些状态是全局/按项目 |
| `SettingsIndex.test.ts` | 分类数量、分组分布、每个分类都有中英标签 |
| `settingsFieldMap.test.ts` | 每个设置项的 label/description/sectionHeader 都能解析 |
| `settingsControls.css.test.ts` | 开关/滑块的尺寸只在全局样式里定义 |

新增插件后至少跑：

```bash
npx vitest run \
  web/src/composables/__tests__/dockTabs.test.ts \
  web/src/composables/__tests__/useDockPlugins.test.ts \
  web/src/components/settings/__tests__/DockPluginSetting.test.ts \
  web/src/components/settings/__tests__/SettingsIndex.test.ts \
  web/src/utils/__tests__/panelState.test.ts
```

## 5. i18n

每个新 key 必须同时存在于 `zh.ts` 与 `en.ts`。漏一个会在界面显示原始 key，不会构建失败。

至少检查：

- `nav.<id>`
- `settings.dockPlugins.desc.<id>`
- 设置分类的 `settings.categories.<id>`（如果有）
- 设置项的 `labelKey` / `descriptionKey` / `sectionHeader`（如果有）

`SettingsIndex.test.ts` 会读真实 locale 文件检查分类标签；`settingsFieldMap.test.ts` 会检查设置项文案。

## 6. 类型检查

```bash
npm run typecheck
```

当前仓库里 `web/src/share/ShareView.vue` 有一个与插件无关的既有类型错误（`fitToScreen` on `never`）。如果只有这一条，说明你的改动没有新增类型问题；不要为了让它变绿去改无关文件。

## 7. 构建

纯前端改动：

```bash
npm run build
```

产物在 `.clawbench-web/`，服务端 disk 模式会立即读取，不需要重启。构建前确认没有其他 agent 正在跑全量测试或构建（会争抢内存，导致假失败）。

## 8. E2E（可选）

涉及“设置页改 Dock → 刷新后保持”的跨组件流程，建议补一条 Playwright spec：

1. 打开 设置 → 插件与 Dock；
2. 隐藏“邮件”；
3. 关闭设置，确认 Dock 没有邮件入口；
4. 刷新页面，确认仍然没有；
5. 重新显示，确认入口和未读角标回来。

再补一条：功能关闭时，通知深链跳邮件被拒绝，当前页签不变。

E2E 只跑新增/受影响的 spec：

```bash
npx playwright test --config e2e/playwright.config.ts \
  --project=chromium-coverage e2e/specs/<your>.spec.ts
```

## 9. 变异验证

对“隐藏/顺序/预设”这种状态逻辑，改完实现后至少做一次变异：

- 把 `hidden.has(id)` 改成 `!hidden.has(id)`，测试应该红；
- 把 `settings` 从固定尾部去掉，测试应该红；
- 把 `preset = 'custom'` 删掉，测试应该红。

如果变异后测试仍然绿，说明测试是同义反复，需要重写。
