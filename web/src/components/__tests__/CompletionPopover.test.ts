import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import CompletionPopover from '@/components/common/CompletionPopover.vue'

// Mock the singleton composable so each test controls state directly.
// Hoisted vi.mock factories cannot reference outer variables, so the mock
// exposes mutable refs via a getter.
const mockState = {
    active: ref(null),
    queue: ref([]),
    dismiss: vi.fn(),
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

    it('renders the last user message as a single line', () => {
        mockState.active = ref(makeItem({ userMessage: '请帮我修复登录 bug' }))
        mountPopover()

        const el = document.querySelector('.completion-popover-user-msg')!
        expect(el.textContent).toContain('请帮我修复登录 bug')
        const styles = window.getComputedStyle(el)
        expect(styles.textOverflow).toBe('ellipsis')
        expect(styles.overflow).toBe('hidden')
        expect(styles.whiteSpace).toBe('nowrap')
    })

    it('hides the user message row when empty', () => {
        mockState.active = ref(makeItem({ userMessage: '' }))
        mountPopover()

        expect(document.querySelector('.completion-popover-user-msg')).toBeFalsy()
    })

    it('renders the project name and path when provided', () => {
        mockState.active = ref(makeItem({ projectName: 'my-app', projectPath: '/home/user/my-app' }))
        mountPopover()

        const el = document.querySelector('.completion-popover-project')!
        expect(el.textContent).toContain('my-app')
        expect(el.textContent).toContain('/home/user/my-app')
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

    it('shows the full summary without line clamping, scrolling when overflow', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const el = document.querySelector('.completion-popover-summary')!
        const styles = window.getComputedStyle(el)
        // Full content visible — no -webkit-box line clamp
        expect(styles.webkitLineClamp).not.toBe('10')
        // Internal scroll for overflow
        expect(styles.overflowY).toBe('auto')
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

    it('clicking outside the card (on the backdrop) hides without navigating', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

        const backdrop = document.querySelector('.completion-popover-backdrop')!
        backdrop.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(dispatchSpy).not.toHaveBeenCalled()
        expect(mockState.dismiss).toHaveBeenCalledTimes(1)
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
        expect(document.querySelector('.completion-popover-send')).toBeTruthy()
    })

    it('sends the message to the session and dismisses on send click', async () => {
        mockState.active = ref(makeItem({ sessionId: 's42' }))
        mountPopover()

        const fetchMock = vi.fn().mockResolvedValue({ ok: true })
        globalThis.fetch = fetchMock

        const textarea = document.querySelector('.completion-popover-textarea') as HTMLTextAreaElement
        textarea.value = '继续说说'
        textarea.dispatchEvent(new Event('input'))

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
    })

    it('does not send when input is empty', () => {
        mockState.active = ref(makeItem())
        mountPopover()

        const fetchMock = vi.fn()
        globalThis.fetch = fetchMock

        const sendBtn = document.querySelector('.completion-popover-send')!
        sendBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))

        expect(fetchMock).not.toHaveBeenCalled()
        expect(mockState.dismiss).not.toHaveBeenCalled()
    })
})
