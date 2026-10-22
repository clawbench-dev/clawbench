import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

/**
 * Wiring contract for "Ctrl/Cmd+Enter submits the ask card".
 *
 * Why a source guard instead of a mount test: the behaviour itself is covered
 * in `utils/__tests__/renderToolDetail.test.ts` (which dispatches real keydowns
 * at a real card). What can still go wrong — and what nothing else would
 * notice — is the binding. The ask card is rendered through `v-html` by two
 * hosts, each of which must forward the keystroke:
 *
 *   ContentBlocks.vue    the chat list (also the summary view, the text-tag
 *                        card, and nested sub-agent blocks)
 *   ToolDetailDrawer.vue the tool-detail sheet
 *
 * FileDiffsDrawer is deliberately NOT one of them: it only renders Write/Edit
 * diffs for one file (its blocks are filtered by tool name and file_path), so
 * no ask card can appear there.
 *
 * A missed host is silent: the shortcut simply does nothing on that surface,
 * and every behavioural test keeps passing because they all drive the handler
 * directly. `ContentBlocks.test.ts` mocks the whole renderToolDetail module, so
 * it cannot see this either.
 *
 * The other half of the contract is the scoping. The handler keys off the
 * event's target (DOM containment), NOT off "the newest card" — several cards
 * can be on screen at once and guessing would send the wrong answer. That only
 * holds if the binding lives on the card container itself.
 */
describe('ask-card submit shortcut wiring', () => {
  const HOSTS = [
    'src/components/chat/ContentBlocks.vue',
    'src/components/chat/ToolDetailDrawer.vue',
  ] as const

  it('every host that renders an ask card forwards the keystroke', () => {
    for (const rel of HOSTS) {
      const src = readWebFile(rel)
      expect(src, `${rel} must import handleAskSubmitShortcut`).toContain('handleAskSubmitShortcut')
      // Bound on the container that already carries the card's click/input
      // handlers — that is what scopes the keystroke to the card it happened in.
      expect(src, `${rel} must bind @keydown on the card container`).toMatch(/@keydown="handle\w*Keydown"/)
    }
  })

  it('the handler is scoped by the event target, not by focus or card order', () => {
    const src = readWebFile('src/utils/renderToolDetail.ts')
    const start = src.indexOf('export function handleAskSubmitShortcut(')
    expect(start, 'handleAskSubmitShortcut must exist').toBeGreaterThan(-1)
    const end = src.indexOf('\n}\n', start)
    const body = src.slice(start, end)

    // Target containment. `document.activeElement` would be wrong: clicking an
    // option leaves focus outside the card, so a focused-scoped shortcut would
    // stop working exactly after the user picked an answer.
    expect(body, 'must locate the card from the event target').toMatch(/target\?\.closest\?\.\('\.ask-question-view'\)/)
    expect(body, 'must not fall back to the globally focused element').not.toContain('activeElement')
    // Must be a no-op outside a card, or every Ctrl+Enter in the app is eaten.
    expect(body, 'must bail out when the keystroke is not in a card').toMatch(/if \(!view\) return false/)
  })

  it('both the click and the keystroke share one submit implementation', () => {
    // Two copies drift on the persistence step, and the card then comes back
    // answerable after a re-render (the bug the answer-state store exists to
    // prevent). Pin that the click handler delegates rather than re-implements.
    const src = readWebFile('src/utils/renderToolDetail.ts')
    const clickStart = src.indexOf("registerToolActionHandler('AskUserQuestion'")
    const clickEnd = src.indexOf('\n})\n', clickStart)
    const clickBody = src.slice(clickStart, clickEnd)
    expect(clickBody, 'the Submit click must delegate to submitAskView').toContain('submitAskView(view, emit)')
    // Only ONE place may persist `submitted: true` for the submit path.
    const occurrences = src.split("patchAskState(cardKey, { selected: readSelectedFromDom(view), submitted: true })").length - 1
    expect(occurrences, 'submitted:true must be persisted in exactly one place').toBe(1)
  })

  it('read-only (share) mode never dispatches the shortcut', () => {
    // The emit would try to POST an answer to an auth-protected endpoint from an
    // anonymous viewer — same reason handleToolDetailClick bails out there.
    const src = readWebFile('src/components/chat/ContentBlocks.vue')
    const start = src.indexOf('function handleToolDetailKeydown(')
    expect(start, 'ContentBlocks must own a keydown handler').toBeGreaterThan(-1)
    const end = src.indexOf('\n}\n', start)
    expect(src.slice(start, end), 'the keydown path must honour readOnly').toContain('if (props.readOnly) return')
  })
})

/**
 * The hint is plain muted text in the free space to the LEFT of the actions row.
 *
 * jsdom has no CSS engine, so placement and form cannot be asserted by
 * rendering — this is a source contract, same pattern as
 * askOptionIndicator.css.test.ts.
 *
 * Two decisions are pinned here, both from explicit user feedback:
 *   1. PLACEMENT — `.ask-question-actions` is `justify-content: flex-end`, so
 *      `margin-right: auto` parks the hint at the far left. Dropping it slides
 *      the hint over next to the buttons, the layout the user asked to move
 *      away from.
 *   2. FORM — it must read as text, not as a control. The bordered keycap box
 *      made it look clickable, and the chord glyphs it held (⌃↵) were too
 *      abstract to parse. A re-added border/background would silently restore
 *      the "clickable chip" look.
 */
describe('ask-submit hint placement and form', () => {
  const GLOBAL_CSS = readWebFile('css/components.css')

  /** Declarations of the first top-level `.ask-submit-kbd { … }` rule. */
  function kbdRule(): string {
    const re = /(?:^|\n)\.ask-submit-kbd\s*\{([^}]*)\}/
    const m = re.exec(GLOBAL_CSS)
    expect(m, '.ask-submit-kbd must be declared in the global sheet').not.toBeNull()
    return m![1]
  }

  it('is pushed to the left edge of the right-aligned actions row', () => {
    // `margin-right: auto` on a flex item is what consumes the free space; no
    // other property achieves "left edge of a flex-end row".
    expect(kbdRule()).toMatch(/margin-right:\s*auto/)
  })

  it('does not sit inside the buttons, so it needs no left margin', () => {
    // A left margin was the old in-button spacing; keeping it would leave a
    // stray gap at the row's left edge.
    expect(kbdRule()).not.toMatch(/margin-left:/)
  })

  it('reads as plain text, not as a clickable keycap', () => {
    // A border or a filled background turns the hint into what looks like a
    // third button sitting next to the two real ones.
    const rule = kbdRule()
    expect(rule, 'the hint must not draw a keycap box').not.toMatch(/border/)
    expect(rule, 'the hint must not draw a keycap background').not.toMatch(/background/)
    // Muted, so it stays subordinate to the buttons it sits beside.
    expect(rule).toMatch(/color:\s*var\(--text-muted\)/)
  })

  it('never spells the shortcut with chord glyphs', () => {
    // ⌃ ⌘ ↵ are unlabeled shapes; the words match the shortcut help dialog.
    const src = readWebFile('src/utils/renderToolDetail.ts')
    const start = src.indexOf('function askSubmitChord(')
    expect(start, 'askSubmitChord must exist').toBeGreaterThan(-1)
    const end = src.indexOf('\n}\n', start)
    const body = src.slice(start, end)
    expect(body, 'must spell the chord in words').toMatch(/Cmd\+Enter/)
    expect(body, 'must spell the chord in words').toMatch(/Ctrl\+Enter/)
    for (const glyph of ['⌃', '⌘', '↵']) {
      expect(body, `must not use the glyph ${glyph}`).not.toContain(glyph)
    }
  })

  it('names the action too, and both halves are localized', () => {
    // Keys alone ("Ctrl+Enter") leave the reader to infer the action. The
    // action word must come from i18n — a hardcoded English "Submit" would sit
    // next to a Chinese 提交 button.
    const src = readWebFile('src/utils/renderToolDetail.ts')
    const start = src.indexOf('function askSubmitHint(')
    expect(start, 'askSubmitHint must exist').toBeGreaterThan(-1)
    const end = src.indexOf('\n}\n', start)
    const body = src.slice(start, end)
    expect(body, 'must render through i18n, not a literal').toContain("gt('tool.askUser.submitHint'")
    expect(body, 'must pass the platform chord in').toMatch(/key:\s*askSubmitChord\(\)/)

    // Both locales define the key with an action word + a {key} placeholder.
    for (const [name, locale] of [['zh', zh], ['en', en]] as const) {
      const hint = (locale as any).tool?.askUser?.submitHint
      expect(typeof hint, `${name} tool.askUser.submitHint must exist`).toBe('string')
      expect(hint, `${name} hint must interpolate the keys`).toContain('{key}')
      // Whatever is left once the placeholder is removed is the action word.
      expect(hint.replace('{key}', '').trim().length, `${name} hint must name the action`).toBeGreaterThan(0)
    }
    expect((zh as any).tool.askUser.submitHint).toContain('提交')
    expect((en as any).tool.askUser.submitHint).toContain('Submit')
  })

  it('is styled globally, never from a scoped block', () => {
    // v-html-injected nodes carry no scope attribute, so a scoped rule can
    // never match them (design-guide red line 1).
    expect(GLOBAL_CSS).toContain('.ask-submit-kbd')
    for (const host of ['src/components/chat/ContentBlocks.vue', 'src/components/chat/ToolDetailDrawer.vue']) {
      expect(readWebFile(host), `${host} must not style the hint itself`).not.toContain('.ask-submit-kbd')
    }
  })

  it('suppresses the focus ring for mouse-driven focus, keeps it for keyboard', () => {
    // Focus on the card is a side effect of clicking an option (see
    // focusAskView). A UA ring around the whole card on every click reads as a
    // glitch; `:focus-visible` keeps it for keyboard users only.
    expect(GLOBAL_CSS).toMatch(/\.ask-question-view:focus\s*\{[^}]*outline:\s*none/)
    expect(GLOBAL_CSS).toMatch(/\.ask-question-view:focus-visible\s*\{[^}]*outline:/)
  })

  it('hides on touch pointers and after submit', () => {
    // No physical keyboard on a phone; and a terminal card has no action left
    // for the chord to point at.
    const coarse = /@media \(pointer: coarse\)\s*\{[\s\S]*?\.ask-submit-kbd\s*\{[\s\S]*?display:\s*none/
    expect(GLOBAL_CSS).toMatch(coarse)
    expect(GLOBAL_CSS).toMatch(/\.ask-question-view\.ask-submitted\s+\.ask-submit-kbd\s*\{[^}]*display:\s*none/)
  })
})
