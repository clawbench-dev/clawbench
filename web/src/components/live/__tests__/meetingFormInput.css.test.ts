import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The meeting form's text fields must not be squashed by their own layout.
 *
 * The create/join bodies are `display: flex; flex-direction: column` scroll
 * containers (`flex: 1; min-height: 0; overflow-y: auto`). Once the content is
 * taller than the panel — which it is on a phone, with the type tiles, the
 * source grid and the hints — every child is shrunk to fit before the container
 * scrolls. Non-replaced children are protected by their automatic minimum size
 * (their content height); an `<input>` is a replaced element, so its automatic
 * minimum is 0 and it is the ONLY child that actually shrinks. Measured in
 * Chromium at 393×700: a `height: 32px` input renders 16px tall.
 *
 * That is the "扁的要死" report: the fields, not the panel, looked wrong. The
 * fix is `flex-shrink: 0` — the body then scrolls, as it is meant to.
 *
 * The rule now lives in `assets/meeting-form.css`, shared with the running
 * meeting's form so one field has one definition. jsdom has no CSS engine (and
 * no layout), so this reads the sources and pins both halves: the declaration
 * itself, and the fact that each form actually loads the sheet.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

const SHARED = 'src/assets/meeting-form.css'
const FORMS = ['MeetingCreate.vue', 'MeetingJoin.vue']

/** The declaration block of the first rule that targets `.meeting-input`. */
function inputRule(): string {
  const css = stripComments(readWebFile(SHARED))
  const m = css.match(/\.meeting-input\s*\{([^}]*)\}/)
  expect(m, `${SHARED} must style .meeting-input`).not.toBeNull()
  return m![1]
}

describe('meeting form inputs', () => {
  it('opts out of flex shrinking', () => {
    // Without this the field collapses to roughly half height on a phone; the
    // panel is a column flex container and an input has no content minimum.
    expect(inputRule()).toMatch(/flex-shrink:\s*0\b/)
  })

  it('keeps a comfortable field height', () => {
    // A floor, so "it fits" cannot be bought back by shrinking the box again.
    const height = inputRule().match(/height:\s*calc\((\d+)\s*\*\s*var\(--dp\)\)/)
    expect(height, `.meeting-input height must use the --dp scale`).not.toBeNull()
    expect(Number(height![1])).toBeGreaterThanOrEqual(36)
  })

  it('sizes the typed value above the field label', () => {
    // Same rule the settings form follows: a value never renders smaller than
    // the label naming it (labels here are --font-size-sm).
    expect(inputRule()).not.toMatch(/font-size:\s*var\(--font-size-(2xs|xs|sm)\)/)
  })

  it.each(FORMS)('%s loads the shared form stylesheet', (file) => {
    // A component that renders .meeting-input without the sheet has no rule at
    // all — the styles are global, and "an ancestor happened to import it" is
    // not a contract.
    expect(readWebFile(`src/components/live/${file}`)).toContain("import '@/assets/meeting-form.css'")
  })
})
