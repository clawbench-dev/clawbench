import { describe, expect, it, vi, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useTabDrawer, onTabSwitch, resetTabDrawerState } from '@/composables/useTabDrawer'

// Reset global state between tests
afterEach(() => {
  resetTabDrawerState()
})

describe('useTabDrawer', () => {
  it('registers drawer and returns effectiveOpen computed', async () => {
    const openRef = ref(false)
    const drawer = useTabDrawer('browse', openRef)

    // Initially currentTab is 'chat', so effectiveOpen is false
    expect(drawer.effectiveOpen.value).toBe(false)

    // Switch to browse tab
    onTabSwitch('browse')
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(false) // openRef is still false

    // Open the drawer
    openRef.value = true
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true)
  })

  it('effectiveOpen becomes false when tab switches away but openRef is preserved', async () => {
    const openRef = ref(false)
    const drawer = useTabDrawer('browse', openRef)

    onTabSwitch('browse')
    openRef.value = true
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true)

    // Switch away from browse — effectiveOpen becomes false but openRef is preserved
    onTabSwitch('chat')
    await nextTick()
    expect(openRef.value).toBe(true)   // preserved — not force-closed
    expect(drawer.effectiveOpen.value).toBe(false) // visually hidden via computed

    // Switch back — drawer re-opens automatically
    onTabSwitch('browse')
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true)
  })

  it('effectiveOpen guards against opening on wrong tab', async () => {
    const openRef = ref(false)
    const drawer = useTabDrawer('terminal', openRef)

    // We're on 'chat' tab (default)
    onTabSwitch('chat')
    openRef.value = true // Someone sets it to true while on wrong tab
    await nextTick()
    // effectiveOpen should be false because currentTab !== 'terminal'
    expect(drawer.effectiveOpen.value).toBe(false)
  })

  it('open() and close() set the ref directly', () => {
    const openRef = ref(false)
    const drawer = useTabDrawer('browse', openRef)

    drawer.open()
    expect(openRef.value).toBe(true)

    drawer.close()
    expect(openRef.value).toBe(false)
  })
})

describe('onTabSwitch', () => {
  it('preserves openRef values; effectiveOpen handles visibility', async () => {
    const browseDrawer = ref(false)
    const chatDrawer = ref(false)
    const terminalDrawer = ref(false)

    const browse = useTabDrawer('browse', browseDrawer)
    const chat = useTabDrawer('chat', chatDrawer)
    const terminal = useTabDrawer('terminal', terminalDrawer)

    // Open all drawers
    browseDrawer.value = true
    chatDrawer.value = true
    terminalDrawer.value = true

    // Switch to chat — openRefs are preserved, effectiveOpen hides non-chat drawers
    onTabSwitch('chat')
    await nextTick()

    expect(browseDrawer.value).toBe(true)   // preserved
    expect(chatDrawer.value).toBe(true)     // preserved
    expect(terminalDrawer.value).toBe(true) // preserved

    expect(browse.effectiveOpen.value).toBe(false)  // visually hidden
    expect(chat.effectiveOpen.value).toBe(true)     // visible
    expect(terminal.effectiveOpen.value).toBe(false) // visually hidden

    // Switch back to browse — browse drawer re-appears
    onTabSwitch('browse')
    await nextTick()
    expect(browse.effectiveOpen.value).toBe(true)
    expect(chat.effectiveOpen.value).toBe(false)
  })

  it('handles switching to a tab with no registered drawers', async () => {
    const chatDrawer = ref(true)
    const chat = useTabDrawer('chat', chatDrawer)

    // Switch to 'settings' which has no drawers — openRef preserved
    onTabSwitch('settings')
    await nextTick()

    expect(chatDrawer.value).toBe(true)           // preserved
    expect(chat.effectiveOpen.value).toBe(false)  // visually hidden
  })
})

describe('resetTabDrawerState', () => {
  it('closes all drawers and resets currentTab to chat', async () => {
    const drawer1 = ref(true)
    const drawer2 = ref(true)
    useTabDrawer('browse', drawer1)
    useTabDrawer('terminal', drawer2)

    onTabSwitch('terminal')
    await nextTick()

    resetTabDrawerState()

    expect(drawer1.value).toBe(false)
    expect(drawer2.value).toBe(false)
  })
})
