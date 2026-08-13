# Design: Header 中段快捷键提示滚动条

## 目标

在 PC 版 `AppHeader` 中段空白区（`.badge-capsule` 与 `.server-toggle` 之间）显示一条自动滚动的快捷键使用提示。每条提示先在**横向**方向滚动展示（内容溢出时），滚动完毕后**纵向**（向上滑出）切换到下一条，循环播放。

核心诉求：**数据驱动**——后续新增提示只需补充文本（i18n 文案 + 数据数组一条），不改组件代码。

## 范围

- 仅 PC / 宽屏显示（非 APP 模式）。APP/窄屏不渲染，避免干扰移动端。
- 展示固定 5 条提示（见「Tip 内容」）。

## 架构

### 1. i18n 文案（双语）

在 `web/src/i18n/locales/zh.ts` 与 `en.ts` 新增命名空间 `appHeader.shortcutTip`，含每条的 `context`（面板/前提）与 `action`（说明/开启方法）文案。按键键名（`Enter`、`Ctrl+F` 等）为通用文本，不翻译。

### 2. 数据模块 `web/src/config/shortcutTips.ts`

```ts
export interface ShortcutTipDef {
  contextKey: string  // i18n key → 面板 + 前提
  keys?: string[]     // 高亮按键，可选
  actionKey: string   // i18n key → 说明 / 开启方法
}
export const SHORTCUT_TIPS: ShortcutTipDef[] = [ ... 5 条 ... ]
```

组件只读该数组。新增提示 = 在数组追加一条 + 在 en/zh 各加一条文案，不改组件。

### 3. 组件 `web/src/components/common/ShortcutTipTicker.vue`

- `props`: `tips: ShortcutTipDef[]`（默认 `SHORTCUT_TIPS`），便于测试注入。
- 内部维护 `currentIndex`，展示 `tips[currentIndex]`。
- 渲染：`context` 文本 + `<kbd>` 按键 + `action` 文本。
- **横向滚动**：内容溢出时，基于 `HeaderMarquee` 的行为做横向滚动；未溢出则直接等待。
- **纵向切换**：当前条展示/滚动完成后，`currentIndex` 推进，外层用 CSS transition 向上滑出并滑入下一条。
- 计时：每条默认停留 `SHOW_MS`（如 4000ms）；溢出则横向滚动完成后再停留 `SCROLL_PAUSE_MS`（如 1000ms）才切下一条。
- 循环播放：到最后一条后回到第一条。

### 4. 接入 `AppHeader.vue`

- 在 `.badge-capsule` 与 `.server-toggle` 之间插入 `<ShortcutTipTicker />`。
- 仅在非 APP 模式显示（`v-if="!isAppMode"`）。
- 容器 `flex: 1; min-width: 0; overflow: hidden; margin: 0 8px;`，占据中段空白区。

### 5. 开关设置（默认开启）

新增本地设置 `headerShortcutTips`（`source: 'local'`，localStorage），默认 `true`：
- `web/src/composables/useSettingsConfig.ts` `localDefaults` 加入 `headerShortcutTips: true`。
- `settingsFieldMap.ts` `appearance` 分类新增 switch 项（分组「顶栏」）。
- i18n `settings.items.headerShortcutTips` / `headerShortcutTipsDesc`（双语）。
- `AppHeader.vue` 用 `v-if="!isAppMode && localConfig.headerShortcutTips"` 控制显示，关闭即隐藏，改动即时生效（localConfig 为响应式）。

## Tip 内容（5 条）

1. 聊天页 · 输入框内：`Enter` 发送 · `Shift+Enter` 换行
2. 任意页 · 非输入框：`Ctrl+F` 全局搜索
3. 聊天页 · 聚焦任意处：`Ctrl+←` / `Ctrl+→` 切换聊天会话
4. 聊天页 · 上一条为助手回复时：输入框上方出现「对话推荐」建议，点「填入」采纳下一步操作
5. 对话推荐 · 开启方法：设置 → 对话推荐 → 打开「对话推荐」开关（需配置 AI 摘要模型）

## 错误处理与边界

- 空 tip 数组：组件不渲染、不报错。
- 单条 tip：正常展示，不循环跳变（只有一条时停留展示）。
- `keys` 缺失（如第 4、5 条）：只渲染 context + action，无 `<kbd>`。
- 计时器在组件卸载时清理，避免泄漏。

## 测试

`web/src/components/common/__tests__/ShortcutTipTicker.test.ts`：
- 挂载后显示第一条 context/action。
- `keys` 渲染为 `<kbd>`；无 `keys` 不渲染 `<kbd>`。
- 触发定时器后 `currentIndex` 推进（纵向切换逻辑）。
- 循环：最后一条后回到第一条。
- 空数组不报错。
- 卸载时清理定时器。

## 验收

- PC 版 header 中段自动滚动展示 5 条提示，横向滚动→纵向切换。
- 新增提示只改 i18n + 数据数组，不动组件。
