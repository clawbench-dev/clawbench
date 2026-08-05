# 文件源码编辑模式 — 设计文档

日期：2026-08-05
状态：已批准

## 背景与目标

ClawBench 的文件查看器当前为只读（`CodePreview.vue` 用 highlight.js 渲染代码，无编辑能力）。本功能为「源码 / 纯文本」视图新增一个「编辑」按钮，带选中/非选中状态：

- **非选中（查看模式）**：默认，渲染现有 `CodePreview`。
- **选中（编辑模式）**：进入基于 CodeMirror 6 的编辑界面，支持编辑过程中的实时代码高亮，并提供显式保存按钮。

## 已确认决策

| 决策点 | 选择 |
|--------|------|
| 编辑器库 | **CodeMirror 6**（成熟、模块化轻量、原生实时代码高亮） |
| 保存方式 | **显式保存按钮**（点保存才写盘） |
| 适用范围 | **仅文本 / 源码**（复用现有 `hasTextContent` 判断：有内容、非二进制、非超大；媒体 / PDF / Office 不显示编辑按钮） |

## 架构

组件关系：

```
FileViewer.vue
 ├── FileHeader.vue   （工具栏，新增「编辑」toggle 按钮）
 └── CodePreview.vue  （查看模式，原样保留）
 或
 └── CodeEditor.vue   （编辑模式，CodeMirror 6）
      └── codeEditorLang.ts （文件语言 → CodeMirror 语言扩展）
```

## 组件设计

### 1. 新增 `CodeEditor.vue`（web/src/components/file/CodeEditor.vue）

- 使用 `vue-codemirror` 的 `<Codemirror v-model="code" :extensions="extensions">`。
- 本地 `code` ref，从 `file.content` 初始化。
- props：`file`（含 `path`）、`content`、`language`（文件语言字符串）、`wordWrap`。
- extensions 组成：
  - `history()`、`lineNumbers()`
  - 语言扩展（来自 `codeEditorLang.buildLangExtension(language)`）
  - 主题：暗色 `oneDark`（`data-theme="dark"` 时），否则亮色默认
  - `wordWrap` 时追加 `EditorView.lineWrapping`
- 底部操作栏：显式「保存」按钮 → `emit('save', code)`；「取消」→ `emit('cancel')`。
- 监听 `content` prop 变化，若与当前未编辑状态一致则同步。

### 2. 新增 `codeEditorLang.ts`（web/src/utils/codeEditorLang.ts）

- 导出 `buildLangExtension(fileLang: string): Extension`。
- 维护 `FILE_LANG → Extension` 映射（静态导入官方语言包）：
  `javascript`→`lang-javascript`、`typescript`→`lang-javascript({typescript:true})`、`json`→`lang-json`、`yaml`→`lang-yaml`、`xml`→`lang-xml`、`html`→`lang-html`、`css`→`lang-css`、`markdown`→`lang-markdown`、`go`→`lang-go`、`python`→`lang-python`、`rust`→`lang-rust`、`java`→`lang-java`、`cpp`/`c`→`lang-cpp`、`sql`→`lang-sql`、`php`→`lang-php`。
- 其余语言（bash、swift、toml、ini 等）返回 `[]`（纯文本编辑，仍可编辑仅无高亮）。

### 3. 修改 `FileHeader.vue`

- 工具栏（内联 + 下拉菜单）新增「编辑」按钮。
- 显示条件：`hasTextContent && !isMediaFile && !isMarkdownRendered`（纯源码/文本视图）。
- 新增 `editing: Boolean` prop；`:class="{ active: editing }"`；emit `toggleEdit`。
- 图标 `Pencil`；tooltip：非编辑时 `file.header.edit`，编辑时 `file.header.finishEditing`。
- `toolbarInlineIds`/`toolbarCollapsedIds` 列表加入 `edit` 项。

### 4. 修改 `FileViewer.vue`

- 新增本地 `editing` ref，`watch(props.file)` 切换文件时重置为 `false`。
- 向 FileHeader 传 `:editing`、监听 `@toggle-edit`（`editing.value = !editing.value`）。
- 源码/文本分支、HTML raw、OpenAPI raw 分支：`editing` 为 true 时渲染 `CodeEditor` 替代 `CodePreview`。
- `handleSave(content)`：
  - `fetch('/api/file/write', { path, content })`（复用 DiffDrawer 调用方式）
  - 成功：`store.selectFile(path)` 刷新内容、`editing=false`、toast `file.editor.saved`
  - 失败：toast `file.editor.saveFailed`，保持编辑态不丢内容
- `handleCancel`：`editing=false`（丢弃未保存修改）。

### 5. i18n（zh.ts + en.ts）

新增键：
- `file.header.edit`（编辑 / Edit）
- `file.header.finishEditing`（完成编辑 / Finish editing）
- `file.editor.save`（保存 / Save）
- `file.editor.cancel`（取消 / Cancel）
- `file.editor.saved`（保存成功 / Saved）
- `file.editor.saveFailed`（保存失败 / Save failed）

## 数据流 / 错误处理

- 保存走已有的 `POST /api/file/write`（`file_ops.go` 原子写：临时文件 + rename）。
- 网络/权限失败：toast 报错，保持编辑态，内容不丢失。
- 进入编辑前 content 为 `null`（加载中/刷新）时不渲染编辑器。

## 测试

- `web/src/utils/__tests__/codeEditorLang.test.ts`：`javascript`→返回扩展、`typescript`→typescript 模式、`bash`→`[]`、未知→`[]`。
- `web/src/components/file/__tests__/FileHeader.test.ts`：文本文件显示编辑按钮；点击 emit `toggleEdit`；`editing=true` 时有 active class；markdown rendered / 媒体文件不显示。
- `web/src/components/file/__tests__/FileViewer.test.ts`：编辑态渲染 CodeEditor；保存调 fetch 并退出编辑、toast 成功；失败保持编辑态。

## 非目标（YAGNI）

- 不做自动保存 / 退出时未保存确认（显式保存已满足需求）。
- 不引入 Monaco / 完整 IntelliSense。
- 不为每种语言引入语言包（仅官方常用集，其余回退纯文本）。
