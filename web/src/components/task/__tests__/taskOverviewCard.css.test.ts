import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

/**
 * Guard: the task-detail card chrome is declared once, globally.
 *
 * The gating script card once reused `.overview-card` / `.card-title` without
 * declaring them, on the assumption that the parent's styles applied. They did
 * not: those rules are `<style scoped>` in TaskOverviewTab, and a scoped rule
 * carries `[data-v-x]` that a child component's root never has. The card
 * rendered with no background, border or padding — visually nothing like the
 * prompt card beside it.
 *
 * So: the shape lives in assets/task-overview-card.css (global), and no task
 * component may re-declare it in its own scoped block. See
 * docs/spec/client/design-guide.md red line 2.
 */

const taskDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const webSrc = resolve(taskDir, '..', '..')

const SHARED_FILE = resolve(webSrc, 'assets/task-overview-card.css')

/** Selectors that must be declared only in the shared global file. */
const SHARED_SELECTORS = [
  '.overview-card',
  '.card-title',
  '.card-icon',
  '.card-title-text',
  '.card-toggle-btn',
  '.card-chevron',
]

function scopedBlocks(source: string): string[] {
  // A .vue file may hold several <style> blocks; only scoped ones matter, and
  // a selector may sit in a comma group. Extract every <style scoped> body.
  const blocks: string[] = []
  const re = /<style\b[^>]*\bscoped\b[^>]*>([\s\S]*?)<\/style>/g
  let m: RegExpExecArray | null
  while ((m = re.exec(source)) !== null) blocks.push(m[1])
  return blocks
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

describe('task overview card chrome is global', () => {
  it('declares the shared selectors in the global stylesheet', () => {
    const css = stripComments(readFileSync(SHARED_FILE, 'utf8'))
    for (const sel of SHARED_SELECTORS) {
      // Match the selector as a whole class token, not a substring of another.
      const re = new RegExp(`(^|[\\s,])${sel.replace('.', '\\.')}\\s*[,{]`, 'm')
      expect(re.test(css), `${sel} must be declared in task-overview-card.css`).toBe(true)
    }
  })

  it('never re-declares them inside a task component scoped block', () => {
    const files = readdirSync(taskDir).filter(f => f.endsWith('.vue'))
    for (const file of files) {
      const source = readFileSync(resolve(taskDir, file), 'utf8')
      for (const block of scopedBlocks(source)) {
        const css = stripComments(block)
        for (const sel of SHARED_SELECTORS) {
          const re = new RegExp(`(^|[\\s,])${sel.replace('.', '\\.')}\\s*[,{]`, 'm')
          expect(
            re.test(css),
            `${file} re-declares ${sel} in a scoped block — it belongs in assets/task-overview-card.css`,
          ).toBe(false)
        }
      }
    }
  })

  it('imports the shared stylesheet from every card that uses the classes', () => {
    // A card using the classes without importing the sheet renders unstyled
    // (the exact original bug). The import is what makes the global rule
    // present on that route.
    //
    // Match the import STATEMENT, not the filename: several of these files
    // mention the path in a comment, and a bare `toContain('assets/…')` is
    // satisfied by that comment even after the real import is deleted.
    const importRe = /^\s*import\s+['"]@\/assets\/task-overview-card\.css['"]\s*$/m
    const cards = ['TaskOverviewTab.vue', 'TaskScheduleCard.vue', 'TaskEventCard.vue', 'TaskScriptCard.vue']
    for (const file of cards) {
      const source = readFileSync(resolve(taskDir, file), 'utf8')
      expect(importRe.test(source), `${file} must import the shared card stylesheet`).toBe(true)
    }
  })
})
