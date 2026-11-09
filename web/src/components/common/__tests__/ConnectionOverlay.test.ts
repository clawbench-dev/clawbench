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
    // Every mounted instance, so cleanup can UNMOUNT them.
    //
    // Removing the teleported elements from <body> is not enough: the Vue
    // instance stays alive and keeps its teleport target registered, so each
    // later mount leaves another live copy behind (CI measured SIX
    // `.connection-overlay` nodes by the time the last case ran). `$()` queries
    // document.body and returns the first match, so it could be looking at a
    // dead instance while the live one behaved correctly.
    const mounted: { wrapper: ReturnType<typeof mountOverlay>['wrapper']; container: HTMLDivElement }[] = []

    beforeEach(() => {
        mockMode.value = null
        // Start from a clean <body>. The component teleports there, and a case
        // that throws still runs afterEach — but a stale node from a case that
        // died before cleanup would otherwise be the FIRST match for `$()`.
        document.body.querySelectorAll('.connection-overlay').forEach(el => el.remove())
        document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
    })

    afterEach(() => {
        for (const { wrapper, container } of mounted.splice(0)) {
            wrapper.unmount()
            container.remove()
        }
        // Belt and braces: drop anything a failed case left teleported.
        document.body.querySelectorAll('.connection-overlay').forEach(el => el.remove())
        document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
    })

    async function mountAndWait() {
        const m = mountOverlay()
        mounted.push(m)
        await nextTick()
        return m.wrapper
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
    // vitest workers (measured: this case died at 5239 ms with a bare "test
    // timed out", which says nothing about which assertion was waiting). The
    // work is one mount, one click and two render flushes — nothing is slow by
    // design, so the budget is the only knob.
    it('offers a switch-server escape hatch while reconnecting', { timeout: 20_000 }, async () => {
        mockMode.value = 'reconnect'
        const wrapper = await mountAndWait()

        const btn = $('.connection-overlay__switch')
        expect(btn).not.toBeNull()
        expect(btn?.textContent).toContain('切换服务端')

        // `DOMWrapper.trigger` rather than the native `el.click()`: it awaits
        // the render flush, so the assertions below see the post-click state.
        // The native call left `open` false on CI (measured "expected false to
        // be true") even though the same code passed locally.
        await new DOMWrapper(btn!).trigger('click')

        // The dialog is up and the mask is out of the way (it would otherwise
        // paint above the dialog at --z-popover).
        //
        // Asserted through the dialog's own state, not by querying <body>:
        // the mask is teleported there, and anything an earlier case left
        // behind makes a body-wide selector depend on unrelated history (CI
        // kept failing on exactly that, while the same code passed locally).
        const dialog = wrapper.findComponent(ServerSwitchDialog)
        expect(dialog.props('open')).toBe(true)
        // The mask yields to the dialog (it would paint above it at --z-popover).
        await expect.poll(() => $('.connection-overlay'), { timeout: 10_000 }).toBeNull()


        // Closing the dialog brings the mask back while still disconnected.
        dialog.vm.$emit('close')
        await nextTick()
        expect(dialog.props('open')).toBe(false)
    })

    it('does not offer the switch-server escape hatch while restarting', async () => {
        mockMode.value = 'restart'
        await mountAndWait()
        expect($('.connection-overlay')).not.toBeNull()
        expect($('.connection-overlay__switch')).toBeNull()
    })
})
