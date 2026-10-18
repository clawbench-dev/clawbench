import { inject } from 'vue'
import { copyText } from '@/utils/clipboard.ts'
import { flashElement } from '@/utils/domFlash'
import { gt } from '@/composables/useLocale'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { isExternalLink, isAnchorLink, slugifyForHeading, stripLeadingNumbering } from '@/utils/doubleClickUtils.ts'
import type { ToastIconKey } from '@/composables/useToast'

const BLOCK_SELECTORS = 'p, h1, h2, h3, h4, h5, h6, li, pre, blockquote, table, .mermaid'

/**
 * Try to decode a percent-encoded href.
 * Browsers may encode non-ASCII chars (e.g. 中文 → %E4%B8%AD%E6%96%87) in href
 * attributes when HTML is inserted via innerHTML/v-html. This ensures file paths
 * with Chinese characters are decoded back to their original form before being
 * used as filesystem paths.
 */
function tryDecodeHref(href: string): string {
    try {
        // Only decode if the href contains percent-encoded sequences
        if (!href.includes('%')) return href
        return decodeURIComponent(href)
    } catch {
        // If decoding fails (e.g. malformed percent encoding), return as-is
        return href
    }
}

interface ToastShow {
    show: (msg: string, opts?: { icon?: ToastIconKey; duration?: number }) => void
}

export interface LinkHandler {
    (path: string, lineStart?: number, lineEnd?: number, lineRanges?: string): void
}

export interface DoubleClickCopyOptions {
    /** 行级选择器（如 '.code-line'），设置后双击查找行级元素而非块级元素 */
    lineSelector?: string
    /** 复制成功后的回调，接收 (target元素, 复制的文本) */
    onCopy?: (target: EventTarget | null, text: string) => void
}

/**
 * 双击复制块级或行级元素的文本
 * 使用 click 事件手动判断双击，确保两次点击的是同一个元素
 */
export function useDoubleClickCopy(options?: DoubleClickCopyOptions) {
    const toast = inject<ToastShow | null>('toast', null)
    const { isTouchPrimary, isAndroidApp } = usePlatformDetect()
    let lastTarget: EventTarget | null = null
    let lastTime = 0
    const DBLCLICK_THRESHOLD = 300 // ms，与浏览器默认双击间隔一致

    /**
     * Resolve the element and text a double-tap should act on, or null.
     *
     * Split out from {@link doCopy} because the two delivery paths need the same
     * resolution but do different things with it: the in-page path copies it
     * (plus flash/toast/onCopy), the Android path hands it to the native
     * selection menu.
     */
    function resolveTarget(target: EventTarget | null): { element: HTMLElement; text: string; isLineMode: boolean } | null {
        // Try line selector first (code view), then fall back to block selectors (markdown view)
        let element: HTMLElement | null = null
        let isLineMode = false
        if (options?.lineSelector) {
            element = (target as HTMLElement | null)?.closest<HTMLElement>(options.lineSelector) ?? null
            isLineMode = true
        }
        if (!element) {
            element = (target as HTMLElement | null)?.closest<HTMLElement>(BLOCK_SELECTORS) ?? null
            isLineMode = false
        }
        if (!element) return null

        // 行级模式：只取 .code-text 的文本（不含行号）
        let text: string
        if (isLineMode) {
            const codeText = element.querySelector('.code-text')
            text = (codeText?.textContent ?? element.textContent)?.trim() || ''
        } else {
            text = element.textContent?.trim() || ''
        }
        if (!text) return null
        return { element, text, isLineMode }
    }

    /**
     * Put the resolved element's text into the document selection and return the
     * selection's bounding rect (CSS px), or null when it cannot be measured.
     *
     * Android's native menu reads the selection through the SAME snapshot the
     * long-press path uses (`useQuoteQuestion.evaluateSelection`), so the
     * selection has to be real DOM state — the native side only knows the text
     * it was handed for Copy.
     *
     * Line mode targets `.code-text` so a code line's gutter number is not
     * included (the in-page path strips it the same way).
     *
     * The rect is what anchors the floating menu. It is measured from the RANGE
     * (not the element) so it tracks the selected text; under the app's CSS
     * `zoom` on <html> a range rect is already in visual CSS px, the same space
     * as `MouseEvent.clientX/clientY`.
     */
    function selectResolved(element: HTMLElement, isLineMode: boolean): DOMRect | null {
        const node = isLineMode
            ? (element.querySelector('.code-text') as HTMLElement | null) ?? element
            : element
        const sel = window.getSelection()
        if (!sel) return null
        const range = document.createRange()
        range.selectNodeContents(node)
        sel.removeAllRanges()
        sel.addRange(range)
        try {
            return range.getBoundingClientRect()
        } catch {
            return null
        }
    }

    /**
     * 执行复制操作
     */
    function doCopy(target: EventTarget | null): boolean {
        const resolved = resolveTarget(target)
        if (!resolved) return false
        const { element, text } = resolved

        copyText(text, () => {
            // 触发闪烁动画
            flashElement(element, { className: 'copy-flash' })

            // 显示 toast 提示
            if (toast) {
                toast.show(gt('common.copied'), { icon: 'copy', duration: 1500 })
            }
        })

        // 复制成功后调用回调
        if (options?.onCopy) {
            options.onCopy(target, text)
        }

        return true
    }

    /**
     * Android delivery: select the resolved text and open the NATIVE selection
     * menu (Copy / 引用 / 讨论) instead of copying outright.
     *
     * This is what makes the double-tap behave exactly like a long-press
     * selection — one menu, one set of actions. The in-page copy + toast and the
     * in-page floating bar are deliberately NOT used here (issue: "移动端双击
     * 复制功能也改成弹出原生上下文菜单"). Quote/Discuss come back through
     * `window.__clawbenchSelectionAction`, which reads the selection we just
     * made (see useQuoteQuestion.handleNativeSelectionAction).
     *
     * Coordinates are sent as FRACTIONS of the viewport, not pixels. The web
     * layer knows the size in CSS px and the native layer knows it in physical
     * px, and the ratio between the two (devicePixelRatio × the app's CSS `zoom`
     * on <html>) is exactly what neither side can determine alone. Passing raw
     * CSS px made the menu land far above the text on a real phone: y=700 CSS px
     * was read as 700 physical px, i.e. ~29% down the screen instead of ~76%.
     * A fraction is immune to that, and to the UI-scale setting.
     *
     * Returns false when the bridge is missing (e.g. an older APK), so the
     * caller can fall back to the in-page copy rather than doing nothing.
     */
    function openNativeSelectionMenu(target: EventTarget | null, event: MouseEvent): boolean {
        const resolved = resolveTarget(target)
        if (!resolved) return false

        const native = (window as unknown as {
            ClawBenchNative?: {
                showSelectionMenu?: (
                    text: string,
                    left: number, top: number, right: number, bottom: number,
                ) => void
            }
        }).ClawBenchNative
        if (!native?.showSelectionMenu) return false

        const rect = selectResolved(resolved.element, resolved.isLineMode)
        const vw = window.innerWidth || 1
        const vh = window.innerHeight || 1
        // Fall back to the tap point when the range could not be measured.
        const left = rect ? rect.left : event.clientX
        const top = rect ? rect.top : event.clientY
        const right = rect ? rect.right : event.clientX
        const bottom = rect ? rect.bottom : event.clientY

        const frac = (v: number, span: number) => Math.min(Math.max(v / span, 0), 1)
        native.showSelectionMenu(
            resolved.text,
            frac(left, vw), frac(top, vh), frac(right, vw), frac(bottom, vh),
        )
        return true
    }

    /**
     * 处理锚点链接点击
     */
    function handleAnchorClick(event: MouseEvent, onOpenFile?: LinkHandler): boolean {
        const target = event.target as HTMLElement
        const anchor = target.closest<HTMLAnchorElement>('a[href]')

        if (!anchor) return false

        const href = anchor.getAttribute('href')
        if (!href) return false

        // 处理锚点链接 (#xxx)
        if (isAnchorLink(href)) {
            return handleHashLink(event, href, anchor)
        }

        // Decode percent-encoded href (e.g. %E4%B8%AD%E6%96%87 → 中文)
        // Browsers may encode non-ASCII chars in href attributes when inserting via innerHTML/v-html
        const decodedHref = tryDecodeHref(href)

        // 处理相对路径链接 (非 http/https 链接)
        if (!isExternalLink(decodedHref) && onOpenFile) {
            // Prefer the annotated, resolved path (set by annotateFilePaths) and
            // its line range; fall back to the raw decoded href (parsed later).
            const dataFilePath = anchor.getAttribute('data-file-path')
            const dataLineStart = anchor.getAttribute('data-line-start')
            const dataLineEnd = anchor.getAttribute('data-line-end')
            const dataLineRanges = anchor.getAttribute('data-line-ranges') || undefined
            const filePath = dataFilePath || decodedHref
            const lineStart = dataLineStart ? parseInt(dataLineStart, 10) : undefined
            const lineEnd = dataLineEnd ? parseInt(dataLineEnd, 10) : undefined
            event.preventDefault()
            if (dataLineRanges) {
                onOpenFile(filePath, lineStart, lineEnd, dataLineRanges)
            } else if (lineStart !== undefined) {
                onOpenFile(filePath, lineStart, lineEnd)
            } else {
                onOpenFile(filePath)
            }
            return true
        }

        return false
    }

    /**
     * 处理锚点链接 (#xxx)
     */
    function handleHashLink(event: MouseEvent, href: string, anchor: HTMLAnchorElement): boolean {
        // 解码 URL 编码的 href
        const targetId = decodeURIComponent(href.substring(1))
        const linkText = anchor.textContent?.trim() || ''
        
        // 先尝试直接查找 ID
        let targetElement = document.querySelector(`[id="${CSS.escape(targetId)}"]`)
        
        // 如果找不到,尝试用 slugify 转换后查找
        if (!targetElement) {
            const slugifiedId = slugifyForHeading(targetId)
            targetElement = document.querySelector(`[id="${CSS.escape(slugifiedId)}"]`)
        }
        
        // 如果还是找不到,尝试通过链接文本匹配标题
        if (!targetElement && linkText) {
            const allHeadings = document.querySelectorAll('.markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4, .markdown-body h5, .markdown-body h6')
            for (const heading of allHeadings) {
                const headingText = heading.textContent?.trim() || ''
                // 精确匹配
                if (headingText === linkText) {
                    targetElement = heading
                    break
                }
            }
            
            // 如果精确匹配失败,尝试去除序号后的匹配
            if (!targetElement) {
                // 去除开头的数字和标点,如 "5. 第四部分" -> "第四部分"
                const cleanLinkText = stripLeadingNumbering(linkText)
                for (const heading of allHeadings) {
                    const headingText = heading.textContent?.trim() || ''
                    if (headingText === cleanLinkText || headingText.includes(cleanLinkText)) {
                        targetElement = heading
                        break
                    }
                }
            }
        }
        
        if (targetElement) {
            // 阻止默认行为
            event.preventDefault()
            
            // 找到滚动容器
            const scrollContainer = anchor.closest('.file-viewer-content') || 
                                   anchor.closest('.markdown-body')?.parentElement ||
                                   document.documentElement
            
            // 计算目标位置
            const containerRect = scrollContainer.getBoundingClientRect()
            const targetRect = targetElement.getBoundingClientRect()
            const scrollTop = (scrollContainer as HTMLElement).scrollTop || 0
            const targetTop = scrollTop + targetRect.top - containerRect.top - 20 // 20px offset
            
            // 瞬间滚动
            scrollContainer.scrollTo({
                top: targetTop
            })
            
            return true
        }

        return false
    }

    /**
     * 处理原生 click 事件，手动判断双击
     * 只有在短时间内点击同一个元素时才触发双击复制
     *
     * PC 模式下禁用"双击复制段落"（markdown 预览的段落/代码行复制），
     * 但仍保留文件路径/锚点链接的处理。
     *
     * On Android the double-tap opens the NATIVE selection menu instead of
     * copying (see openNativeSelectionMenu); everywhere else it copies in-page.
     */
    function handleDblClick(event: MouseEvent, onOpenFile?: LinkHandler): void {
        // 首先检查是否点击了链接
        if (handleAnchorClick(event, onOpenFile)) {
            return
        }

        // 鼠标/键盘环境禁用双击复制段落（触摸面才用双击）
        if (!isTouchPrimary.value) {
            return
        }

        const now = Date.now()
        const target = event.target
        const timeDiff = now - lastTime

        // 判断是否是双击：短时间内点击同一个元素
        if (lastTarget === target && timeDiff < DBLCLICK_THRESHOLD) {
            // 清除状态，防止连续触发
            lastTarget = null
            lastTime = 0

            // Android: hand off to the native selection menu so a double-tap and
            // a long-press produce the SAME menu (Copy / 引用 / 讨论). Falls back
            // to the in-page copy when the bridge is absent (older APK).
            if (isAndroidApp.value && openNativeSelectionMenu(target, event)) {
                return
            }

            // 执行复制
            doCopy(target)
        } else {
            // 记录这次点击
            lastTarget = target
            lastTime = now
        }
    }

    return {
        handleDblClick,
    }
}
