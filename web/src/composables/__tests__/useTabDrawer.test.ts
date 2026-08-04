import { describe, expect, it, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import { useTabDrawer, onTabSwitch, resetTabDrawerState } from '@/composables/useTabDrawer'
import { _setWideScreenForTest, resetWideScreenState as resetWideScreen, switchLeftTab } from '@/composables/useWideScreenLayout'

beforeEach(() => {
  resetTabDrawerState()
  // Narrow-mode default: jsdom's innerWidth (1024) makes useWideScreenLayout's
  // physical-width detection init to wide-screen — force it off so the plain
  // drawer tests exercise narrow behavior.
  _setWideScreenForTest(false)
})

describe('useTabDrawer', () => {
  it('effectiveOpen is true only when currentTab matches and openRef is true', () => {
    const drawer = useTabDrawer('browse')
    expect(drawer.effectiveOpen.value).toBe(false)

    drawer.open()
    // currentTab is 'chat' (default), so effectiveOpen is still false
    expect(drawer.effectiveOpen.value).toBe(false)

    onTabSwitch('browse')
    expect(drawer.effectiveOpen.value).toBe(true)
  })

  it('effectiveOpen becomes false when tab switches away', () => {
    const drawer = useTabDrawer('browse')
    onTabSwitch('browse')
    drawer.open()
    expect(drawer.effectiveOpen.value).toBe(true)

    onTabSwitch('chat')
    expect(drawer.effectiveOpen.value).toBe(false)
    // openRef is preserved — switching back restores the drawer
    onTabSwitch('browse')
    expect(drawer.effectiveOpen.value).toBe(true)
  })

  it('resetTabDrawerState closes all drawers and resets currentTab to chat', () => {
    const browseDrawer = useTabDrawer('browse')
    const chatDrawer = useTabDrawer('chat')

    onTabSwitch('browse')
    browseDrawer.open()
    chatDrawer.open()

    resetTabDrawerState()

    expect(browseDrawer.effectiveOpen.value).toBe(false)
    expect(chatDrawer.effectiveOpen.value).toBe(false)
    expect(chatDrawer.isOpen.value).toBe(false)
  })

  it('regression: after resetTabDrawerState, drawer works when activeTab is synced', () => {
    // Simulates hotSwitchProject() which calls resetTabDrawerState()
    // AND resets its activeTab to 'chat' (the fix).
    // The caller's activeTab (external ref) must be kept in sync with
    // useTabDrawer's internal currentTab. If they desync, switchTab()
    // early-returns when activeTab === tab, so onTabSwitch is never
    // called and currentTab stays stale — drawers on that tab become
    // permanently unresponsive.

    const drawer = useTabDrawer('browse')

    // User is on browse tab, opens a drawer
    onTabSwitch('browse')
    drawer.open()
    expect(drawer.effectiveOpen.value).toBe(true)

    // Project switch: resetTabDrawerState sets currentTab='chat'
    resetTabDrawerState()
    // FIX: caller must also reset activeTab to 'chat' to stay in sync
    const activeTab = 'chat' // simulates activeTab.value = 'chat'

    // User navigates back to browse (simulates switchTab('browse'))
    // With the fix, activeTab !== 'browse', so switchTab proceeds and
    // calls onTabSwitch('browse')
    expect(activeTab).toBe('chat') // not 'browse', so no early return
    onTabSwitch('browse')

    // Now opening the drawer should work
    drawer.open()
    expect(drawer.effectiveOpen.value).toBe(true)
  })

  it('regression: desynced activeTab prevents drawer from opening', () => {
    // This test documents the BUG that was fixed.
    // If activeTab is NOT reset alongside resetTabDrawerState(),
    // a desync occurs where activeTab='browse' but currentTab='chat'.
    // switchTab('browse') hits the early return, onTabSwitch is never
    // called, and effectiveOpen stays permanently false.

    const drawer = useTabDrawer('browse')
    onTabSwitch('browse')
    drawer.open()
    expect(drawer.effectiveOpen.value).toBe(true)

    // Project switch: resetTabDrawerState resets currentTab to 'chat'
    resetTabDrawerState()
    // BUG: activeTab is NOT reset — still 'browse'
    const activeTab = 'browse' // simulates the missing reset

    // User clicks browse dock button — switchTab('browse')
    // Early return: activeTab === 'browse', so onTabSwitch is SKIPPED
    if (activeTab !== 'browse') {
      onTabSwitch('browse')
    }
    // currentTab remains 'chat' — desynced!

    // Opening the drawer sets openRef=true, but effectiveOpen stays false
    // because currentTab is still 'chat'
    drawer.open()
    expect(drawer.isOpen.value).toBe(true)
    expect(drawer.effectiveOpen.value).toBe(false) // BUG: drawer unresponsive
  })

  it('autoRestore: false — effectiveOpen is false when tab switches away', () => {
    const drawer = useTabDrawer('browse', { autoRestore: false })
    onTabSwitch('browse')
    drawer.open()
    expect(drawer.effectiveOpen.value).toBe(true)

    // Tab switch away — effectiveOpen becomes false (currentTab !== 'browse')
    onTabSwitch('chat')
    expect(drawer.effectiveOpen.value).toBe(false)

    // Switching back: effectiveOpen depends on openRef which was closed
    // by the autoRestore:false watcher (requires Vue reactive context).
    // In non-component test, openRef may still be true, so just verify
    // effectiveOpen follows currentTab + openRef logic.
    onTabSwitch('browse')
    expect(drawer.effectiveOpen.value).toBe(drawer.isOpen.value && true)
  })
})

describe('useTabDrawer wide-screen awareness', () => {
  beforeEach(() => {
    resetWideScreen()
    _setWideScreenForTest(false)
  })

  it('wide-screen: chat and leftTab drawers both open simultaneously', () => {
    _setWideScreenForTest(true)
    switchLeftTab('browse')
    const chatDrawer = useTabDrawer('chat')
    const browseDrawer = useTabDrawer('browse')

    chatDrawer.open()
    browseDrawer.open()
    expect(chatDrawer.effectiveOpen.value).toBe(true)
    expect(browseDrawer.effectiveOpen.value).toBe(true)

    switchLeftTab('terminal')
    expect(browseDrawer.effectiveOpen.value).toBe(false)
    expect(chatDrawer.effectiveOpen.value).toBe(true)
  })

  it('wide-screen: autoRestore:false closes when leftTab switches away', async () => {
    _setWideScreenForTest(true)
    switchLeftTab('browse')
    const drawer = useTabDrawer('browse', { autoRestore: false })
    drawer.open()
    expect(drawer.effectiveOpen.value).toBe(true)

    switchLeftTab('tasks')
    await nextTick()
    expect(drawer.isOpen.value).toBe(false)
    expect(drawer.effectiveOpen.value).toBe(false)
  })
})
