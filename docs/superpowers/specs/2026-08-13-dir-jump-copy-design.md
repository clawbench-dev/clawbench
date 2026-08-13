# 目录快速跳转 + 面包屑复制路径 设计文档

日期：2026-08-13
状态：已批准

## 1. 目标

在文件管理器与项目选择器两个界面中，各新增一个「跳转」按钮。点击后弹出对话框，用户可直接输入目录名称/路径，快速导航到该目录。同时，在文件面包屑（DirBreadcrumb）尾部增加一个「复制路径」按钮，一键复制当前目录的完整路径。

## 2. 需求要点

- 文件管理器工具栏增加跳转按钮。
- 项目选择器（ProjectDialog）工具栏增加跳转按钮。
- 点击后弹出对话框，输入目录路径后直接跳转；不存在时提示错误并保持当前浏览位置。
- 输入采用「直接路径输入」方式（相对路径或完整路径），不做子目录下拉联想。
- 需正确处理 Windows 风格路径（`\` 分隔符、盘符根如 `C:\`）。
- 文件面包屑尾部增加复制按钮，复制完整路径（Windows 下用 `\`），带复制反馈与 toast。

## 3. 架构与组件

### 3.1 共享跳转对话框 — `web/src/components/file/JumpDirDialog.vue`

一个基于 `ModalDialog` 的小型对话框，包含文本输入框 + 确认/取消按钮。

- props：`open: Boolean`。
- emits：`close`、`confirm(path: string)`。
- 确认时对输入做 trim；解析路径的「相对 vs 绝对」由调用方决定：
  - 文件管理器 → 项目相对路径。
  - 项目选择器 → 绝对路径（可含盘符）。
- 不在组件内调用任何后端 API，保持纯展示 + 输入归一化。

### 3.2 文件管理器 — `web/src/components/file/FileManagerContent.vue`

- 在 `dir-toolbar` 增加跳转按钮（`LocateFixed` / `CornerDownLeft` 图标）。
- 在 `useToolbarOverflow` 的 id 列表（当前第 682 行 `['refresh', 'newFile', 'newFolder', 'upload', 'uploadFolder', 'viewToggle', 'multiselect', 'hidden']`）中注册跳转按钮，窄屏自动收纳进 More 菜单。
- 确认回调 → `store.navigateToDir(path)`。复用 `loadFiles`（`stores/app.ts:356`）已有的失败回滚 + toast 逻辑，无需新增后端调用。

### 3.3 项目选择器 — `web/src/components/ProjectDialog.vue`

- 在 `dialog-toolbar-row`（第 9 行）增加跳转按钮。
- 确认回调 → `browseNavigate(path)`（第 149 行），已支持绝对路径与 Windows 盘符根（内部走 `/api/projects?path=`）。
- 路径错误时显示 toast 并保持当前浏览位置（复用现有 `loadBrowse` 失败分支）。

### 3.4 面包屑复制按钮 — `web/src/components/file/DirBreadcrumb.vue`

- 在面包屑末尾新增复制按钮（`Copy` 图标）。
- 复制 `props.path`，用原生分隔符（Windows `\`，Unix `/`）。
- 复用 `FileDetailsDrawer.vue:42` 的 `navigator.clipboard` + `execCommand` fallback 模式。
- 复制成功：短暂「copied」样式反馈 + toast。
- 该组件被文件管理器（相对路径）与项目选择器（绝对路径）共用，复制内容即各自传入的 `props.path`，两种场景均正确。

## 4. 数据流

```
用户点击跳转按钮
  → JumpDirDialog.open = true
  → 用户输入路径 → Confirm
  → emit('confirm', path)
  → 调用方：
      文件管理器: store.navigateToDir(path)  → loadFiles 失败时回滚+toast
      项目选择器: browseNavigate(path)       → /api/projects 失败时 toast+保持位置
  → 成功后关闭对话框
```

复制按钮：
```
点击复制 → clipboard.writeText(props.path)
  → 成功: copied 样式 + toast
```

## 5. 错误处理

- 文件管理器：`loadFiles` 已处理目录不存在（回滚到上一目录或项目根 + 提示）。
- 项目选择器：`loadBrowse` 已处理加载失败（toast + 清空列表，保持 browsePath）。
- 跳转对话框：空输入直接忽略（不导航）。

## 6. i18n

新增 `jump:` 块于 `zh.ts` / `en.ts`：

| key | zh | en |
|-----|----|----|
| `jump.title` | 跳转到目录 | Jump to Directory |
| `jump.placeholder` | 输入目录路径，如 src/utils | Enter a directory path, e.g. src/utils |
| `jump.confirm` | 跳转 | Jump |
| `jump.cancel` | 取消 | Cancel |
| `jump.button` | 跳转 | Jump |
| `jump.copyPath` | 复制路径 | Copy path |

## 7. 测试

- `JumpDirDialog.test.ts`：渲染、Enter/确认触发 `confirm`、取消触发 `close`、trim 输入。
- `FileManagerContent.test.ts`（扩展）：跳转按钮打开对话框，确认后调用 `navigateToDir`。
- `ProjectDialog.test.ts`（扩展）：跳转按钮打开对话框，确认后调用 `browseNavigate`。
- `DirBreadcrumb.test.ts`（扩展）：复制按钮写入完整路径、显示 copied 状态；Windows 路径分隔符正确。

## 8. 范围外（YAGNI）

- 不做子目录模糊联想/自动补全。
- 不做跳转历史。
- 不改动后端 API。
