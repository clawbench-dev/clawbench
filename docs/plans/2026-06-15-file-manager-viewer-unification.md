# FileManager + FileViewer 合一实现方案

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将独立的 browse/viewer 双 Tab 合并为单 Tab 堆叠导航，目录浏览为主视图，文件预览为右侧滑入的全屏覆盖层。

**Architecture:** FileManagerContent 作为主视图常驻，FileViewer 作为覆盖层从右侧滑入。新增 `useFileNavStack` composable（模块级单例）管理路径栈和覆盖层开关，文件内容始终从 `store.state.currentFile` 读取（单一真相源），通过 `watch` 同步路径栈与 store。覆盖层支持跨文件路径链接导航，文件栈有历史时可逐层回退，关闭覆盖层恢复原目录状态。

**Tech Stack:** Vue 3 Composition API, TypeScript, CSS transitions, existing FileViewer/FileManagerContent components

---

## 设计决策速查

| # | 决策 | 选择 |
|---|------|------|
| 1 | 交互模型 | 堆叠导航 |
| 2 | 导航语义 | 目录推入 + 文件覆盖 |
| 3 | 覆盖层行为 | 预览 + 跨文件导航（路径链接跳转） |
| 4 | 导航历史 | 文件自有栈（与目录栈隔离） |
| 5 | 目录栈 | 保持当前无栈（dirName 回退 + 面包屑） |
| 6 | 覆盖层方向 | 从右侧滑入 |
| 7 | Tab 结构 | 合并为单 Tab（保留 browse 命名） |
| 8 | 覆盖层尺寸 | 全屏覆盖 |
| 9 | 同目录切换 | 不做 |
| 10 | 跨文件导航 | Markdown 路径 + 代码路径字符串 + 文件内符号跳转 |
| 11 | 关闭后目录 | 恢复原目录 |
| 12 | 返回优先级 | 文件栈优先 → 关闭覆盖层 → 目录回退 |
| 13 | 返回控件 | 两个按钮分离（关闭 + 返回上级文件） |
| 14 | 组件复用 | 直接复用 FileViewer |
| 15 | WelcomeView | 移除 |
| 16 | 附属组件 | 跟随覆盖层 |
| 17 | 聊天打开文件 | 切到文件 Tab + 仅打开覆盖层 |
| 18 | Tab 命名 | 保留 browse |
| 19 | FileWatch | 文件 Tab 活跃时始终连接 |
| 20 | 动画 | 滑入覆盖（目录不动） |
| 21 | 多选模式 | 禁用预览 |

---

### Task 1: 创建 useFileNavStack composable

管理文件预览覆盖层的导航栈。**关键设计：只存路径栈和覆盖层开关，不复制文件对象。** 文件内容始终从 `store.state.currentFile` 读取（单一真相源），避免双状态源不同步问题。栈中回退时调用 `store.selectFile(path)` 同步 store 状态。

采用模块级单例模式，确保所有组件共享同一状态。

**Files:**
- Create: `web/src/composables/useFileNavStack.ts`
- Test: `web/src/composables/__tests__/useFileNavStack.test.ts`

**Step 1: 写失败测试**

```typescript
// web/src/composables/__tests__/useFileNavStack.test.ts
import { describe, it, expect, beforeEach } from 'vitest'
import { useFileNavStack } from '../useFileNavStack'

describe('useFileNavStack', () => {
    // 模块级单例，每次测试前重置
    beforeEach(() => {
        const nav = useFileNavStack()
        nav.closeOverlay()
    })

    it('初始状态：覆盖层关闭，无路径，空栈', () => {
        const nav = useFileNavStack()
        expect(nav.overlayOpen.value).toBe(false)
        expect(nav.currentFilePath.value).toBeNull()
        expect(nav.canGoBack.value).toBe(false)
    })

    it('openFile: 打开文件路径，覆盖层开启', () => {
        const nav = useFileNavStack()
        nav.openFile('/src/main.go')
        expect(nav.overlayOpen.value).toBe(true)
        expect(nav.currentFilePath.value).toBe('/src/main.go')
        expect(nav.canGoBack.value).toBe(false)
    })

    it('openFile 多次：形成导航栈', () => {
        const nav = useFileNavStack()
        nav.openFile('/a.go')
        nav.openFile('/b.go')
        nav.openFile('/c.go')
        expect(nav.currentFilePath.value).toBe('/c.go')
        expect(nav.canGoBack.value).toBe(true)
    })

    it('goBack: 逐层回退路径', () => {
        const nav = useFileNavStack()
        nav.openFile('/a.go')
        nav.openFile('/b.go')
        nav.openFile('/c.go')
        nav.goBack()
        expect(nav.currentFilePath.value).toBe('/b.go')
        nav.goBack()
        expect(nav.currentFilePath.value).toBe('/a.go')
        expect(nav.canGoBack.value).toBe(false)
    })

    it('closeOverlay: 关闭覆盖层并清空栈', () => {
        const nav = useFileNavStack()
        nav.openFile('/a.go')
        nav.openFile('/b.go')
        nav.closeOverlay()
        expect(nav.overlayOpen.value).toBe(false)
        expect(nav.currentFilePath.value).toBeNull()
        expect(nav.canGoBack.value).toBe(false)
    })

    it('goBack 到栈底不关闭覆盖层', () => {
        const nav = useFileNavStack()
        nav.openFile('/a.go')
        nav.openFile('/b.go')
        nav.goBack()
        expect(nav.overlayOpen.value).toBe(true)
        expect(nav.currentFilePath.value).toBe('/a.go')
    })

    it('goBack 在栈底时是 no-op', () => {
        const nav = useFileNavStack()
        nav.openFile('/a.go')
        nav.goBack() // 无历史，no-op
        expect(nav.currentFilePath.value).toBe('/a.go')
        expect(nav.overlayOpen.value).toBe(true)
    })

    it('openFile 同一路径两次：栈中有两个条目', () => {
        const nav = useFileNavStack()
        nav.openFile('/a.go')
        nav.openFile('/a.go')
        expect(nav.canGoBack.value).toBe(true)
        nav.goBack()
        expect(nav.currentFilePath.value).toBe('/a.go')
        expect(nav.canGoBack.value).toBe(false)
    })

    it('closeOverlay 后再次 openFile：栈重新开始', () => {
        const nav = useFileNavStack()
        nav.openFile('/a.go')
        nav.openFile('/b.go')
        nav.closeOverlay()
        nav.openFile('/c.go')
        expect(nav.currentFilePath.value).toBe('/c.go')
        expect(nav.canGoBack.value).toBe(false)
    })

    it('模块级单例：多次调用返回同一实例', () => {
        const nav1 = useFileNavStack()
        const nav2 = useFileNavStack()
        nav1.openFile('/a.go')
        expect(nav2.currentFilePath.value).toBe('/a.go')
        expect(nav2.overlayOpen.value).toBe(true)
    })
})
```

**Step 2: 运行测试验证失败**

Run: `cd /home/xulongzhe/projects/clawbench/web && npx vitest run src/composables/__tests__/useFileNavStack.test.ts`
Expected: FAIL — module not found

**Step 3: 实现 useFileNavStack**

```typescript
// web/src/composables/useFileNavStack.ts
import { ref, computed } from 'vue'

// 模块级单例状态（确保所有组件共享同一实例）
const overlayOpen = ref(false)
const currentFilePath = ref<string | null>(null)
const history = ref<string[]>([])

const canGoBack = computed(() => history.value.length > 0)

function openFile(path: string) {
    if (currentFilePath.value) {
        history.value.push(currentFilePath.value)
    }
    currentFilePath.value = path
    overlayOpen.value = true
}

function goBack(): string | null {
    if (history.value.length === 0) return null
    const prevPath = history.value.pop()!
    currentFilePath.value = prevPath
    return prevPath
}

function closeOverlay() {
    overlayOpen.value = false
    currentFilePath.value = null
    history.value = []
}

/**
 * 文件导航栈 composable（模块级单例）。
 *
 * 设计原则：只存路径栈和覆盖层开关，不复制文件对象。
 * 文件内容始终从 store.state.currentFile 读取（单一真相源）。
 * goBack() 返回回退到的路径，由调用方负责调用 store.selectFile() 同步。
 */
export function useFileNavStack() {
    return {
        overlayOpen,
        currentFilePath,
        canGoBack,
        openFile,
        goBack,
        closeOverlay,
    }
}
```

**Step 4: 运行测试验证通过**

Run: `cd /home/xulongzhe/projects/clawbench/web && npx vitest run src/composables/__tests__/useFileNavStack.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
git add web/src/composables/useFileNavStack.ts web/src/composables/__tests__/useFileNavStack.test.ts
git commit -m "feat: add useFileNavStack composable for file overlay navigation"
```

---

### Task 2: 创建 FileOverlay 组件

全屏覆盖层容器，从右侧滑入动画，内嵌 FileViewer + 附属组件，双按钮（关闭 + 返回）。

**关键：FileOverlay 不持有文件数据，文件对象始终从 `store.state.currentFile` 传入 props。**

**Files:**
- Create: `web/src/components/file/FileOverlay.vue`

**Step 1: 实现 FileOverlay 组件**

```vue
<!-- web/src/components/file/FileOverlay.vue -->
<template>
  <Transition name="file-overlay">
    <div v-if="overlayOpen" class="file-overlay">
      <div class="file-overlay-header">
        <button class="overlay-btn overlay-close-btn" @click="emit('close')" :title="t('common.close')">
          <X :size="18" />
        </button>
        <button v-if="canGoBack" class="overlay-btn overlay-back-btn" @click="emit('goBack')" :title="t('file.overlay.back')">
          <ChevronLeft :size="18" />
        </button>
      </div>
      <div class="file-overlay-content" @click="handleContentClick">
        <FileViewer
          v-if="currentFile"
          ref="fileViewerRef"
          :file="currentFile"
          :toc-open="tocOpen"
          :search-open="searchOpen"
          :markdown-view-mode="markdownViewMode"
          @delete="emit('delete', $event)"
          @show-details="emit('showDetails')"
          @open-git-history="emit('openGitHistory')"
          @toggle-toc="emit('toggleToc')"
          @toggle-search="emit('toggleSearch')"
          @toggle-view="emit('toggleView')"
          @refresh="emit('refresh')"
        />
      </div>
      <!-- 附属组件跟随覆盖层 -->
      <TocDrawer
        :file="tocFile"
        :pdf-outline="pdfOutline"
        :open="overlayOpen && tocOpen"
        @close="emit('toggleToc')"
        @jump="emit('jump', $event)"
        @jump-page="emit('jumpPage', $event)"
      />
      <SearchDrawer
        :file="currentFile"
        :open="overlayOpen && searchOpen"
        :view-mode="currentFileIsMarkdown ? markdownViewMode : undefined"
        @close="emit('toggleSearch')"
        @jump="emit('jump', $event)"
      />
      <GitHistoryDrawer
        :open="overlayOpen && fileHistoryOpen"
        mode="file"
        :file="currentFile"
        @close="emit('closeGitHistory')"
        @open-file="emit('openFile', $event)"
      />
    </div>
  </Transition>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { X, ChevronLeft } from 'lucide-vue-next'
import FileViewer from './FileViewer.vue'
import TocDrawer from '../TocDrawer.vue'
import SearchDrawer from '../common/SearchDrawer.vue'
import GitHistoryDrawer from '../git/GitHistoryDrawer.vue'

const props = defineProps({
  overlayOpen: Boolean,
  currentFile: Object,   // 来自 store.state.currentFile，单一真相源
  canGoBack: Boolean,
  tocOpen: Boolean,
  searchOpen: Boolean,
  markdownViewMode: String,
  fileHistoryOpen: Boolean,
  tocFile: Object,
  pdfOutline: Object,
})

const emit = defineEmits([
  'close', 'goBack', 'delete', 'showDetails', 'openGitHistory',
  'toggleToc', 'toggleSearch', 'toggleView', 'refresh',
  'jump', 'jumpPage', 'closeGitHistory', 'openFile',
])

const { t } = useI18n()

const currentFileIsMarkdown = computed(() => {
  if (!props.currentFile) return false
  const ext = props.currentFile.name?.split('.').pop()?.toLowerCase()
  return ext === 'md' || ext === 'markdown'
})

const fileViewerRef = ref(null)

// 拦截覆盖层内的文件路径链接点击，推入文件栈而非打开新 Tab
function handleContentClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  const fileBtn = target.closest('.chat-file-open-btn')
  const filePathEl = target.closest('[data-file-path]')

  let path: string | null = null
  if (fileBtn) {
    path = fileBtn.getAttribute('data-file-path')
  } else if (filePathEl) {
    path = filePathEl.getAttribute('data-file-path')
  }

  if (path) {
    e.preventDefault()
    e.stopPropagation()
    emit('openFile', path)
  }
}

// 暴露 fileViewerRef 给父组件（用于 scrollToLine 等）
defineExpose({ fileViewerRef })
</script>

<style scoped>
.file-overlay {
  position: absolute;
  inset: 0;
  z-index: 100;
  background: var(--bg-primary, #fff);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.file-overlay-header {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border-bottom: 1px solid var(--border-color, #e0e0e0);
  flex-shrink: 0;
}

.overlay-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-primary, #1a1a1a);
  cursor: pointer;
  transition: background 0.15s;
}

.overlay-btn:hover {
  background: var(--bg-secondary, #e0e0e0);
}

.file-overlay-content {
  flex: 1;
  overflow: hidden;
}

/* 从右侧滑入/滑出 */
.file-overlay-enter-active,
.file-overlay-leave-active {
  transition: transform 0.25s ease;
}

.file-overlay-enter-from,
.file-overlay-leave-to {
  transform: translateX(100%);
}
</style>
```

**Step 2: Commit**

```bash
git add web/src/components/file/FileOverlay.vue
git commit -m "feat: add FileOverlay component for file preview overlay"
```

---

### Task 3: 改造 App.vue — 合并双 Tab 为单 Tab

核心改造：移除 viewer Tab，将 FileOverlay 嵌入 browse Tab 内，修改所有相关的事件处理和状态。

**Files:**
- Modify: `web/src/App.vue:42-106` — 移除 viewer TabPanel，在 browse TabPanel 内嵌入 FileOverlay
- Modify: `web/src/App.vue:150-154` — FileDetailsDialog 条件改为 browse tab + overlayOpen
- Modify: `web/src/App.vue:192-197` — 底部 dock 移除 viewer 按钮，保留 browse 按钮

**Step 1: 在 App.vue 的 `<script setup>` 中引入 useFileNavStack**

在 App.vue 的 import 区域添加：

```typescript
import { useFileNavStack } from './composables/useFileNavStack'
```

在 script setup 中创建实例：

```typescript
const fileNav = useFileNavStack()
```

**Step 2: 修改 handleBrowseSelectFile**

当前代码（App.vue 约 520 行）：
```typescript
async function handleBrowseSelectFile(path) {
    const ok = await store.selectFile(path)
    if (ok) activeTab.value = 'viewer'
}
```

改为：
```typescript
async function handleBrowseSelectFile(path) {
    // 多选模式下禁用预览（通过 FileManagerContent emit 的 multiSelect prop 检测）
    if (fileManagerRef.value?.multiSelectState?.active) return
    const ok = await store.selectFile(path)
    if (ok) {
        fileNav.openFile(path)
    }
}
```

**Step 3: 修改 handleSelectFile（从聊天/其他来源打开文件）**

当前代码：
```typescript
async function handleSelectFile(path) {
    await store.selectFile(path)
}
```

改为：
```typescript
async function handleSelectFile(path) {
    const ok = await store.selectFile(path)
    if (ok) {
        // 切到文件 Tab + 仅打开覆盖层，不改变目录
        activeTab.value = 'browse'
        fileNav.openFile(path)
    }
}
```

**Step 4: 修改 handleTaskOpenFile**

当前代码：
```typescript
async function handleTaskOpenFile(filePath, lineStart) {
    const ok = await store.selectFile(filePath)
    if (ok) {
        switchTab('viewer')
        if (lineStart) scrollToLine(lineStart)
    }
}
```

改为：
```typescript
async function handleTaskOpenFile(filePath, lineStart) {
    const ok = await store.selectFile(filePath)
    if (ok) {
        activeTab.value = 'browse'
        fileNav.openFile(filePath)
        if (lineStart) scrollToLine(lineStart)
    }
}
```

**Step 5: 修改 handleOpenFileManager**

当前代码：
```typescript
function handleOpenFileManager() {
    activeTab.value = 'browse'
}
```

不变，保持原样。

**Step 6: 修改模板 — 移除 viewer TabPanel，在 browse 内嵌入 FileOverlay**

移除整个 `<!-- File Viewer Tab -->` TabPanel（约第 63-106 行）。

将 browse TabPanel（约第 42-61 行）改为：

```html
<!-- File Browse Tab (合一：目录浏览 + 文件覆盖预览) -->
<TabPanel tabId="browse" :activeTab="activeTab" :noHeader="true">
  <div class="browse-panel">
    <FileManagerContent
      :entries="dirEntries"
      :current-dir="currentDir"
      :current-file="currentFile"
      :show-hidden="showHidden"
      :sort-field="sortField"
      :sort-dir="sortDir"
      :dir-loading="store.state.dirLoading"
      @navigate-dir="handleNavigateDir"
      @select-file="handleBrowseSelectFile"
      @toggle-sort="handleToggleSort"
      @toggle-hidden="toggleHidden"
      @rename="handleRename"
      @delete="handleDelete"
      @batch-delete="handleBatchDelete"
      @refresh="handleRefresh"
      @open-terminal="handleOpenTerminal"
    />
    <FileOverlay
      :overlay-open="fileNav.overlayOpen.value"
      :current-file="currentFile"
      :can-go-back="fileNav.canGoBack.value"
      :toc-open="tocOpen"
      :search-open="searchOpen"
      :markdown-view-mode="markdownViewMode"
      :file-history-open="fileHistoryOpen"
      :toc-file="tocFile"
      :pdf-outline="pdfOutline"
      @close="handleOverlayClose"
      @go-back="handleOverlayGoBack"
      @delete="handleDelete($event)"
      @show-details="detailsOpen = true"
      @open-git-history="openFileHistory"
      @toggle-toc="tocOpen = !tocOpen"
      @toggle-search="currentFile?.content && (searchOpen = !searchOpen)"
      @toggle-view="markdownViewMode = markdownViewMode === 'rendered' ? 'raw' : 'rendered'"
      @refresh="handleRefresh"
      @jump="scrollToLine"
      @jump-page="handleJumpPdfPage"
      @close-git-history="fileHistoryOpen = false"
      @open-file="handleOverlayOpenFile"
    />
  </div>
</TabPanel>
```

**Step 7: 添加覆盖层事件处理函数**

```typescript
function handleOverlayClose() {
    fileNav.closeOverlay()
    // 关闭附属抽屉
    tocOpen.value = false
    detailsOpen.value = false
    searchOpen.value = false
    fileHistoryOpen.value = false
}

async function handleOverlayGoBack() {
    const prevPath = fileNav.goBack()
    if (prevPath) {
        // 回退到上一个文件路径，同步 store 状态
        await store.selectFile(prevPath)
    }
}

async function handleOverlayOpenFile(path) {
    const ok = await store.selectFile(path)
    if (ok) {
        fileNav.openFile(path)
    }
}
```

**Step 8: 修改 FileDetailsDialog 条件**

当前代码（约第 150-154 行）：
```html
<FileDetailsDialog
  :file="currentFile"
  :open="activeTab === 'viewer' && detailsOpen"
  @close="detailsOpen = false"
/>
```

改为：
```html
<FileDetailsDialog
  :file="currentFile"
  :open="activeTab === 'browse' && fileNav.overlayOpen.value && detailsOpen"
  @close="detailsOpen = false"
/>
```

**Step 9: 修改底部 dock — 移除 viewer 按钮**

移除（约第 192-194 行）：
```html
<button class="dock-btn" :class="{ active: activeTab === 'viewer' }" @click.stop="switchTab('viewer')" :title="t('nav.fileViewer')">
  <FileText />
</button>
```

保留 browse 按钮（第 195-197 行），无需修改。

**Step 10: 移除 WelcomeView import 和使用**

移除 import：
```typescript
import WelcomeView from './components/WelcomeView.vue'
```

模板中已无引用（viewer Tab 已移除），无需额外处理。

**Step 11: 修改 useFileWatch 参数**

当前代码：
```typescript
useFileWatch({
  fileManagerOpen: computed(() => activeTab.value === 'browse'),
  currentDir: computed(() => store.state.currentDir),
  currentFile: computed(() => store.state.currentFile),
})
```

改为（覆盖层打开时监听覆盖层内文件变化，关闭时仍监听 store 中的文件）：
```typescript
useFileWatch({
  fileManagerOpen: computed(() => activeTab.value === 'browse'),
  currentDir: computed(() => store.state.currentDir),
  currentFile: computed(() => store.state.currentFile),
})
```

注意：由于 `store.state.currentFile` 始终是单一真相源（goBack/openFile 都同步调用 store.selectFile），useFileWatch 参数无需特殊处理。当覆盖层打开时 store.state.currentFile 就是当前预览的文件，useFileWatch 自然监听正确的文件。

**Step 12: 修改 useFeatureBackHandler 逻辑**

当前 FileManagerContent 中的 back handler 在目录浏览时回退目录。合一后需要处理覆盖层打开时的文件栈回退。

在 App.vue 中添加 back handler 注册（在 useFileWatch 附近）：

```typescript
// 文件覆盖层的返回手势：文件栈优先
useFeatureBackHandler(
  'file-overlay',
  () => activeTab.value === 'browse' && fileNav.overlayOpen.value,
  () => {
    if (fileNav.canGoBack.value) {
      fileNav.goBack()
    } else {
      fileNav.closeOverlay()
      tocOpen = false
      searchOpen = false
      fileHistoryOpen = false
      detailsOpen = false
    }
  },
)
```

注意：`useFeatureBackHandler` 在 `FileManagerContent` 中也有注册（`'browse'` id）。由于 handler 按注册顺序逆序执行，后注册的 file-overlay handler 会先被检查。当覆盖层打开时，file-overlay handler 的 `canGoBack` 返回 true，会消费返回事件，不再传递到 browse handler。覆盖层关闭后，file-overlay 的 `canGoBack` 返回 false，返回事件自然传递到 browse handler 处理目录回退。

**Step 13: 移除 viewer 相关的 import**

移除不再需要的 `FileText` import（底部 dock 的 viewer 按钮图标）：
```typescript
// 如果 FileText 仅用于 viewer 按钮，从 import 中移除
import { MessageSquare, FolderOpen, GitBranch, EthernetPort, Terminal as TerminalIcon, CalendarClock, MoreHorizontal, Settings } from 'lucide-vue-next'
```

**Step 14: 添加 browse-panel CSS**

```css
.browse-panel {
  position: relative;
  width: 100%;
  height: 100%;
}
```

**Step 15: 验证构建通过**

Run: `cd /home/xulongzhe/projects/clawbench/web && npx vite build`
Expected: 构建成功

**Step 16: Commit**

```bash
git add web/src/App.vue
git commit -m "feat: merge browse/viewer tabs into single browse tab with file overlay"
```

---

### Task 4: 修改 openFilePath — 聊天/外部打开文件走覆盖层

当前 `openFilePath` 直接调用 `store.selectFile()` + dispatch `open-file-manager` 事件。需要改为打开覆盖层而不是切换 Tab。

**Files:**
- Modify: `web/src/composables/useFilePathAnnotation.ts:471-511` — openFilePath 函数

**Step 1: 修改 openFilePath**

当前代码：
```typescript
export async function openFilePath(resolvedPath: string, lineStart?: number): Promise<boolean> {
    // Check if path is a directory
    try {
        const resp = await fetch(`/api/dir?path=${encodeURIComponent(resolvedPath)}`)
        if (resp.ok) {
            await store.navigateToDir(resolvedPath)
            window.dispatchEvent(new CustomEvent('open-file-manager'))
            return true
        }
    } catch {
        // Ignore, fall through to open as file
    }

    // ... batch-exists check ...

    const ok = await store.selectFile(resolvedPath)
    if (ok && lineStart) dispatchScrollToLine(lineStart)
    return ok
}
```

改为（文件路径：切到 browse tab + 打开覆盖层；目录路径：切到 browse tab + 导航到目录）：

```typescript
export async function openFilePath(resolvedPath: string, lineStart?: number): Promise<boolean> {
    // Check if path is a directory
    try {
        const resp = await fetch(`/api/dir?path=${encodeURIComponent(resolvedPath)}`)
        if (resp.ok) {
            await store.navigateToDir(resolvedPath)
            window.dispatchEvent(new CustomEvent('open-file-manager'))
            return true
        }
    } catch {
        // Ignore, fall through to open as file
    }

    // Before selecting the file, verify it actually exists.
    try {
        const resp = await fetch(`/api/file/batch-exists`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: [resolvedPath] }),
        })
        if (resp.ok) {
            const data = await resp.json() as { results: Record<string, string> }
            const type = data.results?.[resolvedPath]
            if (type !== 'file' && type !== 'dir') {
                const { useToast } = await import('@/composables/useToast')
                const { gt } = await import('@/composables/useLocale')
                useToast().show(gt('file.toast.fileNotFound'), { type: 'error', icon: '⚠️', duration: 2000 })
                return false
            }
        }
    } catch {
        // Batch-exists check failed — proceed with selectFile as best-effort
    }

    const ok = await store.selectFile(resolvedPath)
    if (ok) {
        // 通知 App.vue 打开文件覆盖层
        window.dispatchEvent(new CustomEvent('open-file-overlay', { detail: { path: resolvedPath, lineStart } }))
    }
    return ok
}
```

**Step 2: 在 App.vue 中监听 open-file-overlay 事件**

在 App.vue 的 onMounted 中添加：

```typescript
window.addEventListener('open-file-overlay', handleOpenFileOverlay)
```

在 onUnmounted 中移除：

```typescript
window.removeEventListener('open-file-overlay', handleOpenFileOverlay)
```

添加处理函数（包含 lineStart 处理）：

```typescript
function handleOpenFileOverlay(e: CustomEvent) {
    const { path, lineStart } = e.detail
    if (store.state.currentFile && store.state.currentFile.path === path) {
        activeTab.value = 'browse'
        fileNav.openFile(path)
        if (lineStart) scrollToLine(lineStart)
    }
}
```

**Step 3: 修改 ChatMessageList / ChatPanelContent / GitHistoryContent 中的 switchTab('viewer')**

搜索所有直接调用 `switchTab('viewer')` 的地方：

```bash
cd /home/xulongzhe/projects/clawbench/web && grep -rn "switchTab('viewer')\|navigateToFileViewer" src/
```

逐一改为发 `open-file-overlay` 事件或切到 browse tab。主要位置：

- `ChatPanelContent.vue`：`provide('chatUI', { navigateToFileViewer: ... })` → 改为 `() => { activeTab.value = 'browse' }` （inject switchTab 后调用）
- `ChatMessageList.vue`：`chatUI.navigateToFileViewer?.()` → 同上
- `GitHistoryContent.vue`：`switchTab('viewer')` → 发 `open-file-overlay` 事件或用 inject 的 switchTab

**Step 4: 处理 clawbenchLastFile 恢复逻辑**

当前 App.vue onMounted 中恢复 lastFile 后不自动跳到 viewer。合一后，如果有 lastFile 应自动打开覆盖层：

搜索 `clawbenchLastFile`，在恢复文件后添加：
```typescript
if (lastFile && store.state.currentFile) {
    fileNav.openFile(lastFile)
}
```

**Step 5: 项目切换时关闭覆盖层**

在 `hotSwitchProject` 函数中，确保调用 `fileNav.closeOverlay()`：

搜索 `hotSwitchProject`，在重置项目状态的逻辑中添加 `fileNav.closeOverlay()`。

**Step 6: 运行前端测试**

Run: `cd /home/xulongzhe/projects/clawbench/web && npx vitest run`
Expected: 全部通过

**Step 7: Commit**

```bash
git add web/src/composables/useFilePathAnnotation.ts web/src/App.vue web/src/components/chat/ web/src/components/git/
git commit -m "feat: redirect all viewer tab references to file overlay"
```

---

### Task 5: 修改 FileManagerContent — 多选模式时禁用预览

当用户处于多选模式时，点击文件只切换选中状态，不打开预览覆盖层。

**Files:**
- Modify: `web/src/components/file/FileManagerContent.vue`

**Step 1: 暴露多选状态给父组件**

FileManagerContent 已有 `createMultiSelect()` 返回的 `multiSelect` 对象。需要将其暴露出去：

在 FileManagerContent 中添加：
```typescript
// 在 script setup 中
const multiSelectState = multiSelect // createMultiSelect() 的返回值

defineExpose({
    multiSelectState,
})
```

**Step 2: 在 App.vue 中获取多选状态**

使用 template ref 获取 FileManagerContent 的多选状态：

```html
<FileManagerContent
  ref="fileManagerRef"
  ...
/>
```

在 handleBrowseSelectFile 中检查：
```typescript
async function handleBrowseSelectFile(path) {
    // 多选模式下禁用预览
    if (fileManagerRef.value?.multiSelectState?.active) return
    const ok = await store.selectFile(path)
    if (ok && store.state.currentFile) {
        fileNav.openFile(store.state.currentFile)
    }
}
```

**Step 3: Commit**

```bash
git add web/src/components/file/FileManagerContent.vue web/src/App.vue
git commit -m "feat: disable file preview in multi-select mode"
```

---

### Task 6: 代码路径字符串注解 — 在 CodePreview 中识别 import/path

当前 `useFilePathAnnotation` 只在 MarkdownPreview 和 ChatMessageList 中使用。需要将其扩展到 CodePreview 中，识别代码中的 import 语句和文件路径字符串。

**Files:**
- Modify: `web/src/components/file/CodePreview.vue`
- Test: `web/src/components/file/__tests__/CodePreview.test.ts` (如有)

**Step 1: 在 CodePreview 中添加路径注解**

CodePreview 使用 hljs 渲染代码后，需要扫描渲染后的 DOM 中的 `<code>` 元素，检测路径模式并注解。

在 CodePreview 的 `onUpdated` 或 `watch` 中，渲染完成后调用路径注解逻辑：

```typescript
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation'

// 在渲染完成后注解代码中的文件路径
const { annotateFilePaths, verifyFilePaths, resolveFilePath } = useFilePathAnnotation()

// 检测 hljs 渲染后的 <span class="hljs-string"> 中的路径
function annotateCodePaths(container: HTMLElement) {
    if (!props.file?.path) return
    
    // 获取当前文件所在目录作为 baseDir
    const filePath = props.file.path
    const lastSlash = filePath.lastIndexOf('/')
    const baseDir = lastSlash >= 0 ? filePath.substring(0, lastSlash) : ''
    
    // 扫描所有 hljs-string span，检测路径
    const strings = container.querySelectorAll('.hljs-string')
    for (const el of strings) {
        const text = (el.textContent || '').replace(/['"]/g, '').trim()
        const resolved = resolveFilePath(text, store.state.projectRoot, store.state.homeDir)
        if (resolved && !el.classList.contains('chat-file-path')) {
            el.classList.add('chat-file-path')
            el.setAttribute('data-file-path', resolved)
            el.style.cursor = 'pointer'
            el.style.textDecoration = 'underline'
            el.style.textDecorationStyle = 'dotted'
        }
    }
}
```

**Step 2: 为注解的路径添加点击处理**

在 CodePreview 的内容区域添加点击事件委托：

```typescript
function handleCodeClick(e: MouseEvent) {
    const target = e.target as HTMLElement
    const filePath = target.closest('[data-file-path]')?.getAttribute('data-file-path')
    if (filePath) {
        handleOverlayOpenFile(filePath)
    }
}
```

**Step 3: 验证**

手动测试：打开一个包含 import 语句的代码文件，确认路径字符串被注解并可点击跳转。

**Step 4: Commit**

```bash
git add web/src/components/file/CodePreview.vue
git commit -m "feat: annotate file paths in code preview for cross-file navigation"
```

---

### Task 7: FileViewer handleOpenAsText 修复

当前 FileViewer.vue 中 `handleOpenAsText()` 直接调用 `store.selectFile(props.file.path, false, false, false, true)`。这在覆盖层中调用时，会更新 `store.state.currentFile` 但不更新文件栈中的路径。由于我们采用单一真相源（文件数据始终从 store 读取），这不是问题——`store.selectFile` 会原地更新 `store.state.currentFile`，FileOverlay 传入的 `:file="currentFile"` 会自动反映变化。无需额外修改。

验证 handleOpenAsText 在覆盖层中正常工作即可。

**Step 1: Commit（如有修改）**

```bash
git add -A
git commit -m "fix: ensure handleOpenAsText works correctly in overlay mode"
```

---

### Task 8: 清理 — 移除 viewer Tab 相关代码

移除所有 viewer Tab 相关的残留代码。

**Files:**
- Modify: `web/src/App.vue` — 移除 viewer 相关的 lucide icon import、WelcomeView import
- Modify: `web/src/stores/app.ts` — 检查是否有 viewer 特定逻辑需清理

**Step 1: 清理 App.vue imports**

- 移除 `FileText` from lucide imports（如已无其他引用）
- 移除 `WelcomeView` import

**Step 2: 搜索所有 'viewer' 引用**

```bash
cd /home/xulongzhe/projects/clawbench/web && grep -rn "activeTab === 'viewer'\|switchTab('viewer')\|tabId=\"viewer\"" src/
```

逐一确认并移除/替换。

**Step 3: 检查 i18n 中是否有 viewer 专用 key 需清理**

```bash
cd /home/xulongzhe/projects/clawbench/web && grep -rn "nav.fileViewer\|viewer\." src/locales/
```

保留 `nav.fileViewer` key 但不再使用（避免翻译文件报错），或移除。

**Step 4: Commit**

```bash
git add -A
git commit -m "chore: clean up viewer tab references after browse/viewer merge"
```

---

### Task 9: 前端测试补充与 coverage gate

运行所有测试和 coverage gate，确保合一切换没有破坏现有功能。useFileNavStack 的边界测试已在 Task 1 中完整覆盖。

**Step 1: 运行全部前端测试**

Run: `cd /home/xulongzhe/projects/clawbench/web && npx vitest run`
Expected: 全部通过

**Step 2: 运行前端 coverage gate**

Run: `cd /home/xulongzhe/projects/clawbench && ./scripts/check-frontend-coverage.sh`
Expected: 通过

**Step 3: Commit（如有新增测试）**

```bash
git add web/src/
git commit -m "test: verify all tests pass after file manager/viewer unification"
```

---

### Task 10: 端到端手动验证

**Step 1: 构建并启动**

```bash
cd /home/xulongzhe/projects/clawbench && ./build.sh && ./clawbench
```

**Step 2: 验证清单**

- [ ] 浏览器打开，底部 dock 只有一个文件按钮（browse），无 viewer 按钮
- [ ] 点击文件按钮进入目录浏览
- [ ] 点击文件，覆盖层从右侧滑入，显示文件内容
- [ ] 覆盖层左上角有关闭按钮（X），有历史时有返回按钮（←）
- [ ] Markdown 预览中点击文件路径链接，覆盖层内加载新文件
- [ ] 点击返回按钮，回退到上一个文件
- [ ] 点击关闭按钮，覆盖层滑出，回到原目录
- [ ] 从聊天中点击文件路径链接，切到 browse tab 并打开覆盖层
- [ ] 关闭覆盖层后目录不变（恢复原目录）
- [ ] 多选模式下点击文件不打开预览
- [ ] Android 返回键：有文件历史时回退文件栈，无历史时关闭覆盖层，覆盖层关闭后回退目录
- [ ] FileWatch 在 browse Tab 时正常工作（目录变化刷新列表，文件变化刷新预览）
- [ ] TocDrawer / SearchDrawer / GitHistoryDrawer / FileDetailsDialog 正常工作
- [ ] 覆盖层关闭时附属抽屉也关闭

**Step 3: 最终 Commit**

```bash
git add -A
git commit -m "feat: complete file manager/viewer unification with stacked navigation"
```

---

## 实现顺序依赖

```
Task 1 (useFileNavStack) ──→ Task 3 (App.vue 改造) ──→ Task 4 (openFilePath) ──→ Task 8 (清理)
                          ──→ Task 5 (多选禁用)
Task 2 (FileOverlay)    ──→ Task 3                        ──→ Task 7 (路径导航集成)
Task 6 (代码路径注解)   ──→ Task 7
Task 9 (测试)           ──→ 在每个 Task 完成后补充
Task 10 (手动验证)      ──→ 所有 Task 完成后
```

建议执行顺序：1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10

Task 1 和 Task 2 可以并行（无依赖），Task 6 和 Task 8 也可以并行。

---

## 审查修复记录

方案经过 architect-review 审查，以下问题已修复：

| 问题 | 严重度 | 修复方式 |
|------|--------|----------|
| 双文件状态源冲突 | P0 | useFileNavStack 改为只存路径栈+开关，文件数据始终从 store.state.currentFile 读取，goBack 返回路径由调用方调 store.selectFile 同步 |
| useFileNavStack 非单例 | P1 | 改为模块级单例（模块顶层 ref），多次调用返回同一实例 |
| 遗漏 ChatMessageList 等的 switchTab('viewer') | P0 | Task 4 扩展，搜索所有 switchTab('viewer') 引用并改为 open-file-overlay 事件 |
| openFilePath 未传 lineStart | P0 | open-file-overlay 事件 detail 加入 lineStart，handleOpenFileOverlay 处理 scrollToLine |
| FileOverlay.vue 缺 ref 导入 | P0 | 修正 import，添加 ref |
| 项目切换未关闭覆盖层 | P1 | Task 4 中补充 hotSwitchProject 调用 fileNav.closeOverlay() |
| useFileWatch 修改有误 | P1 | 保持 currentFile 参数不变（因为 store.state.currentFile 始终是真相源，覆盖层打开时它就是当前文件） |
| lastFile 恢复不打开覆盖层 | P1 | Task 4 中补充 clawbenchLastFile 恢复后自动打开覆盖层 |
| FileOverlay handleContentClick 集成 | P0 | 已移入 Task 2 FileOverlay 组件中，不再需要单独的 Task 7 |
| handleOpenAsText 在覆盖层中的行为 | P2 | Task 7 改为验证性任务，确认 store.selectFile 原地更新会自动反映到覆盖层 |
