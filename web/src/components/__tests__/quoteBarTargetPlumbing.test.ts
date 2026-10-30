import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The quote bar's `pickTarget` flag has to survive the App.vue hop.
 *
 * The bar emits `(text, { pickTarget })`; App.vue's template forwards it to
 * `useQuoteQuestion`, which decides between "active conversation" and "ask".
 * Dropping the second argument anywhere in that chain silently restores the
 * mandatory picker for the long-press path — and every component test would
 * still pass, because they stop at the bar's own `emit`.
 *
 * This is a source guard rather than a mounted test because App.vue pulls in
 * the whole application (store, sockets, every panel); mounting it for one
 * event signature is not worth the fragility. cwd differs between a bare
 * `vitest` run (web/) and scripts/vitest-run.sh (repo root), so probe both.
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

describe('quote bar → App.vue target plumbing', () => {
  const app = readWebFile('src/App.vue')

  it('binds both bar events to handlers (not to a one-argument expression)', () => {
    // `@add="quoteQuestion.addToConversation($event)"` is exactly the shape that
    // silently drops `pickTarget`.
    expect(app).toMatch(/@add="onQuoteBarAdd"/)
    expect(app).toMatch(/@send="onQuoteBarSend"/)
  })

  it('forwards the options object to the composable', () => {
    const addBody = app.slice(app.indexOf('function onQuoteBarAdd'))
    expect(addBody.slice(0, 200)).toMatch(/addToConversation\(note,\s*opts\)/)

    const sendBody = app.slice(app.indexOf('function onQuoteBarSend'))
    expect(sendBody.slice(0, 200)).toMatch(/sendMessage\(text,\s*opts\)/)
  })

  it('declares the second parameter on both handlers', () => {
    // Without it the object is dropped even though the call site passes it.
    expect(app).toMatch(/function onQuoteBarAdd\([^)]*opts\?[^)]*\)/)
    expect(app).toMatch(/function onQuoteBarSend\([^)]*opts\?[^)]*\)/)
  })

  it('tells the session picker which intent it is serving', () => {
    // `mode` is what drops the "add and open" arrow in the quote bar's
    // `choose` flow (nothing is delivered there, so the arrow would duplicate
    // the row). The dialog defaults to `deliver`, so losing this binding is
    // silent: the arrow simply reappears and every picker test still passes.
    expect(app).toMatch(/<SessionPickerDialog[\s\S]*?:mode="pickerIntent"/)
    expect(app).toMatch(/const pickerIntent = usePickerIntent\(\)/)
  })
})
