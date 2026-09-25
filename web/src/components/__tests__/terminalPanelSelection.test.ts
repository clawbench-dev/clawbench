import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const terminalComponentPaths = [
  '../terminal/TerminalPanelContent.vue',
]

const readTerminalComponent = (path: string) => readFileSync(resolve(__dirname, path), 'utf8')
const readToolbarStyleBlock = (source: string) => {
  const start = source.indexOf('.terminal-toolbar {')
  const end = source.indexOf('</style>', start)

  return source.slice(start, end)
}

describe('TerminalPanel xterm selection defaults', () => {
  it('does not force xterm selection to line mode', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    expect(source).not.toContain("selectionStyle: 'line'")
  })

  it('renders config-driven toolbar with all keys visible in gesture mode', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // Toolbar is config-driven: keys rendered via v-for over visibleKeys
    expect(source).toContain('v-for="def in visibleKeys"')
    // Modifier keys still use toggle behavior with active/locked classes
    expect(source).toContain('toolbarBtnClass(def)')
    // Click handler dispatches via terminalKeys.send() or toggleModifier()
    expect(source).toContain('handleToolbarKeyClick(def)')
    // Keys are never hidden by gesture mode — visibleKeys always returns every key
    expect(source).not.toContain('GESTURE_HIDDEN_KEYS')
    expect(source).toMatch(/const visibleKeys = computed\(\(\) => selectedKeys\.value\)/)
    // Gesture inputs surface an on-screen hint overlay instead of hiding keys
    expect(source).toContain('onGestureHint')
    expect(source).toContain('class="gesture-hint"')
  })

  it('shows the gesture method hint when clicking a gesture-backed key in gesture mode', () => {    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // Gesture-backed keys map to i18n gesture-method labels (how to do it by gesture)
    expect(source).toContain('GESTURE_KEY_LABELS')
    expect(source).toContain("tab: 'gestureDoubleTap'")
    expect(source).toContain("arrow_up: 'gestureSwipeUp'")
    // handleToolbarKeyClick sends the key and, in gesture mode, shows the method hint
    expect(source).toContain('terminalKeys.send(def.id)')
    expect(source).toContain("gestures.mode.value === 'gesture'")
    expect(source).toContain("showGestureHint(t('terminal.' + labelKey))")
  })

  it('re-focuses the xterm textarea on blur to keep the soft keyboard open on tap', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // The Android WebView blurs the textarea before touchstart (uncancellable),
    // so the fix restores focus on blur instead of trying to preventDefault.
    expect(source).toContain("textareaEl.addEventListener('blur'")
    expect(source).toContain('shouldAutoRefocusTerminal(!!props.active, next)')
    // Only restores when focus fell to body/document (tap on the surface), and
    // defers focus() out of the blur dispatch via a microtask so the keyboard
    // never visibly collapses.
    expect(source).toContain('queueMicrotask(() => {')
    expect(source).toContain('ta.focus()')
  })

  it('provides a theme switcher button in the tab bar', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')
    expect(source).toContain('PaletteIcon')
    expect(source).toContain('openThemeMenu')
    expect(source).toContain("t('terminal.theme')")
  })

  it('theme popup lists Follow App Theme + theme ids', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')
    expect(source).toContain('themeFollowApp')
    expect(source).toContain('formatThemeName(id)')
    expect(source).toContain('THEME_IDS')
    // Theme search was intentionally removed (see commit 68c91379)
    expect(source).not.toContain('themeSearchPlaceholder')
    expect(source).not.toContain('filteredThemes')
  })

  it('keeps terminal virtual keys in a borderless, transparent overlay system', () => {
    for (const path of terminalComponentPaths) {
      const source = readTerminalComponent(path)
      const toolbarStyle = readToolbarStyleBlock(source)

      // Borderless: no border on buttons
      expect(toolbarStyle).toContain('border: none')
      // Transparent default background
      expect(toolbarStyle).toContain('background: transparent')
      // Hover/active use semi-transparent overlays
      expect(toolbarStyle).toContain('--toolbar-key-hover')
      expect(toolbarStyle).toContain('--toolbar-key-active')
      // Scroll fade instead of scrollbar
      expect(toolbarStyle).toContain('scrollbar-width: none')
      expect(toolbarStyle).toContain('scroll-fade')
      // No decorative masks or accent colors
      expect(toolbarStyle).not.toContain('var(--color-green)')
      expect(toolbarStyle).not.toContain('var(--color-yellow)')
      expect(toolbarStyle).not.toContain('var(--color-purple)')
    }
  })

  it('does not show the floating copy bar on PC (native selection/copy)', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // The selection copy bar is mobile-only: PC users get native Ctrl+C / right-click copy.
    // Gating on !isPC (same platform flag already used for the virtual key toolbar).
    // `autoCopyFailed` restores the bar when the deferred clipboard write was
    // rejected — without it, a WebView that denies the write would leave mobile
    // users with no copy affordance whatsoever.
    expect(source).toContain('v-if="selectionActive && !isPC && (!copyOnSelect || autoCopyFailed)"')
  })

  it('tracks auto-copy failure so the mobile copy bar can fall back to it', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    expect(source).toContain('const autoCopyFailed = ref(false)')
    // Both clipboard outcomes must be wired, otherwise a failed write would be
    // indistinguishable from a successful one.
    expect(source).toMatch(/autoCopyFailed\.value = false/)
    expect(source).toMatch(/autoCopyFailed\.value = true/)
    // A new selection is a fresh attempt: a stale failure must not pin the bar
    // open for the rest of the session.
    expect(source).toContain('if (text) autoCopyFailed.value = false')
  })

  it('copies the selection automatically when copy-on-select is on', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // The setting defaults on, so the gate must be an explicit opt-out
    // (undefined/true => on) rather than a truthiness check that would leave
    // existing installs without the persisted key on the old behaviour.
    expect(source).toContain("localConfig.terminalCopyOnSelect !== false")
    // The selection-change handler feeds the auto-copy hook.
    expect(source).toContain('autoCopy.onSelectionChanged(text)')
    // Auto-copy must not clear the selection: the user still needs to see what
    // was copied, and the right-click menu operates on the live selection.
    const updateFn = source.slice(
      source.indexOf('function updateSelectionFromTerm'),
      source.indexOf('/** Read the real CSS cell height'),
    )
    expect(updateFn).not.toContain('clearSelection')
    // A pending copy from the previous tab must be dropped on tab switch.
    expect(source).toContain('autoCopy.dispose()')
  })

  it('provides a help button that opens the terminal help drawer', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // Help icon button wired to the help drawer
    expect(source).toContain('CircleHelp as CircleHelpIcon')
    expect(source).toContain('CircleHelpIcon :size="14"')
    expect(source).toContain('helpDrawer.open()')
    // Drawer rendered with the open binding
    expect(source).toContain('TerminalHelpDrawer')
    expect(source).toContain(':open="helpDrawer.effectiveOpen.value"')
    expect(source).toContain('@close="helpDrawer.close()"')
    expect(source).toContain("const helpDrawer = useTabDrawer('terminal')")
  })

  it('uploads dropped OS files into the shell live cwd', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // The drop listeners are bound as an object so the whole set can be omitted
    // on platforms where the server cannot resolve a live cwd — with no correct
    // target directory, intercepting the drop would upload into the wrong place.
    expect(source).toContain('v-on="terminalDropHandlers"')
    expect(source).toContain("if (cwdProbeSupported.value !== true) return {}")
    // The live cwd is fetched per-drag from the status endpoint, not from the
    // tab's launch directory (which goes stale after `cd`).
    expect(source).toContain('getSessionId: () => activeTab.value?.sessionId')
    expect(source).toContain('getFallbackDir: () => activeTab.value?.cwd')
    // Reuse the shared overlay + progress bar rather than bespoke markup.
    expect(source).toContain('<DropOverlay :visible="terminalFileDrop.dropActive.value"')
    expect(source).toContain('<UploadProgressBar')
    expect(source).toContain('@cancel="cancelDirUpload"')
  })

  it('opens the shell current directory in the file manager from both toolbars', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // PC tab bar AND the mobile virtual-key toolbar each get the button, so the
    // action is reachable on every form factor.
    const pcButton = source.indexOf('class="terminal-tab-add"\n        @click="openCurrentDirInFileManager"')
    const mobileButton = source.indexOf('btn-func" @click="openCurrentDirInFileManager"')
    expect(pcButton).toBeGreaterThan(-1)
    expect(mobileButton).toBeGreaterThan(-1)

    // Reuses the shared directory-jump event rather than inventing a new emit,
    // and tags the source so Back returns to the terminal.
    expect(source).toContain("window.dispatchEvent(new CustomEvent('open-directory-from-context'")
    expect(source).toContain("source: 'terminal'")
    expect(source).toContain('FolderOpen as FolderOpenIcon')
  })

  it('resolves the cwd live on click instead of reading the stale tab record', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // A tab's `cwd` is written once from the one-shot WS status message at
    // connect time (the LAUNCH dir), so reading it made the button always open
    // the project root after a `cd`. It must be fetched on demand — the same
    // live source the drag-drop upload uses.
    expect(source).toContain('async function openCurrentDirInFileManager()')
    expect(source).toContain('await fetchTerminalCwd(activeTab.value?.sessionId)')
    expect(source).toContain("import { fetchTerminalCwd } from '@/utils/terminalCwd'")

    // Fallback chain: live value, else the launch dir — and never '' (which
    // would resolve to the project root, i.e. the bug being fixed).
    expect(source).toContain('const dir = live || activeTab.value?.cwd')
    expect(source).not.toContain('const dir = activeTab.value?.cwd')

    // The tab title comes from that same one-shot message, so refresh it too
    // when we happen to have the live value.
    expect(source).toContain('tabManager.updateTabCwd(activeTab.value.id, live)')
  })

  it('hides the open-directory buttons when the server cannot resolve a live cwd', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')

    // Strict `=== true` (not a truthy check): the ref starts as null while the
    // status request is in flight, and a truthy gate would flash the button on
    // macOS before it disappears.
    const gates = source.match(/v-if="cwdProbeSupported === true"/g) ?? []
    expect(gates).toHaveLength(2)

    // Guard against an empty cwd (tab not connected yet): dispatching '' would
    // navigate the file manager to the project root by accident.
    expect(source).toContain("if (!dir) {")
    expect(source).toContain("t('terminal.cwdUnavailable')")
  })
})
