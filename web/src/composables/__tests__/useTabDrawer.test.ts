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

  it('effectiveOpen becomes false when tab switches away and openRef is closed', async () => {
    const openRef = ref(false)
    const drawer = useTabDrawer('browse', openRef)

    onTabSwitch('browse')
    openRef.value = true
    await nextTick()
    expect(drawer.effectiveOpen.value).toBe(true)

    // Switch away from browse — onTabSwitch closes the drawer
    onTabSwitch('chat')
    await nextTick()
    expect(openRef.value).toBe(false)  // closed by onTabSwitch
    expect(drawer.effectiveOpen.value).toBe(false)
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
  it('closes all drawers not belonging to the new tab', () => {
    const browseDrawer = ref(false)
    const chatDrawer = ref(false)
    const terminalDrawer = ref(false)

    useTabDrawer('browse', browseDrawer)
    useTabDrawer('chat', chatDrawer)
    useTabDrawer('terminal', terminalDrawer)

    // Open all drawers
    browseDrawer.value = true
    chatDrawer.value = true
    terminalDrawer.value = true

    // Switch to chat — browse and terminal drawers should close
    onTabSwitch('chat')

    expect(browseDrawer.value).toBe(false)  // closed
    expect(chatDrawer.value).toBe(true)      // stays open
    expect(terminalDrawer.value).toBe(false) // closed
  })

  it('handles switching to a tab with no registered drawers', () => {
    const chatDrawer = ref(true)
    useTabDrawer('chat', chatDrawer)

    // Switch to 'settings' which has no drawers
    onTabSwitch('settings')

    expect(chatDrawer.value).toBe(false)  // closed because not matching
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
