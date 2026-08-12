# 终端独立主题切换（支持 xterm.js 全部 157 主题，懒加载）

日期：2026-08-12

## 目标

为 Web 终端提供独立的主题切换功能，支持社区 `xterm-theme` 包的 **全部 157 种主题**（Dracula、Nord、Solarized、One Dark、Material、Monokai 等）。

- 默认仍「跟随 App 深/浅色主题」（等价于现有 Dark/Light 两主题），可选任意固定主题覆盖，可随时切回「跟随 App 主题」。
- **主题资源懒加载**：`xterm-theme` 通过 `import()` 动态导入，不打包进主 bundle，仅在使用时按需加载。
- 主题选择**持久化**到 localStorage，跨会话保存，对所有终端 tab 统一生效。

## 背景

当前 `TerminalPanelContent.vue` 只有两个硬编码主题 `darkTheme` / `lightTheme`（Catppuccin 风格），通过 `getXtermTheme()` 按 `document.documentElement[data-theme]` 返回，再用 MutationObserver 在 App 主题变化时调用 `tabManager.updateTheme()` 更新。**没有面向用户的切换入口**。

xterm.js 核心本身**不内置任何主题集合**——主题由外部提供。社区 `xterm-theme` 包（v1.1.0，零依赖）是事实上的「全部主题」集合，共 157 个，输出为 xterm 的 `ITheme` 结构（foreground/background/cursor + 16 色）。

## 范围

- 仅 Web 终端前端。不涉及 Go 后端、不改现有终端会话/键盘/手势逻辑。
- 新增主题选择入口 + 懒加载主题数据 + 持久化。

## 架构

### 1. 新增工具模块 `web/src/utils/terminalThemes.ts`（纯函数，可单测）

```ts
export const TERMINAL_THEME_AUTO = 'auto'          // 「跟随 App 主题」特殊值
export const TERMINAL_THEME_STORAGE_KEY = 'terminalTheme'  // localConfig key
```

- `THEME_IDS: string[]`：157 个主题 id 的**静态列表**（构建期硬编码，不 import 数据）。用于渲染选择列表，不触发懒加载。
- `async loadAllThemes(): Promise<Record<string, ITheme>>`：动态 `import('xterm-theme')` 返回全部主题对象。仅在选择列表打开时或选中固定主题时调用。
- `async resolveTheme(selection: string, isAppDark: boolean): Promise<Record<string, unknown>>`：
  - `selection === TERMINAL_THEME_AUTO` → 返回现有 `darkTheme`/`lightTheme`（按 isAppDark），**不触发** xterm-theme 加载。
  - 否则 → `await loadAllThemes()` 后取 `themes[selection]`，若缺失则回退到按 isAppDark 的自动主题。
- `formatThemeName(id: string): string`：把 `Solarized_Dark` → `Solarized Dark`（下划线→空格），用于展示。
- 静态色板（`darkTheme` / `lightTheme`）从 `TerminalPanelContent.vue` 迁移到此模块并导出，供 `getXtermTheme` 与 `resolveTheme` 共用。

### 2. 持久化

在 `useSettingsConfig.ts`：
- `localDefaults` 增加 `terminalTheme: 'auto'`。
- `legacyKeys` 增加 `terminalTheme: { key: '', format: 'raw' }`（无旧 key、无 sideEffect，仅持久化 prefixed key）。
- 读/写统一走现有 `localConfig.terminalTheme` + `setLocalConfig('terminalTheme', ...)`。

### 3. 主题应用与自动跟随

`TerminalPanelContent.vue`：

- 新增 `themeSelection = ref(localConfig.terminalTheme)`，同步自 `localConfig`。
- 原 `getXtermTheme()` 改为**同步返回当前选中主题**（auto 时按 App 深/浅）。因为 xterm 实例创建时 `theme` 是同步选项，固定主题的懒加载可在创建前异步 resolve 后再 open。
- `tabManager` 的 `getXtermTheme` 回调仍同步返回当前主题对象，保证 `createTab` 首次同步可用。
- 保留 MutationObserver：仅当 `themeSelection === TERMINAL_THEME_AUTO` 时，App 主题变化才触发 `tabManager.updateTheme(getXtermTheme())`。
- 新增 `applyTheme(selection)`：设置 `themeSelection` + `setLocalConfig('terminalTheme', selection)`，异步 resolve 主题后 `tabManager.updateTheme(...)`。

### 4. 切换入口 UI（TerminalPanelContent.vue）

- tab 栏 `+ 新建`按钮旁（`terminal-tab-add` 右侧）新增一个调色板图标按钮（`Palette` from lucide-vue-next），PC/移动端均可见。
- 点击弹出 `PopupMenu`（复用现有 `@/components/common/PopupMenu.vue`，参照 `showCommands` 快速指令弹层）：
  - 顶部固定「跟随 App 主题」项（当前项高亮）。
  - 下方为 157 项主题列表，**带搜索过滤输入框**（太多主题，必须可搜）。
  - 点击某项 → `applyTheme(selection)` → 关闭弹层。
- **首次打开弹层时才 `await loadAllThemes()`**（懒加载触发点）；加载中显示 loading。

### 5. i18n

`web/src/i18n/locales/zh.ts` / `en.ts` 的 `terminal` 节新增：
- `theme: '主题'`
- `themeFollowApp: '跟随 App 主题'`
- `themeSearchPlaceholder: '搜索主题...'`
- `themeLoading: '加载主题中...'`

## 数据流

1. 用户点击 tab 栏调色板按钮 → 打开 `PopupMenu`，触发 `loadAllThemes()`（懒加载 chunk）。
2. 列表渲染：顶部「跟随 App 主题」+ 157 项，搜索过滤。
3. 用户选择 → `applyTheme(selection)`：
   - 写 `localConfig.terminalTheme`（持久化）。
   - `resolveTheme(selection, isAppDark)` → theme 对象。
   - `tabManager.updateTheme(theme)` → 所有 tab 的 `xterm.options.theme` 更新。
4. 后续刷新页面：初始化 `themeSelection = localConfig.terminalTheme`，tab 创建时 `getXtermTheme()` 返回对应主题。

## 错误处理

- `loadAllThemes()` 失败（网络/构建问题）：toast 提示「主题加载失败」，列表显示重试，保留当前主题不变。
- 选中 id 在 `THEME_IDS` 中不存在或懒加载对象缺失：回退到「跟随 App 主题」。

## 测试

- `web/src/utils/__tests__/terminalThemes.test.ts`：
  - `resolveTheme('auto', true/false)` 返回对应暗/亮静态主题，且**不触发** `loadAllThemes`（mock 断言未被调用）。
  - `resolveTheme('Dracula', ...)` 懒加载后返回 Dracula 主题；未知 id 回退到自动主题。
  - `formatThemeName` 下划线转空格。
  - `TERMINAL_THEME_STORAGE_KEY === 'terminalTheme'`、`THEME_IDS.length === 157`。
- `web/src/components/__tests__/terminalPanelSelection.test.ts` 或新增组件测试：
  - 调色板按钮存在；点击打开弹层；「跟随 App 主题」默认选中；选择 Dracula 后 `applyTheme` 更新 localConfig 与 tab 主题。
- 手动验证：切换主题 → 刷新页面保持；切回「跟随 App 主题」恢复自动跟随。

## 不做的（YAGNI）

- 不做主题自定义编辑器（逐色自定义）。
- 不做按 tab 独立主题（统一作用于全部 tab）。
- 不引入后端配置（纯前端 localStorage）。
