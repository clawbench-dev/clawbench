import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import QuoteQuestionBar from '../QuoteQuestionBar.vue'
import { useVisualViewportInset } from '@/composables/useVisualViewportInset'

// Partial mock: the component now imports computeSelectionToolbarStyle, whose
// module imports useSettingsConfig — which pulls in @/i18n (createI18n). A
// whitelist mock that only returns `useI18n` makes createI18n undefined and the
// whole suite fails to load, so spread the real module and override useI18n.
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/utils/clipboard.ts', () => ({
  copyText: vi.fn().mockResolvedValue(true),
}))

/**
 * jsdom has no `visualViewport`. The composable reads it on install, so the
 * stub must exist before the component mounts; a single mutable object mirrors
 * the real API (listeners must survive metric changes).
 */
let vvState = { innerHeight: 800, height: 800, offsetTop: 0 }
function installVisualViewport(next?: Partial<typeof vvState>) {
  if (next) Object.assign(vvState, next)
  const vv = {
    get height() { return vvState.height },
    get offsetTop() { return vvState.offsetTop },
    get width() { return 400 },
    addEventListener: () => {},
    removeEventListener: () => {},
  }
  Object.defineProperty(window, 'visualViewport', { value: vv, configurable: true })
  Object.defineProperty(window, 'innerHeight', {
    get: () => vvState.innerHeight,
    configurable: true,
  })
}

const QUOTE = { text: 'build failed on main', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 }

function mountBar(props: Record<string, unknown> = {}) {
  return mount(QuoteQuestionBar, {
    props: { visible: true, quoteData: null, composerMode: false, ...props },
  })
}

beforeEach(() => {
  vvState = { innerHeight: 800, height: 800, offsetTop: 0 }
  installVisualViewport()
  // The component only READS the inset refs; App.vue owns the install. The
  // tests must therefore install it themselves or `measure()` short-circuits
  // and the value never leaves 0 (a silently passing test).
  useVisualViewportInset().install()
})

afterEach(() => {
  const { uninstall, bottomInset } = useVisualViewportInset()
  uninstall()
  // The composable is a module singleton; leaving a value set would leak into
  // the next test (and into other suites sharing the module registry).
  expect(bottomInset.value).toBe(0)
  // @ts-expect-error — drop the stub so other suites see the jsdom default
  delete window.visualViewport
})

describe('QuoteQuestionBar', () => {
  describe('composer mode (no quote yet)', () => {
    it('renders the input even though there is no quote', () => {
      // Regression: the render gate was `visible && quoteData`, so a composer
      // opened with no selection rendered nothing at all.
      const wrapper = mountBar({ composerMode: true, quoteData: null })

      expect(wrapper.find('.quote-question-bar').exists()).toBe(true)
      expect(wrapper.find('.qq-textarea').exists()).toBe(true)
    })

    it('shows no quoted snippet when there is nothing selected', () => {
      const wrapper = mountBar({ composerMode: true, quoteData: null })

      expect(wrapper.find('.qq-quoted-snippet').exists()).toBe(false)
    })

    it('skips the collapsed toolbar and goes straight to the input', () => {
      const wrapper = mountBar({ composerMode: true, quoteData: null })

      expect(wrapper.find('.quote-bar-toolbar').exists()).toBe(false)
      expect(wrapper.find('.quote-bar-expanded').exists()).toBe(true)
    })

    it('renders the snippet once a selection is captured', () => {
      const wrapper = mountBar({ composerMode: true, quoteData: QUOTE })

      expect(wrapper.find('.qq-quoted-snippet').exists()).toBe(true)
      expect(wrapper.text()).toContain('build failed on main')
    })

    it('pins the bar on open so it survives selection loss', () => {
      const wrapper = mountBar({ composerMode: true })
      expect(wrapper.emitted('pin')).toBeTruthy()
    })

    it('emits send with the typed text', async () => {
      const wrapper = mountBar({ composerMode: true })
      await wrapper.find('.qq-textarea').setValue('why is this broken?')
      await wrapper.find('.qq-send-btn').trigger('click')

      expect(wrapper.emitted('send')).toEqual([['why is this broken?', { pickTarget: false }]])
    })

    it('emits add (not a chat-input injection) with the typed note', async () => {
      // The "+" action now produces ONE card carrying the quoted content plus
      // this text as the annotation — the bar only emits; the composable decides
      // what to do with it, and must not inject it into the chat textarea.
      const wrapper = mountBar({ composerMode: true, quoteData: QUOTE })
      await wrapper.find('.qq-textarea').setValue('please explain')
      await wrapper.find('.qq-add-btn').trigger('click')

      expect(wrapper.emitted('add')).toEqual([['please explain', { pickTarget: false }]])
      expect(wrapper.emitted('send')).toBeFalsy()
    })

    it('does not render at all when not visible', () => {
      const wrapper = mountBar({ composerMode: true, visible: false })
      expect(wrapper.find('.quote-question-bar').exists()).toBe(false)
    })

    describe('pending attachment chip', () => {
      it('renders the URL the composer will attach', () => {
        const wrapper = mountBar({
          composerMode: true,
          composerAttachment: { kind: 'url', label: 'acme/widgets#7', url: 'https://github.com/acme/widgets/issues/7' },
        })

        const chip = wrapper.find('.qq-pending-attachment')
        expect(chip.exists()).toBe(true)
        expect(chip.text()).toContain('acme/widgets#7')
      })

      it('renders a local file attachment with the file icon', () => {
        const wrapper = mountBar({
          composerMode: true,
          composerAttachment: { kind: 'file', label: 'main.ts', path: '/proj/src/main.ts' },
        })

        const chip = wrapper.find('.qq-pending-attachment')
        expect(chip.exists()).toBe(true)
        expect(chip.text()).toContain('main.ts')
        expect(chip.find('.lucide-link').exists()).toBe(false)
      })

      it('offers no remove button — the chip is not in the chat context yet', () => {
        // Dismissing the bar is the way to discard it; a close button here would
        // imply the entry already exists somewhere it could be removed from.
        const wrapper = mountBar({
          composerMode: true,
          composerAttachment: { kind: 'url', label: 'acme/widgets#7', url: 'https://github.com/acme/widgets/issues/7' },
        })

        expect(wrapper.find('.qq-pending-attachment .attachment-close-btn').exists()).toBe(false)
        expect(wrapper.find('.qq-pending-attachment button').exists()).toBe(false)
      })

      it('is absent in the normal selection flow', () => {
        const wrapper = mountBar({ composerMode: false, quoteData: QUOTE })

        expect(wrapper.find('.qq-pending-attachment').exists()).toBe(false)
      })

      it('is absent in composer mode with no attachment', () => {
        const wrapper = mountBar({ composerMode: true, composerAttachment: null })

        expect(wrapper.find('.qq-pending-attachment').exists()).toBe(false)
      })
    })
  })

  describe('collapsed floating toolbar (issue #544)', () => {
    it('renders the copy / comment / quote actions for a quote', () => {
      const wrapper = mountBar({ quoteData: QUOTE })

      const actions = wrapper.findAll('.quote-bar-action')
      expect(actions).toHaveLength(3)
      // Copy is the shared CopyButton (so the global is-copied tint applies);
      // comment and quote are plain buttons. Labels are i18n keys under the
      // test's identity `t`.
      expect(actions[0].classes()).toContain('copy-btn')
      expect(actions[1].text()).toBe('quoteBar.comment')
      expect(actions[2].text()).toBe('quoteBar.quoteSelection')
      // The collapsed state has no input until the user chooses "comment".
      expect(wrapper.find('.qq-textarea').exists()).toBe(false)
    })

    it('renders nothing without a quote when not in composer mode', () => {
      const wrapper = mountBar({ composerMode: false, quoteData: null })
      expect(wrapper.find('.quote-question-bar').exists()).toBe(false)
    })

    it('expands to the input when the comment action is clicked', async () => {
      const wrapper = mountBar({ quoteData: QUOTE })
      await wrapper.findAll('.quote-bar-action')[1].trigger('click')
      await wrapper.vm.$nextTick()

      expect(wrapper.find('.qq-textarea').exists()).toBe(true)
    })

    it('emits add (quote) from the collapsed toolbar without opening the input', async () => {
      const wrapper = mountBar({ quoteData: QUOTE })
      await wrapper.findAll('.quote-bar-action')[2].trigger('click')

      expect(wrapper.emitted('add')).toEqual([['', { pickTarget: false }]])
      // Quote stages the card straight away — it must not also open the input.
      expect(wrapper.find('.qq-textarea').exists()).toBe(false)
    })

    it('emits close when the copy action is clicked', async () => {
      // Both commit actions dismiss the toolbar: the user is done with the
      // selection, so leaving it floating over the text would be in the way.
      const wrapper = mountBar({ quoteData: QUOTE })
      await wrapper.findAll('.quote-bar-action')[0].trigger('click')

      expect(wrapper.emitted('close')).toBeTruthy()
    })

    it('emits add with the typed note after expanding via comment', async () => {
      const wrapper = mountBar({ quoteData: QUOTE })
      await wrapper.findAll('.quote-bar-action')[1].trigger('click')
      await wrapper.vm.$nextTick()
      await wrapper.find('.qq-textarea').setValue('please fix')
      await wrapper.find('.qq-add-btn').trigger('click')

      expect(wrapper.emitted('add')).toEqual([['please fix', { pickTarget: false }]])
    })

    it('pins on pointerdown so a click does not lose the selection', async () => {
      const wrapper = mountBar({ quoteData: QUOTE })
      await wrapper.find('.quote-bar-toolbar').trigger('pointerdown')

      expect(wrapper.emitted('pin')).toBeTruthy()
    })
  })

  describe('floating anchor (issue #544)', () => {
    const ANCHOR = { top: 300, bottom: 320, left: 400, right: 600 }

    it('applies the floating position and anchored class when an anchor is given', () => {
      const wrapper = mountBar({ quoteData: QUOTE, anchor: ANCHOR })

      expect(wrapper.find('.quote-question-bar--anchored').exists()).toBe(true)
      const style = wrapper.find('.quote-question-bar').attributes('style') || ''
      expect(style).toContain('position: fixed')
      expect(style).toContain('left:')
    })

    it('keeps the pinned top position (no inline style, no anchored class) without an anchor', () => {
      // The fallback that preserves the old behaviour for terminal / CodeMirror
      // / OpenAPI-iframe selections, which have no measurable DOM selection.
      const wrapper = mountBar({ quoteData: QUOTE, anchor: null })

      expect(wrapper.find('.quote-question-bar--anchored').exists()).toBe(false)
      const style = wrapper.find('.quote-question-bar').attributes('style') || ''
      expect(style).not.toContain('position: fixed')
    })
  })

  /**
   * Soft keyboard. Fixed offsets come from the LAYOUT viewport, which a mobile
   * browser does not shrink for the keyboard — so the bar must be told how much
   * is hidden, or it stays behind the keyboard (the reported bug).
   */
  describe('soft keyboard keeps the bar in the visible viewport', () => {
    const ANCHOR = { top: 600, bottom: 620, left: 400, right: 600 }

    it('repositions the bar when the keyboard shrinks the visible viewport', async () => {
      // No keyboard: the selection sits low, so the bar goes ABOVE it.
      const wrapper = mountBar({ quoteData: QUOTE, anchor: ANCHOR })
      const before = wrapper.find('.quote-question-bar').attributes('style') || ''
      expect(before).toContain('bottom:')

      // Keyboard opens and hides the bottom 700px of the layout viewport — the
      // selection (and any bar above it) is now behind the keyboard. The visible
      // band is [0, 100].
      const { bottomInset } = useVisualViewportInset()
      installVisualViewport({ innerHeight: 800, height: 100 })
      window.dispatchEvent(new Event('resize'))
      await wrapper.vm.$nextTick()

      expect(bottomInset.value).toBe(700)
      const after = wrapper.find('.quote-question-bar').attributes('style') || ''
      // It must NOT still be anchored above the selection (that `bottom` value
      // was measured from the layout bottom and is behind the keyboard).
      expect(after).not.toContain('bottom:')
      // It is clamped inside the visible band: jsdom reports offsetHeight 0, so
      // the component uses its 32px fallback and the band is 100px tall — the
      // lowest legal top is therefore 100 - 32 = 68.
      expect(after).toContain('top: 68px')
    })

    it('pins the bar to the visible bottom when it is taller than the visible band', async () => {
      // A tall expanded panel: give the component a real height by stubbing
      // offsetHeight, since jsdom reports 0 for every element.
      const wrapper = mountBar({ quoteData: QUOTE, anchor: ANCHOR, composerMode: true })
      Object.defineProperty(wrapper.find('.quote-question-bar').element, 'offsetHeight', {
        value: 260, configurable: true,
      })

      installVisualViewport({ innerHeight: 800, height: 150 })
      window.dispatchEvent(new Event('resize'))
      await wrapper.vm.$nextTick()
      // Force a re-measure now that the height is stubbable.
      await wrapper.setProps({ visible: true })

      const after = wrapper.find('.quote-question-bar').attributes('style') || ''
      // Band is 150px, the panel is 260px: pin to the visible bottom (== inset)
      // so the input row — the panel's LAST child — sits just above the keyboard.
      expect(after).toContain('bottom: 650px')
    })

    /**
     * The textarea auto-grows as the user types, so the bar's height changes
     * without any reactive prop changing. The clamp's fit decision reads that
     * height, so a stale value would let the input row cross into the keyboard.
     * A watcher cannot cover this (the growth is a DOM style write), which is
     * why a ResizeObserver exists — pinned here so removing it fails.
     */
    it('re-measures when the bar itself resizes (textarea grew)', async () => {
      // Capture the observer so the test can fire it, since the jsdom polyfill
      // in test-setup.ts is a no-op.
      const observers: Array<{ cb: () => void }> = []
      const Original = globalThis.ResizeObserver
      globalThis.ResizeObserver = class {
        cb: () => void
        constructor(cb: () => void) { this.cb = cb; observers.push(this) }
        observe() {}
        unobserve() {}
        disconnect() {}
      } as unknown as typeof globalThis.ResizeObserver

      try {
        const wrapper = mountBar({ quoteData: QUOTE, anchor: ANCHOR, composerMode: true })
        await wrapper.vm.$nextTick()
        expect(observers.length).toBeGreaterThan(0)

        // The user types → the textarea grows → the bar is 400px tall, taller
        // than the 300px visible band (inset 500), so it must pin to the
        // visible bottom. Before the observer fires, the component still holds
        // the old (32px fallback) height and would place it differently.
        installVisualViewport({ innerHeight: 800, height: 300 })
        window.dispatchEvent(new Event('resize'))
        Object.defineProperty(wrapper.find('.quote-question-bar').element, 'offsetHeight', {
          value: 400, configurable: true,
        })
        observers.forEach((o) => o.cb())
        await wrapper.vm.$nextTick()

        const style = wrapper.find('.quote-question-bar').attributes('style') || ''
        expect(style).toContain('bottom: 500px')

        wrapper.unmount()
      } finally {
        globalThis.ResizeObserver = Original
      }
    })

    it('leaves the position untouched when there is no keyboard', async () => {
      const wrapper = mountBar({ quoteData: QUOTE, anchor: ANCHOR })
      const before = wrapper.find('.quote-question-bar').attributes('style') || ''

      window.dispatchEvent(new Event('resize'))
      await wrapper.vm.$nextTick()

      const after = wrapper.find('.quote-question-bar').attributes('style') || ''
      expect(after).toBe(before)
    })

    /**
     * Android (adjustResize) shrinks the LAYOUT viewport, so the inset correctly
     * stays 0 — but the bar is still positioned against `window.innerHeight`,
     * which `getZoomedViewport()` reads non-reactively. Without a version counter
     * the computed would not re-run and a `bottom`-anchored bar would keep an
     * offset from the old, taller viewport, drifting off the top of the screen
     * when the keyboard opens. This is the user's reported case ("视口布局会变").
     */
    it('recomputes under Android adjustResize (layout height shrinks, inset stays 0)', async () => {
      // No keyboard yet: viewport 800 tall, selection at 600 → bar goes above.
      const wrapper = mountBar({ quoteData: QUOTE, anchor: ANCHOR })
      const before = wrapper.find('.quote-question-bar').attributes('style') || ''
      expect(before).toContain('bottom: 208px') // 800 - 600 + 8

      // adjustResize: the LAYOUT viewport shrinks and the visual one follows, so
      // bottomInset stays 0. Only innerHeight moved.
      installVisualViewport({ innerHeight: 400, height: 400 })
      window.dispatchEvent(new Event('resize'))
      await wrapper.vm.$nextTick()

      const after = wrapper.find('.quote-question-bar').attributes('style') || ''
      // Recomputed against the NEW viewport height: 400 - 600 + 8 would be
      // negative, so the selection is now below the visible band and the bar is
      // clamped instead of keeping the stale 208px.
      expect(after).not.toBe(before)
      expect(after).not.toContain('bottom: 208px')
    })
  })
})
