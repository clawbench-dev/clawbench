import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { DOMWrapper, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, type Ref } from 'vue'
import ConnectionOverlay from '@/components/common/ConnectionOverlay.vue'
import ServerSwitchDialog from '@/components/common/ServerSwitchDialog.vue'
import type { ConnectionOverlayMode } from '@/composables/useConnectionOverlay'

// Mock useConnectionOverlay so the component test focuses purely on RENDERING
// given a mode. The mode-computation logic (restart/reconnect/null + the
// reconnect delay) is covered by the composable's own test
// (__tests__/useConnectionOverlay.test.ts).
const mockMode = vi.hoisted(() => {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { ref } = require('vue') as typeof import('vue')
    return ref<ConnectionOverlayMode>(null)
})
vi.mock('@/composables/useConnectionOverlay', () => ({
    useConnectionOverlay: () => ({ mode: mockMode }),
}))

const i18n = createI18n({
    legacy: false,
    locale: 'zh',
    messages: {
        zh: {
            systemResources: { overlayReconnecting: '连接断开，正在重连…' },
            settings: { restartingPleaseWait: '正在重启，请稍候…' },
            login: { switchServer: '切换服务端' },
        },
    },
})

const LucideStub = { template: '<span class="lucide-stub" />' }

function $(selector: string) {
    return document.body.querySelector(selector) as HTMLElement | null
}

function mountOverlay() {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const wrapper = mount(ConnectionOverlay, {
        attachTo: container,
        global: {
            plugins: [i18n],
            stubs: { 'lucide-vue-next': LucideStub },
        },
    })
    return { wrapper, container }
}

describe('ConnectionOverlay', () => {
    let activeContainer: HTMLDivElement | null = null
    let activeWrapper: ReturnType<typeof mountOverlay>['wrapper'] | null = null

    beforeEach(() => {
        mockMode.value = null
        // Clear anything a previous case left behind BEFORE this one mounts.
        // `$()` queries document.body, and the component teleports there, so a
        // leftover overlay would be the FIRST match — the assertions below would
        // then be looking at a dead instance's DOM while the live one behaves
        // correctly. afterEach already unmounts; this is the belt to that
        // braces, because a case that throws skips part of its cleanup.
        document.body.querySelectorAll('.connection-overlay').forEach(el => el.remove())
        document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
    })

    afterEach(() => {
        // `unmount()` first, not just DOM removal. The component teleports to
        // <body>; removing the elements by hand left the Vue instance alive and
        // its queued renders pending, so a later case could observe an overlay
        // that its own state says should be gone. Unmounting tears down the
        // teleport and the reactive watchers with it.
        activeWrapper?.unmount()
        activeWrapper = null
        document.body.querySelectorAll('.connection-overlay').forEach(el => el.remove())
        document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
        if (activeContainer?.parentNode) {
            document.body.removeChild(activeContainer)
            activeContainer = null
        }
    })

    async function mountAndWait() {
        const mounted = mountOverlay()
        activeContainer = mounted.container
        activeWrapper = mounted.wrapper
        await nextTick()
        return mounted.wrapper
    }

    it('renders nothing when mode is null', async () => {
        mockMode.value = null
        await mountAndWait()
        expect($('.connection-overlay')).toBeNull()
    })

    it('renders reconnect mask with server icon, spinner and text', async () => {
        mockMode.value = 'reconnect'
        await mountAndWait()
        const overlay = $('.connection-overlay')
        expect(overlay).not.toBeNull()
        expect($('.connection-overlay__icon')).not.toBeNull()
        expect($('.connection-overlay__spinner')).not.toBeNull()
        expect($('.connection-overlay__text')?.textContent).toContain('连接断开，正在重连…')
    })

    it('renders restart mask immediately with restart text, taking priority', async () => {
        mockMode.value = 'restart'
        await mountAndWait()
        const overlay = $('.connection-overlay')
        expect(overlay).not.toBeNull()
        expect($('.connection-overlay__text')?.textContent).toContain('正在重启，请稍候…')
    })

    it('shows the reconnect overlay only while mode is reconnect', async () => {
        mockMode.value = 'reconnect'
        await mountAndWait()
        expect($('.connection-overlay')).not.toBeNull()

        // Mount a second instance with a null mode and verify it stays hidden.
        mockMode.value = null
        const second = mountOverlay()
        await nextTick()
        expect(second.wrapper.find('.connection-overlay').exists()).toBe(false)
        if (second.container.parentNode) document.body.removeChild(second.container)
    })

    // The mask covers the whole screen including the header's server button, so
    // without this escape hatch a reconnect that never succeeds leaves the user
    // with no way to reach another server.
    // 20 s budget: the default 5 s is not enough on a CI runner running three
    // vitest workers — the case died there at 5041 ms with a bare "test timed
    // out", which says nothing about which assertion was waiting.
    it('offers a switch-server escape hatch while reconnecting', { timeout: 20_000 }, async () => {
        mockMode.value = 'reconnect'
        const wrapper = await mountAndWait()

        const btn = $('.connection-overlay__switch')
        expect(btn).not.toBeNull()
        expect(btn?.textContent).toContain('切换服务端')

        // Wrap the element in VTU's DOMWrapper instead of calling the native
        // `el.click()`: `trigger` awaits the render flush, so the assertions
        // below see the post-click state instead of racing it. (The element
        // cannot be reached via `wrapper.find` — the component teleports to
        // <body>, outside the wrapper's root.)
        await new DOMWrapper(btn!).trigger('click')

        // The dialog is up and the mask is out of the way (it would otherwise
        // paint above the dialog at --z-popover).
        //
        // Polled rather than asserted after a single `nextTick()`: a native
        // `click()` schedules a Vue update that a busy runner can flush later
        // than one tick, and the bare assertion then failed on CI (job
        // 128956975616, "expected <div .connection-overlay> to be null") while
        // passing on an idle machine. Waiting for the condition is the point —
        // the overlay disappearing IS the behaviour under test.
        // Polled instead of asserted after one `nextTick()`: a native `click()`
        // schedules a Vue update that a loaded runner can flush later than one
        // tick, and the bare assertion failed on CI while passing locally.
        //
        // Explicit 10 s: vitest's `expect.poll` defaults to 1 s, which a loaded
        // runner exceeds (CI measured 1142 ms for this case's work). The case
        // itself is given a 20 s budget above, so this stays inside it and a
        // genuine failure still reports the ASSERTION rather than an opaque
        // "test timed out".
        await expect.poll(() => $('.connection-overlay'), { timeout: 10_000 }).toBeNull()
        const modal = $('.modal-overlay')
        expect(modal).not.toBeNull()
        expect(modal!.style.display).not.toBe('none')

        // Closing the dialog brings the mask back while still disconnected.
        const dialog = wrapper.findComponent(ServerSwitchDialog)
        dialog.vm.$emit('close')
        await nextTick()
        expect($('.modal-overlay')!.style.display).toBe('none')
        expect($('.connection-overlay')).not.toBeNull()
    })

    it('does not offer the switch-server escape hatch while restarting', async () => {
        mockMode.value = 'restart'
        await mountAndWait()
        expect($('.connection-overlay')).not.toBeNull()
        expect($('.connection-overlay__switch')).toBeNull()
    })
})
