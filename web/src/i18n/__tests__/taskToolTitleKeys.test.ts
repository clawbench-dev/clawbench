import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { TASK_TOOL_TITLE_KEYS } from '@/utils/icons'

/**
 * Keys for the task-tool card titles (issue #645).
 *
 * TaskOutput/TaskStop/TaskUpdate carry no descriptive input — only a task id or
 * a status — so their card title has to state the ACTION ("View background task
 * output") and leave the id as the secondary summary.
 *
 * The lookup goes through a name→key MAP, which the literal-key scan in
 * literalKeys.test.ts cannot see (it only matches a quoted key written directly
 * at a translate call site), so a missing key would render the raw key path
 * into the pill with nothing to catch it. This test therefore iterates the
 * exported map itself rather than a hand-copied key list: a hand-copied list
 * silently stops guarding as soon as a seventh tool is added to the map.
 */
describe('task tool titles', () => {
  const entries = Object.entries(TASK_TOOL_TITLE_KEYS)

  function lookup(messages: unknown, path: string[]): unknown {
    return path.reduce<unknown>((acc, part) => (acc as Record<string, unknown> | undefined)?.[part], messages)
  }

  it('covers every task tool the chat renders', () => {
    // Guard against the map being emptied/renamed out from under the lookup:
    // an empty map makes every assertion below vacuously true.
    expect(entries.length).toBeGreaterThanOrEqual(6)
    for (const tool of ['taskcreate', 'taskupdate', 'tasklist', 'taskget', 'taskstop', 'taskoutput']) {
      expect(TASK_TOOL_TITLE_KEYS[tool], `${tool} has no title key`).toBeTruthy()
    }
  })

  it('resolves every mapped key in both locales', () => {
    for (const [tool, key] of entries) {
      for (const [locale, messages] of [['en', en], ['zh', zh]] as const) {
        const value = lookup(messages, key.split('.'))
        expect(typeof value, `${locale} ${key} (${tool}) is not a string`).toBe('string')
        expect((value as string).length, `${locale} ${key} (${tool}) is empty`).toBeGreaterThan(0)
      }
    }
  })

  it('keeps both locales on the same key set', () => {
    expect(Object.keys(en.tool.task.action).sort()).toEqual(Object.keys(zh.tool.task.action).sort())
  })

  it('gives each action its own title', () => {
    // A copy-paste slip would title every task card identically, which is the
    // same defect the issue reports (a card that says nothing about itself).
    for (const messages of [en.tool.task.action, zh.tool.task.action]) {
      const titles = Object.values(messages)
      expect(new Set(titles).size).toBe(titles.length)
    }
  })

  it('distinguishes stopping a background task from viewing its output', () => {
    // The two tools the issue calls out by name; a shared generic "task" title
    // would leave them indistinguishable in the trace.
    expect(en.tool.task.action.stop).not.toBe(en.tool.task.action.output)
    expect(zh.tool.task.action.stop).not.toBe(zh.tool.task.action.output)
  })
})
