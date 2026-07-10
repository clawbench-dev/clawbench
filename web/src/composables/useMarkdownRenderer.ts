import { marked, katex, mermaid, DOMPurify } from '@/utils/globals.ts'
import { escapeHtml } from '@/utils/html.ts'
import { injectTableRowAttrs } from '@/utils/tableRowExpand.ts'
import { annotateCodeBlockHeaders, annotateTableBlockHeaders } from '@/composables/useCodeBlockHeader.ts'
import { rewriteImageUrls, convertAudioLinks } from '@/utils/chatRenderUtils.ts'
import { annotateFilePaths } from '@/composables/useFilePathAnnotation.ts'
import { annotateCommitHashes } from '@/composables/useCommitHashAnnotation.ts'
import { annotateWorktreePaths } from '@/composables/useWorktreeAnnotation.ts'
import { annotateLocalhostUrls } from '@/composables/useLocalhostAnnotation.ts'
import { store } from '@/stores/app.ts'

/**
 * Markdown渲染选项
 */
export interface MarkdownRenderOptions {
    /** 是否净化HTML（防XSS），默认true */
    sanitize?: boolean
    /** 是否包装表格（添加滚动容器），默认true */
    wrapTables?: boolean
    /** 跳过增强步骤（KaTeX、图片/音频/路径注解），流式模式用 */
    skipEnhancements?: boolean
    /** 图片路径修复函数，MarkdownPreview 用 */
    fixImagePaths?: (html: string) => string
}

/** renderMarkdown 返回的检测结果，供调用方做异步 verify */
export interface RenderResult {
    /** 渲染后的 HTML */
    html: string
    /** 检测到的文件路径列表（需 nextTick verifyFilePaths） */
    detectedPaths: string[]
    /** 检测到的 commit SHA 列表（需 nextTick verifyCommitHashes） */
    detectedSHAs: string[]
}

/**
 * 在HTML字符串中渲染KaTeX数学公式（字符串级别，不操作DOM）
 *
 * 【重要】必须使用 katex.renderToString() 在字符串阶段渲染，
 * 不能使用 renderMathInElement() 在DOM阶段渲染。原因：
 * KaTeX 的 renderMathInElement() 会拆分DOM文本节点（把一个文本节点
 * 拆成多个子节点来插入 <span class="katex">），这与 Vue 的 v-html
 * 更新机制冲突——v-html 每次 innerHTML 整体替换，而 KaTeX 在
 * nextTick 中的 DOM 突变可能与 Vue 的 patch 周期交叉执行，导致
 * 虚拟DOM与实际DOM失去同步，引发响应式更新异常（如按钮不显示）。
 *
 * 相比之下，Mermaid 可以用 DOM 级渲染，因为它是整个节点替换
 * （<pre> → <div>+SVG），Vue 下次 innerHTML 覆盖后 Mermaid
 * 重新渲染即可，是幂等的，不会产生冲突。
 */
export function renderKatexInString(html: string): string {
    if (!html) return html

    // Display math: $$...$$  和  \[...\]
    html = html.replace(/\$\$([\s\S]+?)\$\$/g, (_, math) => {
        try {
            return katex.renderToString(math.trim(), { displayMode: true, throwOnError: false })
        } catch {
            return escapeHtml(_)
        }
    })
    html = html.replace(/\\\[([\s\S]+?)\\\]/g, (_, math) => {
        try {
            return katex.renderToString(math.trim(), { displayMode: true, throwOnError: false })
        } catch {
            return escapeHtml(_)
        }
    })

    // Inline math: $...$  和  \(...\)
    // 注意：$ 必须匹配非空内容，且左右不能是数字或字母（避免误匹配价格等）
    html = html.replace(/(?<!\$)\$(?!\$)([^$\n]+?)\$(?!\$)/g, (_, math) => {
        try {
            return katex.renderToString(math.trim(), { displayMode: false, throwOnError: false })
        } catch {
            return escapeHtml(_)
        }
    })
    html = html.replace(/\\\(([\s\S]+?)\\\)/g, (_, math) => {
        try {
            return katex.renderToString(math.trim(), { displayMode: false, throwOnError: false })
        } catch {
            return escapeHtml(_)
        }
    })

    return html
}

// DOMPurify 配置：取所有调用方的并集
const DOMPURIFY_ADD_TAGS = ['math', 'button', 'rag-results', 'rag-item', 'session-id', 'session-title', 'created-at', 'summary']
const DOMPURIFY_ADD_ATTR = ['data-action', 'aria-label', 'title', 'data-file-path', 'data-fallback-path', 'data-line-start', 'data-line-end', 'data-commit-sha', 'data-worktree-path', 'data-url', 'data-port', 'data-protocol', 'data-table-idx', 'data-row-idx']

/**
 * 渲染Markdown内容为HTML（统一管线，所有调用方共用）
 *
 * 管线：marked.parse → [KaTeX] → DOMPurify → fixImagePaths → table-wrap
 *       → injectTableRowAttrs → annotateCodeBlockHeaders → annotateTableBlockHeaders
 *       → [rewriteImageUrls → convertAudioLinks → annotateWorktreePaths
 *          → annotateFilePaths → annotateCommitHashes → annotateLocalhostUrls]
 *
 * 方括号内的步骤在 skipEnhancements=true 时跳过（流式模式用）。
 *
 * @param content Markdown内容
 * @param options 渲染选项
 * @returns 渲染结果（html + detectedPaths/detectedSHAs 供调用方 verify）
 */
export function renderMarkdown(
    content: string,
    options: MarkdownRenderOptions = {}
): RenderResult {
    const {
        sanitize = true,
        wrapTables = true,
        skipEnhancements = false,
        fixImagePaths,
    } = options

    let detectedPaths: string[] = []
    let detectedSHAs: string[] = []

    // 1. Parse markdown
    let html = marked.parse((content || '').trim()) as string

    // 2. KaTeX (skip during streaming — formula may be incomplete)
    if (!skipEnhancements) {
        html = renderKatexInString(html)
    }

    // 3. Sanitize HTML (XSS prevention)
    if (sanitize) {
        html = DOMPurify.sanitize(html, { ADD_TAGS: DOMPURIFY_ADD_TAGS, ADD_ATTR: DOMPURIFY_ADD_ATTR })
    }

    // 4. Fix image paths (MarkdownPreview-specific)
    if (fixImagePaths) {
        html = fixImagePaths(html)
    }

    // 5. Wrap tables
    if (wrapTables) {
        html = html.replace(/<table>/g, '<div class="table-wrap"><table>')
                   .replace(/<\/table>/g, '</table></div>')
    }

    // 6. Rewrite image URLs + inject chat-img class (always, even during streaming)
    //    so images get proper sizing constraints and local-file API paths immediately.
    const projectRoot = store.state.projectRoot
    html = rewriteImageUrls(html, projectRoot)

    // 7. Inject table row attrs
    html = injectTableRowAttrs(html)

    // 8. Code block headers (language label + copy/wrap buttons)
    html = annotateCodeBlockHeaders(html)

    // 9. Table block headers (label + copy/wrap buttons)
    html = annotateTableBlockHeaders(html)

    // 10. Chat enhancements (skipped during streaming, except rewriteImageUrls which ran above)
    if (!skipEnhancements) {
        const homeDir = store.state.homeDir

        html = convertAudioLinks(html)

        // Annotate worktree paths BEFORE file paths — prevents file-path regex from
        // partially matching worktree directory paths
        const { html: worktreeHtml } = annotateWorktreePaths(html, { projectRoot })
        html = worktreeHtml

        const { html: annotatedHtml, detectedPaths: paths } = annotateFilePaths(html, { projectRoot, homeDir })
        html = annotatedHtml
        detectedPaths = paths

        const { html: commitAnnotatedHtml, detectedSHAs: shas } = annotateCommitHashes(html)
        html = commitAnnotatedHtml
        detectedSHAs = shas

        html = annotateLocalhostUrls(html)
    }

    return { html, detectedPaths, detectedSHAs }
}

/**
 * Convenience: render markdown to HTML string only (no detections).
 * For callers that don't need path/commit verification.
 */
export function renderMarkdownHtml(content: string, options: MarkdownRenderOptions = {}): string {
    return renderMarkdown(content, options).html
}

/**
 * 在DOM元素中渲染Mermaid图表
 */
export async function renderMermaidInElement(
    el: HTMLElement,
    prefix: string = 'mermaid',
    specificBlocks?: NodeList
): Promise<void> {
    const blocks = specificBlocks || el.querySelectorAll('pre.mermaid:not([data-rendered])')
    if (blocks.length === 0) return

    const renderPromises = Array.from(blocks).map(async (block, index) => {
        (block as HTMLElement).setAttribute('data-rendered', '1')
        const id = `${prefix}-${Date.now()}-${index}`
        const source = block.textContent?.trim() || ''
        const container = document.createElement('div')
        container.className = 'mermaid'
        container.id = id

        try {
            const result = await mermaid.render(id, source)
            container.innerHTML = result.svg
            container.dataset.mermaid = source
            ;(block as Element).replaceWith(container)
        } catch (err: unknown) {
            container.innerHTML = `<pre style="padding:12px;background:var(--code-bg);border-radius:6px;font-size:13px;overflow-x:auto;">Mermaid Error: ${escapeHtml((err as { message?: string })?.message || String(err))}</pre>`
            ;(block as Element).replaceWith(container)
        }
    })

    await Promise.all(renderPromises)
}

/**
 * 组合式函数：Markdown渲染器
 */
export function useMarkdownRenderer() {
    return {
        renderMarkdown,
        renderMarkdownHtml,
        renderMermaidInElement,
    }
}
