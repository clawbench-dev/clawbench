import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { TOOL_ICONS, FALLBACK_TOOL_ICON, getToolIcon, toolDisplayName, isAgentFamilyToolName } from '@/utils/icons'

describe('TOOL_ICONS', () => {
  it('has an entry for every known tool name', () => {
    const expectedTools = [
      'Read', 'Write', 'Edit', 'Bash', 'Grep', 'Glob', 'WebSearch', 'WebFetch',
      'Agent', 'Task', 'Skill', 'AskUserQuestion', 'TaskCreate', 'TaskUpdate',
      'TaskList', 'TaskGet', 'TaskStop', 'TaskOutput', 'EnterPlanMode', 'ExitPlanMode',
      'LS', 'PowerShell', 'SendMessage', 'NotebookEdit', 'TodoWrite', 'LSP',
      'ImageGen', 'EnterWorktree', 'LeaveWorktree', 'ComputerUse', 'TeamCreate',
      'TeamDelete', 'WeChatReply', 'WeComReply', 'save_memory', 'DeepThink',
      'StructuredOutput', 'SkillManage', 'Monitor', 'PermissionApproval',
      'MultiEdit', 'TodoRead', 'Git',
    ]
    for (const name of expectedTools) {
      expect(TOOL_ICONS[name]).toBeDefined()
      expect(TOOL_ICONS[name].category).toBeTruthy()
      expect(TOOL_ICONS[name].icon).toBeDefined()
    }
  })

  it('each entry has icon and category properties', () => {
    for (const [name, entry] of Object.entries(TOOL_ICONS)) {
      expect(entry.icon, `${name} should have an icon`).toBeDefined()
      expect(entry.category, `${name} should have a category`).toBeTruthy()
    }
  })
})

describe('FALLBACK_TOOL_ICON', () => {
  it('has icon and category properties', () => {
    expect(FALLBACK_TOOL_ICON.icon).toBeDefined()
    expect(FALLBACK_TOOL_ICON.category).toBe('fallback')
  })
})

describe('getToolIcon', () => {
  it('returns the correct entry for a known tool name', () => {
    const result = getToolIcon('Read')
    expect(result.category).toBe('file')
    expect(result.icon).toBe(TOOL_ICONS['Read'].icon)
  })

  it('performs case-insensitive lookup', () => {
    expect(getToolIcon('read').category).toBe('file')
    expect(getToolIcon('BASH').category).toBe('bash')
    expect(getToolIcon('websearch').category).toBe('search')
  })

  it('returns FALLBACK_TOOL_ICON for unknown tool names', () => {
    const result = getToolIcon('UnknownTool')
    expect(result.category).toBe('fallback')
    expect(result.icon).toBe(FALLBACK_TOOL_ICON.icon)
  })

  it('returns Agent icon for known agent sub-type names', () => {
    expect(getToolIcon('explore').category).toBe('agent')
    expect(getToolIcon('plan').category).toBe('agent')
    expect(getToolIcon('general-purpose').category).toBe('agent')
    expect(getToolIcon('general').category).toBe('agent')
    expect(getToolIcon('claude').category).toBe('agent')
    expect(getToolIcon('code-reviewer').category).toBe('agent')
    expect(getToolIcon('statusline-setup').category).toBe('agent')
    expect(getToolIcon('fork').category).toBe('agent')
    // OpenCode 2.x delegation tool; old stored blocks still carry this name.
    expect(getToolIcon('subagent').category).toBe('agent')
  })

  it('agent sub-type lookup is case-insensitive', () => {
    expect(getToolIcon('Explore').category).toBe('agent')
    expect(getToolIcon('PLAN').category).toBe('agent')
  })

  it('returns fallback for empty string', () => {
    expect(getToolIcon('').category).toBe('fallback')
  })

  it('returns fallback for non-agent unknown names', () => {
    expect(getToolIcon('random-thing').category).toBe('fallback')
  })

  // The lookup table is prebuilt at module load. If it is ever rebuilt per
  // call (or the key normalization drifts), these still hold — but the
  // identity assertions below catch a regression to a fresh object per call,
  // which would break the template's memoization assumptions.
  it('returns the same object identity across repeated calls', () => {
    expect(getToolIcon('Read')).toBe(TOOL_ICONS['Read'])
    expect(getToolIcon('read')).toBe(TOOL_ICONS['Read'])
    expect(getToolIcon('READ')).toBe(TOOL_ICONS['Read'])
    expect(getToolIcon('explore')).toBe(TOOL_ICONS['Agent'])
    expect(getToolIcon('nope')).toBe(FALLBACK_TOOL_ICON)
    expect(getToolIcon('')).toBe(FALLBACK_TOOL_ICON)
  })

  it('resolves every TOOL_ICONS entry case-insensitively', () => {
    // Guards the prebuilt lowercase index: a missing/renamed key would only
    // surface as a wrong icon in the UI, not as a thrown error.
    for (const [name, entry] of Object.entries(TOOL_ICONS)) {
      expect(getToolIcon(name), name).toBe(entry)
      expect(getToolIcon(name.toLowerCase()), name).toBe(entry)
      expect(getToolIcon(name.toUpperCase()), name).toBe(entry)
    }
  })
})

describe('toolDisplayName', () => {
  it('returns the tool name as-is for regular tools', () => {
    expect(toolDisplayName('Read')).toBe('Read')
    expect(toolDisplayName('Bash')).toBe('Bash')
  })

  it('returns displayName when provided', () => {
    expect(toolDisplayName('Agent', undefined, 'Explore Agent')).toBe('Explore Agent')
  })

  it('PascalCases the subagent_type for Agent calls', () => {
    expect(toolDisplayName('Agent', { subagent_type: 'explore' })).toBe('Explore')
    expect(toolDisplayName('Agent', { subagent_type: 'general-purpose' })).toBe('General-purpose')
  })

  it('PascalCases the subagent_type for Task calls', () => {
    expect(toolDisplayName('Task', { subagent_type: 'plan' })).toBe('Plan')
  })

  it('resolves OpenCode subagent calls from the agent input', () => {
    expect(toolDisplayName('subagent', { agent: 'general' })).toBe('General')
    expect(toolDisplayName('Sub Agent', { agent: 'explore' })).toBe('Explore')
    // No type reported → the generic Agent label, never the raw "subagent".
    expect(toolDisplayName('subagent', {})).toBe('Agent')
  })

  it('ignores subagent_type for non-Agent/Task tools', () => {
    expect(toolDisplayName('Read', { subagent_type: 'explore' })).toBe('Read')
  })

  it('returns empty string for empty name', () => {
    expect(toolDisplayName('')).toBe('')
  })

  it('handles case-insensitive Agent/Task matching', () => {
    expect(toolDisplayName('agent', { subagent_type: 'explore' })).toBe('Explore')
    expect(toolDisplayName('task', { subagent_type: 'plan' })).toBe('Plan')
  })

  it('prefers displayName over subagent_type', () => {
    expect(toolDisplayName('Agent', { subagent_type: 'explore' }, 'Custom')).toBe('Custom')
  })

  it('returns name when input has no subagent_type', () => {
    expect(toolDisplayName('Agent', {})).toBe('Agent')
    expect(toolDisplayName('Agent', { other: 'value' })).toBe('Agent')
  })

  it('handles null/undefined input gracefully', () => {
    expect(toolDisplayName('Agent')).toBe('Agent')
    expect(toolDisplayName('Agent', undefined)).toBe('Agent')
  })

  // ── Task-tool family (issue #645) ──
  //
  // TaskOutput/TaskStop inputs are a bare task_id and TaskUpdate is
  // {taskId, status}, so the tool name alone told the user nothing. The card
  // title must state the action, in the UI language.
  describe('task-tool titles', () => {
    const original = i18n.global.locale.value
    function withLocale(locale: 'en' | 'zh', fn: () => void) {
      i18n.global.locale.value = locale
      try {
        fn()
      } finally {
        i18n.global.locale.value = original
      }
    }

    it('names the action instead of the bare wire name', () => {
      withLocale('en', () => {
        expect(toolDisplayName('TaskOutput')).toBe('View background task output')
        expect(toolDisplayName('TaskStop')).toBe('Stop background task')
        expect(toolDisplayName('TaskUpdate')).toBe('Update task')
        expect(toolDisplayName('TaskCreate')).toBe('Create task')
        expect(toolDisplayName('TaskList')).toBe('List tasks')
        expect(toolDisplayName('TaskGet')).toBe('Get task')
      })
    })

    it('follows the UI language', () => {
      withLocale('zh', () => {
        expect(toolDisplayName('TaskOutput')).toBe('查看后台任务输出')
        expect(toolDisplayName('TaskStop')).toBe('停止后台任务')
      })
    })

    it('is case-insensitive like the rest of the lookup', () => {
      withLocale('en', () => {
        expect(toolDisplayName('taskoutput')).toBe('View background task output')
        expect(toolDisplayName('TASKSTOP')).toBe('Stop background task')
      })
    })

    it('does not turn the id itself into the title', () => {
      withLocale('en', () => {
        // The id stays the SUMMARY (toolCallSummary); the title must never be it.
        expect(toolDisplayName('TaskOutput', { task_id: 'cyqjeL' })).toBe('View background task output')
        expect(toolDisplayName('TaskUpdate', { taskId: '3', status: 'in_progress' })).toBe('Update task')
      })
    })

    it('keeps an explicit display_name override', () => {
      withLocale('en', () => {
        expect(toolDisplayName('TaskOutput', undefined, 'Custom')).toBe('Custom')
      })
    })

    it('leaves non-task tools untouched', () => {
      withLocale('en', () => {
        expect(toolDisplayName('Bash')).toBe('Bash')
        expect(toolDisplayName('Read')).toBe('Read')
      })
    })

    it('does not resolve inherited object keys as task tools', () => {
      // The name comes from the agent; a custom ACP/MCP server can report any
      // string. A plain-object lookup would hit Object.prototype and render
      // the constructor's source text (or "[object Object]") into the pill.
      withLocale('en', () => {
        for (const name of ['constructor', '__proto__', 'toString', 'hasOwnProperty', 'valueOf']) {
          expect(toolDisplayName(name), name).toBe(name)
        }
      })
    })
  })
})

describe('isAgentFamilyToolName', () => {
  it('recognizes every delegation-tool spelling', () => {
    expect(isAgentFamilyToolName('Agent')).toBe(true)
    expect(isAgentFamilyToolName('task')).toBe(true)
    expect(isAgentFamilyToolName('subagent')).toBe(true)
    expect(isAgentFamilyToolName('Sub Agent')).toBe(true)
    expect(isAgentFamilyToolName('sub-agent')).toBe(true)
  })

  it('rejects ordinary tools and empty names', () => {
    expect(isAgentFamilyToolName('Read')).toBe(false)
    expect(isAgentFamilyToolName('')).toBe(false)
    expect(isAgentFamilyToolName(undefined)).toBe(false)
  })
})
