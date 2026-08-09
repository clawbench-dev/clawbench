# 文件浏览独立 Tab 设计

日期：2026-08-09

## 背景

当前 `browse` 标签页是「合一」设计：一个 TabPanel 同时承载 `FileManagerContent`（文件管理器/目录浏览）和 `FileOverlay`（文件浏览/预览覆盖层）。打开文件时 `FileOverlay` 直接盖在目录浏览之上，两者耦合在同一标签。

目标：把「文件浏览」拆为独立的顶层 Tab（id = `view`），与「文件管理器」(`browse`) 解耦。

## 目标标签结构

- `browse`（文件管理器）：仅 `FileManagerContent`，不再叠加 `FileOverlay`。
- `view`（文件浏览，新增）：仅 `FileOverlay`（含其全部抽屉与事件）。无文件打开时显示空状态「未打开文件」。

## 关闭文件后的去向

关闭/返回当前文件后，停留在 `view` 页并显示空状态，不自动跳回文件管理器。

## 改动清单

### App.vue
1. 模板：`browse` 的 TabPanel 移除 `FileOverlay`；新增 `view` TabPanel 放入 `FileOverlay`（props/事件接线不变）。
2. 所有「打开文件」入口的 `switchTab('browse')` 改为 `switchTab('view')`：
   - `handleSelectFile`、`handleTaskOpenFile`、`handleAppHeaderRecentFileSelect`、`handleOpenFileOverlay`
   - `ChatPanelContent.navigateToFileViewer`、`TableRowModal`、`GitHistoryContent`
   - `open-file-manager` 事件保持切 `browse`；`open-file-overlay` 切 `view`。
3. 抽屉作用域重组：
   - `detailsDrawer`、`tocDrawer`、`searchDrawer`、`fileHistoryDrawer` → `useTabDrawer('view')`
   - `fileSearchDrawer` 保持 `useTabDrawer('browse')`
4. 快捷键/手势门控：
   - `fileManagerShortcutActive` 拆分为 browse 与 view 两组。
   - Ctrl+F 路由：`browse` → 目录搜索；`view` → 文件内搜索（去掉 `fileNav.overlayOpen` 分流）。
   - 边滑返回手势（`useFeatureBackHandler('file-overlay')`）门控改为 `panelIsActive('view')`。
5. `useFileWatch` 的 `fileManagerOpen` 包含 `browse` 与 `view`。
6. `switchTab('browse')` 副作用与 `handleWideDockTabClick`：`browse` 加载目录；`view` 无需额外加载（文件已全局）。
7. dock：
   - `overflowTabs` 加入 `'view'`；`overflowTabMeta` 加入 `view`（icon + `nav.fileView`）。
   - dock 溢出弹出层加入 `view` 按钮项。

### useWideScreenLayout.ts
- `WIDE_SCREEN_DOCK_TABS` 加入 `'view'`（默认左标签仍为 `browse`）。

### FileManagerContent.vue / FileViewer.vue
- 移除 `!fileNav.overlayOpen` 守卫（browse 不再承载 overlay），门控各自标签。

### 入口组件
- `ChatPanelContent.vue`：`navigateToFileViewer` → `switchTab('view')`。
- `TableRowModal.vue`、`GitHistoryContent.vue`：打开文件 → `switchTab('view')`。
- `AppHeader.vue`：最近文件点击 → `switchTab('view')`；「打开文件管理器」按钮保持 `switchTab('browse')`。

### i18n
- `nav.fileView`：en `File` / zh `文件`。

### 测试
- 同步 `'view'` 相关 tab 断言（useTabDrawer、useWideScreenLayout、FileManagerContent、TableRowModal、ChatPanelContent）。
- 新增：打开文件切到 `view`、关闭文件停留 `view` 空态。

## 影响面

- 纯前端导航重构，无后端/API/数据迁移改动。
- 改动约 8-12 个文件，主要集中在 App.vue 的 tab 编排与所有「打开文件 → 切标签」入口。
- 关键风险：漏改任一打开文件的入口，会导致打开文件后停留在管理器而非文件浏览页。
