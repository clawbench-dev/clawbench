# 文件跳转测试

本文件用于测试 Markdown 预览和 AI 聊天中的各类文件路径跳转功能。

---

## 1. Markdown 链接跳转（`<a>` 标签）

### 相对路径链接

- [同级目录文件](./file-link-test.md) — 同目录下的另一个 md 文件
- [上级目录文件](../README.md) — 项目根目录的 README
- [上级目录带子路径](../web/src/App.vue) — 项目中的 Vue 组件
- [test 目录下的文件](../test/formula-demo.md) — test 目录的 md 文件
- [带 ./ 前缀](./subdir/nested-file.md) — 子目录中的文件

### 目录链接

- [跳转到 web 目录](../web) — 应触发目录导航 + 打开侧边栏
- [跳转到 test 目录](../test) — 同上

---

## 2. 裸文件路径（annotateFilePaths 自动识别）

### 项目相对路径

- 主入口：`cmd/server/main.go`
- 前端入口：`web/src/main.ts`
- 应用 store：`web/src/stores/app.ts`
- 聊天面板：`web/src/components/ChatPanel.vue`
- Markdown 预览：`web/src/components/MarkdownPreview.vue`
- 路径注解 composable：`web/src/composables/useFilePathAnnotation.ts`
- 共享样式：`web/css/markdown-common.css`
- 配置文件：`config.yaml`
- 构建脚本：`build.sh`

### 带 ./ 前缀的相对路径

- `./file-link-test.md`
- `../web/src/App.vue`
- `../test/mermaid-demo.md`

### 绝对路径（仅当与 projectRoot 匹配时生效）

- `/home/xulongzhe/projects/clawbench/web/src/App.vue`
- `/home/xulongzhe/projects/clawbench/config.yaml`

---

### 多区间行号（逗号分隔）

点击跳转到**最早**区间，并闪烁**全部**指定行；预览卡高亮所有落在窗口内的区间。

- `internal/rag/store_sqlite.go:90-91,309,324,343,938-943`
- `web/src/App.vue:879-885,1000,1200-1205`
- 乱序 + 空格 + L 前缀：`web/src/composables/useFilePathAnnotation.ts:L430, L400, L410-L415`
- 区间过宽时预览卡只高亮窗口内部分：`cmd/server/main.go:1-1000`

---

## 3. 内联代码中的文件路径

AI 回复经常将文件路径包裹在内联代码中：

- 请修改 `web/src/components/ChatPanel.vue` 中的渲染逻辑
- 配置项在 `web/src/stores/app.ts` 中定义
- 样式文件位于 `web/css/markdown-common.css`

---

## 4. 代码块中的路径（不应被注解）

```bash
# 这些路径在代码块内，不应出现跳转按钮
vim web/src/App.vue
cd web/src/components/
cat config.yaml
```

---

## 5. 不存在的路径（按钮应在验证后被移除）

- `web/src/nonexistent-file.ts` — 文件不存在，按钮应被隐藏
- `fake/path/to/missing.go` — 同上

---

## 6. 混合场景

这段文本混合了多种路径格式：首先打开 `cmd/server/main.go` 查看服务端入口，然后参考 [构建脚本](../build.sh) 了解编译流程，最后在 `web/src/composables/useFilePathAnnotation.ts` 中查看路径解析逻辑。
