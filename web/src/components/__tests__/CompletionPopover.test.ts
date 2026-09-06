import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import CompletionPopover from '@/components/common/CompletionPopover.vue'

// Mock the singleton composable so each test controls state directly.
// Hoisted vi.mock factories cannot reference outer variables, so the mock
// exposes mutable refs via a getter.
const mockState = {
    active: ref(null),
    queue: ref([]),
    dismiss: vi.fn(),
    dismissOnBackdrop: vi.fn(),
}

const { mockGetAgentBackend, mockToastShow } = vi.hoisted(() => ({
    mockGetAgentBackend: vi.fn(() => ''),
    mockToastShow: vi.fn(),
}))

vi.mock('@/composables/useCompletionPopover', () => ({
    useCompletionPopover: () => mockState,
}))

vi.mock('@/composables/useToast', () => ({
    useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useAgents', async (importOriginal) => {
    const actual = await importOriginal<typeof import('@/composables/useAgents')>()
    return {
        ...actual,
        useAgents: () => ({ ...actual.useAgents(), getAgentBackend: mockGetAgentBackend }),
    }
})

function makeItem(overrides = {}) {
    return {
        sessionId: 's1',
        title: '这是一个很长的会话标题用于测试溢出省略号的显示效果',
        summary: '**加粗的摘要内容** 以及普通文本',
        kind: 'session',
        projectPath: '',
        ...overrides,
    }
}

describe('CompletionPopover', () => {
    beforeEach(() => {
        document.body.innerHTML = ''
        vi.clearAllMocks()
        mockState.active = ref(null)
        mockState.queue = ref([])
        mockGetAgentBackend.mockReturnValue('')
    })
    function mountPopover() {
        return mount(CompletionPopover, { attachTo: document.body })
    }

    it('renders nothing when no item is active', () => {
        mountPopover()
        expect(document.querySelector('.completion-popover')).toBeFalsy()
    })

    it('renders the session title', () => {
        mockState.active = ref(makeItem({ title: '修复登录 bug' }))
        mountPopover()

        const el = document.querySelector('.completion-popover-title')!
        expect(el.textContent).toContain('修复登录 bug')
    })

    it('renders the last user message as a single line quote block', () => {
        mockState.active = ref(makeItem({ userMessage: '请帮我修复登录 bug' }))
        mountPopover()

        const el = document.querySelector('.completion-popover-user-quote')!
        const text = el.querySelector('.completion-popover-user-quote-text')!
        // 左侧消息图标（lucide 组件直接渲染为 svg，class 即元素本身）
        const icon = el.querySelector('.completion-popover-user-quote-icon')
        expect(icon).toBeTruthy()
        expect(icon!.tagName.toLowerCase()).toBe('svg')
        expect(text.textContent).toContain('请帮我修复登录 bug')
        // 折叠态单行省略逻辑在内层文本 span（flex 容器内 text-overflow 不生效）
        const styles = window.getComputedStyle(text)
        expect(styles.textOverflow).toBe('ellipsis')
        expect(styles.overflow).toBe('hidden')
        expect(styles.whiteSpace).toBe('nowrap')
    })

    it('shows an attachment chip alongside the user message text when the message has files', () => {
        mockState.active = ref(makeItem({ userMessage: '看图', userHasFiles: true }))
        mountPopover()

        const row = document.querySelector('.completion-popover-meta-user')!
        const quote = row.querySelector('.completion-popover-user-quote')!
        const chip = row.querySelector('.completion-popover-attachment-chip')!
        expect(quote).toBeTruthy()
        expect(chip).toBeTruthy()
        // chip 文案 + 图标（当前测试环境默认 en：Attachment）
        expect(chip.textContent).toContain('Attachment')
        expect(chip.querySelector('svg')).toBeTruthy()
    })

    it('shows the attachment chip alone for an attachment-only user message (no text)', () => {
        mockState.active = ref(makeItem({ userMessage: '', userHasFiles: true }))
        mountPopover()

        const row = document.querySelector('.completion-popover-meta-user')!
        const chip = row.querySelector('.completion-popover-attachment-chip')!
        expect(chip).toBeTruthy()
        // 无文字时不渲染引用块，只保留附件 chip
        expect(row.querySelector('.completion-popover-user-quote')).toBeFalsy()
        expect(chip.textContent).toContain('Attachment')
    })

    it('hides the attachment chip when the user message has no files', () => {
        mockState.active = ref(makeItem({ userMessage: '没有附件', userHasFiles: false }))
        mountPopover()

        const row = document.querySelector('.completion-popover-meta-user')!
        expect(row.querySelector('.completion-popover-user-quote')).toBeTruthy()
        expect(row.querySelector('.completion-popover-attachment-chip')).toBeFalsy()
    })

    it('does not render the user message row when neither text nor files present', () => {
        mockState.active = ref(makeItem({ userMessage: '', userHasFiles: false }))
        mountPopover()

        expect(document.querySelector('.completion-popover-meta-user')).toBeFalsy()
    })

    it('styles the user message as a quote block (left accent border, no radius, tinted bg)', () => {
        mockState.active = ref(makeItem({ userMessage: '请帮我修复登录 bug' }))
        mountPopover()

        const row = document.querySelector('.completion-popover-meta-user')!
        const el = document.querySelector('.completion-popover-user-quote')!
        const rowStyles = window.getComputedStyle(row)
        // 行容器靠左对齐
        expect(rowStyles.justifyContent).toBe('flex-start')
        // 引用块：inline-flex 布局容纳图标 + 文本，宽度随内容自适应（max-width: 100% 仅作上限）
        const styles = window.getComputedStyle(el)
        expect(styles.display).toBe('inline-flex')
        expect(styles.maxWidth).toBe('100%')
        expect(styles.fontSize).toBe('12px')

        // 元信息行无负 margin（引用块不铺满卡片宽度）
        const cssRow = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        const metaRule = cssRow.split('\n').filter((line) => line.includes('.completion-popover-meta-user')).join('\n')
        expect(metaRule).not.toContain('margin-left: -10px')
        expect(metaRule).not.toContain('margin-right: -10px')

        // jsdom 无法解析 color-mix()/var() 与 border-radius 计算值，
        // 这些改为断言组件注入的 CSS 规则文本（其余走计算值）
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        const quoteRule = cssText.split('\n').filter((line) => line.includes('.completion-popover-user-quote')).join('\n')
        // 左侧 accent 竖线描边、无圆角、淡色底
        expect(quoteRule).toContain('border-left: 2px solid var(--accent-color)')
        expect(quoteRule).toContain('border-radius: 0')
        expect(quoteRule).toContain('background: color-mix(in srgb, var(--accent-color) 10%')
        // 折叠态不应保留胶囊气泡样式
        expect(quoteRule).not.toContain('999px')
        expect(quoteRule).not.toContain('var(--user-msg-color)')
    })

    it('expands the user message quote block on click and collapses on second click', async () => {
        mockState.active = ref(makeItem({ userMessage: '请帮我修复登录 bug' }))
        mountPopover()

        const el = document.querySelector('.completion-popover-user-quote')!
        const text = el.querySelector('.completion-popover-user-quote-text')!

        // 初始折叠：单行省略、未展开态
        expect(el.classList.contains('is-expanded')).toBe(false)
        expect(el.getAttribute('aria-expanded')).toBe('false')
        expect(window.getComputedStyle(text).whiteSpace).toBe('nowrap')

        // 无展开提示图标（用户点击即可展开，不展示额外提示）
        expect(el.querySelector('.completion-popover-user-quote-chevron')).toBeFalsy()

        // 点击展开：文本换行显示完整内容
        el.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()
        expect(el.classList.contains('is-expanded')).toBe(true)
        expect(el.getAttribute('aria-expanded')).toBe('true')
        expect(window.getComputedStyle(text).whiteSpace).toBe('pre-wrap')

        // 再次点击收起
        el.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()
        expect(el.classList.contains('is-expanded')).toBe(false)
        expect(el.getAttribute('aria-expanded')).toBe('false')
        expect(window.getComputedStyle(text).whiteSpace).toBe('nowrap')
    })

    it('removes the divider between the user message quote block and the assistant summary', () => {
        mockState.active = ref(makeItem({ userMessage: '请帮我修复登录 bug' }))
        mountPopover()

        // 用户消息行自身不应有 border（引用块自身已带左侧描边分层）
        const metaRow = document.querySelector('.completion-popover-meta-user')!
        const rowStyles = window.getComputedStyle(metaRow)
        expect(rowStyles.borderTopStyle).toBe('none')
        expect(rowStyles.borderBottomStyle).toBe('none')

        // 分隔线规则存在，但被限定为"非用户消息行"的元信息——
        // 用户消息行紧跟摘要时选择器不匹配，即用户消息与助手消息之间无分隔线
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        const dividerRule = cssText.split('\n').filter((line) => line.includes('.completion-popover-summary')).join('\n')
        expect(dividerRule).toContain('.completion-popover-meta:not(.completion-popover-meta-user) + .completion-popover-summary')
        expect(dividerRule).toContain('border-top')
    })

    it('keeps the divider between the project row and the assistant summary', () => {
        mockState.active = ref(makeItem({ projectName: 'my-app', userMessage: '' }))
        mountPopover()

        // jsdom 不解析 color-mix()，border 计算值不可靠；
        // 改为断言分隔线规则仍存在于样式表中且选择器覆盖项目行
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        const dividerRule = cssText.split('\n').filter((line) => line.includes('.completion-popover-meta:not(.completion-popover-meta-user)')).join('\n')
        expect(dividerRule).toContain('border-top: 1px solid color-mix(in srgb, var(--text-primary) 16%, transparent)')
    })

    it('hides the user message row when empty', () => {
        mockState.active = ref(makeItem({ userMessage: '' }))
        mountPopover()

        expect(document.querySelector('.completion-popover-user-quote')).toBeFalsy()
    })

    it('keeps a long user message on a single line and expands to full content on click', async () => {
        const longMessage = '这是一个非常非常非常非常非常非常非常非常非常非常非常非常长的用户消息，用来验证引用块内的文本超出宽度时保持单行并显示省略号'
        mockState.active = ref(makeItem({ userMessage: longMessage }))
        mountPopover()

        const el = document.querySelector('.completion-popover-user-quote')!
        const text = el.querySelector('.completion-popover-user-quote-text')!
        // 完整文本仍在 DOM（省略号只是视觉裁剪，点击可展开查看完整内容）
        expect(text.textContent).toBe(longMessage)
        // 折叠态：单行不换行 + 溢出隐藏 + 省略号
        const styles = window.getComputedStyle(text)
        expect(styles.whiteSpace).toBe('nowrap')
        expect(styles.overflow).toBe('hidden')
        expect(styles.textOverflow).toBe('ellipsis')
        // 文本 span 可收缩（min-width: 0），否则长文本会撑破引用块容器
        expect(styles.minWidth).toBe('0px')
        // 点击后展开为完整可读内容
        el.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()
        expect(window.getComputedStyle(text).whiteSpace).toBe('pre-wrap')
    })

    it('renders the project name and path when provided', () => {
        mockState.active = ref(makeItem({ projectName: 'my-app', projectPath: '/home/user' }))
        mountPopover()

        const nameEl = document.querySelector('.completion-popover-project-name')!
        expect(nameEl.textContent).toBe('my-app')
        const pathEl = document.querySelector('.completion-popover-project-path')!
        expect(pathEl.textContent).toBe('/home/user')
        // 项目行位于底部 footer：无外边距、横向铺满卡片的独立区隔带
        const footer = document.querySelector('.completion-popover-footer')!
        expect(footer).toBeTruthy()
        expect(footer.querySelector('.completion-popover-project')).toBeTruthy()
        // jsdom 不解析 color-mix()，改断言 CSS 规则文本
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        const footerRule = cssText.split('\n').filter((line) => line.includes('.completion-popover-footer')).join('\n')
        expect(footerRule).toContain('background: color-mix(in srgb, var(--accent-color) 8%, var(--bg-primary')
        // 贴边区隔：负 margin 铺满卡片宽度、顶部描边、无圆角
        expect(footerRule).toContain('margin: 8px -10px -8px')
        expect(footerRule).toContain('border-top: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent)')
        expect(footerRule).not.toContain('border-radius')
        // "外部"徽章：图标 + 文字，提示这是其他项目的会话
        const badge = document.querySelector('.completion-popover-project-badge')!
        expect(badge).toBeTruthy()
        expect(badge.querySelector('svg')).toBeTruthy()
        expect(badge.textContent!.trim().length).toBeGreaterThan(0)
        // 路径 span 承担单行省略（flex 容器内 text-overflow 不生效，
        // 省略逻辑须落在路径上，保证图标/项目名不被截断）
        const pathStyles = window.getComputedStyle(pathEl)
        expect(pathStyles.whiteSpace).toBe('nowrap')
        expect(pathStyles.overflow).toBe('hidden')
        expect(pathStyles.textOverflow).toBe('ellipsis')
        // 徽章与项目名不收缩，保持完整
        const nameStyles = window.getComputedStyle(nameEl)
        expect(nameStyles.flexShrink).toBe('0')
        const badgeStyles = window.getComputedStyle(badge)
        expect(badgeStyles.flexShrink).toBe('0')
    })

    it('hides the project path span when projectPath is empty', () => {
        mockState.active = ref(makeItem({ projectName: 'my-app', projectPath: '' }))
        mountPopover()

        expect(document.querySelector('.completion-popover-project-name')).toBeTruthy()
        expect(document.querySelector('.completion-popover-project-path')).toBeFalsy()
    })

    it('hides the project row when projectName is empty (same project)', () => {
        mockState.active = ref(makeItem({ projectName: '', projectPath: '' }))
        mountPopover()

        expect(document.querySelector('.completion-popover-project')).toBeFalsy()
    })

    it('animates with Android-notification style slide-down enter transition', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        // jsdom cannot observe <Transition> class lifecycle, so assert the
        // injected stylesheet contains the Android-notification style slide-down.
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        expect(cssText).toContain('.completion-popover-card-enter-from')
        expect(cssText).toContain('translateY(-120%)')
        expect(cssText).toContain('cubic-bezier(0.4, 0, 0.2, 1)')
    })

    it('wraps the card in a Transition inside the static backdrop (animation layer guard)', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        // The card must be a direct child of a <Transition> that sits inside the
        // backdrop — this layer structure is what makes enter/leave animations
        // actually play. Regression guard for the animation-layer fix.
        const backdrop = document.querySelector('.completion-popover-backdrop')!
        const card = document.querySelector('.completion-popover')!
        const transitionEl = card.parentElement!
        // Card's direct parent is the Transition's rendered slot root
        expect(transitionEl.parentElement).toBe(backdrop)
        // The Transition wraps exactly one conditional element (the card)
        expect(transitionEl.querySelectorAll('.completion-popover')).toHaveLength(1)
    })

    it('renders the agent backend icon when agentId resolves', () => {
        mockGetAgentBackend.mockReturnValue('codebuddy')
        mockState.active = ref(makeItem({ agentId: 'cb-1' }))
        mountPopover()

        expect(mockGetAgentBackend).toHaveBeenCalledWith('cb-1')
        expect(document.querySelector('.agent-icon-svg')).toBeTruthy()
    })

    it('skips the agent icon when agentId is unknown or missing', () => {
        mockGetAgentBackend.mockReturnValue('')
        mockState.active = ref(makeItem({ agentId: 'unknown-agent' }))
        mountPopover()

        expect(document.querySelector('.agent-icon-svg')).toBeFalsy()
    })

    it('renders the summary as markdown HTML', () => {
        mockState.active = ref(makeItem({ summary: '**加粗摘要**' }))
        mountPopover()

        const el = document.querySelector('.completion-popover-summary')!
        expect(el.querySelector('strong')).toBeTruthy()
        expect(el.querySelector('strong')!.textContent).toBe('加粗摘要')
    })

    it('collapses by default: no scroll (clipped preview) and no visible input', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const el = document.querySelector('.completion-popover-summary')!
        const styles = window.getComputedStyle(el)
        // 默认折叠态：富文本预览固定高度裁剪、不滚动
        expect(el.classList.contains('is-collapsed')).toBe(true)
        expect(styles.overflowY).toBe('hidden')
        expect(styles.maxHeight).toBe('132px')
        // 无 -webkit-box line clamp（用 max-height + 淡出裁剪，而非文字行截断）
        expect(styles.webkitLineClamp).not.toBe('10')
        // 输入框隐藏（v-show）
        const input = document.querySelector('.completion-popover-input')!
        expect(window.getComputedStyle(input).display).toBe('none')
        // 不存在收起按钮（一旦展开不回折叠）
        expect(document.querySelector('.completion-popover-collapse')).toBeFalsy()
        // 折叠态底部动作行与摘要淡出带重叠（负 margin 上移，省纵向空间）
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        const actionsRule = cssText.split('\n').filter((line) => line.includes('.completion-popover:not(.is-expanded) .completion-popover-actions')).join('\n')
        expect(actionsRule).toContain('margin-top: -34px')
    })

    it('clips the collapsed summary with a bottom fade and hides images', () => {
        mockState.active = ref(makeItem({ summary: '![pic](img/a.png)\n\n正文内容' }))
        mountPopover()

        // 折叠态摘要容器：mask 渐变淡出底部 + overflow hidden
        const el = document.querySelector('.completion-popover-summary.is-collapsed')!
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        // jsdom 将 #000 序列化为 rgb(0, 0, 0)，此处只断言渐变 mask 的存在
        expect(cssText).toContain('-webkit-mask-image: linear-gradient(rgb')
        expect(cssText).toContain('transparent 100%)')
        // 图片在折叠态不展示（CSS display:none），避免裂图占位
        expect(cssText).toContain('.completion-popover-summary.is-collapsed img')
        const img = el.querySelector('img')!
        expect(window.getComputedStyle(img).display).toBe('none')
    })

    it('expands to a near-fullscreen panel on content click: input visible, no collapse button, summary scrolls', async () => {
        mockState.active = ref(makeItem({ summary: '很长'.repeat(200) }))
        mountPopover()

        const summary = document.querySelector('.completion-popover-summary.is-collapsed')!
        // 点击折叠摘要正文 → 展开
        summary.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()

        // 输入框出现
        const input = document.querySelector('.completion-popover-input')!
        expect(window.getComputedStyle(input).display).not.toBe('none')
        expect(document.querySelector('.completion-popover-textarea')).toBeTruthy()
        // 展开后不再提供"收起"按钮（回到折叠态无意义）
        expect(document.querySelector('.completion-popover-collapse')).toBeFalsy()
        // 展开态摘要可滚动（无 max-height 钳制）
        const expanded = document.querySelector('.completion-popover-summary.is-expanded-summary')!
        expect(expanded).toBeTruthy()
        const styles = window.getComputedStyle(expanded)
        expect(styles.overflowY).toBe('auto')
        // 展开态已无折叠裁剪 class
        expect(expanded.classList.contains('is-collapsed')).toBe(false)
    })

    it('auto-expands the user message quote block when the panel expands', async () => {
        mockState.active = ref(makeItem({ summary: '正文', userMessage: '一个非常长的用户消息'.repeat(10) }))
        mountPopover()

        // 初始折叠：用户消息单行省略、未展开
        const quote = document.querySelector('.completion-popover-user-quote')!
        expect(quote.classList.contains('is-expanded')).toBe(false)
        const text = quote.querySelector('.completion-popover-user-quote-text')!
        expect(window.getComputedStyle(text).whiteSpace).toBe('nowrap')

        // 点击折叠摘要正文 → 展开面板
        const summary = document.querySelector('.completion-popover-summary.is-collapsed')!
        summary.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()

        // 用户消息引用块随面板展开而展开，确保能看全问题
        expect(quote.classList.contains('is-expanded')).toBe(true)
        expect(quote.getAttribute('aria-expanded')).toBe('true')
        expect(window.getComputedStyle(text).whiteSpace).toBe('pre-wrap')
    })

    it('stays expanded once opened — clicking the expanded content does not collapse it', async () => {
        mockState.active = ref(makeItem({ summary: '很长'.repeat(200) }))
        mountPopover()

        const collapsed = document.querySelector('.completion-popover-summary.is-collapsed')!
        collapsed.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()

        // 展开态点击正文不收起
        const expanded = document.querySelector('.completion-popover-summary.is-expanded-summary')!
        expanded.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()
        expect(document.querySelector('.completion-popover-summary.is-expanded-summary')).toBeTruthy()
        expect(document.querySelector('.completion-popover-input')!).toBeTruthy()
        expect(document.querySelector('.completion-popover-collapse')).toBeFalsy()
    })

    it('renders rewritten images (lightbox wrapped), hidden when collapsed and shown when expanded', async () => {
        mockState.active = ref(makeItem({ summary: '![pic](img/a.png)', projectPath: '/proj' }))
        mountPopover()

        // 折叠态：HTML 已完全渲染（图片已按归属项目重写为缩略图），但 CSS 隐藏
        const collapsed = document.querySelector('.completion-popover-summary.is-collapsed')!
        const collapsedImg = collapsed.querySelector('img')!
        expect(window.getComputedStyle(collapsedImg).display).toBe('none')
        // 折叠态下图片同样完成 src 重写（与展开态共用同一份渲染）
        expect(collapsedImg.src).toContain('/api/file/thumb?path=img/a.png')

        // 点击展开：同一 DOM 元素切换 class，图片变为可见
        collapsed.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()

        const expanded = document.querySelector('.completion-popover-summary.is-expanded-summary')!
        const img = expanded.querySelector('img')!
        // 仍是同一个摘要元素（折叠/展开共用渲染，无重复解析）
        expect(expanded).toBe(collapsed)
        expect(window.getComputedStyle(img).display).not.toBe('none')
        // rewriteImageUrls 按路径段逐个 encodeURIComponent（'/' 保留），
        // src 是缩略图端点，data-full-src 保留原图供 lightbox 使用
        expect(img.src).toContain('/api/file/thumb?path=img/a.png')
        expect(img.src).toContain('w=')
        expect(img.getAttribute('data-full-src')).toContain('/api/local-file/img/a.png')
        const wrap = img.closest('.lightbox-img-wrap')!
        expect(wrap).toBeTruthy()
        expect(wrap.querySelector('.lightbox-expand-icon')).toBeTruthy()
    })

    it('truncates the title with ellipsis via CSS', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const el = document.querySelector('.completion-popover-title')!
        const styles = window.getComputedStyle(el)
        expect(styles.textOverflow).toBe('ellipsis')
        expect(styles.overflow).toBe('hidden')
        expect(styles.whiteSpace).toBe('nowrap')
    })

    it('renders an open-session icon button and no close button', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        expect(document.querySelector('.completion-popover-open')).toBeTruthy()
        expect(document.querySelector('.completion-popover-close')).toBeFalsy()
    })

    it('clicking the open button dispatches clawbench-open-session for session kind', () => {
        mockState.active = ref(makeItem({ sessionId: 's42', projectPath: '/proj' }))
        mountPopover()

        const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

        const openBtn = document.querySelector('.completion-popover-open')!
        openBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(dispatchSpy).toHaveBeenCalledTimes(1)
        const ev = dispatchSpy.mock.calls[0][0] as CustomEvent
        expect(ev.type).toBe('clawbench-open-session')
        expect(ev.detail).toEqual({ sessionId: 's42', projectPath: '/proj' })
        expect(mockState.dismiss).toHaveBeenCalledTimes(1)
    })

    it('clicking the open button dispatches clawbench-open-task for task kind', () => {
        mockState.active = ref(makeItem({ kind: 'task', taskId: '7', executionId: 'e9' }))
        mountPopover()

        const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

        const openBtn = document.querySelector('.completion-popover-open')!
        openBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(dispatchSpy).toHaveBeenCalledTimes(1)
        const ev = dispatchSpy.mock.calls[0][0] as CustomEvent
        expect(ev.type).toBe('clawbench-open-task')
        expect(ev.detail).toEqual({ taskId: '7', executionId: 'e9', projectPath: '' })
        expect(mockState.dismiss).toHaveBeenCalledTimes(1)
    })

    it('clicking the card body does NOT navigate (only the open button does)', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

        const card = document.querySelector('.completion-popover')!
        card.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(dispatchSpy).not.toHaveBeenCalled()
        expect(mockState.dismiss).not.toHaveBeenCalled()
    })

    it('clicking outside the card (on the backdrop) goes through the guarded dismiss', () => {
        mockState.active = ref(makeItem())
        mockState.dismissOnBackdrop.mockReturnValue(true)
        mountPopover()

        const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

        const backdrop = document.querySelector('.completion-popover-backdrop')!

        backdrop.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(dispatchSpy).not.toHaveBeenCalled()
        // backdrop 关闭走带防误触保护的 dismissOnBackdrop（最小停留时长在 composable 层拦截）
        expect(mockState.dismissOnBackdrop).toHaveBeenCalledTimes(1)
        expect(mockState.dismiss).not.toHaveBeenCalled()
    })

    it('clicking a code-block copy button inside the summary does not navigate', () => {
        mockState.active = ref(makeItem({ summary: '```js\nconst a = 1\n```' }))
        mountPopover()

        const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

        const copyBtn = document.querySelector('.completion-popover-summary .code-block-copy-btn')!
        copyBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(dispatchSpy).not.toHaveBeenCalled()
        expect(mockState.dismiss).not.toHaveBeenCalled()
    })

    it('clicking a code-block wrap button inside the summary does not navigate', () => {
        mockState.active = ref(makeItem({ summary: '```js\nconst a = 1\n```' }))
        mountPopover()

        const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

        const wrapBtn = document.querySelector('.completion-popover-summary .code-block-wrap-btn')!
        wrapBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(dispatchSpy).not.toHaveBeenCalled()
        expect(mockState.dismiss).not.toHaveBeenCalled()
    })

    it('renders a quick-reply input box', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        expect(document.querySelector('.completion-popover-textarea')).toBeTruthy()
        // 空输入时：发送按钮存在但 disabled；标记已读按钮在底部动作行始终存在
        const sendBtn = document.querySelector('.completion-popover-send')!
        expect(sendBtn.classList.contains('disabled')).toBe(true)
        expect(document.querySelector('.completion-popover-mark-read')).toBeTruthy()
    })

    it('keeps the mark-as-read and open buttons in the bottom actions row (right-aligned), independent of input', async () => {
        mockState.active = ref(makeItem())
        mountPopover()

        // 操作按钮不属于标题栏
        const header = document.querySelector('.completion-popover-header')!
        const actions = document.querySelector('.completion-popover-actions')!
        expect(actions).toBeTruthy()
        const markRead = document.querySelector('.completion-popover-mark-read')!
        const open = document.querySelector('.completion-popover-open')!
        expect(markRead).toBeTruthy()
        expect(open).toBeTruthy()
        // mark-read 与 open 位于底部动作行，不在 header 内
        expect(header.contains(markRead)).toBe(false)
        expect(header.contains(open)).toBe(false)
        expect(actions.contains(markRead)).toBe(true)
        expect(actions.contains(open)).toBe(true)
        // mark-read 位于 open 左边
        expect(markRead.compareDocumentPosition(open) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
        // 胶囊按钮：图标 + 文字标签（当前测试环境默认 en：Mark as read / Open session）
        expect(markRead.querySelector('.completion-popover-action-label')!.textContent).toContain('Mark as read')
        expect(open.querySelector('.completion-popover-action-label')!.textContent).toContain('Open session')
        // 动作行内容靠右对齐
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        const actionsRule = cssText.split('\n').filter((line) => line.includes('.completion-popover-actions')).join('\n')
        expect(actionsRule).toContain('justify-content: flex-end')
        // 胶囊样式：圆角 999px（非圆形按钮），内边距容纳文字（jsdom 序列化为 0px 12px）
        const btnRule = cssText.split('\n').filter((line) => line.includes('.completion-popover-action-btn')).join('\n')
        expect(btnRule).toContain('border-radius: 999px')
        expect(btnRule).toContain('padding: 0px 12px')

        // 输入内容后 mark-read 依然存在（不随输入联动）
        const textarea = document.querySelector('.completion-popover-textarea') as HTMLTextAreaElement
        textarea.value = '回复内容'
        textarea.dispatchEvent(new Event('input'))
        await nextTick()

        expect(document.querySelector('.completion-popover-mark-read')).toBeTruthy()
        expect(document.querySelector('.completion-popover-send')!.classList.contains('disabled')).toBe(false)
    })

    it('aligns the input box with the chat input bar (radius 20px, 16px textarea, 28px send button)', async () => {
        mockState.active = ref(makeItem())
        mountPopover()

        // 输入文本使发送按钮变为可点状态
        const textareaEl = document.querySelector('.completion-popover-textarea') as HTMLTextAreaElement
        textareaEl.value = '测试'
        textareaEl.dispatchEvent(new Event('input'))
        await nextTick()

        expect(document.querySelector('.completion-popover-input')).toBeTruthy()
        // textarea 与聊天输入框对齐：16px 字号、行高 20px、上下 padding 4px
        const ta = document.querySelector('.completion-popover-textarea')!
        const taStyles = window.getComputedStyle(ta)
        expect(taStyles.fontSize).toBe('16px')
        expect(taStyles.lineHeight).toBe('20px')
        expect(taStyles.paddingTop).toBe('4px')
        expect(taStyles.paddingBottom).toBe('4px')
        expect(taStyles.minHeight).toBe('28px')
        // 发送按钮与聊天输入框对齐：28px 圆形
        const btn = document.querySelector('.completion-popover-send')!
        const btnStyles = window.getComputedStyle(btn)
        expect(btnStyles.width).toBe('28px')
        expect(btnStyles.height).toBe('28px')
        // jsdom 不解析 border-radius 简写计算值，改为断言 CSS 规则
        const cssText = Array.from(document.styleSheets)
            .map((s) => {
                try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
                catch { return '' }
            })
            .join('\n')
        const inputRule = cssText.split('\n').filter((line) => line.includes('.completion-popover-input')).join('\n')
        expect(inputRule).toContain('border-radius: 20px')
        // 背景用 --bg-primary，与卡片 tertiary 底色区分（避免融合）
        expect(inputRule).toContain('background: var(--bg-primary')
        // 不再使用胶囊圆角 999px
        expect(inputRule).not.toContain('999px')
    })

    it('sends the message to the session and dismisses on send click', async () => {
        mockState.active = ref(makeItem({ sessionId: 's42' }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: true })
        globalThis.fetch = fetchMock

        const textarea = document.querySelector('.completion-popover-textarea') as HTMLTextAreaElement
        textarea.value = '继续说说'
        textarea.dispatchEvent(new Event('input'))
        // 等待 v-model 更新，canSend 变为 true 后发送按钮出现
        await nextTick()

        const sendBtn = document.querySelector('.completion-popover-send')!
        sendBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        await vi.waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                expect.stringContaining('/api/ai/chat?session_id=s42'),
                expect.objectContaining({
                    method: 'POST',
                    body: expect.stringContaining('继续说说'),
                })
            )
            expect(mockState.dismiss).toHaveBeenCalledTimes(1)
        })
        // 发送成功后清空该会话未读（独立 /read 调用）
        expect(fetchMock).toHaveBeenCalledWith(
            expect.stringContaining('/api/ai/chat/read?session_id=s42'),
            expect.objectContaining({ method: 'POST' })
        )
        // 发送成功弹出确认气泡（文案随当前语言环境，这里只断言调用发生与类型）
        expect(mockToastShow).toHaveBeenCalledTimes(1)
        expect(mockToastShow).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ type: 'success' }))
    })

    it('does not show a sent toast when sending fails (keeps popover open)', async () => {
        mockState.active = ref(makeItem({ sessionId: 's42' }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: false })
        globalThis.fetch = fetchMock

        const textarea = document.querySelector('.completion-popover-textarea') as HTMLTextAreaElement
        textarea.value = '会失败的回复'
        textarea.dispatchEvent(new Event('input'))
        await nextTick()

        const sendBtn = document.querySelector('.completion-popover-send')!
        sendBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        await vi.waitFor(() => {
            // 失败时不得弹出"已发送"气泡，也不关闭弹窗
            expect(mockToastShow).not.toHaveBeenCalled()
            expect(mockState.dismiss).not.toHaveBeenCalled()
        })
    })

    it('does not send when input is empty', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const fetchMock = vi.fn()
        globalThis.fetch = fetchMock

        // 空输入时发送按钮是 disabled 态，点击不发送
        const sendBtn = document.querySelector('.completion-popover-send')!
        expect(sendBtn.classList.contains('disabled')).toBe(true)
        sendBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(fetchMock).not.toHaveBeenCalledWith(
            expect.stringContaining('/api/ai/chat?'),
            expect.objectContaining({ method: 'POST' })
        )
    })

    it('marks the session as read via the mark-read button and dismisses', async () => {
        mockState.active = ref(makeItem({ sessionId: 's99' }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: true })
        globalThis.fetch = fetchMock

        const markReadBtn = document.querySelector('.completion-popover-mark-read')!
        markReadBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        await vi.waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                '/api/ai/chat/read?session_id=s99',
                expect.objectContaining({ method: 'POST' })
            )
            expect(mockState.dismiss).toHaveBeenCalledTimes(1)
        })
        // 标记已读成功弹出确认气泡（文案随当前语言环境，这里只断言调用发生与类型）
        expect(mockToastShow).toHaveBeenCalledTimes(1)
        expect(mockToastShow).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ type: 'success' }))
    })

    it('sends cross-project replies with the owning project_path so the reply lands in that project', async () => {
        // 弹窗里回复的是"另一个项目"的会话：handleSend 必须携带该会话所属
        // 项目路径（project_path），后端据此覆盖 cookie 项目做归属校验，并把
        // 消息持久化到会话所属项目下——否则切回原项目后回复会"丢失"（issue #420）。
        mockState.active = ref(makeItem({ sessionId: 'ext-session', projectPath: '/path/to/project-a' }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: true })
        globalThis.fetch = fetchMock

        const textarea = document.querySelector('.completion-popover-textarea') as HTMLTextAreaElement
        textarea.value = '提交吧'
        textarea.dispatchEvent(new Event('input'))
        await nextTick()

        const sendBtn = document.querySelector('.completion-popover-send')!
        sendBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        await vi.waitFor(() => {
            expect(fetchMock).toHaveBeenCalledWith(
                '/api/ai/chat?session_id=ext-session&project_path=%2Fpath%2Fto%2Fproject-a',
                expect.objectContaining({ method: 'POST' })
            )
            expect(mockState.dismiss).toHaveBeenCalledTimes(1)
        })
    })
})
