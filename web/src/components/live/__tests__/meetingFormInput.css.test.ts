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
 * jsdom has no CSS engine (and no layout), so this reads the SFC styles and
 * pins the declaration, following `settingsRowTypography.css.test.ts`.
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

/** The declaration block of the first rule that targets `.meeting-input`. */
function inputRule(file: string): string {
  const css = stripComments(readWebFile(`src/components/live/${file}`))
  const m = css.match(/\.meeting-input\s*\{([^}]*)\}/)
  expect(m, `${file} must style .meeting-input`).not.toBeNull()
  return m![1]
}

const FILES = ['MeetingCreate.vue', 'MeetingJoin.vue']

describe('meeting form inputs', () => {
  it.each(FILES)('%s opts out of flex shrinking', (file) => {
    // Without this the field collapses to roughly half height on a phone; the
    // panel is a column flex container and an input has no content minimum.
    expect(inputRule(file)).toMatch(/flex-shrink:\s*0\b/)
  })

  it.each(FILES)('%s keeps a comfortable field height', (file) => {
    // A floor, so "it fits" cannot be bought back by shrinking the box again.
    const height = inputRule(file).match(/height:\s*calc\((\d+)\s*\*\s*var\(--dp\)\)/)
    expect(height, `${file} .meeting-input height must use the --dp scale`).not.toBeNull()
    expect(Number(height![1])).toBeGreaterThanOrEqual(36)
  })

  it.each(FILES)('%s sizes the typed value above the field label', (file) => {
    // Same rule the settings form follows: a value never renders smaller than
    // the label naming it (labels here are --font-size-sm).
    expect(inputRule(file)).not.toMatch(/font-size:\s*var\(--font-size-(2xs|xs|sm)\)/)
  })
})
