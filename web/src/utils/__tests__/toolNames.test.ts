import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { TOOL_ICONS } from '@/utils/icons'
import { localizedToolName, TASK_TOOL_TITLE_KEYS, TOOL_NAME_KEYS } from '@/utils/toolNames'

function lookup(messages: unknown, path: string[]): unknown {
  return path.reduce<unknown>((acc, part) => (acc as Record<string, unknown> | undefined)?.[part], messages)
}

describe('tool name localization', () => {
  const original = i18n.global.locale.value
  function withLocale(locale: 'en' | 'zh', fn: () => void) {
    i18n.global.locale.value = locale
    try {
      fn()
    } finally {
      i18n.global.locale.value = original
    }
  }

  it('names every tool that has an icon', () => {
    // The icon table is what "known tool" means in this codebase. A tool with
    // an icon but no name entry silently falls back to the raw wire name, which
    // is exactly the defect this table exists to remove.
    for (const name of Object.keys(TOOL_ICONS)) {
      expect(TOOL_NAME_KEYS[name.toLowerCase()], `${name} has no localized name`).toBeTruthy()
    }
  })

  it('names the known tool that has no icon of its own', () => {
    // Codex's collaboration wait has a registered renderer but no icon entry,
    // so it would otherwise be missed by the icon-table guard above.
    expect(TOOL_NAME_KEYS.wait).toBeTruthy()
  })

  it('resolves every mapped key in both locales', () => {
    for (const [tool, key] of Object.entries(TOOL_NAME_KEYS)) {
      for (const [locale, messages] of [['en', en], ['zh', zh]] as const) {
        const value = lookup(messages, key.split('.'))
        expect(typeof value, `${locale} ${key} (${tool}) is not a string`).toBe('string')
        expect((value as string).length, `${locale} ${key} (${tool}) is empty`).toBeGreaterThan(0)
      }
    }
  })

  it('keeps both locales on the same key set', () => {
    expect(Object.keys(en.tool.name).sort()).toEqual(Object.keys(zh.tool.name).sort())
  })

  it('never shows the raw wire name in Chinese', () => {
    withLocale('zh', () => {
      for (const name of Object.keys(TOOL_ICONS)) {
        const label = localizedToolName(name)
        expect(label.toLowerCase(), `${name} still shows its wire name`).not.toBe(name.toLowerCase())
      }
    })
  })

  it('still names the task-tool family by its action', () => {
    withLocale('en', () => {
      expect(localizedToolName('TaskOutput')).toBe(en.tool.task.action.output)
      expect(localizedToolName('TaskStop')).toBe(en.tool.task.action.stop)
    })
    // The action keys are shared with the generic name table, not copied into it.
    expect(TOOL_NAME_KEYS.taskoutput).toBe(TASK_TOOL_TITLE_KEYS.taskoutput)
  })

  it('follows the UI language', () => {
    withLocale('en', () => {
      expect(localizedToolName('Bash')).toBe('Run command')
      expect(localizedToolName('WebSearch')).toBe('Web search')
    })
    withLocale('zh', () => {
      expect(localizedToolName('Bash')).toBe('执行命令')
      expect(localizedToolName('WebSearch')).toBe('联网搜索')
    })
  })

  it('is case-insensitive like the icon lookup', () => {
    withLocale('en', () => {
      expect(localizedToolName('bash')).toBe('Run command')
      expect(localizedToolName('TASKOUTPUT')).toBe('View background task output')
    })
  })

  it('passes unknown tools through unchanged', () => {
    // Custom ACP agents and MCP servers can report any name; inventing a label
    // would be worse than showing the name they chose.
    for (const name of ['mcp__acme__deploy', 'my_custom_tool', 'CUSTOM']) {
      expect(localizedToolName(name)).toBe(name)
    }
    expect(localizedToolName('')).toBe('')
    expect(localizedToolName(undefined)).toBe('')
  })

  it('does not resolve inherited object keys', () => {
    for (const name of ['constructor', '__proto__', 'toString', 'hasOwnProperty', 'valueOf']) {
      expect(localizedToolName(name)).toBe(name)
    }
  })
})
