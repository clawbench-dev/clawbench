# 文件源码编辑模式 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为文件查看器的源码/纯文本视图新增一个「编辑」切换按钮，选中进入 CodeMirror 6 编辑界面（含实时代码高亮 + 显式保存），非选中保持现有只读查看。

**Architecture:** `FileViewer.vue` 持有 `editing` 本地状态，编辑态用新增的 `CodeEditor.vue`（CodeMirror 6）替代 `CodePreview.vue`。语言映射抽到 `codeEditorLang.ts`，保存逻辑抽到 `useCodeEditorSave` composable（复用已有 `POST /api/file/write`）。`FileHeader.vue` 工具栏新增编辑 toggle 按钮，仅文本/源码文件可见。

**Tech Stack:** Vue 3 + TypeScript、vue-codemirror（CodeMirror 6）、@codemirror/* 官方语言包、vitest + @vue/test-utils。

**前置说明：** 依赖安装运行在仓库根目录（`package.json` 在根目录，`web/package.json` 只是 `cd ..` 的薄封装）。后端 `POST /api/file/write` 已存在（`internal/handler/file_ops.go:62`，`DiffDrawer.vue:93` 已在用），无需后端改动。

---

### Task 1: 安装 CodeMirror 依赖

**Files:**
- Modify: `package.json`

- [ ] **Step 1: 安装依赖**

Run（仓库根目录）：
```bash
npm install \
  vue-codemirror \
  @codemirror/state @codemirror/view @codemirror/commands @codemirror/theme-one-dark \
  @codemirror/lang-javascript @codemirror/lang-json @codemirror/lang-yaml \
  @codemirror/lang-xml @codemirror/lang-html @codemirror/lang-css @codemirror/lang-markdown \
  @codemirror/lang-go @codemirror/lang-python @codemirror/lang-rust @codemirror/lang-java \
  @codemirror/lang-cpp @codemirror/lang-sql @codemirror/lang-php
```
Expected: 依赖写入 `package.json` 的 `dependencies`，无报错。

- [ ] **Step 2: 验证安装**

Run: `npm ls vue-codemirror @codemirror/view`
Expected: 打印已安装版本，无 `UNMET DEPENDENCY`。

- [ ] **Step 3: Commit**

```bash
git add package.json package-lock.json
git commit -m "deps: add CodeMirror 6 editor and language packs"
```

---

### Task 2: `codeEditorLang.ts` 语言映射（TDD）

**Files:**
- Create: `web/src/utils/codeEditorLang.ts`
- Test: `web/src/utils/__tests__/codeEditorLang.test.ts`

- [ ] **Step 1: 写失败测试**

`web/src/utils/__tests__/codeEditorLang.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { buildLangExtension } from '@/utils/codeEditorLang'
import { javascript } from '@codemirror/lang-javascript'
import { go } from '@codemirror/lang-go'

describe('buildLangExtension', () => {
  it('returns a truthy extension for mapped languages', () => {
    expect(buildLangExtension('javascript')).toBeTruthy()
    expect(buildLangExtension('go')).toBeTruthy()
    expect(buildLangExtension('json')).toBeTruthy()
  })
  it('maps typescript to typescript mode', () => {
    expect(buildLangExtension('typescript')).toBeTruthy()
  })
  it('returns empty array for unknown languages (plain text fallback)', () => {
    expect(buildLangExtension('bash')).toEqual([])
    expect(buildLangExtension('toml')).toEqual([])
  })
  it('returns empty array for empty string', () => {
    expect(buildLangExtension('')).toEqual([])
  })
})
```
注意：为确认映射真实存在，`javascript`/`go` 断言里 import 的 `javascript`/`go` 只是占位引用；失败阶段仅需 `buildLangExtension` 未导出即可验证失败。

- [ ] **Step 2: 运行测试验证失败**

Run: `cd web && npx vitest run src/utils/__tests__/codeEditorLang.test.ts`
Expected: FAIL，`Failed to resolve import "@/utils/codeEditorLang"`。

- [ ] **Step 3: 实现**

`web/src/utils/codeEditorLang.ts`:
```ts
import type { Extension } from '@codemirror/state'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { yaml } from '@codemirror/lang-yaml'
import { xml } from '@codemirror/lang-xml'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { markdown } from '@codemirror/lang-markdown'
import { go } from '@codemirror/lang-go'
import { python } from '@codemirror/lang-python'
import { rust } from '@codemirror/lang-rust'
import { java } from '@codemirror/lang-java'
import { cpp } from '@codemirror/lang-cpp'
import { sql } from '@codemirror/lang-sql'
import { php } from '@codemirror/lang-php'

const LANG_EXT: Record<string, () => Extension> = {
    javascript: () => javascript(),
    typescript: () => javascript({ typescript: true }),
    json: () => json(),
    yaml: () => yaml(),
    xml: () => xml(),
    html: () => html(),
    css: () => css(),
    markdown: () => markdown(),
    go: () => go(),
    python: () => python(),
    rust: () => rust(),
    java: () => java(),
    c: () => cpp(),
    cpp: () => cpp(),
    sql: () => sql(),
    php: () => php(),
}

export function buildLangExtension(fileLang: string): Extension {
    const factory = LANG_EXT[fileLang]
    return factory ? factory() : []
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd web && npx vitest run src/utils/__tests__/codeEditorLang.test.ts`
Expected: PASS，4 个用例通过。

- [ ] **Step 5: Commit**

```bash
git add web/src/utils/codeEditorLang.ts web/src/utils/__tests__/codeEditorLang.test.ts
git commit -m "feat: map file languages to CodeMirror extensions"
```

---

### Task 3: `useCodeEditorSave` 保存 composable（TDD）

**Files:**
- Create: `web/src/composables/useCodeEditorSave.ts`
- Test: `web/src/composables/__tests__/useCodeEditorSave.test.ts`

- [ ] **Step 1: 写失败测试**

`web/src/composables/__tests__/useCodeEditorSave.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'

const toastShow = vi.fn()
const selectFile = vi.fn()

vi.mock('@/composables/useToast.ts', () => ({
    useToast: () => ({ show: toastShow }),
}))
vi.mock('@/stores/app.ts', () => ({
    store: { state: {}, selectFile },
}))
vi.mock('vue-i18n', () => ({
    useI18n: () => ({ t: (k: string) => k }),
}))

import { useCodeEditorSave } from '@/composables/useCodeEditorSave'

describe('useCodeEditorSave', () => {
    beforeEach(() => {
        toastShow.mockReset()
        selectFile.mockReset()
        vi.restoreAllMocks()
    })

    it('returns true and refreshes file on successful write', async () => {
        globalThis.fetch = vi.fn().mockResolvedValue({ ok: true }) as any
        const { saveFile } = useCodeEditorSave()
        const ok = await saveFile('/tmp/a.go', 'package main')
        expect(ok).toBe(true)
        expect(globalThis.fetch).toHaveBeenCalledWith('/api/file/write', expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({ path: '/tmp/a.go', content: 'package main' }),
        }))
        expect(selectFile).toHaveBeenCalledWith('/tmp/a.go', false, false, false)
        expect(toastShow).toHaveBeenCalled()
    })

    it('returns false and shows error toast when write fails', async () => {
        globalThis.fetch = vi.fn().mockResolvedValue({ ok: false }) as any
        const { saveFile } = useCodeEditorSave()
        const ok = await saveFile('/tmp/a.go', 'package main')
        expect(ok).toBe(false)
        expect(selectFile).not.toHaveBeenCalled()
        expect(toastShow).toHaveBeenCalled()
    })

    it('returns false without fetch when path is empty', async () => {
        globalThis.fetch = vi.fn() as any
        const { saveFile } = useCodeEditorSave()
        const ok = await saveFile('', 'x')
        expect(ok).toBe(false)
        expect(globalThis.fetch).not.toHaveBeenCalled()
    })
})
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd web && npx vitest run src/composables/__tests__/useCodeEditorSave.test.ts`
Expected: FAIL，`Cannot find module '@/composables/useCodeEditorSave'`。

- [ ] **Step 3: 实现**

`web/src/composables/useCodeEditorSave.ts`:
```ts
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast.ts'
import { store } from '@/stores/app.ts'

export function useCodeEditorSave() {
    const { show } = useToast()
    const { t } = useI18n()
    const saving = ref(false)

    async function saveFile(path: string, content: string): Promise<boolean> {
        if (!path) return false
        saving.value = true
        try {
            const resp = await fetch('/api/file/write', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path, content }),
            })
            if (!resp.ok) throw new Error('write failed')
            await store.selectFile(path, false, false, false)
            show(t('file.editor.saved'), { icon: '✅', type: 'success', duration: 2000 })
            return true
        } catch {
            show(t('file.editor.saveFailed'), { icon: '❌', type: 'error', duration: 2000 })
            return false
        } finally {
            saving.value = false
        }
    }

    return { saving, saveFile }
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd web && npx vitest run src/composables/__tests__/useCodeEditorSave.test.ts`
Expected: PASS，3 个用例通过。

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useCodeEditorSave.ts web/src/composables/__tests__/useCodeEditorSave.test.ts
git commit -m "feat: add code editor save composable"
```

---

### Task 4: `CodeEditor.vue` 组件

**Files:**
- Create: `web/src/components/file/CodeEditor.vue`

- [ ] **Step 1: 实现组件**

`web/src/components/file/CodeEditor.vue`:
```vue
<template>
  <div class="code-editor-wrapper">
    <Codemirror
      v-model="code"
      :extensions="extensions"
      :autofocus="true"
      :style="{ height: '100%' }"
      placeholder=""
    />
    <div class="code-editor-actions">
      <span class="code-editor-status">{{ t('file.editor.dirty') }}</span>
      <button class="editor-btn" :disabled="saving" @click="emit('cancel')">{{ t('file.editor.cancel') }}</button>
      <button class="editor-btn primary" :disabled="saving" @click="emit('save', code)">
        {{ saving ? t('file.editor.saving') : t('file.editor.save') }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Codemirror } from 'vue-codemirror'
import { EditorView, lineNumbers } from '@codemirror/view'
import { history } from '@codemirror/commands'
import { oneDark } from '@codemirror/theme-one-dark'
import { buildLangExtension } from '@/utils/codeEditorLang'

const props = defineProps({
    file: Object,
    content: { type: String, default: '' },
    language: { type: String, default: 'plaintext' },
    wordWrap: { type: Boolean, default: false },
    saving: { type: Boolean, default: false },
})
const emit = defineEmits(['save', 'cancel'])

const { t } = useI18n()
const code = ref(props.content || '')

const isDark = computed(() => document.documentElement.getAttribute('data-theme') === 'dark')

const extensions = computed(() => {
    const exts = [history(), lineNumbers(), buildLangExtension(props.language)]
    if (props.wordWrap) exts.push(EditorView.lineWrapping)
    if (isDark.value) exts.push(oneDark)
    return exts
})

watch(() => props.content, (c) => {
    code.value = c || ''
})

defineExpose({ getValue: () => code.value })
</script>

<style scoped>
.code-editor-wrapper {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    background: var(--code-bg);
}
.code-editor-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    justify-content: flex-end;
    padding: 6px 12px;
    border-top: 1px solid var(--border-color);
    background: var(--bg-secondary);
    flex-shrink: 0;
}
.code-editor-status {
    margin-right: auto;
    font-size: 12px;
    color: var(--text-muted);
}
.editor-btn {
    padding: 5px 14px;
    border: 1px solid var(--border-color);
    border-radius: 14px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
}
.editor-btn:hover { border-color: var(--accent-color); color: var(--accent-color); }
.editor-btn.primary { background: var(--accent-color); border-color: var(--accent-color); color: #fff; }
.editor-btn.primary:hover { filter: brightness(1.1); }
.editor-btn:disabled { opacity: 0.5; cursor: not-allowed; pointer-events: none; }
</style>
```

- [ ] **Step 2: 运行 typecheck 验证编译**

Run: `cd web && npx vue-tsc --noEmit -p web/tsconfig.json`
Expected: 无新增错误（`Codemirror` 组件类型、`@codemirror/*` 导入均解析）。

- [ ] **Step 3: Commit**

```bash
git add web/src/components/file/CodeEditor.vue
git commit -m "feat: add CodeMirror-based code editor component"
```

---

### Task 5: i18n 文案

**Files:**
- Modify: `web/src/i18n/locales/zh.ts:715-737`（`file.header` 段）
- Modify: `web/src/i18n/locales/en.ts:715-737`（`file.header` 段）

- [ ] **Step 1: zh.ts 增加键**

在 `zh.ts` 的 `file.header` 对象中（`openDirectory: '打开目录',` 之后）追加：
```ts
        edit: '编辑',
        finishEditing: '完成编辑',
```
并在 `file` 下新增 `editor` 段（放在 `header` 之后）：
```ts
        editor: {
            save: '保存',
            saving: '保存中...',
            cancel: '取消',
            saved: '保存成功',
            saveFailed: '保存失败',
            dirty: '未保存的修改',
        },
```

- [ ] **Step 2: en.ts 增加键**

`en.ts` 的 `file.header` 对象追加：
```ts
        edit: 'Edit',
        finishEditing: 'Finish editing',
```
`file` 下新增：
```ts
        editor: {
            save: 'Save',
            saving: 'Saving...',
            cancel: 'Cancel',
            saved: 'Saved',
            saveFailed: 'Save failed',
            dirty: 'Unsaved changes',
        },
```

- [ ] **Step 3: Commit**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(i18n): add file editor labels"
```

---

### Task 6: `FileHeader.vue` 编辑按钮

**Files:**
- Modify: `web/src/components/file/FileHeader.vue`

- [ ] **Step 1: 模板加内联编辑按钮**

在 `FileHeader.vue` 模板的 `stickyScroll` 按钮之后（第 59 行 `</button>` 之后、`More` 下拉之前）插入：
```html
      <!-- Edit toggle button -->
      <button v-if="toolbarInlineIds.includes('edit')" class="file-header-btn" :class="{ active: editing }" @click.stop="handleToggleEdit" :title="editing ? t('file.header.finishEditing') : t('file.header.edit')">
        <Pencil :size="14" />
      </button>
```

- [ ] **Step 2: 模板加下拉菜单项**

在 `stickyScroll` 下拉项（`</button>` 之后、Always-in-dropdown 注释前）插入：
```html
            <button v-if="toolbarCollapsedIds.includes('edit')" class="dropdown-item" :class="{ active: editing }" @click="handleToggleEdit">
              <Pencil :size="14" />
              {{ editing ? t('file.header.finishEditing') : t('file.header.edit') }}
            </button>
```

- [ ] **Step 3: script 增加 props/emit/import/computed/handler**

- imports 行（第 163 行）的 lucide 列表加入 `Pencil`。
- `props`（第 173 行）加入：
```ts
    editing: Boolean,
```
- `emit`（第 184 行）加入：
```ts
'toggleEdit',
```
- 在 `hasTextContent`（第 249 行）之后新增 computed：
```ts
// Editable: text/source files in raw view (excludes markdown-rendered & media)
const isEditable = computed(() => hasTextContent.value && !isMediaFile.value && !isMarkdown.value && !isMarkdownRendered.value)
```
- `useToolbarOverflow` 的 ids 列表（第 214 行后）加入：
```ts
    if (isEditable.value) ids.push('edit')
```
- 新增 handler（放在 `handleToggleView` 附近）：
```ts
function handleToggleEdit() {
    menuOpen.value = false
    emit('toggleEdit')
}
```

- [ ] **Step 4: 运行现有 FileHeader 测试确认无回归**

Run: `cd web && npx vitest run src/components/file/__tests__/FileHeader.test.ts`
Expected: 现有用例全过；若 i18n mock 缺新键导致渲染报错，在测试文件的 i18n messages 补 `edit: 'Edit', finishEditing: 'Finish editing'`。

- [ ] **Step 5: Commit**

```bash
git add web/src/components/file/FileHeader.vue
git commit -m "feat: add edit toggle button to file header"
```

---

### Task 7: `FileViewer.vue` 编辑模式接线

**Files:**
- Modify: `web/src/components/file/FileViewer.vue`

- [ ] **Step 1: import 与状态**

在 script 中：
- import `CodeEditor from './CodeEditor.vue'`（第 222 行 `CodePreview` import 后）
- import `{ useCodeEditorSave } from '@/composables/useCodeEditorSave.ts'`
- 在 `editing` 相关位置新增：
```ts
const editing = ref(false)
const { saving, saveFile } = useCodeEditorSave()

async function handleSave(content: string) {
    const ok = await saveFile(props.file?.path || '', content)
    if (ok) editing.value = false
}
function handleToggleEdit() {
    editing.value = !editing.value
}
```

- [ ] **Step 2: 切换文件时重置编辑态**

在现有 `watch(() => props.file, ...)`（第 393 行）回调开头加：
```ts
    editing.value = false
```

- [ ] **Step 3: 传给 FileHeader**

模板 `FileHeader` 标签加：
```html
      :editing="editing"
      @toggle-edit="handleToggleEdit"
```

- [ ] **Step 4: 源码/文本分支渲染 CodeEditor**

将「Code / plain text」分支（第 180-196 行）的 `CodePreview` 改为条件渲染：
```html
        <CodeEditor
          v-if="editing"
          :file="file"
          :content="file.content"
          :language="rawFileLanguage"
          :word-wrap="wordWrap"
          :saving="saving"
          @save="handleSave"
          @cancel="editing = false"
        />
        <CodePreview
          v-else
          :content="file.content"
          ...
        />
```

- [ ] **Step 5: 运行 typecheck 验证编译**

Run: `cd web && npx vue-tsc --noEmit -p web/tsconfig.json`
Expected: 无新增错误。

- [ ] **Step 6: Commit**

```bash
git add web/src/components/file/FileViewer.vue
git commit -m "feat: wire edit mode into file viewer"
```

---

### Task 8: 前端测试（TDD）

**Files:**
- Modify: `web/src/components/file/__tests__/FileHeader.test.ts`
- Modify: `web/src/components/file/__tests__/FileViewer.test.ts`

- [ ] **Step 1: FileHeader 测试（编辑按钮）**

在 `web/src/components/file/__tests__/FileHeader.test.ts`：
- i18n messages 的 `file.header` 补 `edit: 'Edit', finishEditing: 'Finish editing'`。
- 在 describe 中追加用例：
```ts
  it('shows edit button for editable text file', () => {
    const wrapper = mountHeader()
    const vm = wrapper.vm as any
    expect(vm.$.setupState.isEditable).toBe(true)
    expect(vm.$.setupState.toolbarInlineIds).toContain('edit')
  })

  it('hides edit button for markdown files', () => {
    const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' } })
    const vm = wrapper.vm as any
    expect(vm.$.setupState.isEditable).toBe(false)
    expect(vm.$.setupState.toolbarInlineIds).not.toContain('edit')
  })

  it('emits toggleEdit when edit button is clicked', async () => {
    const wrapper = mountHeader()
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleEdit()
    await nextTick()
    expect(wrapper.emitted('toggleEdit')).toBeTruthy()
  })

  it('applies active class on edit button when editing', async () => {
    const wrapper = mountHeader({ editing: true })
    const activeBtn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.classes().includes('active'))
    expect(activeBtn).toBeTruthy()
  })
```

- [ ] **Step 2: 运行 FileHeader 测试**

Run: `cd web && npx vitest run src/components/file/__tests__/FileHeader.test.ts`
Expected: 新旧用例全部 PASS。

- [ ] **Step 3: FileViewer 测试（编辑模式）**

`web/src/components/file/__tests__/FileViewer.test.ts`：
- i18n messages `file` 下补 `editor: { save: 'Save', cancel: 'Cancel' }`、`file.header` 补 `edit: 'Edit'`。
- 在 stubs 对象加 `CodeEditor: true`。
- 追加 `useCodeEditorSave` mock：
```ts
const mockSaveFile = vi.fn()
vi.mock('@/composables/useCodeEditorSave.ts', () => ({
  useCodeEditorSave: () => ({ saving: { value: false }, saveFile: mockSaveFile }),
}))
```
- 追加用例：
```ts
  it('renders CodeEditor when editing is toggled on', async () => {
    const wrapper = mountViewer({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1', isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, isOffice: false, isBinary: false, tooLarge: false } })
    const header = wrapper.findComponent({ name: 'FileHeader' })
    header.vm.$emit('toggleEdit')
    await nextTick()
    expect(wrapper.findComponent({ name: 'CodeEditor' }).exists()).toBe(true)
  })

  it('exits edit mode on cancel', async () => {
    const wrapper = mountViewer({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1', isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, isOffice: false, isBinary: false, tooLarge: false } })
    const header = wrapper.findComponent({ name: 'FileHeader' })
    header.vm.$emit('toggleEdit')
    await nextTick()
    wrapper.findComponent({ name: 'CodeEditor' }).vm.$emit('cancel')
    await nextTick()
    expect(wrapper.findComponent({ name: 'CodeEditor' }).exists()).toBe(false)
  })

  it('saves file and exits edit mode on success', async () => {
    mockSaveFile.mockResolvedValue(true)
    const wrapper = mountViewer({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1', isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, isOffice: false, isBinary: false, tooLarge: false } })
    const header = wrapper.findComponent({ name: 'FileHeader' })
    header.vm.$emit('toggleEdit')
    await nextTick()
    wrapper.findComponent({ name: 'CodeEditor' }).vm.$emit('save', 'const x = 2')
    await flushPromises()
    expect(mockSaveFile).toHaveBeenCalledWith('/tmp/main.ts', 'const x = 2')
    expect(wrapper.findComponent({ name: 'CodeEditor' }).exists()).toBe(false)
  })

  it('stays in edit mode when save fails', async () => {
    mockSaveFile.mockResolvedValue(false)
    const wrapper = mountViewer({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1', isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, isOffice: false, isBinary: false, tooLarge: false } })
    const header = wrapper.findComponent({ name: 'FileHeader' })
    header.vm.$emit('toggleEdit')
    await nextTick()
    wrapper.findComponent({ name: 'CodeEditor' }).vm.$emit('save', 'const x = 2')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'CodeEditor' }).exists()).toBe(true)
  })
```
（在文件顶部 import `flushPromises`，或在 `@vue/test-utils` 已有导出。）

- [ ] **Step 4: 运行 FileViewer 测试**

Run: `cd web && npx vitest run src/components/file/__tests__/FileViewer.test.ts`
Expected: 新旧用例全部 PASS。若 FileHeader stub 名称匹配问题，改用 `wrapper.findComponent({ name: 'FileHeader' })` 为 `wrapper.findComponent({ name: 'FileHeader' })` 已用，或 `(wrapper.vm as any).$refs` 方式；stubs 中 `FileHeader: true` 会使 stub 组件的 name 保留为 `FileHeader`。

- [ ] **Step 5: Commit**

```bash
git add web/src/components/file/__tests__/FileHeader.test.ts web/src/components/file/__tests__/FileViewer.test.ts
git commit -m "test: cover file edit mode toggle and save flow"
```

---

### Task 9: 全量验证

**Files:** 无（验证命令）

- [ ] **Step 1: 前端测试**

Run: `cd web && npx vitest run`
Expected: 全部通过。

- [ ] **Step 2: lint**

Run: `npm run lint`
Expected: 无 error（新文件符合 eslint 规则）。

- [ ] **Step 3: typecheck**

Run: `npm run typecheck`
Expected: 无类型错误。

- [ ] **Step 4: 构建**

Run: `npm run build`
Expected: `vite build` 成功，CodeMirror 打包无报错。

- [ ] **Step 5: 全量检查脚本**

Run: `./scripts/pre-push-checks.sh`
Expected: 通过（覆盖线上未改动的包基线）。

- [ ] **Step 6: Commit（如有 lint 修复）**

```bash
git add -A
git commit -m "chore: apply lint fixes after code editor integration"
```

---

## 自检

- **Spec 覆盖**：Task1（依赖）、Task2（语言映射）、Task3（保存）、Task4（CodeEditor 组件）、Task5（i18n）、Task6（Header 按钮）、Task7（Viewer 接线）、Task8（测试）覆盖 spec 全部 7 节。
- **无占位符**：所有代码步骤含完整实现。
- **类型一致性**：`buildLangExtension` 返回 `Extension`，`CodeEditor` 用其组装 `extensions`；`saveFile(path, content): Promise<boolean>` 在 Task3 定义、Task7 `handleSave` 使用一致；props/emits 命名跨 Task4/6/7 一致（`toggleEdit`、`editing`、`save`、`cancel`、`saving`）。
- **编辑按钮可见性**：仅 `isEditable`（文本/源码、非媒体、非 markdown、非 rendered）时显示，与 FileViewer 中 CodePreview 分支一致；markdown 保持只读（见 spec 非目标）。
