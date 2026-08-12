import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { nextTick, ref, type Ref } from 'vue'
import { useMenuKeyboard } from '../useMenuKeyboard'

// jsdom does not implement scrollIntoView.
beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn()
})

// Track every opened menu so afterEach can close it (unbind its document
// listener). Without a real component unmount, listeners would otherwise leak
// across tests and, via stopImmediatePropagation, block later tests' handling.
const activeMenus: { isOpen: Ref<boolean> }[] = []

afterEach(async () => {
    activeMenus.forEach((m) => { m.isOpen.value = false })
    activeMenus.length = 0
    await nextTick()
})

function createPanel() {
    const panel = document.createElement('div')
    panel.className = 'app-menu'
    panel.innerHTML = `
        <div class="app-menu-title">Title</div>
        <div class="app-menu-item" data-item="0">Item 0</div>
        <div class="app-menu-item" data-item="1">Item 1</div>
        <div class="app-menu-item" data-item="2">Item 2</div>
        <div class="app-menu-item other-item" data-item="other">Browse</div>
    `
    document.body.appendChild(panel)
    return panel
}

async function openMenu(onConfirm?: (element: HTMLElement) => void) {
    const panelRef = ref<HTMLElement | null>(null)
    const isOpen = ref(false)
    useMenuKeyboard({ panelRef, isOpen, onConfirm })
    const panel = createPanel()
    panelRef.value = panel
    isOpen.value = true
    activeMenus.push({ isOpen })
    await nextTick()
    return { panel, isOpen }
}

function dispatch(target: EventTarget, key: string) {
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
}

describe('useMenuKeyboard', () => {
    it('navigates via document keydown when opened (no focus stealing)', async () => {
        const { panel } = await openMenu()

        expect(panel.getAttribute('tabindex')).toBeNull()

        dispatch(document, 'ArrowDown')
        expect(panel.querySelectorAll('.keyboard-hover')).toHaveLength(1)
        expect(panel.querySelector('.keyboard-hover')?.getAttribute('data-item')).toBe('0')
    })

    it('ArrowDown highlights the first item, then wraps to the next', async () => {
        const { panel } = await openMenu()

        dispatch(document, 'ArrowDown')
        expect(panel.querySelectorAll('.keyboard-hover')).toHaveLength(1)
        expect(panel.querySelector('.keyboard-hover')?.getAttribute('data-item')).toBe('0')

        dispatch(document, 'ArrowDown')
        expect(panel.querySelector('.keyboard-hover')?.getAttribute('data-item')).toBe('1')
    })

    it('ArrowUp from the first item wraps to the last item', async () => {
        const { panel } = await openMenu()

        dispatch(document, 'ArrowDown')
        dispatch(document, 'ArrowUp')
        // Wrapped around: up from first → last item
        expect(panel.querySelector('.keyboard-hover')?.getAttribute('data-item')).toBe('other')
    })

    it('ArrowUp with no highlight selects the last item', async () => {
        const { panel } = await openMenu()

        dispatch(document, 'ArrowUp')
        expect(panel.querySelector('.keyboard-hover')?.getAttribute('data-item')).toBe('other')
    })

    it('Enter clicks the highlighted item', async () => {
        const onConfirm = vi.fn()
        const { panel } = await openMenu(onConfirm)

        const clicked = vi.fn()
        const item = panel.querySelector('[data-item="1"]') as HTMLElement
        item.addEventListener('click', clicked)

        dispatch(document, 'ArrowDown')
        dispatch(document, 'ArrowDown')
        dispatch(document, 'Enter')

        expect(onConfirm).toHaveBeenCalledWith(item)
        expect(clicked).toHaveBeenCalledOnce()
    })

    it('Enter with no highlight confirms the first item', async () => {
        const onConfirm = vi.fn()
        const { panel } = await openMenu(onConfirm)

        const clicked = vi.fn()
        const first = panel.querySelector('[data-item="0"]') as HTMLElement
        first.addEventListener('click', clicked)

        dispatch(document, 'Enter')

        expect(onConfirm).toHaveBeenCalledWith(first)
        expect(clicked).toHaveBeenCalledOnce()
    })

    it('skips disabled and hidden items', async () => {
        const panelRef = ref<HTMLElement | null>(null)
        const isOpen = ref(false)
        useMenuKeyboard({ panelRef, isOpen })
        const panel = createPanel()
        const first = panel.querySelector('[data-item="0"]') as HTMLElement
        first.setAttribute('disabled', '')
        panelRef.value = panel
        isOpen.value = true
        activeMenus.push({ isOpen })
        await nextTick()

        dispatch(document, 'ArrowDown')
        expect(panel.querySelector('.keyboard-hover')?.getAttribute('data-item')).toBe('1')
    })

    it('Escape closes the menu', async () => {
        const { isOpen } = await openMenu()

        dispatch(document, 'Escape')
        expect(isOpen.value).toBe(false)
    })

    it('detaches the keydown listener when closed', async () => {
        const { panel, isOpen } = await openMenu()

        isOpen.value = false
        await nextTick()

        // After close, ArrowDown must no longer highlight anything.
        dispatch(document, 'ArrowDown')
        expect(panel.querySelector('.keyboard-hover')).toBeNull()
    })

    it('clears the highlight when reopened', async () => {
        const { panel, isOpen } = await openMenu()

        dispatch(document, 'ArrowDown')
        expect(panel.querySelector('.keyboard-hover')).not.toBeNull()

        isOpen.value = false
        await nextTick()
        isOpen.value = true
        await nextTick()

        expect(panel.querySelector('.keyboard-hover')).toBeNull()
    })

    it('ignores keys typed into an editable field', async () => {
        const { panel, isOpen } = await openMenu()

        const input = document.createElement('input')
        document.body.appendChild(input)

        dispatch(input, 'ArrowDown')
        expect(panel.querySelector('.keyboard-hover')).toBeNull()

        // Enter on the input is ignored too — the menu stays open.
        dispatch(input, 'Enter')
        expect(isOpen.value).toBe(true)
    })

    it('does nothing when the dropdown is closed', async () => {
        const { panel, isOpen } = await openMenu()
        isOpen.value = false
        await nextTick()

        dispatch(document, 'ArrowDown')
        expect(panel.querySelector('.keyboard-hover')).toBeNull()
    })

    it('stops propagation so a competing document listener does not react', async () => {
        const { panel } = await openMenu()

        // Simulate the file manager: a document-level bubble handler for the
        // same keys. It must NOT run while the dropdown handles the key.
        const competitor = vi.fn()
        document.addEventListener('keydown', competitor)

        // Keys are pressed on a descendant (e.g. a focused file item), so the
        // event travels document(capture) → target → document(bubble). Our
        // capture-phase handler must stop it before it reaches the bubble one.
        const target = document.createElement('div')
        document.body.appendChild(target)

        target.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }))
        target.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))

        expect(panel.querySelector('.keyboard-hover')?.getAttribute('data-item')).toBe('0')
        expect(competitor).not.toHaveBeenCalled()

        document.removeEventListener('keydown', competitor)
    })
})
