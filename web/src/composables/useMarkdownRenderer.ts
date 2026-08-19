import { marked, katex, DOMPurify } from '@/utils/globals.ts'
import { escapeHtml } from '@/utils/html.ts'
import { injectTableRowAttrs } from '@/utils/tableRowExpand.ts'
import { annotateCodeBlockHeaders, annotateTableBlockHeaders } from '@/composables/useCodeBlockHeader.ts'
import { rewriteImageUrls, convertAudioLinks, convertVideoLinks, getThumbWidth } from '@/utils/chatRenderUtils.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect.ts'
import { annotateFilePaths } from '@/composables/useFilePathAnnotation.ts'
import { annotateCommitHashes } from '@/composables/useCommitHashAnnotation.ts'
import { annotateWorktreePaths } from '@/composables/useWorktreeAnnotation.ts'
import { annotateLocalhostUrls } from '@/composables/useLocalhostAnnotation.ts'
import { store } from '@/stores/app.ts'
import { resetHeadingIds } from '@/utils/markedConfig.ts'

/**
 * Markdown渲染选项
 */
export interface MarkdownRenderOptions {
    /** 是否净化HTML（防XSS），默认true */
    sanitize?: boolean
    /** 是否包装表格（添加滚动容器），默认true */
    wrapTables?: boolean
    /** 跳过增强步骤（路径注解、媒体转换等），流式模式用。不影响KaTeX渲染 */
    skipEnhancements?: boolean
    /** 跳过KaTeX渲染，流式模式用（公式可能不完整）。默认false */
    skipKatex?: boolean
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

// ---------------------------------------------------------------------------
// Math block extraction: protect LaTeX from marked's emphasis parsing
// ---------------------------------------------------------------------------

/**
 * Math block extraction: protect LaTeX formulas from marked's emphasis parsing.
 *
 * marked.parse treats _ and * as emphasis delimiters. LaTeX subscripts (e.g. _{i})
 * get misinterpreted, especially when multiple _ appear in one formula block
 * (e.g. a^{0}_{i} + b^{0}_{j} → a^{0}<em>{i} + b^{0}</em>{j}).
 *
 * Solution: extract all math blocks before marked.parse, replace with NUL-delimited
 * placeholders that encode display/inline mode, then restore+render in renderKatexInString.
 *
 * Placeholder format:
 *   Display: \x00MATHD<n>\x00
 *   Inline:  \x00MATHI<n>\x00
 * (NUL bytes cannot appear in valid HTML/text content)
 *
 * Code span protection: markdown code spans (backtick-wrapped) and fenced code blocks
 * are extracted FIRST, before math, so that math-like content inside code
 * (e.g. `$a_{i}$` inside a backtick span) is never incorrectly extracted.
 */

/** Pre-extracted math entry with display mode info */
export interface MathEntry {
    math: string
    displayMode: boolean
}

// eslint-disable-next-line no-control-regex -- NUL bytes are intentional placeholder delimiters
const MATH_PH_RE = /\x00MATH([DI])(\d+)\x00/g

/**
 * Extract markdown code spans/blocks before math, so $...$ inside code
 * is not mistakenly treated as math delimiters.
 *
 * Matches:
 * - Fenced code blocks: ```...``` or ~~~...~~~ (with optional info string)
 * - Inline code spans: `...` (including backtick-escaped spans like ``...``)
 *
 * Placeholders use \x01 (SOH) — distinct from math placeholders (\x00)
 * so they don't interfere.
 */
// eslint-disable-next-line no-control-regex -- SOH bytes are intentional placeholder delimiters
const CODE_SPAN_PH_RE = /\x01CODE(\d+)\x01/g

function extractCodeAndMath(markdown: string): {
    protected: string
    mathEntries: MathEntry[]
} {
    // Phase 1: Protect code spans/blocks
    const codeBlocks: string[] = []
    let codeIdx = 0

    // 1a. Fenced code blocks: ```...``` or ~~~...~~~ (with optional info string)
    //     Must match before inline code spans to avoid partial matches.
    let result = markdown.replace(/(?:^|\n)(~~~+|```+)[^\n]*\n[\s\S]*?\n\1[ \t]*(?=\n|$)/g, (match) => {
        const ph = `\x01CODE${codeIdx++}\x01`
        codeBlocks.push(match)
        return ph
    })

    // 1b. Inline code spans: one or more backticks, content between matching runs.
    //     [^`] forbids backticks in content (standard markdown rule).
    result = result.replace(/(`+)([^`]+?)\1/g, (match) => {
        const ph = `\x01CODE${codeIdx++}\x01`
        codeBlocks.push(match)
        return ph
    })

    // Phase 2: Extract math blocks (same logic as before)
    const mathEntries: MathEntry[] = []
    let idx = 0

    const ph = (displayMode: boolean) => {
        const prefix = displayMode ? 'MATHD' : 'MATHI'
        return `\x00${prefix}${idx++}\x00`
    }

    // 2a. Display math: $$...$$
    result = result.replace(/\$\$([\s\S]+?)\$\$/g, (_, math) => {
        mathEntries.push({ math: math.trim(), displayMode: true })
        return ph(true)
    })

    // 2b. Display math: \[...\]
    result = result.replace(/\\\[([\s\S]+?)\\\]/g, (_, math) => {
        mathEntries.push({ math: math.trim(), displayMode: true })
        return ph(true)
    })

    // 2c. Inline math: $...$ (same exclusion rules as INLINE_MATH_RE)
    result = result.replace(/(^|[^$\d\\])\$(?!\$)([^$\n]+?)\$(?!\d)/g, (_whole, pre, math) => {
        mathEntries.push({ math: math.trim(), displayMode: false })
        return pre + ph(false)
    })

    // 2d. Inline math: \(...\)
    result = result.replace(/\\\(([^\\\n]+?)\\\)/g, (_, math) => {
        mathEntries.push({ math: math.trim(), displayMode: false })
        return ph(false)
    })

    // Phase 3: Restore code spans/blocks — they pass through marked.parse intact
    // (the placeholders are plain text that marked won't transform)
    result = result.replace(CODE_SPAN_PH_RE, (_, ci) => codeBlocks[parseInt(ci, 10)])

    return { protected: result, mathEntries }
}

// ---------------------------------------------------------------------------
// INLINE_MATH_RE — kept for backward compatibility (standalone renderKatexInString)
// ---------------------------------------------------------------------------

/**
 * Inline math $...$ 匹配正则。
 *
 * 不使用 lookbehind（Safari/iPadOS < 16.4 不支持，导致 bundle 解析失败白屏），
 * 用捕获组 `(^|[^$\d\\])` 保留前置字符并在回调中回填，语义与 `(?<![\$\d\\])` 等价。
 *
 * 前置排除：
 * - $: 避免匹配 $$$（连续美元符号）
 * - \d: 避免匹配价格如 "花费$5"（数字后的 $ 是货币符号）
 * - \\: 避免匹配转义 \$（转义美元应保持字面意思）
 *
 * 后置排除：`(?!\d)` 避免匹配如 "$5"（$ 后紧跟数字是价格）
 */
export const INLINE_MATH_RE = /(^|[^$\d\\])\$(?!\$)([^$\n]+?)\$(?!\d)/g

// ---------------------------------------------------------------------------
// renderKatexInString
// ---------------------------------------------------------------------------

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
 *
 * 渲染模式：
 * - 占位符模式（mathEntries 非空）：由 extractCodeAndMath 预提取，
 *   直接从 mathEntries 数组还原并渲染，不重新匹配定界符。
 * - 回退模式（mathEntries 为空/undefined）：兼容旧调用方，
 *   从 HTML 中匹配 $...$ / $$...$$ / \[...\] / \(...\) 定界符并渲染。
 *
 * 保护措施：
 * - <code>...</code> 内容不受 KaTeX 匹配影响（先提取占位，渲染后还原）
 * - \$ 转义序列保持字面意思，不被当成公式定界符
 */
export function renderKatexInString(html: string, mathEntries?: MathEntry[]): string {
    if (!html) return html

    // 0. Protect <code> blocks from KaTeX matching (inline code and code blocks)
    const CODE_PH_L = '\u200B\u200C'
    const CODE_PH_R = '\u200C\u200B'
    const codeBlocks: string[] = []
    let codeIdx = 0
    html = html.replace(/<code[\s>][^]*?<\/code>/gi, (match) => {
        const placeholder = `${CODE_PH_L}CODE${codeIdx}${CODE_PH_R}`
        codeBlocks.push(match)
        codeIdx++
        return placeholder
    })

    // 0b. Handle escaped \$ — replace with placeholder to prevent KaTeX matching
    const ESC_DOLLAR_PH = '\u200D\u200D'
    html = html.replace(/\\\$/g, ESC_DOLLAR_PH)

    if (mathEntries && mathEntries.length > 0) {
        // --- Placeholder path: math blocks were pre-extracted before marked.parse ---
        // mode ('D' or 'I') is intentionally captured but not used here —
        // displayMode info comes from mathEntries[idx].displayMode.
        html = html.replace(MATH_PH_RE, (_, _mode, idxStr) => {
            const idx = parseInt(idxStr, 10)
            const entry = mathEntries[idx]
            if (!entry) return _
            try {
                return katex.renderToString(entry.math, { displayMode: entry.displayMode, throwOnError: false })
            } catch {
                return escapeHtml(entry.math)
            }
        })
    } else {
        // --- Legacy path: no pre-extraction, match delimiters in HTML ---

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
        html = html.replace(INLINE_MATH_RE, (whole, pre, math) => {
            try {
                return pre + katex.renderToString(math.trim(), { displayMode: false, throwOnError: false })
            } catch {
                return pre + escapeHtml(whole.slice(pre.length))
            }
        })
        html = html.replace(/\\\(([^\\\n]+?)\\\)/g, (_, math) => {
            try {
                return katex.renderToString(math.trim(), { displayMode: false, throwOnError: false })
            } catch {
                return escapeHtml(_)
            }
        })
    }

    // Restore escaped \$ — replace placeholder back to literal $
    html = html.replace(/\u200D\u200D/g, '$')

    // Restore <code> blocks (use function form to avoid $$ special replacement patterns)
    for (let i = 0; i < codeBlocks.length; i++) {
        html = html.replace(`${CODE_PH_L}CODE${i}${CODE_PH_R}`, () => codeBlocks[i])
    }

    return html
}

/**
 * Strip NUL-delimited math placeholders from HTML, replacing them with
 * escaped raw math text. Used when skipKatex=true (streaming mode)
 * to avoid leaking NUL bytes or garbage text into the rendered output.
 */
function stripMathPlaceholders(html: string, mathEntries: MathEntry[]): string {
    return html.replace(MATH_PH_RE, (_, _mode, idxStr) => {
        const idx = parseInt(idxStr, 10)
        const entry = mathEntries[idx]
        if (!entry) return ''
        const delimiter = entry.displayMode ? '$$' : '$'
        return escapeHtml(delimiter + entry.math + delimiter)
    })
}

// ---------------------------------------------------------------------------
// DOMPurify config
// ---------------------------------------------------------------------------

const DOMPURIFY_ADD_TAGS = ['math', 'button']
const DOMPURIFY_ADD_ATTR = ['data-action', 'aria-label', 'title', 'data-file-path', 'data-fallback-path', 'data-line-start', 'data-line-end', 'data-commit-sha', 'data-worktree-path', 'data-url', 'data-port', 'data-protocol', 'data-path', 'data-table-idx', 'data-row-idx']
const DOMPURIFY_ALLOWED_URI_REGEXP = /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|sms|cid|xmpp|file):|[^a-z]|[a-z+.-]+(?:[/?#][\s\S]*)?$)/i

// ---------------------------------------------------------------------------
// renderMarkdown
// ---------------------------------------------------------------------------

/**
 * 渲染Markdown内容为HTML（统一管线，所有调用方共用）
 *
 * 管线：extractCodeAndMath → marked.parse → [renderKatexInString | stripMathPlaceholders]
 *       → DOMPurify → fixImagePaths → table-wrap → injectTableRowAttrs
 *       → annotateCodeBlockHeaders → annotateTableBlockHeaders
 *       → [rewriteImageUrls → convertAudioLinks → convertVideoLinks → annotateWorktreePaths
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
        skipKatex,
        fixImagePaths,
    } = options

    let detectedPaths: string[] = []
    let detectedSHAs: string[] = []

    const trimmed = (content || '').trim()

    // 0. Extract code spans/blocks and math blocks BEFORE marked.parse
    //    to protect _ and * from emphasis parsing (issue #384)
    const { protected: protectedMarkdown, mathEntries } = extractCodeAndMath(trimmed)

    // 1. Parse markdown (reset heading ID counter for deduplication)
    resetHeadingIds()
    let html = marked.parse(protectedMarkdown) as string

    // 2. KaTeX rendering — restore math placeholders and render
    //    skipKatex=true: strip placeholders to escaped raw math (streaming, formulas incomplete)
    //    skipKatex omitted or false: render KaTeX (even with skipEnhancements)
    if (!skipKatex) {
        html = renderKatexInString(html, mathEntries)
    } else if (mathEntries.length > 0) {
        html = stripMathPlaceholders(html, mathEntries)
    }

    // 3. Sanitize HTML (XSS prevention)
    if (sanitize) {
        html = DOMPurify.sanitize(html, { ADD_TAGS: DOMPURIFY_ADD_TAGS, ADD_ATTR: DOMPURIFY_ADD_ATTR, ALLOWED_URI_REGEXP: DOMPURIFY_ALLOWED_URI_REGEXP })
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

    // 6. Inject table row attrs
    html = injectTableRowAttrs(html)

    // 7. Code block headers (language label + copy/wrap buttons)
    html = annotateCodeBlockHeaders(html)

    // 8. Table block headers (label + copy/wrap buttons)
    html = annotateTableBlockHeaders(html)

    // 9. Chat enhancements (all skipped during streaming)
    if (!skipEnhancements) {
        const projectRoot = store.state.projectRoot
        const homeDir = store.state.homeDir
        const { isPC } = usePlatformDetect()

        html = rewriteImageUrls(html, projectRoot, getThumbWidth(isPC.value))
        html = convertAudioLinks(html, projectRoot)
        html = convertVideoLinks(html, projectRoot)

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

// Re-export for backward compatibility — dynamic import to avoid
// pulling mermaid into the initial chunk. Includes DOM existence check
// to skip the import entirely when no mermaid blocks are present.
export function renderMermaidInElement(
    el: HTMLElement,
    prefix: string = 'mermaid',
    specificBlocks?: NodeList
): Promise<void> {
    // Skip dynamic import if no mermaid blocks exist (avoids loading 608KB chunk)
    if (!specificBlocks && el.querySelectorAll('pre.mermaid:not([data-rendered])').length === 0) {
        return Promise.resolve()
    }
    return import('@/utils/mermaid.ts').then(m => m.renderMermaidInElement(el, prefix, specificBlocks))
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
