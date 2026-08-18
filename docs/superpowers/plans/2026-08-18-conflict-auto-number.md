# 同名文件冲突自动加编号（不弹框）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 文件管理内复制/粘贴、拖拽移动命中同名文件时，自动追加顺序编号（`name_1.ext`）重试，不再弹命名框，与上传后端行为一致。

**Architecture:** 纯改动集中在两处：新增纯函数 `numberedName` 到 `web/src/utils/fileManager.ts`；`FileManagerContent.vue` 的 `transferEntries` 将 409 的 `dialog.prompt` 分支替换为编号递增重试循环。上传路径（`/api/upload/file`）后端已自动编号，不改。

**Tech Stack:** Vue 3 + TypeScript，Vitest。后端 Go 未改动。

---

### Task 1: 新增 `numberedName` 纯函数

**Files:**
- Modify: `web/src/utils/fileManager.ts`（在 `formatSize` 之后追加）
- Test: `web/src/utils/__tests__/fileManager.test.ts`（新建）

- [ ] **Step 1: 写失败测试**

创建 `web/src/utils/__tests__/fileManager.test.ts`：

```ts
import { describe, expect, it } from 'vitest'
import { numberedName } from '@/utils/fileManager.ts'

describe('numberedName', () => {
  it('appends a numeric suffix before the extension', () => {
    expect(numberedName('report.txt', 1)).toBe('report_1.txt')
    expect(numberedName('report.txt', 2)).toBe('report_2.txt')
  })

  it('handles names with multiple dots (uses last dot as extension)', () => {
    expect(numberedName('archive.tar.gz', 1)).toBe('archive.tar_1.gz')
  })

  it('handles names without an extension', () => {
    expect(numberedName('notes', 1)).toBe('notes_1')
    expect(numberedName('notes', 3)).toBe('notes_3')
  })

  it('handles hidden files without extension', () => {
    expect(numberedName('.env', 1)).toBe('.env_1')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `npx vitest run web/src/utils/__tests__/fileManager.test.ts`
Expected: FAIL with "numberedName is not exported" / "toBe is not a function" 之类引用错误。

- [ ] **Step 3: 实现**

在 `web/src/utils/fileManager.ts` 的 `formatSize` 函数（`~L79`）之后追加：

```ts
/**
 * Build a numbered name for same-name conflict auto-resolution,
 * mirroring the backend upload auto-numbering (name_1.ext, name_2.ext, …).
 * index must be >= 1. Hidden files like ".env" are treated as extensionless.
 */
export function numberedName(baseName: string, index: number): string {
  const lastDot = baseName.lastIndexOf('.')
  if (lastDot <= 0) {
    return `${baseName}_${index}`
  }
  const nameWithoutExt = baseName.slice(0, lastDot)
  const ext = baseName.slice(lastDot)
  return `${nameWithoutExt}_${index}${ext}`
}
```

- [ ] **Step 4: 运行确认通过**

Run: `npx vitest run web/src/utils/__tests__/fileManager.test.ts`
Expected: PASS（4 个用例）。

- [ ] **Step 5: 提交**

```bash
git add web/src/utils/fileManager.ts web/src/utils/__tests__/fileManager.test.ts
git commit -m "feat: add numberedName helper for conflict auto-number"
```

---

### Task 2: `transferEntries` 409 改为自动编号重试

**Files:**
- Modify: `web/src/components/file/FileManagerContent.vue:997-1011`
- Modify: `web/src/components/file/FileManagerContent.vue` 的 import 区（确认已导入 `numberedName`）
- Test: `web/src/components/file/__tests__/FileManagerContent.test.ts`

- [ ] **Step 1: 引入 `numberedName`**

在 `FileManagerContent.vue` 顶部（`~L423-427`）的 `@/utils/fileManager.ts` 导入块中加入 `numberedName`：

```ts
import {
  buildThumbUrl,
  isThumbable as isThumbableEntry, formatSize as formatFileSize,
  createMultiSelect as _createMultiSelect, createClipboard as _createClipboard,
  numberedName,
} from '@/utils/fileManager.ts'
```

- [ ] **Step 2: 写失败测试（409 自动编号重试）**

在 `web/src/components/file/__tests__/FileManagerContent.test.ts` 的 `clipboard paste (doPaste)` describe 内（`~L1873` 后）追加一个 `it`：

```ts
it('auto-numbers the destination name on 409 instead of prompting', async () => {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce({ ok: false, status: 409, text: async () => '' })
    .mockResolvedValueOnce({ ok: true, status: 200, text: async () => '' })
  vi.stubGlobal('fetch', fetchMock)
  const wrapper = mountContent({ currentDir: '' })
  await nextTick()
  wrapper.vm.ctxMenu.visible = true
  wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'test.ts' }
  await wrapper.vm.doCopy()
  await nextTick()

  await wrapper.vm.doPaste()
  await nextTick()

  // First call: original name. Second call: auto-numbered name.
  expect(fetchMock).toHaveBeenCalledTimes(2)
  const secondBody = JSON.parse(fetchMock.mock.calls[1][1].body)
  expect(secondBody.dest).toBe('test_1.ts')
  // No naming dialog should be invoked
  expect(mockDialogPrompt).not.toHaveBeenCalled()
  expect(wrapper.emitted('refresh')).toBeTruthy()
})
```

> 注意：`beforeEach` 里已有 `vi.stubGlobal('fetch', ...)`，此用例内二次 stub 会覆盖它，`afterEach` 的 `vi.unstubAllGlobals()` 会清理。

- [ ] **Step 3: 运行确认失败**

Run: `npx vitest run web/src/components/file/__tests__/FileManagerContent.test.ts`
Expected: 新用例 FAIL（当前仍走 `dialog.prompt`，未触发自动编号）。

- [ ] **Step 4: 实现**

把 `FileManagerContent.vue` 的 `transferEntries`（`~L997-1011`）中 `fetch` 与 409 处理替换为：

```ts
            appLog.d(TAG, '[transfer]', isMove ? 'moving' : 'copying', srcEntry.path, '→', destPath)
            let resp
            let attempt = 0
            while (true) {
              resp = await fetch(api, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: srcEntry.path, dest: destPath }),
              })
              // Same-name conflict: auto-append a numeric suffix and retry
              // (mirrors backend upload numbering), no naming dialog.
              if (resp.status === 409 && attempt < 9999) {
                attempt++
                const candidate = numberedName(srcEntry.name, attempt)
                destPath = (destDir ? destDir + '/' : '') + candidate
                appLog.d(TAG, '[transfer] conflict, retrying as:', destPath)
                continue
              }
              break
            }
```

保持下方 `if (!resp.ok) { ... allOk = false }` 及 catch 逻辑不变。

- [ ] **Step 5: 运行确认通过**

Run: `npx vitest run web/src/components/file/__tests__/FileManagerContent.test.ts`
Expected: 全部 PASS（含新用例）。

- [ ] **Step 6: 提交**

```bash
git add web/src/components/file/FileManagerContent.vue web/src/components/file/__tests__/FileManagerContent.test.ts
git commit -m "feat: auto-number same-name files on paste/move instead of prompting"
```

---

### Task 3: 删除死 i18n 键 `pasteNewName`

**Files:**
- Modify: `web/src/i18n/locales/zh.ts:856`
- Modify: `web/src/i18n/locales/en.ts:855`
- Modify: `web/src/components/file/__tests__/FileManagerContent.test.ts:231`
- Modify: `web/src/components/__tests__/fileManagerContent.test.ts:131`

- [ ] **Step 1: 删除 zh/en 键**

删除 `zh.ts:856` 的 `pasteNewName: '"{name}" 已存在，请输入新名称：',` 行，以及 `en.ts:855` 对应行。

- [ ] **Step 2: 删除测试 stub 中的键**

在两个测试文件（`FileManagerContent.test.ts:231`、`fileManagerContent.test.ts:131`）的 `prompt` stub 对象中删除 `pasteNewName: '新名称',`。

- [ ] **Step 3: 运行前端全量测试**

Run: `npm test`
Expected: 全部 PASS，无对 `pasteNewName` 的引用。

- [ ] **Step 4: 提交**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts web/src/components/file/__tests__/FileManagerContent.test.ts web/src/components/__tests__/fileManagerContent.test.ts
git commit -m "chore: remove unused pasteNewName i18n key"
```

---

### Task 4: 推送前全量检查

**Files:** 无（仅运行脚本）

- [ ] **Step 1: 运行 pre-push-checks**

Run: `./scripts/pre-push-checks.sh`
Expected: lint、test、build、typecheck 全部通过。

- [ ] **Step 2: 失败则修复**

若脚本报错，修复对应问题并重跑，直至通过。
