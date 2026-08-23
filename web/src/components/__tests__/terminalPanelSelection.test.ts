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
})
