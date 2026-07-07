import { marked, katex, mermaid, DOMPurify } from '@/utils/globals.ts'
import { escapeHtml } from '@/utils/html.ts'
import { injectTableRowAttrs } from '@/utils/tableRowExpand.ts'

/**
 * Markdown渲染选项
 */
export interface MarkdownRenderOptions {
    /** 是否净化HTML（防XSS），默认true */
    sanitize?: boolean
    /** 是否渲染KaTeX数学公式，默认true */
    renderKatex?: boolean
    /** 是否渲染Mermaid图表，默认true */
    renderMermaid?: boolean
    /** 是否包装表格（添加滚动容器），默认true */
    wrapTables?: boolean
    /** 图片路径修复函数，可选 */
    fixImagePaths?: (html: string) => string
    /** 自定义HTML后处理函数，可选 */
    postProcess?: (html: string) => string
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
 *
 * 渲染顺序：marked.parse() → renderKatexInString() → DOMPurify.sanitize()
 * @param html 包含公式分隔符的HTML字符串
 * @returns 公式已渲染为KaTeX HTML的字符串
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

/**
 * 渲染Markdown内容为HTML
 * @param content Markdown内容
 * @param options 渲染选项
 * @returns 渲染后的HTML字符串
 */
export function renderMarkdown(
    content: string,
    options: MarkdownRenderOptions = {}
): string {
    const {
        sanitize = true,
        wrapTables = true,
        fixImagePaths,
        postProcess
    } = options

    // 1. 解析Markdown
    let html = marked.parse((content || '').trim()) as string

    // 2. 渲染KaTeX数学公式（字符串级别，不能改用DOM级渲染，见 renderKatexInString 注释）
    html = renderKatexInString(html) as string

    // 3. 净化HTML（防止XSS攻击）
    // 注意：KaTeX渲染后的HTML需要 ADD_TAGS:['math'] 保留 <math> 标签
    if (sanitize) {
        html = DOMPurify.sanitize(html, { ADD_TAGS: ['math'] })
    }

    // 4. 修复图片路径
    if (fixImagePaths) {
        html = fixImagePaths(html)
    }

    // 5. 包装表格
    if (wrapTables) {
        html = html.replace(/<table>/g, '<div class="table-wrap"><table>')
                   .replace(/<\/table>/g, '</table></div>')
    }

    // 6. 注入表格行属性标识
    html = injectTableRowAttrs(html)

    // 7. 自定义后处理
    if (postProcess) {
        html = postProcess(html)
    }

    return html
}

/**
 * 在DOM元素中渲染Mermaid图表
 * @param el DOM元素
 * @param prefix 图表ID前缀，默认 'mermaid'
 * @param specificBlocks 可选：只渲染指定的块（NodeList）
 */
export async function renderMermaidInElement(
    el: HTMLElement,
    prefix: string = 'mermaid',
    specificBlocks?: NodeList
): Promise<void> {
    // marked配置会将 ```mermaid 渲染为 <pre class="mermaid">
    // 而不是 <pre><code class="language-mermaid">
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
        } catch (err: any) {
            container.innerHTML = `<pre style="padding:12px;background:var(--code-bg);border-radius:6px;font-size:13px;overflow-x:auto;">Mermaid Error: ${escapeHtml(err.message)}</pre>`
            ;(block as Element).replaceWith(container)
        }
    })

    await Promise.all(renderPromises)
}

/**
 * 组合式函数：Markdown渲染器
 * 提供统一的Markdown渲染功能，可被多个组件复用
 */
export function useMarkdownRenderer() {
    return {
        renderMarkdown,
        renderMermaidInElement,
    }
}
