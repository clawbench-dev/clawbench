<template>
  <div class="markdown-preview">
    <!-- Rendered markdown -->
    <div v-if="viewMode === 'rendered'" class="markdown-body" ref="bodyRef" :data-file-path="file?.path || ''" v-html="renderedHtml" @click="handleClick" />

    <!-- Raw markdown -->
    <CodePreview
      v-else
      :content="file.content"
      language="markdown"
      :file-path="file.path"
      :word-wrap="wordWrap"
      :show-line-numbers="showLineNumbers"
      :flash-ranges="flashRanges"
      :flash-type="flashType"
      :sticky-scroll="stickyScroll"
    />
  </div>
</template>

<script setup>
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import CodePreview from './CodePreview.vue'
import { useMarkdownRenderer } from '@/composables/useMarkdownRenderer.ts'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { store } from '@/stores/app.ts'
import { dirName, splitPath } from '@/utils/path.ts'
import { flashRanges, flashType, flashTextSnippets } from '@/composables/useFileRefresh.ts'

const props = defineProps({
    file: Object,
    viewMode: String,
    wordWrap: Boolean,
    showLineNumbers: { type: Boolean, default: true },
    stickyScroll: { type: Boolean, default: true },
})

const renderedHtml = ref('')
const bodyRef = ref(null)
const imageTimestamp = ref(Date.now())
let currentRenderId = 0

const quoteQuestion = useQuoteQuestion()

const { handleDblClick } = useDoubleClickCopy({
    lineSelector: '.code-line',
    onCopy(target, text) {
        // Check if double-click was on a .code-line inside a code block
        const lineEl = target && 'closest' in target ? target.closest('.code-line') : null
        if (lineEl) {
            // Per-line copy with language + line number context
            const preEl = lineEl.closest('pre')
            const block = lineEl.closest('.markdown-body')
            const filePath = block?.getAttribute('data-file-path') || props.file?.path || ''
            const language = preEl?.getAttribute('data-language') || ''
            const lineNum = parseInt(lineEl.getAttribute('data-line') || '0')
            quoteQuestion.showBar({
                text,
                filePath,
                language,
                startLine: lineNum,
                endLine: lineNum,
            })
            return
        }
        // Fallback: general markdown body copy
        const block = target && 'closest' in target ? target.closest('.markdown-body') : null
        const filePath = block?.getAttribute('data-file-path') || props.file?.path || ''
        quoteQuestion.showBar({
            text,
            filePath,
            language: '',
            startLine: 0,
            endLine: 0,
        })
    },
})
const { renderMarkdown, renderMermaidInElement } = useMarkdownRenderer()
const { annotateFilePaths, verifyFilePaths, resolveRelativePath, openFilePath } = useFilePathAnnotation()

function handleClick(event) {
    // Check for commit-hash click first (span or button)
    const commitEl = (event.target).closest('.chat-commit-hash, .chat-commit-open-btn')
    if (commitEl) {
        event.preventDefault()
        event.stopPropagation()
        const sha = commitEl.getAttribute('data-commit-sha')
        if (sha) {
            window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
        }
        return
    }
    // Check for file-open button click
    const btn = (event.target).closest('.chat-file-open-btn')
    if (btn) {
        event.preventDefault()
        event.stopPropagation()
        const filePath = btn.getAttribute('data-file-path')
        if (filePath) {
            openFilePath(filePath)
        }
        return
    }
    // In-page anchor links: scroll smoothly + flash highlight (matches TocDrawer behavior)
    const linkEl = (event.target).closest('a[href^="#"]')
    if (linkEl) {
        const href = linkEl.getAttribute('href') || ''
        // Ignore empty anchors (e.g., <a href="#">) so they don't interfere
        if (href.length > 1) {
            const targetId = decodeURIComponent(href.slice(1))
            const targetEl = bodyRef.value?.querySelector(`#${CSS.escape(targetId)}`)
            if (targetEl) {
                event.preventDefault()
                event.stopPropagation()
                targetEl.scrollIntoView({ behavior: 'smooth', block: 'start' })
                targetEl.classList.add('line-flash')
                targetEl.addEventListener('animationend', () => targetEl.classList.remove('line-flash'), { once: true })
                return
            }
        }
    }
    // Handle <a> link clicks (relative paths) + double-click copy
    handleDblClick(event, (href) => {
        const currentDir = props.file?.path ? dirName(props.file.path) : ''
        const resolvedPath = resolveRelativePath(href, currentDir)
        openFilePath(resolvedPath)
    })
}

function fixLocalImagePaths(html) {
    const currentDir = props.file?.path ? dirName(props.file.path) : ''
    return html.replace(/<img\s+([^>]*src=[^>]*)>/gi, (match, attrs) => {
        const srcMatch = attrs.match(/src="([^"]*)"/)
        if (!srcMatch) return match
        const src = srcMatch[1]
        if (/^(https?:|\/\/|^\/)/i.test(src)) return match
        // Decode percent-encoded src first (marked may encode Chinese chars),
        // then re-encode each segment properly for the URL
        let resolved = currentDir ? currentDir + '/' + src : src
        try {
            resolved = decodeURIComponent(resolved)
        } catch { /* malformed encoding, use as-is */ }
        const parts = splitPath(resolved)
        const normalized = []
        for (const part of parts) {
            if (part === '.' || part === '') continue
            if (part === '..') { normalized.pop(); continue }
            normalized.push(encodeURIComponent(part))
        }
        return match.replace(`src="${src}"`, `src="/api/local-file/${normalized.join('/')}?t=${imageTimestamp.value}"`)
    })
}

async function doRender(f) {
    const renderId = ++currentRenderId
    imageTimestamp.value = Date.now()
    let html = renderMarkdown(f.content, {
        sanitize: false, // MarkdownPreview不需要净化，因为是受信任的文件内容
        fixImagePaths: fixLocalImagePaths
    })

    // Annotate file paths with open buttons
    const currentDir = f?.path ? dirName(f.path) : ''
    const { html: annotatedHtml, detectedPaths } = annotateFilePaths(html, {
        projectRoot: store.state.projectRoot,
        baseDir: currentDir,
        homeDir: store.state.homeDir
    })
    renderedHtml.value = annotatedHtml

    if (renderId !== currentRenderId) return
    await nextTick()
    if (renderId !== currentRenderId) return
    const el = bodyRef.value
    if (!el) return

    // Verify file existence and hide buttons for non-existent files
    if (detectedPaths.length > 0) {
        const uniquePaths = [...new Set(detectedPaths)]
        verifyFilePaths(uniquePaths, el)
    }

    // 【注意】KaTeX 已在 renderMarkdown 内的 renderKatexInString 完成字符串级渲染，
    // 这里只做 Mermaid 的 DOM 级渲染（Mermaid 是整体节点替换，与 v-html 不冲突）
    await renderMermaidInElement(el, 'md-preview')
}

watch(() => props.file, (f, oldF) => {
    if (!f || f.error) {
        renderedHtml.value = ''
        return
    }
    // Cancel any in-flight render from old file.
    // Actual rendering is handled by the content watcher below.
    currentRenderId++
}, { immediate: true })

watch(() => props.file?.content, (content) => {
    if (!content) return
    const f = props.file
    if (!f || f.error) return
    doRender(f)
}, { immediate: true })

// 当 viewMode 切换回 rendered 时，DOM 会被 v-if 重建，
// Mermaid 的 SVG 渲染结果丢失，需要重新执行 DOM 级渲染
watch(() => props.viewMode, async (mode) => {
    if (mode !== 'rendered') return
    const f = props.file
    if (!f || f.error || !f.content) return
    // renderedHtml 已有值，只需等 DOM 挂载后重新渲染 Mermaid
    await nextTick()
    const el = bodyRef.value
    if (!el) return
    await renderMermaidInElement(el, 'md-preview')
})

// ─── Rendered markdown flash-highlight via DOM search ───
// When flashTextSnippets changes and we're in rendered mode,
// search the rendered DOM for matching text and wrap it in flash spans.

/** Remove all previously added flash spans/classes from the DOM */
function removeFlashSpans(container) {
    if (!container) return
    // Remove flash spans (wrapping approach)
    const existing = container.querySelectorAll('.md-char-flash-delete, .md-char-flash-add')
    for (const span of existing) {
        const parent = span.parentNode
        if (parent) {
            // Move all child nodes out of the span, then remove the span
            while (span.firstChild) {
                parent.insertBefore(span.firstChild, span)
            }
            parent.removeChild(span)
        }
    }
    // Remove flash classes (fallback approach — class added directly to element)
    const classBased = container.querySelectorAll('.md-flash-delete, .md-flash-add')
    for (const el of classBased) {
        el.classList.remove('md-flash-delete', 'md-flash-add')
    }
    // Normalize merges adjacent text nodes that were split
    container.normalize()
}

/**
 * Build a flat text + node-offset map from all text nodes in the container.
 * This lets us search the concatenated text and map offsets back to DOM nodes.
 */
function buildTextNodeMap(container) {
    const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT, null)
    const nodes = []
    const offsets = [] // cumulative text offset at the start of each node
    let cumLen = 0
    while (walker.nextNode()) {
        const node = walker.currentNode
        nodes.push(node)
        offsets.push(cumLen)
        cumLen += node.textContent.length
    }
    // Concatenate all text
    const fullText = nodes.map(n => n.textContent).join('')
    return { nodes, offsets, fullText }
}

/**
 * Given a start/end offset in the concatenated text, wrap the corresponding
 * DOM range in a flash span. Handles matches that span multiple text nodes.
 */
function wrapRangeInSpan(nodes, offsets, startOff, endOff, cls, container) {
    // Find the text node containing startOff
    let startNodeIdx = 0
    for (let i = 0; i < offsets.length; i++) {
        if (offsets[i] <= startOff) startNodeIdx = i
        else break
    }
    // Find the text node containing endOff
    let endNodeIdx = startNodeIdx
    for (let i = startNodeIdx; i < offsets.length; i++) {
        if (offsets[i] < endOff) endNodeIdx = i
        else break
    }

    const startNode = nodes[startNodeIdx]
    const endNode = nodes[endNodeIdx]
    const localStart = startOff - offsets[startNodeIdx]
    const localEnd = endOff - offsets[endNodeIdx]

    if (startNode === endNode) {
        // Match is within a single text node — best case
        try {
            const range = document.createRange()
            range.setStart(startNode, localStart)
            range.setEnd(endNode, localEnd)
            const span = document.createElement('span')
            span.className = cls
            range.surroundContents(span)
            return true
        } catch {
            // surroundContents can fail for edge cases
        }
    }

    // Match spans multiple text nodes or surroundContents failed.
    // Fallback: highlight the nearest common ancestor element.
    try {
        const range = document.createRange()
        range.setStart(startNode, localStart)
        range.setEnd(endNode, localEnd)
        // Use extractContents + reinsert wrapped in span
        const fragment = range.extractContents()
        const span = document.createElement('span')
        span.className = cls
        span.appendChild(fragment)
        range.insertNode(span)
        return true
    } catch {
        // Last resort: highlight parent elements
        const startParent = startNode.parentElement
        if (startParent && container.contains(startParent) && !startParent.classList.contains('markdown-body')) {
            startParent.classList.add(cls)
            return true
        }
    }
    return false
}

/**
 * Search for snippet text in the rendered DOM and wrap matches in flash spans.
 * Uses a concatenated text map to find matches that span multiple text nodes
 * (e.g., code blocks, bold text with nested elements).
 */
function applyFlashToRenderedDOM(container, snippets, type) {
    if (!container || !snippets || snippets.length === 0) return

    const cls = type === 'delete' ? 'md-char-flash-delete' : 'md-char-flash-add'
    const matchedNodes = new Set() // avoid double-highlighting

    for (const snippet of snippets) {
        if (!snippet || snippet.length < 3) continue

        // Rebuild text map each time (DOM changes after wrapping)
        const { nodes, offsets, fullText } = buildTextNodeMap(container)

        // Search for snippet in the concatenated text
        const idx = fullText.indexOf(snippet)
        if (idx === -1) continue

        // Check if any node in the range was already highlighted
        const endIdx = idx + snippet.length
        let alreadyMatched = false
        for (let i = 0; i < nodes.length; i++) {
            if (offsets[i] >= endIdx) break
            if (offsets[i] + nodes[i].textContent.length > idx && matchedNodes.has(nodes[i])) {
                alreadyMatched = true
                break
            }
        }
        if (alreadyMatched) continue

        // Wrap the match
        const ok = wrapRangeInSpan(nodes, offsets, idx, endIdx, cls, container)
        if (ok) {
            // Mark nodes as matched
            for (let i = 0; i < nodes.length; i++) {
                if (offsets[i] >= endIdx) break
                if (offsets[i] + nodes[i].textContent.length > idx) {
                    matchedNodes.add(nodes[i])
                }
            }
        }
    }
}

/** Track the current flash apply so we can cancel if needed */
let flashApplyId = 0

watch([flashTextSnippets, flashType], async () => {
    // Only apply to rendered mode
    if (props.viewMode !== 'rendered') return

    const applyId = ++flashApplyId

    // Wait for DOM to be ready
    await nextTick()
    await nextTick()
    if (applyId !== flashApplyId) return

    const el = bodyRef.value
    if (!el) return

    // Remove any previous flash highlights
    removeFlashSpans(el)

    // Apply new ones
    const snippets = flashTextSnippets.value
    const type = flashType.value
    if (snippets.length > 0) {
        applyFlashToRenderedDOM(el, snippets, type)
    }
})

// Clean up flash spans when viewMode switches away from rendered
watch(() => props.viewMode, (mode) => {
    if (mode !== 'rendered') {
        const el = bodyRef.value
        if (el) removeFlashSpans(el)
    }
})

// Clean up on unmount
onBeforeUnmount(() => {
    const el = bodyRef.value
    if (el) removeFlashSpans(el)
})

</script>

<style scoped>
.markdown-preview {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
}
</style>

<style>
/* ─── Markdown rendered flash animations ─── */

@keyframes md-flash-delete-anim {
    0%, 100% { background: transparent; }
    8%, 28%  { background: rgba(255, 80, 80, 0.45); }
    18%, 38% { background: transparent; }
    48%, 68% { background: rgba(255, 80, 80, 0.3); }
    58%, 78% { background: transparent; }
    88%      { background: rgba(255, 80, 80, 0.15); }
}
.md-char-flash-delete {
    animation: md-flash-delete-anim 1.2s ease-out forwards;
    border-radius: 2px;
    text-decoration: line-through;
    text-decoration-color: rgba(255, 80, 80, 0.6);
}

@keyframes md-flash-add-anim {
    0%, 100% { background: transparent; }
    8%, 28%  { background: rgba(100, 200, 255, 0.45); }
    18%, 38% { background: transparent; }
    48%, 68% { background: rgba(100, 200, 255, 0.3); }
    58%, 78% { background: transparent; }
    88%      { background: rgba(100, 200, 255, 0.15); }
}
.md-char-flash-add {
    animation: md-flash-add-anim 1.5s ease-out forwards;
    border-radius: 2px;
}
</style>
