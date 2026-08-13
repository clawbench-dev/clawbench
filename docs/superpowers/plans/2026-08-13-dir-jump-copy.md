# 目录快速跳转 + 面包屑复制路径 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在文件管理器与项目选择器工具栏增加「跳转」按钮（弹出对话框输入目录路径快速导航），并在文件面包屑尾部增加「复制路径」按钮。

**Architecture:** 新增一个共享的 `JumpDirDialog.vue` 纯展示对话框（无后端调用），由文件管理器 `FileManagerContent.vue` 与项目选择器 `ProjectDialog.vue` 各自嵌入。文件管理器确认后调用 `store.navigateToDir`；项目选择器确认后调用已有的 `browseNavigate`（内部走 `/api/projects`）。`DirBreadcrumb.vue` 尾部加复制按钮，复用现有 clipboard 模式。i18n 新增 `jump:` 块。

**Tech Stack:** Vue 3 (script setup, Composition API), Vitest + @vue/test-utils, vue-i18n, lucide-vue-next, Vite.

参考：设计文档 `docs/superpowers/specs/2026-08-13-dir-jump-copy-design.md`

---

### Task 1: i18n 文案

**Files:**
- Modify: `web/src/i18n/locales/zh.ts`（`jump:` 块，放在 `projectDialog:` 块之前，即第 941 行 `search:` 前）
- Modify: `web/src/i18n/locales/en.ts`（同样位置）

- [ ] **Step 1: 在 zh.ts 的 `projectDialog` 块之后、`search` 块之前插入 `jump` 块**

在 `web/src/i18n/locales/zh.ts` 中，`projectDialog` 块（结束于 `setProjectFailedDetail` 后的 `},`）之后插入：

```ts
  jump: {
    title: '跳转到目录',
    placeholder: '输入目录路径，如 src/utils',
    confirm: '跳转',
    cancel: '取消',
    button: '跳转',
    copyPath: '复制路径',
  },
```

- [ ] **Step 2: 在 en.ts 同样位置插入英文 `jump` 块**

```ts
  jump: {
    title: 'Jump to Directory',
    placeholder: 'Enter a directory path, e.g. src/utils',
    confirm: 'Jump',
    cancel: 'Cancel',
    button: 'Jump',
    copyPath: 'Copy path',
  },
```

- [ ] **Step 3: 验证类型检查**

Run: `npx tsc --noEmit`
Expected: 无类型错误（`jump` 块字段与 `zh.ts` 对齐）。

- [ ] **Step 4: Commit**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(i18n): 新增 jump 跳转目录与复制路径文案"
```

---

### Task 2: 共享跳转对话框组件 `JumpDirDialog.vue`

**Files:**
- Create: `web/src/components/file/JumpDirDialog.vue`
- Test: `web/src/components/file/__tests__/JumpDirDialog.test.ts`

组件职责：纯展示 + 输入归一化。不调用任何后端 API；解析「相对/绝对」交给调用方。

- [ ] **Step 1: 写失败测试**

创建 `web/src/components/file/__tests__/JumpDirDialog.test.ts`：

```ts
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import JumpDirDialog from '../JumpDirDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      jump: {
        title: 'Jump to Directory',
        placeholder: 'Enter a directory path, e.g. src/utils',
        confirm: 'Jump',
        cancel: 'Cancel',
        button: 'Jump',
        copyPath: 'Copy path',
      },
    },
  },
})

const TeleportStub = { template: '<div><slot /></div>' }

function mountDialog(props = {}) {
  return mount(JumpDirDialog, {
    props: { open: false, ...props },
    global: { stubs: { Teleport: TeleportStub }, plugins: [i18n] },
  })
}

describe('JumpDirDialog', () => {
  beforeEach(() => { vi.restoreAllMocks() })

  it('renders input and confirm/cancel buttons when open', () => {
    const wrapper = mountDialog({ open: true })
    expect(wrapper.find('input').exists()).toBe(true)
    expect(wrapper.findAll('button').length).toBeGreaterThanOrEqual(2)
  })

  it('emits confirm with trimmed value on confirm click', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('input').setValue('  src/utils  ')
    await wrapper.find('.jump-confirm-btn').trigger('click')
    expect(wrapper.emitted('confirm')![0]).toEqual(['src/utils'])
  })

  it('emits confirm with trimmed value on Enter key', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('input').setValue('src')
    await wrapper.find('input').trigger('keydown.enter')
    expect(wrapper.emitted('confirm')![0]).toEqual(['src'])
  })

  it('does not emit confirm when input is empty', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('input').setValue('   ')
    await wrapper.find('.jump-confirm-btn').trigger('click')
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })

  it('emits close on cancel click', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('.jump-cancel-btn').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('clears input when reopened', async () => {
    const wrapper = mountDialog({ open: true })
    await wrapper.find('input').setValue('src')
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm test -- --run web/src/components/file/__tests__/JumpDirDialog.test.ts`
Expected: FAIL（找不到 `JumpDirDialog.vue` 组件）。

- [ ] **Step 3: 实现组件**

创建 `web/src/components/file/JumpDirDialog.vue`：

```vue
<template>
  <ModalDialog :open="open" :title="t('jump.title')" :z-index="2400" @close="$emit('close')">
    <div class="jump-dialog-body">
      <input
        ref="inputRef"
        v-model="pathInput"
        class="jump-path-input"
        type="text"
        :placeholder="t('jump.placeholder')"
        spellcheck="false"
        @keydown.enter="doConfirm"
      />
    </div>
    <template #footer>
      <button class="jump-cancel-btn" @click="$emit('close')">{{ t('jump.cancel') }}</button>
      <button class="jump-confirm-btn" @click="doConfirm">{{ t('jump.confirm') }}</button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '../common/ModalDialog.vue'

const props = defineProps({
  open: Boolean,
})
const emit = defineEmits(['close', 'confirm'])

const { t } = useI18n()
const pathInput = ref('')
const inputRef = ref(null)

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    pathInput.value = ''
    nextTick(() => inputRef.value?.focus())
  }
})

function doConfirm() {
  const value = pathInput.value.trim()
  if (!value) return
  emit('confirm', value)
}
</script>

<style scoped>
.jump-dialog-body {
  padding: 12px 16px;
}
.jump-path-input {
  width: 100%;
  box-sizing: border-box;
  padding: 8px 10px;
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  background: var(--bg-primary);
  color: var(--text-primary);
  outline: none;
}
.jump-path-input:focus {
  border-color: var(--accent-color, #4a90d9);
}
.jump-cancel-btn {
  padding: 7px 14px;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
}
.jump-cancel-btn:hover { background: var(--bg-secondary); }
.jump-confirm-btn {
  padding: 7px 14px;
  background: var(--accent-color, #0066cc);
  color: #fff;
  border: none;
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
}
.jump-confirm-btn:hover { background: #0055aa; }
</style>
```

注意：`ModalDialog.vue` 的 footer slot 与头部样式在 `ProjectDialog.vue` 中已使用同款 `.cancel-btn`/`.confirm-btn`，此处用独立类名避免冲突。

- [ ] **Step 4: 运行测试确认通过**

Run: `npm test -- --run web/src/components/file/__tests__/JumpDirDialog.test.ts`
Expected: PASS（全部用例）。

- [ ] **Step 5: Commit**

```bash
git add web/src/components/file/JumpDirDialog.vue web/src/components/file/__tests__/JumpDirDialog.test.ts
git commit -m "feat(file): 新增共享跳转目录对话框 JumpDirDialog"
```

---

### Task 3: 文件管理器集成跳转按钮

**Files:**
- Modify: `web/src/components/file/FileManagerContent.vue`
- Modify: `web/src/components/file/__tests__/FileManagerContent.test.ts`

- [ ] **Step 1: 写失败测试（追加到现有测试文件）**

在 `web/src/components/file/__tests__/FileManagerContent.test.ts` 末尾追加一个新 describe。先修改 store mock（第 133-141 行）增加 `navigateToDir`：

将：
```ts
vi.mock('@/stores/app', () => ({
  store: {
    state: { projectRoot: '/project', currentDir: '', currentFile: null, dirEntries: [] },
    loadGitBranch: vi.fn(),
    loadFiles: vi.fn(),
    selectFile: vi.fn(),
    setProject: vi.fn(),
  },
}))
```
改为：
```ts
const mockNavigateToDir = vi.hoisted(() => vi.fn())
vi.mock('@/stores/app', () => ({
  store: {
    state: { projectRoot: '/project', currentDir: '', currentFile: null, dirEntries: [] },
    loadGitBranch: vi.fn(),
    loadFiles: vi.fn(),
    selectFile: vi.fn(),
    setProject: vi.fn(),
    navigateToDir: mockNavigateToDir,
  },
}))
```

将 useToolbarOverflow mock（第 115-123 行）的 `inlineIds` 加入 `'jump'`：
```ts
vi.mock('@/composables/useToolbarOverflow', () => ({
  useToolbarOverflow: () => ({
    inlineIds: computed(() => ['refresh', 'newFile', 'newFolder', 'upload', 'viewToggle', 'multiselect', 'hidden', 'jump']),
    collapsedIds: computed(() => mockToolbarCollapsedIds),
    contentWidth: ref(800),
    startObserving: vi.fn(),
    stopObserving: vi.fn(),
  }),
}))
```

mock JumpDirDialog 组件（放在 DirBreadcrumb mock 附近）：
```ts
vi.mock('@/components/file/JumpDirDialog.vue', () => ({
  default: defineComponent({
    props: ['open'],
    emits: ['close', 'confirm'],
    template: '<div class="jump-dialog-stub" />',
  }),
}))
```

在 i18n mock（`common` 块内第 227 行）加入 jump 文案，并确保 mockNavigateToDir 在 beforeEach 中 reset。在 beforeEach（第 265-290 行）加入：
```ts
  mockNavigateToDir.mockReset()
```

在文件末尾追加：
```ts
describe('FileManagerContent — jump to dir', () => {
  it('opens jump dialog when jump button clicked', async () => {
    const wrapper = mountContent()
    const jumpBtn = wrapper.find('.toolbar-btn.jump-btn')
    expect(jumpBtn.exists()).toBe(true)
    await jumpBtn.trigger('click')
    await nextTick()
    expect(wrapper.find('.jump-dialog-stub').exists()).toBe(true)
  })

  it('navigates to dir on jump confirm', async () => {
    mockNavigateToDir.mockResolvedValue(undefined)
    const wrapper = mountContent()
    const vm = wrapper.vm as any
    vm.$.setupState.handleJumpConfirm('src/utils')
    await nextTick()
    expect(mockNavigateToDir).toHaveBeenCalledWith('src/utils')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm test -- --run web/src/components/file/__tests__/FileManagerContent.test.ts`
Expected: FAIL（jump-btn 不存在 / handleJumpConfirm 未定义）。

- [ ] **Step 3: 实现组件集成**

在 `web/src/components/file/FileManagerContent.vue`：

a) 模板：在 `dir-toolbar-btns` 中，hidden 按钮之后加入跳转按钮。在 `<button v-if="toolbarInlineIds.includes('hidden')" ...>...</button>`（第 66-69 行）之后插入：

```html
          <button v-if="toolbarInlineIds.includes('jump')" class="toolbar-btn jump-btn" @click="jumpOpen = true" :title="t('jump.button')">
            <LocateFixed :size="16" />
          </button>
```

b) 在文件末尾（`</Teleport>` 之后，`</div>` 之前，即第 380 行 FileSearchDrawer 之后）加入 JumpDirDialog：

```html
    <JumpDirDialog :open="jumpOpen" @close="jumpOpen = false" @confirm="handleJumpConfirm" />
```

c) script：引入图标与组件。在 lucide import（第 407 行）中加入 `LocateFixed`：
```ts
import { ... , FolderDown, LocateFixed } from 'lucide-vue-next'
```
在组件 import 区（第 428 行附近）加入：
```ts
import JumpDirDialog from './JumpDirDialog.vue'
```

d) 在 useToolbarOverflow id 列表（第 682 行）中加入 `'jump'`：
```ts
  () => ['refresh', 'newFile', 'newFolder', 'upload', 'uploadFolder', 'viewToggle', 'multiselect', 'hidden', 'jump'],
```

e) 在 `<script setup>` 内（例如 `dialog` 定义附近，第 606 行后）加入状态与处理函数：
```ts
const jumpOpen = ref(false)
async function handleJumpConfirm(path: string) {
  jumpOpen.value = false
  await store.navigateToDir(path)
}
```

注意 `store` 已从 `@/stores/app.ts` 导入（第 413 行）。

- [ ] **Step 4: 运行测试确认通过**

Run: `npm test -- --run web/src/components/file/__tests__/FileManagerContent.test.ts`
Expected: PASS。

- [ ] **Step 5: 运行前端类型检查**

Run: `npx tsc --noEmit`
Expected: 无类型错误。

- [ ] **Step 6: Commit**

```bash
git add web/src/components/file/FileManagerContent.vue web/src/components/file/__tests__/FileManagerContent.test.ts
git commit -m "feat(file): 文件管理器工具栏增加跳转目录按钮"
```

---

### Task 4: 项目选择器集成跳转按钮

**Files:**
- Modify: `web/src/components/ProjectDialog.vue`
- Modify: `web/src/components/__tests__/projectDialog.test.ts`

- [ ] **Step 1: 写失败测试（改写现有纯逻辑测试为组件测试）**

将 `web/src/components/__tests__/projectDialog.test.ts` 内容替换为：

```ts
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import ProjectDialog from '../ProjectDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      projectDialog: { title: 'Select Project Directory' },
      jump: {
        title: 'Jump to Directory',
        placeholder: 'Enter a directory path',
        confirm: 'Jump',
        cancel: 'Cancel',
        button: 'Jump',
        copyPath: 'Copy path',
      },
    },
  },
})

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

vi.mock('@/stores/app', () => ({
  store: { state: { rootPaths: ['/'], homeDir: '/home/user' } },
}))

const TeleportStub = { template: '<div><slot /></div>' }

// Provide a controllable JumpDirDialog stub that emits confirm
const JumpStub = {
  props: ['open'],
  emits: ['close', 'confirm'],
  template: '<div class="jump-dialog-stub" @click="$emit(\'confirm\', \'src/utils\')" />',
}

function mountDialog(props = {}) {
  return mount(ProjectDialog, {
    props: { open: true, ...props },
    global: {
      stubs: { Teleport: TeleportStub, JumpDirDialog: JumpStub },
      plugins: [i18n],
      provide: {
        toast: { show: vi.fn() },
        hotSwitchProject: vi.fn(),
      },
    },
  })
}

describe('ProjectDialog', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockFetch.mockReset()
  })

  it('opens jump dialog when jump button clicked', async () => {
    const wrapper = mountDialog()
    const jumpBtn = wrapper.find('.toolbar-btn.jump-btn')
    expect(jumpBtn.exists()).toBe(true)
    await jumpBtn.trigger('click')
    await nextTick()
    expect(wrapper.find('.jump-dialog-stub').exists()).toBe(true)
  })

  it('navigates browse to entered path on jump confirm', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ path: '/home/user/src/utils', items: [] }),
    })
    const wrapper = mountDialog()
    await wrapper.find('.toolbar-btn.jump-btn').trigger('click')
    await nextTick()
    await wrapper.find('.jump-dialog-stub').trigger('click')
    await nextTick()
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/projects?path=' + encodeURIComponent('src/utils'))
    )
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm test -- --run web/src/components/__tests__/projectDialog.test.ts`
Expected: FAIL（jump-btn 不存在）。

- [ ] **Step 3: 实现组件集成**

在 `web/src/components/ProjectDialog.vue`：

a) 模板：在 `dialog-toolbar-row`（第 8-21 行）的 SearchInput 之后加入跳转按钮：
```html
        <button class="toolbar-btn jump-btn" @click="jumpOpen = true" :title="t('jump.button')">
          <LocateFixed :size="16" />
        </button>
```

b) 在 `<template #footer>` 之前（第 48 行后）加入 JumpDirDialog：
```html
    <JumpDirDialog :open="jumpOpen" @close="jumpOpen = false" @confirm="handleJumpConfirm" />
```

c) script：图标 import（第 60 行）加入 `LocateFixed`；组件 import（第 63 行附近）加入 `JumpDirDialog`：
```ts
import { Folder, FolderPlus, Eye, EyeOff, Pencil, Trash2, RotateCw, LocateFixed } from 'lucide-vue-next'
...
import JumpDirDialog from './file/JumpDirDialog.vue'
```

d) 加入状态与处理函数（在 `const toast = inject('toast', null)` 附近）：
```ts
const jumpOpen = ref(false)
async function handleJumpConfirm(path: string) {
  jumpOpen.value = false
  browseNavigate(path)
}
```

说明：`browseNavigate`（第 149 行）已存在，内部调 `loadBrowse()` 走 `/api/projects?path=`，失败时清空列表并 toast、保持当前位置。

- [ ] **Step 4: 运行测试确认通过**

Run: `npm test -- --run web/src/components/__tests__/projectDialog.test.ts`
Expected: PASS。

- [ ] **Step 5: 运行前端类型检查**

Run: `npx tsc --noEmit`
Expected: 无类型错误。

- [ ] **Step 6: Commit**

```bash
git add web/src/components/ProjectDialog.vue web/src/components/__tests__/projectDialog.test.ts
git commit -m "feat(file): 项目选择器增加跳转目录按钮"
```

---

### Task 5: 面包屑复制路径按钮

**Files:**
- Modify: `web/src/components/file/DirBreadcrumb.vue`
- Modify: `web/src/components/file/__tests__/DirBreadcrumb.test.ts`

- [ ] **Step 1: 写失败测试（追加到现有测试文件）**

在 `web/src/components/file/__tests__/DirBreadcrumb.test.ts` 追加。先给 i18n 与 toast mock 提供必要的依赖。该文件当前无 i18n/toast，需扩展 mountBreadcrumb。修改文件头部：

```ts
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DirBreadcrumb from '@/components/file/DirBreadcrumb.vue'

const LucideStub = { template: '<span class="lucide-stub" />' }

const mockToast = { show: vi.fn() }
const mockWriteText = vi.fn(() => Promise.resolve())

function mountBreadcrumb(props: Record<string, any> = {}) {
  return mount(DirBreadcrumb, {
    props: { path: '', ...props },
    global: {
      stubs: { 'lucide-vue-next': LucideStub },
      provide: { toast: mockToast },
    },
  })
}
```

追加 describe：
```ts
describe('DirBreadcrumb — copy path', () => {
  it('copies full Unix path on copy button click', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: mockWriteText },
      configurable: true,
    })
    mockWriteText.mockResolvedValue(undefined)
    const wrapper = mountBreadcrumb({ path: '/home/user/docs' })
    const copyBtn = wrapper.find('.crumb-copy-btn')
    expect(copyBtn.exists()).toBe(true)
    await copyBtn.trigger('click')
    await vi.waitFor(() => expect(mockWriteText).toHaveBeenCalledWith('/home/user/docs'))
  })

  it('copies full Windows path using backslashes', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: mockWriteText },
      configurable: true,
    })
    mockWriteText.mockResolvedValue(undefined)
    const wrapper = mountBreadcrumb({ path: 'C:\\Users\\admin' })
    await wrapper.find('.crumb-copy-btn').trigger('click')
    await vi.waitFor(() => expect(mockWriteText).toHaveBeenCalledWith('C:\\Users\\admin'))
  })

  it('shows copied feedback and toast after copy', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: mockWriteText },
      configurable: true,
    })
    mockWriteText.mockResolvedValue(undefined)
    const wrapper = mountBreadcrumb({ path: '/home/user' })
    await wrapper.find('.crumb-copy-btn').trigger('click')
    await vi.waitFor(() => expect(mockWriteText).toHaveBeenCalled())
    await nextTick()
    expect(wrapper.find('.crumb-copy-btn').classes()).toContain('copied')
    expect(mockToast.show).toHaveBeenCalled()
  })
})
```

需要导入 `nextTick`：在 `import { describe, expect, it } from 'vitest'` 处补 `nextTick`：
```ts
import { nextTick } from 'vue'
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npm test -- --run web/src/components/file/__tests__/DirBreadcrumb.test.ts`
Expected: FAIL（`.crumb-copy-btn` 不存在）。

- [ ] **Step 3: 实现组件**

修改 `web/src/components/file/DirBreadcrumb.vue`：

a) 模板：在 `</template>` 面包屑遍历结束后加复制按钮：
```html
  <div v-if="parts.length > 0" class="dir-breadcrumb">
    <span class="crumb" @click="$emit('navigate', '')">
      <Home :size="14" />
    </span>
    <template v-for="(part, i) in parts" :key="i">
      <span class="crumb-sep">›</span>
      <span
        class="crumb"
        :class="{ current: i === parts.length - 1 }"
        @click="i < parts.length - 1 && $emit('navigate', reconstructPath(parts.slice(0, i + 1)))"
      >{{ part }}</span>
    </template>
    <span class="crumb-sep" />
    <button class="crumb-copy-btn" :title="t('jump.copyPath')" @click.stop="copyFullPath">
      <Copy :size="13" />
    </button>
  </div>
```

b) script：引入 `Copy`、`useI18n`、`inject`、复制逻辑：
```vue
<script setup>
import { computed, inject } from 'vue'
import { Home, Copy } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { splitPath } from '@/utils/path.ts'

const props = defineProps({
  path: { type: String, default: '' },
})
const emit = defineEmits(['navigate'])
const { t } = useI18n()
const toast = inject('toast', null)
const copied = ref(false)

function copyFullPath() {
  const value = props.path
  if (!value) return
  const doCopy = () => {
    copied.value = true
    setTimeout(() => { copied.value = false }, 800)
    if (toast) toast.show(t('common.copied'), { icon: '📋', type: 'success', duration: 1500 })
  }
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(value).then(doCopy).catch(() => fallbackCopy(value, doCopy))
  } else {
    fallbackCopy(value, doCopy)
  }
}

function fallbackCopy(value, cb) {
  const ta = document.createElement('textarea')
  ta.value = value
  ta.style.cssText = 'position:fixed;opacity:0;top:0;left:0'
  document.body.appendChild(ta)
  ta.select()
  document.execCommand('copy')
  document.body.removeChild(ta)
  cb()
}

// reconstructPath 与 parts 逻辑保持原样（省略号表示保留原有代码）
...
</script>
```

注意：需导入 `ref`：`import { computed, inject, ref } from 'vue'`。保留原有 `reconstructPath`、`parts` 逻辑。若测试中不需要 i18n，可保留；但 mount 时提供 `t` 依赖——测试用 createI18n。建议在测试里加 createI18n（见下方补充）。

> 补充：DirBreadcrumb 使用 `useI18n`，因此 mountBreadcrumb 需注册 i18n 插件。在测试文件加：
> ```ts
> import { createI18n } from 'vue-i18n'
> const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { jump: { copyPath: 'Copy path' }, common: { copied: 'Copied' } } } })
> // 并在 mount 的 global.plugins 加入 [i18n]
> ```

c) 样式：追加
```css
.crumb-copy-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 3px 6px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.15s, color 0.15s;
}
.crumb-copy-btn:hover {
  background: var(--bg-secondary, #e0e0e0);
  color: var(--accent-color, #4a90d9);
}
.crumb-copy-btn.copied {
  color: #22c55e;
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `npm test -- --run web/src/components/file/__tests__/DirBreadcrumb.test.ts`
Expected: PASS。

- [ ] **Step 5: 运行前端类型检查**

Run: `npx tsc --noEmit`
Expected: 无类型错误。

- [ ] **Step 6: Commit**

```bash
git add web/src/components/file/DirBreadcrumb.vue web/src/components/file/__tests__/DirBreadcrumb.test.ts
git commit -m "feat(file): 面包屑尾部增加复制路径按钮"
```

---

### Task 6: 全量前端测试 + 检查

**Files:**
- 无新增文件。

- [ ] **Step 1: 运行前端全量测试**

Run: `npm test -- --run`
Expected: 全部 PASS，无回归。

- [ ] **Step 2: 运行 lint**

Run: `npx eslint web/src/components/file web/src/components/ProjectDialog.vue`
Expected: 无 error。

- [ ] **Step 3: 运行类型检查**

Run: `npx tsc --noEmit`
Expected: 无错误。

- [ ] **Step 4: 运行推送前检查（如时间允许）**

Run: `./scripts/pre-push-checks.sh --skip-coverage --skip-android`
Expected: 通过。

---

## 自检（Self-Review）

**Spec 覆盖：**
- 跳转按钮（文件管理器 + 项目选择器）→ Task 3、Task 4 ✓
- 对话框输入目录路径直接跳转 → Task 2 + Task 3/4 ✓
- Windows 路径处理 → 复用 `store.navigateToDir`（内部 strip 前导 `/`）与 `browseNavigate`（`/api/projects` 支持绝对/盘符）；Task 5 测试覆盖 `C:\` 复制 ✓
- 面包屑复制路径 → Task 5 ✓
- i18n `jump:` 块 → Task 1 ✓
- 测试 → 每个 Task 均含失败测试 + 通过测试 ✓

**类型一致性：**
- `JumpDirDialog` 的 props（`open`）、emits（`close`、`confirm`）在 Task 2/3/4 中一致 ✓
- `handleJumpConfirm(path: string)` 在 FileManagerContent 与 ProjectDialog 中同名同签名 ✓
- `.jump-btn`、`.jump-confirm-btn`、`.jump-cancel-btn`、`.crumb-copy-btn` 类名在实现与测试中一致 ✓
- `store.navigateToDir` 在 Task 3 的 mock 与实现中一致 ✓
