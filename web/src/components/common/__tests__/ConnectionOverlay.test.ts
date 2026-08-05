import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, type Ref } from 'vue'
import ConnectionOverlay from '@/components/common/ConnectionOverlay.vue'
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

    beforeEach(() => {
        mockMode.value = null
    })

    afterEach(() => {
        document.body.querySelectorAll('.connection-overlay').forEach(el => el.remove())
        if (activeContainer?.parentNode) {
            document.body.removeChild(activeContainer)
            activeContainer = null
        }
    })

    async function mountAndWait() {
        const mounted = mountOverlay()
        activeContainer = mounted.container
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
})
