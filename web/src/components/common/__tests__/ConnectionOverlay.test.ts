import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, nextTick, type Ref } from 'vue'
import ConnectionOverlay from '@/components/common/ConnectionOverlay.vue'

// Ref holder read by the mocked composables AT CALL TIME. The refs are created with
// the same ESM `vue` instance the component uses, so cross-module reactivity works.
const holder = vi.hoisted(() => ({} as {
    wsStatusRef?: Ref<string>
    hasConnectedOnceRef?: Ref<boolean>
    restartingOverlayRef?: Ref<boolean>
}))

vi.mock('@/composables/useGlobalEvents', () => ({
    useGlobalEvents: () => ({
        wsStatus: holder.wsStatusRef,
        hasConnectedOnce: holder.hasConnectedOnceRef,
    }),
}))

vi.mock('@/composables/useSettingsNavigation', () => ({
    useSettingsNavigation: () => ({
        restartingOverlay: holder.restartingOverlayRef,
    }),
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
        holder.wsStatusRef = ref('connected')
        holder.hasConnectedOnceRef = ref(false)
        holder.restartingOverlayRef = ref(false)
        vi.useFakeTimers()
    })

    afterEach(() => {
        vi.useRealTimers()
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

    it('renders nothing while connected', async () => {
        holder.hasConnectedOnceRef!.value = true
        await mountAndWait()
        expect($('.connection-overlay')).toBeNull()
    })

    it('renders reconnect mask with server icon and text after delay', async () => {
        holder.hasConnectedOnceRef!.value = true
        await mountAndWait()
        holder.wsStatusRef!.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(1600)
        await nextTick()
        const overlay = $('.connection-overlay')
        expect(overlay).not.toBeNull()
        expect($('.connection-overlay__icon')).not.toBeNull()
        expect($('.connection-overlay__spinner')).not.toBeNull()
        expect($('.connection-overlay__text')?.textContent).toContain('连接断开，正在重连…')
    })

    it('does not render on cold start (never connected before)', async () => {
        await mountAndWait()
        holder.wsStatusRef!.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(1600)
        await nextTick()
        expect($('.connection-overlay')).toBeNull()
    })

    it('renders restart mask immediately with restart text, taking priority', async () => {
        holder.hasConnectedOnceRef!.value = true
        await mountAndWait()
        holder.restartingOverlayRef!.value = true
        holder.wsStatusRef!.value = 'reconnecting'
        await nextTick()
        const overlay = $('.connection-overlay')
        expect(overlay).not.toBeNull()
        expect($('.connection-overlay__text')?.textContent).toContain('正在重启，请稍候…')
    })

    it('hides the mask once connection is restored', async () => {
        holder.hasConnectedOnceRef!.value = true
        await mountAndWait()
        holder.wsStatusRef!.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(1600)
        await nextTick()
        expect($('.connection-overlay')).not.toBeNull()
        holder.wsStatusRef!.value = 'connected'
        await nextTick()
        expect($('.connection-overlay')).toBeNull()
    })
})
