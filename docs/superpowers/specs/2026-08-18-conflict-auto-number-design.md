# 同名文件冲突：自动加编号（不弹框）

## 背景

文件管理中存在多套"向目录写入同名文件"的路径，其冲突处理方式不一致：

| 场景 | 路径 | 现状 |
|------|------|------|
| 复制/粘贴、拖拽移动（文件管理内） | `FileManagerContent.transferEntries` → `/api/file/copy`·`/api/file/move` | 409 时弹命名对话框（`dialog.prompt`），改名后若再撞名静默失败 |
| 系统剪贴板粘贴文件 | `onPaste` → `handleFileDropToDir` → `/api/upload/file` | 后端自动顺序命名 `name_1.ext` |
| 拖拽文件入目录 | `handleFileDropToDir` → `/api/upload/file` | 后端自动顺序命名 `name_1.ext` |

目标：统一为**同名时自动追加顺序编号，不弹命名框**，与现有上传后端行为一致。

## 决策

- 三个场景统一"自动加编号，不弹框"。
- 上传路径后端已自动编号（`upload.go` 中 `name_1.ext`、`name_2.ext`… 递增，上限 9999），**不改**。
- 仅改动文件管理内复制/粘贴/拖拽移动的 409 处理：改为自动尝试 `name_1.ext`、`name_2.ext`…直至成功。

## 行为规格

`transferEntries(entries, destDir, isMove)`：

1. 首次目标名 = 原名。
2. 命中 409 时，不弹框，自动按编号递增重试，上限 9999（与上传一致）。
3. 编号规则：`name_1.ext`、`name_2.ext`…；无扩展名文件/目录为 `name_1`、`name_2`…。
4. 自动改名成功以 `appLog.d` 记录；整体成功仍 toast 提示复制/移动完成。
5. 上传路径保持后端自动编号，无前端改动。

## 实现要点

- 新增纯函数 `numberedName(baseName, index)` 到 `web/src/utils/fileManager.ts`，便于单元测试。
- `FileManagerContent.vue` 的 `transferEntries` 将 `dialog.prompt`（现 ~L1003）替换为 409 循环尝试编号名。
- 删除不再使用的 i18n 键 `file.prompt.pasteNewName`（`zh.ts`/`en.ts`）及测试对应 stub。

## 测试

- `utils/fileManager.ts`：`numberedName` 纯函数单测（带/不带扩展名、索引规则）。
- `FileManagerContent.test.ts`：`transferEntries` 409 → 自动编号重试成功；不再调用 `dialog.prompt`。
- 删除旧 `pasteNewName` stub。

## 验证

- `npm test`（后端未改动，无需 Go 测试）。
- 提交前 `./scripts/pre-push-checks.sh`。
