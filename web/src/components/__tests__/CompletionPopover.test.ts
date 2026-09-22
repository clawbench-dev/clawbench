import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import CompletionPopover from '@/components/common/CompletionPopover.vue'
import { _setIsPCForTest, _resetPlatformForTest } from '@/composables/usePlatformDetect'
import { readWebFile } from '@/testUtils/readWebFile'

// Mock the singleton composable so each test controls state directly.
// Hoisted vi.mock factories cannot reference outer variables, so the mock
// exposes mutable refs via a getter.
const mockState = {
    active: ref(null),
    queue: ref([]),
    dismiss: vi.fn(),
    pauseAutoDismiss: vi.fn(),
    resumeAutoDismiss: vi.fn(),
}

const { mockGetAgentBackend } = vi.hoisted(() => ({
    mockGetAgentBackend: vi.fn(() => ''),
}))

vi.mock('@/composables/useCompletionPopover', () => ({
    useCompletionPopover: () => mockState,
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
        groupKey: 'session:s1',
        sessionId: 's1',
        eventLabel: '会话已完成',
        eventTone: 'success',
        title: '这是一个很长的会话标题用于测试溢出省略号的显示效果',
        body: '**加粗的摘要内容** 以及普通文本',
        kind: 'session',
        ...overrides,
    }
}

/** Collect window CustomEvents of the given types for the duration of one test. */
function captureEvents(types: string[]) {
    const captured: CustomEvent[] = []
    const listeners = types.map((type) => {
        const fn = (e: Event) => captured.push(e as CustomEvent)
        window.addEventListener(type, fn)
        return [type, fn] as const
    })
    return {
        captured,
        stop() {
            for (const [type, fn] of listeners) window.removeEventListener(type, fn)
        },
    }
}

function cssText(): string {
    return Array.from(document.styleSheets)
        .map((s) => {
            try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
            catch { return '' }
        })
        .join('\n')
}

describe('CompletionPopover', () => {
    beforeEach(() => {
        document.body.innerHTML = ''
        vi.clearAllMocks()
        mockState.active = ref(null)
        mockState.queue = ref([])
        mockGetAgentBackend.mockReturnValue('')
        _resetPlatformForTest()
    })
    afterEach(() => {
        _resetPlatformForTest()
    })

    function mountPopover() {
        return mount(CompletionPopover, { attachTo: document.body })
    }

    it('renders nothing when no item is active', () => {
        mountPopover()

        expect(document.querySelector('.completion-notify')).toBeFalsy()
        expect(document.querySelector('.completion-notify-layer')).toBeFalsy()
    })

    it('renders the session title', () => {
        mockState.active = ref(makeItem({ title: '修复登录超时' }))
        mountPopover()

        expect(document.querySelector('.completion-notify-title')!.textContent).toBe('修复登录超时')
    })

    it('renders the plain-text body clamped to 4 lines', () => {
        mockState.active = ref(makeItem({ body: '已完成登录流程重构' }))
        mountPopover()

        const el = document.querySelector('.completion-notify-body')!
        expect(el.textContent).toBe('已完成登录流程重构')
        const styles = window.getComputedStyle(el)
        // 多行截断：4 行封顶 + 省略号，而非单行 nowrap
        expect(styles.overflow).toBe('hidden')
        expect(styles.whiteSpace).not.toBe('nowrap')
    })

    it('clamps the body via -webkit-line-clamp so the line count tracks the line height', () => {
        // 用 clamp 而不是固定 max-height：改字号/行高时行数自动跟随，
        // 不需要同步改一个 px 数值。jsdom 的 getComputedStyle 不暴露
        // -webkit-line-clamp，只能查源码声明。
        const src = readWebFile('src/components/common/CompletionPopover.vue')
        const rule = src.match(/\.completion-notify-body\s*\{[^}]*\}/)?.[0] || ''
        expect(rule).toContain('-webkit-line-clamp: 4')
        expect(rule).toContain('display: -webkit-box')
        expect(rule).toContain('-webkit-box-orient: vertical')
        // 不得退化成固定高度（会与行高脱钩）
        expect(rule).not.toMatch(/max-height:\s*\d+px/)
    })

    it('sizes the body from the same type scale as chat messages', () => {
        // 用户反馈"字体有点小"：正文原来比聊天正文低一档（sm/12px），
        // 现在对齐 .chat-message 的 md + snug。
        const src = readWebFile('src/components/common/CompletionPopover.vue')
        const rule = src.match(/\.completion-notify-body\s*\{[^}]*\}/)?.[0] || ''
        expect(rule).toContain('font-size: var(--font-size-md)')
        expect(rule).toContain('line-height: var(--line-height-snug)')
        // 防回归：不得退回更小的一档
        expect(rule).not.toContain('font-size: var(--font-size-sm)')
        expect(rule).not.toContain('font-size: var(--font-size-xs)')

        // 与聊天正文的实际声明逐字一致
        const chatItem = readWebFile('src/components/chat/ChatMessageItem.vue')
        const chatRule = chatItem.match(/\.chat-message\s*\{[^}]*\}/)?.[0] || ''
        expect(chatRule).toContain('font-size: var(--font-size-md)')
        expect(chatRule).toContain('line-height: var(--line-height-snug)')
    })

    it('omits the body row entirely when there is no body', () => {
        mockState.active = ref(makeItem({ body: '' }))
        mountPopover()

        expect(document.querySelector('.completion-notify-body')).toBeFalsy()
    })

    it('does NOT render markdown markup for the body (plain text only)', () => {
        // The old card rendered summary Markdown. The notification must not:
        // a plain body means the same string reads identically here and in the
        // OS notification.
        mockState.active = ref(makeItem({ body: '**加粗摘要**' }))
        mountPopover()

        const el = document.querySelector('.completion-notify-body')!
        expect(el.querySelector('strong')).toBeFalsy()
        expect(el.textContent).toBe('**加粗摘要**')
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

    it('truncates the title with ellipsis via CSS', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const el = document.querySelector('.completion-notify-title')!
        const styles = window.getComputedStyle(el)
        expect(styles.textOverflow).toBe('ellipsis')
        expect(styles.overflow).toBe('hidden')
        expect(styles.whiteSpace).toBe('nowrap')
    })

    // ── 关闭 ──

    it('renders a close button', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        expect(document.querySelector('.completion-notify-close')).toBeTruthy()
    })

    it('clicking the close button dismisses without navigating', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const events = captureEvents(['clawbench-open-session', 'clawbench-open-task', 'clawbench-open-forge'])
        const closeBtn = document.querySelector('.completion-notify-close')!
        closeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        events.stop()

        expect(mockState.dismiss).toHaveBeenCalledTimes(1)
        expect(events.captured).toHaveLength(0)
    })

    // ── 悬停暂停（鼠标放上去就不要消失）──

    it('pauses the auto-dismiss timer on mouseenter and resumes on mouseleave', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const card = document.querySelector('.completion-notify')!
        card.dispatchEvent(new MouseEvent('mouseenter'))
        expect(mockState.pauseAutoDismiss).toHaveBeenCalledTimes(1)
        expect(mockState.resumeAutoDismiss).not.toHaveBeenCalled()

        card.dispatchEvent(new MouseEvent('mouseleave'))
        expect(mockState.resumeAutoDismiss).toHaveBeenCalledTimes(1)
    })

    it('also pauses while the card holds keyboard focus (not just hover)', () => {
        // 键盘用户 tab 到卡片上时鼠标并未悬停，若只绑 mouseenter，
        // 卡片会在他们读内容的过程中消失。
        mockState.active = ref(makeItem())
        mountPopover()

        const card = document.querySelector('.completion-notify')!
        card.dispatchEvent(new FocusEvent('focusin'))
        expect(mockState.pauseAutoDismiss).toHaveBeenCalledTimes(1)

        card.dispatchEvent(new FocusEvent('focusout'))
        expect(mockState.resumeAutoDismiss).toHaveBeenCalledTimes(1)
    })

    // ── 跳转（整卡点击） ──

    it('clicking the card dispatches clawbench-open-session for session kind', () => {
        mockState.active = ref(makeItem({ sessionId: 's42', projectPath: '/proj' }))
        mountPopover()

        const events = captureEvents(['clawbench-open-session'])
        document.querySelector('.completion-notify')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        events.stop()

        expect(events.captured).toHaveLength(1)
        expect(events.captured[0].type).toBe('clawbench-open-session')
        expect(events.captured[0].detail).toEqual({ sessionId: 's42', projectPath: '/proj' })
        expect(mockState.dismiss).toHaveBeenCalledTimes(1)
    })

    it('clicking the card dispatches clawbench-open-task for task kind', () => {
        mockState.active = ref(makeItem({ kind: 'task', taskId: '7', executionId: 'e9' }))
        mountPopover()

        const events = captureEvents(['clawbench-open-task'])
        document.querySelector('.completion-notify')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        events.stop()

        expect(events.captured).toHaveLength(1)
        expect(events.captured[0].detail).toEqual({ taskId: '7', executionId: 'e9', projectPath: undefined })
        expect(mockState.dismiss).toHaveBeenCalledTimes(1)
    })

    it('clicking the card dispatches clawbench-open-forge for forge kind', () => {
        mockState.active = ref(makeItem({
            kind: 'forge',
            groupKey: 'forge:acme/web',
            title: 'acme/web',
            projectPath: '/proj-b',
        }))
        mountPopover()

        const events = captureEvents(['clawbench-open-forge'])
        document.querySelector('.completion-notify')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        events.stop()

        expect(events.captured).toHaveLength(1)
        expect(events.captured[0].detail).toEqual({ projectPath: '/proj-b' })
        expect(mockState.dismiss).toHaveBeenCalledTimes(1)
    })

    // ── 跳转顺带标记已读 ──

    it('navigating a session marks it read via the session read endpoint', () => {
        mockState.active = ref(makeItem({ sessionId: 's42', projectPath: '/proj' }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: true })
        globalThis.fetch = fetchMock

        document.querySelector('.completion-notify')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(fetchMock).toHaveBeenCalledWith(
            expect.stringContaining('/api/ai/chat/read?session_id=s42'),
            expect.objectContaining({ method: 'POST' }),
        )
        // 跨项目会话必须带上归属项目路径，否则后端归属校验会拒绝
        expect(fetchMock.mock.calls[0][0]).toContain('project_path=%2Fproj')
    })

    it('navigating a task marks that execution read via the task endpoint', () => {
        mockState.active = ref(makeItem({ kind: 'task', taskId: '7', executionId: 'e9' }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: true })
        globalThis.fetch = fetchMock

        document.querySelector('.completion-notify')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(fetchMock).toHaveBeenCalledWith(
            '/api/tasks/7',
            expect.objectContaining({
                method: 'PUT',
                body: JSON.stringify({ action: 'read', executionId: 'e9' }),
            }),
        )
    })

    it('navigating a forge issue/PR marks that item read', () => {
        mockState.active = ref(makeItem({
            kind: 'forge',
            groupKey: 'forge:acme/web',
            title: 'acme/web',
            forgeItemKey: 'pr/42',
        }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: true })
        globalThis.fetch = fetchMock

        document.querySelector('.completion-notify')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(fetchMock).toHaveBeenCalledWith(
            '/api/forge/read',
            expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ itemKey: 'pr/42' }),
            }),
        )
    })

    it('navigating a pipeline forge event does NOT call the forge read endpoint', () => {
        // 流水线的 run id 未随事件下发，构造不出条目键——只能跳转，不能标记
        mockState.active = ref(makeItem({
            kind: 'forge',
            groupKey: 'forge:acme/web',
            title: 'acme/web',
            forgeItemKey: undefined,
        }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: true })
        globalThis.fetch = fetchMock

        document.querySelector('.completion-notify')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(fetchMock).not.toHaveBeenCalled()
    })

    it('a failed read call does not block navigation', () => {
        mockState.active = ref(makeItem({ sessionId: 's42' }))
        mountPopover()

        globalThis.fetch = vi.fn().mockRejectedValue(new Error('network down'))

        const events = captureEvents(['clawbench-open-session'])
        document.querySelector('.completion-notify')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        events.stop()

        expect(events.captured).toHaveLength(1)
        expect(mockState.dismiss).toHaveBeenCalledTimes(1)
    })

    // ── 事件类型标识 ──

    it('renders the event-type chip with its label', () => {
        mockState.active = ref(makeItem({ eventLabel: '会话已取消', eventTone: 'warning' }))
        mountPopover()

        const chips = document.querySelectorAll('.completion-notify-kind')
        // 无 kindLabel 时只有事件 chip 一枚
        expect(chips).toHaveLength(1)
        expect(chips[0].textContent).toBe('会话已取消')
    })

    // ── 类别标识（会话 / 任务 / 议题与合并）──

    it('renders the category chip alongside the event chip', () => {
        mockState.active = ref(makeItem({ kindLabel: '会话', eventLabel: '会话已完成' }))
        mountPopover()

        const chips = document.querySelectorAll('.completion-notify-kind')
        expect(chips).toHaveLength(2)
        expect(chips[0].textContent).toBe('会话')
        expect(chips[1].textContent).toBe('会话已完成')
        // 类别 chip 走中性色，不参与语义配色
        expect(chips[0].className).toContain('is-category')
    })

    it.each([
        ['会话', 'session'],
        ['任务', 'task'],
        ['议题与合并', 'forge'],
    ])('renders the "%s" category label regardless of kind', (label) => {
        mockState.active = ref(makeItem({ kindLabel: label }))
        mountPopover()

        expect(document.querySelector('.completion-notify-kind.is-category')!.textContent).toBe(label)
    })

    it('omits the category chip when kindLabel is absent', () => {
        mockState.active = ref(makeItem({ kindLabel: undefined }))
        mountPopover()

        expect(document.querySelector('.completion-notify-kind.is-category')).toBeFalsy()
        // 事件 chip 仍在
        expect(document.querySelectorAll('.completion-notify-kind')).toHaveLength(1)
    })

    // ── 外部项目区隔带（跨项目显著区分）──

    it('renders the external-project strip with badge, name and path', () => {
        mockState.active = ref(makeItem({
            projectPath: '/home/me/other-project',
            projectName: 'other-project',
        }))
        mountPopover()

        const strip = document.querySelector('.completion-notify-project')!
        expect(strip).toBeTruthy()
        // 「外部」徽章
        expect(strip.querySelector('.completion-notify-project-badge')).toBeTruthy()
        // 项目名突出显示（独立元素 + 加粗）
        const name = strip.querySelector('.completion-notify-project-name')!
        expect(name.textContent).toBe('other-project')
        expect(window.getComputedStyle(name).fontWeight).toBe('var(--font-weight-semibold)')
        // 完整路径弱化展示
        expect(strip.querySelector('.completion-notify-project-path')!.textContent)
            .toBe('/home/me/other-project')
    })

    it('falls back to the path when no project name is provided', () => {
        mockState.active = ref(makeItem({ projectPath: '/home/me/other-project', projectName: '' }))
        mountPopover()

        expect(document.querySelector('.completion-notify-project-name')!.textContent)
            .toBe('/home/me/other-project')
    })

    it('omits the strip entirely for a same-project event', () => {
        mockState.active = ref(makeItem({ projectPath: '', projectName: '' }))
        mountPopover()

        expect(document.querySelector('.completion-notify-project')).toBeFalsy()
        expect(document.querySelector('.completion-notify.is-external')).toBeFalsy()
    })

    it('marks the whole card as external (distinct border + background)', () => {
        // 只靠底部一条区隔带时，卡片主体看起来仍像本项目的通知；
        // 整卡换色让"不是本项目"在余光里就能分辨。
        mockState.active = ref(makeItem({
            projectPath: '/home/me/other-project',
            projectName: 'other-project',
        }))
        mountPopover()

        expect(document.querySelector('.completion-notify')!.className).toContain('is-external')
        const src = readWebFile('src/components/common/CompletionPopover.vue')
        const rule = src.match(/\.completion-notify\.is-external\s*\{[^}]*\}/)?.[0] || ''
        expect(rule).toContain('border-color')
        expect(rule).toContain('background')
    })

    it('truncates a long project path on one line', () => {
        mockState.active = ref(makeItem({
            projectPath: '/very/long/path/to/some/deeply/nested/project/directory/name',
            projectName: 'name',
        }))
        mountPopover()

        const styles = window.getComputedStyle(document.querySelector('.completion-notify-project-path')!)
        expect(styles.whiteSpace).toBe('nowrap')
        expect(styles.overflow).toBe('hidden')
        expect(styles.textOverflow).toBe('ellipsis')
    })

    it('does not attach native tooltips anywhere on the card', () => {
        // 悬停卡片时不该再弹原生 tooltip（title 属性）——整卡已是可点击的通知，
        // 悬浮时冒出浏览器提示框只会和 hover 暂停倒计时的交互打架。
        mockState.active = ref(makeItem({
            title: '很长的会话标题',
            body: '很长的摘要正文',
            projectPath: '/home/me/other-project',
            projectName: 'other-project',
        }))
        mountPopover()

        const card = document.querySelector('.completion-notify')!
        expect(card.querySelectorAll('[title]')).toHaveLength(0)
    })

    it('keeps an aria-label on the close button for screen readers', () => {
        // 去掉 title 不等于去掉无障碍名称：关闭按钮仍需 aria-label。
        mockState.active = ref(makeItem())
        mountPopover()

        expect(document.querySelector('.completion-notify-close')!.getAttribute('aria-label')).toBeTruthy()
    })

    it.each([
        ['success', '会话已完成'],
        ['danger', '任务失败'],
        ['warning', '任务已取消'],
        ['info', '操作需批准'],
    ])('applies the %s tone class for "%s"', (tone, label) => {
        mockState.active = ref(makeItem({ eventLabel: label, eventTone: tone }))
        mountPopover()

        const chip = document.querySelector('.completion-notify-kind')!
        expect(chip.className).toContain(`is-${tone}`)
        // 只应有一个 tone 类，避免多档配色叠加
        const toneClasses = ['success', 'danger', 'warning', 'info'].filter(t => chip.className.includes(`is-${t}`))
        expect(toneClasses).toEqual([tone])
    })

    it('renders the chip alongside the title, not instead of it', () => {
        mockState.active = ref(makeItem({ eventLabel: '任务已启动', title: '每日构建' }))
        mountPopover()

        const head = document.querySelector('.completion-notify-head')!
        expect(head.querySelector('.completion-notify-kind')!.textContent).toBe('任务已启动')
        expect(head.querySelector('.completion-notify-title')!.textContent).toBe('每日构建')
    })

    it('gives the chip a non-shrinking slot so it is never clipped', () => {
        // chip 是"要不要点这条通知"的判断依据；标题可以被省略号截断，它不行。
        mockState.active = ref(makeItem())
        mountPopover()

        const chip = document.querySelector('.completion-notify-kind')!
        expect(window.getComputedStyle(chip).flexShrink).toBe('0')
    })

    // ── 合并展示 ──

    it('replaces the chip label with the merged count for a merged forge item', () => {
        mockState.active = ref(makeItem({
            kind: 'forge',
            groupKey: 'forge:acme/web',
            eventLabel: '合并请求 #42 · 已合并',
            eventTone: 'info',
            title: 'acme/web',
            count: 3,
        }))
        mountPopover()

        const chip = document.querySelector('.completion-notify-kind')!
        expect(chip.textContent).toContain('3')
        // 合并态不再展示单条事件标识
        expect(chip.textContent).not.toContain('#42')
        // 主体标题（仓库）保持可见
        expect(document.querySelector('.completion-notify-title')!.textContent).toBe('acme/web')
    })

    it('shows the single-item chip label when count is 1 or absent', () => {
        mockState.active = ref(makeItem({
            kind: 'forge',
            groupKey: 'forge:acme/web',
            eventLabel: '合并请求 #42 · 已合并',
            title: 'acme/web',
            count: 1,
        }))
        mountPopover()

        expect(document.querySelector('.completion-notify-kind')!.textContent).toContain('#42')
    })

    // ── 位置与动效 ──

    it('uses the mobile top slide-down transition by default', () => {
        // jsdom 的 UA 是桌面 Chrome，usePlatformDetect 会推出 isPC=true；
        // 这里显式钉成移动端，才能验证默认分支。
        _setIsPCForTest(false)
        mockState.active = ref(makeItem())
        mountPopover()

        expect(document.querySelector('.completion-notify-layer')!.className).not.toContain('is-desktop')
        expect(cssText()).toContain('.completion-notify-mobile-enter-from')
        expect(cssText()).toContain('translateY(-120%)')
        expect(cssText()).toContain('cubic-bezier(0.4, 0, 0.2, 1)')
    })

    it('uses the desktop bottom-right slide-in transition on a PC', () => {
        _setIsPCForTest(true)
        mockState.active = ref(makeItem())
        mountPopover()

        expect(document.querySelector('.completion-notify-layer')!.className).toContain('is-desktop')
        expect(document.querySelector('.completion-notify')!.className).toContain('is-desktop')
        expect(cssText()).toContain('.completion-notify-desktop-enter-from')
        expect(cssText()).toContain('translateX(120%)')
    })

    it('pins the desktop layer to the bottom-right corner', () => {
        _setIsPCForTest(true)
        mockState.active = ref(makeItem())
        mountPopover()

        const layer = document.querySelector('.completion-notify-layer')!
        const styles = window.getComputedStyle(layer)
        expect(styles.alignItems).toBe('flex-end')
        expect(styles.justifyContent).toBe('flex-end')
    })

    it('keeps the desktop card narrow (a notification, not a panel)', () => {
        // 680px 时一行能塞下整段摘要，读起来像在看正文；收窄到 420px 更贴近
        // 系统通知的形态，右下角占用也更小。
        const src = readWebFile('src/components/common/CompletionPopover.vue')
        const desktopRule = src.match(/@media \(min-width: 768px\)\s*\{[\s\S]*?\n\}/)?.[0] || ''
        expect(desktopRule).toContain('max-width: min(420px, 92vw)')
        // 防回归：不得再回到 680px
        expect(desktopRule).not.toContain('680px')
    })

    // ── 非模态：不再有遮罩拦截 / 点空白关闭 ──

    it('lets clicks through the layer (pointer-events: none)', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const layer = document.querySelector('.completion-notify-layer')!
        expect(window.getComputedStyle(layer).pointerEvents).toBe('none')
    })

    it('keeps the card itself clickable despite the pass-through layer', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const card = document.querySelector('.completion-notify')!
        expect(window.getComputedStyle(card).pointerEvents).toBe('auto')
    })

    it('clicking the layer outside the card does NOT dismiss', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const layer = document.querySelector('.completion-notify-layer')!
        layer.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(mockState.dismiss).not.toHaveBeenCalled()
    })

    it('has no backdrop that swallows clicks', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        expect(document.querySelector('.completion-popover-backdrop')).toBeFalsy()
    })

    it('renders the card inside a Transition so enter/leave animations play', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const card = document.querySelector('.completion-notify')!
        // The card's direct parent is the Transition's rendered slot root,
        // which sits inside the positioning layer.
        const transitionRoot = card.parentElement!
        expect(transitionRoot.parentElement!.className).toContain('completion-notify-layer')
        expect(transitionRoot.querySelectorAll('.completion-notify')).toHaveLength(1)
    })

    it('keeps every corner on the sharp scale (no rounded controls)', async () => {
        mockState.active = ref(makeItem())
        mountPopover()
        await nextTick()

        // 只看本组件自己的规则：document.styleSheets 里还混着全局 CSS
        // （agent-icon 的 20% 圆角、KaTeX 等），整体扫描会误判成回归。
        const own = cssText()
            .split('\n')
            .filter((line) => line.includes('.completion-notify'))
            .join('\n')
        // 与 QuoteQuestionBar 同一套硬朗几何：容器 3px、内部块 0/3px。
        // 任何新元素都不得再引入 --radius-md/lg/full 或 50%/20px。
        expect(own).not.toContain('border-radius: 50%')
        expect(own).not.toContain('border-radius: 20px')
        expect(own).not.toContain('border-radius: var(--radius-md)')
        expect(own).not.toContain('border-radius: var(--radius-lg)')
        expect(own).not.toContain('border-radius: var(--radius-full)')
        expect(own).not.toContain('999px')
    })

    it('gives the close button an explicit border: none (icon-button reuse guard)', () => {
        // 图标按钮 class 若从 <a> 复用会被 UA 样式带出边框——必须显式清除。
        // 走源码断言而非 getComputedStyle：jsdom 会把 `border: none` 序列化成
        // `border: medium`，读样式表拿不到作者写下的那个声明。
        const src = readWebFile('src/components/common/CompletionPopover.vue')
        const closeRule = src.match(/\.completion-notify-close\s*\{[^}]*\}/)?.[0] || ''
        expect(closeRule).toContain('border: none')
    })

    it('resets the enter/leave animation under prefers-reduced-motion', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const text = cssText()
        expect(text).toContain('prefers-reduced-motion')
    })
})
