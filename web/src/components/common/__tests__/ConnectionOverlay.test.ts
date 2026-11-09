import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
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
    // later mount leaves another live copy behind. CI showed SIX
    // `.connection-overlay` nodes by the time this case ran (measured with a
    // temporary log), and `$()` returns the first — a dead instance's DOM — so
    // clicking the live one looked like it had no effect.
    const mounted: { wrapper: ReturnType<typeof mountOverlay>['wrapper']; container: HTMLDivElement }[] = []

    beforeEach(() => {
        mockMode.value = null
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
    it('offers a switch-server escape hatch while reconnecting', async () => {
        mockMode.value = 'reconnect'
        const wrapper = await mountAndWait()

        const btn = $('.connection-overlay__switch')
        expect(btn).not.toBeNull()
        expect(btn?.textContent).toContain('切换服务端')

        btn?.click()
        await nextTick()

        // The dialog is up and the mask is out of the way (it would otherwise
        // paint above the dialog at --z-popover).
        const modal = $('.modal-overlay')
        expect(modal).not.toBeNull()
        expect(modal!.style.display).not.toBe('none')
        expect($('.connection-overlay')).toBeNull()

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
