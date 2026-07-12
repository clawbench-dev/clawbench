import { describe, expect, it, vi, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useTabDrawer, onTabSwitch, resetTabDrawerState } from '@/composables/useTabDrawer'

// Suppress dev-mode legacy warnings in test output
vi.stubGlobal('import.meta', { env: { DEV: false } })

// Reset global state between tests
afterEach(() => {
  resetTabDrawerState()
})

describe('useTabDrawer (new API)', () => {
  it('creates internal ref; open()/close()/toggle() work', async () => {
    const drawer = useTabDrawer('browse')

    expect(drawer.isOpen.value).toBe(false)
    expect(drawer.effectiveOpen.value).toBe(false)

    drawer.open()
    expect(drawer.isOpen.value).toBe(true)

    drawer.close()
    expect(drawer.isOpen.value).toBe(false)

    drawer.toggle()
    expect(drawer.isOpen.value).toBe(true)

    drawer.toggle()
    expect(drawer.isOpen.value).toBe(false)
  })

  it('effectiveOpen reacts to tab switch and preserves isOpen', async () => {
    const drawer = useTabDrawer('browse')

    onTabSwitch('browse')
    drawer.open()
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true)
    expect(drawer.isOpen.value).toBe(true)

    // Switch away — effectiveOpen becomes false but isOpen is preserved
    onTabSwitch('chat')
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(false)
    expect(drawer.isOpen.value).toBe(true) // preserved!

    // Switch back — drawer re-opens automatically
    onTabSwitch('browse')
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true)
  })

  it('effectiveOpen guards against opening on wrong tab', async () => {
    const drawer = useTabDrawer('terminal')

    onTabSwitch('chat')
    drawer.open()
    await nextTick()
    // effectiveOpen is false because currentTab !== 'terminal'
    expect(drawer.effectiveOpen.value).toBe(false)
    expect(drawer.isOpen.value).toBe(true) // ref is true but visually hidden
  })

  it('autoRestore: false closes drawer on tab switch away', async () => {
    const drawer = useTabDrawer('settings', { autoRestore: false })

    onTabSwitch('settings')
    drawer.open()
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true)

    // Switch away — autoRestore:false closes the ref
    onTabSwitch('chat')
    await nextTick()
    expect(drawer.isOpen.value).toBe(false) // closed, not preserved
    expect(drawer.effectiveOpen.value).toBe(false)

    // Switch back — drawer does NOT auto-restore
    onTabSwitch('settings')
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(false)
  })

  it('autoRestore: true (default) preserves drawer on tab switch away', async () => {
    const drawer = useTabDrawer('settings', { autoRestore: true })

    onTabSwitch('settings')
    drawer.open()
    await nextTick()

    onTabSwitch('chat')
    await nextTick()
    expect(drawer.isOpen.value).toBe(true) // preserved

    onTabSwitch('settings')
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true) // auto-restored
  })
})

describe('useTabDrawer (legacy API)', () => {
  it('registers drawer with external ref and returns effectiveOpen', async () => {
    const openRef = ref(false)
    const drawer = useTabDrawer('browse', openRef)

    onTabSwitch('browse')
    expect(drawer.effectiveOpen.value).toBe(false)

    openRef.value = true
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true)
  })

  it('preserves openRef on tab switch away', async () => {
    const openRef = ref(false)
    const drawer = useTabDrawer('browse', openRef)

    onTabSwitch('browse')
    openRef.value = true
    await nextTick()

    onTabSwitch('chat')
    await nextTick()
    expect(openRef.value).toBe(true)   // preserved
    expect(drawer.effectiveOpen.value).toBe(false) // visually hidden

    onTabSwitch('browse')
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true) // restored
  })
})

describe('onTabSwitch', () => {
  it('preserves open state; effectiveOpen handles visibility', async () => {
    const browse = useTabDrawer('browse')
    const chat = useTabDrawer('chat')
    const terminal = useTabDrawer('terminal')

    browse.open()
    chat.open()
    terminal.open()

    onTabSwitch('chat')
    await nextTick()

    expect(browse.isOpen.value).toBe(true)    // preserved
    expect(chat.isOpen.value).toBe(true)       // preserved
    expect(terminal.isOpen.value).toBe(true)   // preserved

    expect(browse.effectiveOpen.value).toBe(false)  // visually hidden
    expect(chat.effectiveOpen.value).toBe(true)     // visible
    expect(terminal.effectiveOpen.value).toBe(false) // visually hidden

    onTabSwitch('browse')
    await nextTick()
    expect(browse.effectiveOpen.value).toBe(true)
    expect(chat.effectiveOpen.value).toBe(false)
  })
})

describe('resetTabDrawerState', () => {
  it('closes all drawers and resets currentTab to chat', async () => {
    const drawer1 = useTabDrawer('browse')
    const drawer2 = useTabDrawer('terminal')

    drawer1.open()
    drawer2.open()

    onTabSwitch('terminal')
    await nextTick()

    resetTabDrawerState()

    expect(drawer1.isOpen.value).toBe(false)
    expect(drawer2.isOpen.value).toBe(false)
  })
})
