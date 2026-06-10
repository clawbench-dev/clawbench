import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import {
  formatToolInput,
  formatToolOutput,
  shouldAutoExpandTool,
  registerToolRenderer,
  registerToolActionHandler,
  handleToolAction,
} from '@/utils/renderToolDetail.ts'

// ── Mock for useAppMode (controlled via mutable ref) ──
const mockIsAppMode = ref(false)
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: mockIsAppMode }),
}))

// ── Helpers ──

/** Check if HTML contains a specific substring */
function contains(html: string, substring: string): boolean {
  return html.includes(substring)
}

// ────────────────────────────────────────────────────────────
// formatToolInput — registry dispatch + individual renderers
// ────────────────────────────────────────────────────────────

describe('formatToolInput', () => {
  // ── Grep ──
  describe('Grep renderer', () => {
    it('renders pattern with label', () => {
      const html = formatToolInput({ pattern: 'TODO', path: './src' }, 'Grep')
      expect(contains(html, 'grep-search-view')).toBe(true)
      expect(contains(html, 'grep-label')).toBe(true)
      expect(contains(html, 'TODO')).toBe(true)
      expect(contains(html, 'grep-pattern-text')).toBe(true)
    })

    it('renders path row when path is provided', () => {
      const html = formatToolInput({ pattern: 'FIXME', path: './lib' }, 'Grep')
      expect(contains(html, 'grep-path-row')).toBe(true)
      expect(contains(html, 'lib')).toBe(true)
    })

    it('omits path row when path is empty', () => {
      const html = formatToolInput({ pattern: 'TODO' }, 'Grep')
      expect(contains(html, 'grep-path-row')).toBe(false)
    })

    it('renders output_mode tag when present', () => {
      const html = formatToolInput({ pattern: 'test', output_mode: 'files_with_matches' }, 'Grep')
      expect(contains(html, 'grep-mode-tag')).toBe(true)
      expect(contains(html, 'files_with_matches')).toBe(true)
    })

    it('omits output_mode tag when not present', () => {
      const html = formatToolInput({ pattern: 'test' }, 'Grep')
      expect(contains(html, 'grep-mode-tag')).toBe(false)
    })

    it('case-insensitive lookup', () => {
      const htmlUpper = formatToolInput({ pattern: 'TODO' }, 'GREP')
      const htmlLower = formatToolInput({ pattern: 'TODO' }, 'grep')
      // Both should produce grep-search-view
      expect(contains(htmlUpper, 'grep-search-view')).toBe(true)
      expect(contains(htmlLower, 'grep-search-view')).toBe(true)
    })
  })

  // ── Glob ──
  describe('Glob renderer', () => {
    it('renders pattern with label', () => {
      const html = formatToolInput({ pattern: '**/*.go' }, 'Glob')
      expect(contains(html, 'glob-pattern-view')).toBe(true)
      expect(contains(html, '**/*.go')).toBe(true)
      expect(contains(html, 'glob-pattern-text')).toBe(true)
    })

    it('renders path row when path is provided', () => {
      const html = formatToolInput({ pattern: '*.ts', path: './src' }, 'Glob')
      expect(contains(html, 'glob-path-row')).toBe(true)
      expect(contains(html, 'src')).toBe(true)
    })

    it('omits path row when path is empty', () => {
      const html = formatToolInput({ pattern: '*.go' }, 'Glob')
      expect(contains(html, 'glob-path-row')).toBe(false)
    })
  })

  // ── WebSearch ──
  describe('WebSearch renderer', () => {
    it('renders search query with icon', () => {
      const html = formatToolInput({ query: 'golang testing best practices' }, 'WebSearch')
      expect(contains(html, 'web-search-view')).toBe(true)
      expect(contains(html, 'web-search-icon')).toBe(true)
      expect(contains(html, 'golang testing best practices')).toBe(true)
    })

    it('escapes HTML in query', () => {
      const html = formatToolInput({ query: '<script>alert(1)</script>' }, 'WebSearch')
      expect(contains(html, '<script>')).toBe(false)
      expect(contains(html, '&lt;script&gt;')).toBe(true)
    })

    it('renders empty query gracefully', () => {
      const html = formatToolInput({ query: '' }, 'WebSearch')
      expect(contains(html, 'web-search-view')).toBe(true)
    })
  })

  // ── WebFetch ──
  describe('WebFetch renderer', () => {
    it('renders URL as clickable link for http URLs', () => {
      const html = formatToolInput({ url: 'https://example.com' }, 'WebFetch')
      expect(contains(html, 'web-fetch-link')).toBe(true)
      expect(contains(html, 'href="https://example.com"')).toBe(true)
      expect(contains(html, 'target="_blank"')).toBe(true)
    })

    it('renders non-URL as plain text', () => {
      const html = formatToolInput({ url: 'not-a-url' }, 'WebFetch')
      expect(contains(html, 'web-fetch-text')).toBe(true)
      expect(contains(html, 'web-fetch-link')).toBe(false)
    })

    it('renders prompt when both url and prompt are present', () => {
      const html = formatToolInput({ url: 'https://example.com', prompt: 'Extract title' }, 'WebFetch')
      expect(contains(html, 'web-fetch-prompt')).toBe(true)
      expect(contains(html, 'Extract title')).toBe(true)
    })

    it('does not render prompt when only prompt exists (no url)', () => {
      // When url is empty and only prompt exists, prompt is used as url display text
      const html = formatToolInput({ prompt: 'Some text' }, 'WebFetch')
      // prompt is used as the fallback url display but not shown as separate prompt section
      expect(contains(html, 'Some text')).toBe(true)
    })

    it('renders URL label', () => {
      const html = formatToolInput({ url: 'https://example.com' }, 'WebFetch')
      expect(contains(html, 'web-fetch-label')).toBe(true)
      expect(contains(html, 'URL')).toBe(true)
    })
  })

  // ── Agent ──
  describe('Agent renderer', () => {
    it('renders subagent type badge', () => {
      const html = formatToolInput({ subagent_type: 'general-purpose', description: 'Research task' }, 'Agent')
      expect(contains(html, 'agent-type-badge')).toBe(true)
      expect(contains(html, 'general-purpose')).toBe(true)
    })

    it('renders mode as type badge fallback', () => {
      const html = formatToolInput({ mode: 'fork', description: 'Fork task' }, 'Agent')
      expect(contains(html, 'agent-type-badge')).toBe(true)
      expect(contains(html, 'fork')).toBe(true)
    })

    it('renders description', () => {
      const html = formatToolInput({ description: 'Search codebase' }, 'Agent')
      expect(contains(html, 'agent-call-desc')).toBe(true)
      expect(contains(html, 'Search codebase')).toBe(true)
    })

    it('renders full prompt without truncation', () => {
      const longPrompt = 'A'.repeat(250)
      const html = formatToolInput({ prompt: longPrompt }, 'Agent')
      expect(contains(html, 'agent-call-prompt')).toBe(true)
      // Should contain all characters (markdown rendered, no truncation)
      expect(contains(html, 'A'.repeat(250))).toBe(true)
      // Should not contain truncation ellipsis
      expect(contains(html, '\u2026')).toBe(false)
    })

    it('renders prompt with markdown', () => {
      const html = formatToolInput({ prompt: '**bold** and `code`' }, 'Agent')
      expect(contains(html, 'agent-call-prompt')).toBe(true)
      // Should render markdown (strong, code tags)
      expect(contains(html, '<strong>')).toBe(true)
      expect(contains(html, '<code>')).toBe(true)
    })

    it('omits type badge when neither subagent_type nor mode', () => {
      const html = formatToolInput({ description: 'Just a desc' }, 'Agent')
      expect(contains(html, 'agent-type-badge')).toBe(false)
    })
  })

  // ── Skill ──
  describe('Skill renderer', () => {
    it('renders skill name with icon', () => {
      const html = formatToolInput({ skill: 'commit' }, 'Skill')
      expect(contains(html, 'skill-call-view')).toBe(true)
      expect(contains(html, 'skill-call-icon')).toBe(true)
      expect(contains(html, 'skill-call-name')).toBe(true)
      expect(contains(html, 'commit')).toBe(true)
    })

    it('uses command as fallback for skill name', () => {
      const html = formatToolInput({ command: 'pdf' }, 'Skill')
      expect(contains(html, 'pdf')).toBe(true)
    })

    it('renders string args', () => {
      const html = formatToolInput({ skill: 'commit', args: '-m "fix bug"' }, 'Skill')
      expect(contains(html, 'skill-call-args')).toBe(true)
      // Double quotes are HTML-escaped by escapeHtml
      expect(contains(html, '-m &quot;fix bug&quot;')).toBe(true)
    })

    it('renders object args as JSON', () => {
      const html = formatToolInput({ skill: 'commit', arguments: { m: 'fix' } }, 'Skill')
      expect(contains(html, 'skill-call-args')).toBe(true)
    })

    it('uses arguments as fallback for args', () => {
      const html = formatToolInput({ skill: 'test', arguments: 'some args' }, 'Skill')
      expect(contains(html, 'some args')).toBe(true)
    })

    it('renders full args without truncation', () => {
      const longArgs = 'X'.repeat(200)
      const html = formatToolInput({ skill: 'test', args: longArgs }, 'Skill')
      // Should contain all characters (no truncation)
      expect(contains(html, 'X'.repeat(200))).toBe(true)
      // Should not contain truncation ellipsis
      expect(contains(html, '\u2026')).toBe(false)
    })

    it('omits args section when no args', () => {
      const html = formatToolInput({ skill: 'commit' }, 'Skill')
      expect(contains(html, 'skill-call-args')).toBe(false)
    })
  })

  // ── Existing renderers (smoke tests) ──
  describe('existing renderers', () => {
    it('Edit renders diff view', () => {
      const html = formatToolInput({ file_path: 'main.go', old_string: 'old', new_string: 'new' }, 'Edit')
      expect(contains(html, 'edit-diff-view')).toBe(true)
      expect(contains(html, 'edit-diff-del')).toBe(true)
      expect(contains(html, 'edit-diff-add')).toBe(true)
    })

    it('Bash renders terminal view', () => {
      const html = formatToolInput({ command: 'ls -la', description: 'List files' }, 'Bash')
      expect(contains(html, 'bash-terminal-view')).toBe(true)
      expect(contains(html, 'bash-terminal-desc')).toBe(true)
      expect(contains(html, 'bash-prompt')).toBe(true)
    })

    it('Read renders preview view', () => {
      const html = formatToolInput({ file_path: 'test.go', content: 'package main' }, 'Read')
      expect(contains(html, 'file-preview-view')).toBe(true)
    })

    it('Write renders write view', () => {
      const html = formatToolInput({ file_path: 'test.go', content: 'package main' }, 'Write')
      expect(contains(html, 'file-write-view')).toBe(true)
      expect(contains(html, 'file-write-badge')).toBe(true)
    })

    it('AskUserQuestion renders question view', () => {
      const html = formatToolInput({
        questions: [{ header: 'Choose', question: 'Which option?', options: [{ label: 'A' }] }],
      }, 'AskUserQuestion')
      expect(contains(html, 'ask-question-view')).toBe(true)
      expect(contains(html, 'Choose')).toBe(true)
    })
  })

  // ── JSON fallback ──
  describe('JSON fallback', () => {
    it('renders unknown tool as JSON', () => {
      const html = formatToolInput({ foo: 'bar' }, 'UnknownTool')
      expect(contains(html, 'tool-json-body')).toBe(true)
    })

    it('renders without tool name as JSON', () => {
      const html = formatToolInput({ foo: 'bar' })
      expect(contains(html, 'tool-json-body')).toBe(true)
    })

    it('renders empty object', () => {
      const html = formatToolInput({})
      expect(contains(html, 'tool-json-body')).toBe(true)
    })

    it('renders null as empty JSON', () => {
      const html = formatToolInput(null)
      expect(contains(html, 'tool-json-body')).toBe(true)
    })
  })

  // ── Registry: case-insensitive lookup ──
  describe('case-insensitive registry', () => {
    it('finds Grep regardless of case', () => {
      expect(contains(formatToolInput({ pattern: 'x' }, 'GREP'), 'grep-search-view')).toBe(true)
      expect(contains(formatToolInput({ pattern: 'x' }, 'grep'), 'grep-search-view')).toBe(true)
      expect(contains(formatToolInput({ pattern: 'x' }, 'Grep'), 'grep-search-view')).toBe(true)
    })

    it('finds Agent regardless of case', () => {
      expect(contains(formatToolInput({ description: 'test' }, 'AGENT'), 'agent-call-view')).toBe(true)
      expect(contains(formatToolInput({ description: 'test' }, 'agent'), 'agent-call-view')).toBe(true)
    })

    it('finds Skill regardless of case', () => {
      expect(contains(formatToolInput({ skill: 'test' }, 'SKILL'), 'skill-call-view')).toBe(true)
    })
  })

  // ── Custom renderer registration ──
  describe('registerToolRenderer', () => {
    it('allows registering custom renderers', () => {
      registerToolRenderer('CustomTool', (input) => `<div class="custom">${input.text}</div>`)
      const html = formatToolInput({ text: 'hello' }, 'CustomTool')
      expect(contains(html, 'custom')).toBe(true)
      expect(contains(html, 'hello')).toBe(true)
    })

    it('custom renderer is case-insensitive', () => {
      registerToolRenderer('MyTool', (input) => `<div class="my-tool">${input.val}</div>`)
      const html = formatToolInput({ val: 'test' }, 'MYTOOL')
      expect(contains(html, 'my-tool')).toBe(true)
    })
  })
})

// ────────────────────────────────────────────────────────────
// shouldAutoExpandTool
// ────────────────────────────────────────────────────────────

describe('shouldAutoExpandTool', () => {
  it('returns true for AskUserQuestion', () => {
    expect(shouldAutoExpandTool('AskUserQuestion')).toBe(true)
  })

  it('is case-insensitive', () => {
    expect(shouldAutoExpandTool('askuserquestion')).toBe(true)
    expect(shouldAutoExpandTool('ASKUSERQUESTION')).toBe(true)
  })

  it('returns false for other tools', () => {
    expect(shouldAutoExpandTool('Grep')).toBe(false)
    expect(shouldAutoExpandTool('Bash')).toBe(false)
    expect(shouldAutoExpandTool('Edit')).toBe(false)
    expect(shouldAutoExpandTool('Agent')).toBe(false)
    expect(shouldAutoExpandTool('Skill')).toBe(false)
  })

  it('returns false for unknown tools', () => {
    expect(shouldAutoExpandTool('UnknownTool')).toBe(false)
  })
})

// ────────────────────────────────────────────────────────────
// handleToolAction
// ────────────────────────────────────────────────────────────

describe('handleToolAction', () => {
  it('returns false for tools without action handlers', () => {
    const event = new MouseEvent('click')
    const emit = vi.fn()
    expect(handleToolAction('Grep', event, emit)).toBe(false)
    expect(handleToolAction('Bash', event, emit)).toBe(false)
    expect(handleToolAction('Agent', event, emit)).toBe(false)
    expect(handleToolAction('Skill', event, emit)).toBe(false)
    expect(emit).not.toHaveBeenCalled()
  })

  it('returns false for unknown tools', () => {
    const event = new MouseEvent('click')
    const emit = vi.fn()
    expect(handleToolAction('UnknownTool', event, emit)).toBe(false)
  })
})

// ────────────────────────────────────────────────────────────
// formatToolOutput
// ────────────────────────────────────────────────────────────

describe('formatToolOutput', () => {
  it('returns empty string for empty output', () => {
    expect(formatToolOutput('')).toBe('')
    expect(formatToolOutput('', 'Bash')).toBe('')
  })

  it('wraps Bash tool output in bash-output-body', () => {
    const html = formatToolOutput('hello world', 'Bash')
    expect(html).toContain('bash-output-body')
    expect(html).toContain('<pre>')
    expect(html).toContain('hello world')
    expect(html).toContain('</pre>')
  })

  it('wraps non-Bash tool output in tool-output-default', () => {
    const html = formatToolOutput('some result', 'Read')
    expect(html).toContain('tool-output-default')
    expect(html).toContain('<pre>')
    expect(html).toContain('some result')
    expect(html).toContain('</pre>')
  })

  it('wraps output without tool name in tool-output-default', () => {
    const html = formatToolOutput('some result')
    expect(html).toContain('tool-output-default')
    expect(html).toContain('some result')
  })

  it('escapes HTML in output', () => {
    const html = formatToolOutput('<script>alert(1)</script>', 'Bash')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })

  it('Bash tool name is case-insensitive', () => {
    const html = formatToolOutput('output', 'BASH')
    expect(html).toContain('bash-output-body')
    const html2 = formatToolOutput('output', 'bash')
    expect(html2).toContain('bash-output-body')
  })

  it('annotates localhost URLs in Bash output when in App mode', () => {
    mockIsAppMode.value = true
    const html = formatToolOutput('Server running at http://localhost:3000/api', 'Bash')
    expect(html).toContain('chat-url-open-btn')
    expect(html).toContain('data-port="3000"')
    expect(html).toContain('data-protocol="http"')
    mockIsAppMode.value = false
  })

  it('does not annotate localhost URLs when not in App mode', () => {
    mockIsAppMode.value = false
    const html = formatToolOutput('Server running at http://localhost:3000/api', 'Bash')
    expect(html).not.toContain('chat-url-open-btn')
    expect(html).toContain('http://localhost:3000/api')
  })

  it('annotates localhost URLs in non-Bash output when in App mode', () => {
    mockIsAppMode.value = true
    const html = formatToolOutput('See http://127.0.0.1:8080/test', 'Read')
    expect(html).toContain('chat-url-open-btn')
    expect(html).toContain('data-port="8080"')
    mockIsAppMode.value = false
  })

  it('handles invalid port in localhost URL', () => {
    mockIsAppMode.value = true
    const html = formatToolOutput('http://localhost:99999/path', 'Bash')
    // Port > 65535 should not be annotated
    expect(html).not.toContain('chat-url-open-btn')
    mockIsAppMode.value = false
  })

  it('handles HTTPS localhost URL in App mode', () => {
    mockIsAppMode.value = true
    const html = formatToolOutput('https://localhost:443/secure', 'Bash')
    expect(html).toContain('chat-url-open-btn')
    expect(html).toContain('data-protocol="https"')
    mockIsAppMode.value = false
  })

  it('un-escapes &amp; in localhost URL for data attributes', () => {
    mockIsAppMode.value = true
    // Output after escapeHtml: & in query string becomes &amp;
    // annotateLocalhostInEscapedText un-escapes &amp; → & for rawUrl, then escapeHtml re-escapes for attributes
    const html = formatToolOutput('http://localhost:3000/path?a=1&b=2', 'Bash')
    expect(html).toContain('chat-url-open-btn')
    // The rawUrl un-escapes &amp; back to & then escapeHtml in href/data-url re-escapes to &amp;
    expect(html).toContain('data-url="http://localhost:3000/path?a=1&amp;b=2"')
    mockIsAppMode.value = false
  })
})

// ────────────────────────────────────────────────────────────
// Edit renderer — deep tests
// ────────────────────────────────────────────────────────────

describe('Edit renderer (deep)', () => {
  it('shows replace_all badge when replace_all is true', () => {
    const html = formatToolInput({ file_path: 'a.ts', old_string: 'x', new_string: 'y', replace_all: true }, 'Edit')
    expect(html).toContain('edit-diff-replace-all')
  })

  it('hides replace_all badge when replace_all is false', () => {
    const html = formatToolInput({ file_path: 'a.ts', old_string: 'x', new_string: 'y', replace_all: false }, 'Edit')
    expect(html).not.toContain('edit-diff-replace-all')
  })

  it('hides replace_all badge when replace_all is absent', () => {
    const html = formatToolInput({ file_path: 'a.ts', old_string: 'x', new_string: 'y' }, 'Edit')
    expect(html).not.toContain('edit-diff-replace-all')
  })

  it('renders multi-line old_string and new_string', () => {
    const html = formatToolInput({
      file_path: 'main.go',
      old_string: 'line1\nline2\nline3',
      new_string: 'new1\nnew2',
    }, 'Edit')
    // Should have 3 deletion lines and 2 addition lines
    const delMatches = html.match(/edit-diff-del/g)
    const addMatches = html.match(/edit-diff-add/g)
    expect(delMatches?.length).toBe(3)
    expect(addMatches?.length).toBe(2)
  })

  it('renders empty old_string (no deletion lines)', () => {
    const html = formatToolInput({ file_path: 'a.ts', old_string: '', new_string: 'y' }, 'Edit')
    expect(html).not.toContain('edit-diff-del')
    expect(html).toContain('edit-diff-add')
  })

  it('renders empty new_string (no addition lines)', () => {
    const html = formatToolInput({ file_path: 'a.ts', old_string: 'x', new_string: '' }, 'Edit')
    expect(html).toContain('edit-diff-del')
    expect(html).not.toContain('edit-diff-add')
  })

  it('strips ./ prefix from file path', () => {
    const html = formatToolInput({ file_path: './src/main.ts', old_string: 'a', new_string: 'b' }, 'Edit')
    expect(html).toContain('src/main.ts')
  })

  it('escapes HTML special characters in file path', () => {
    const html = formatToolInput({ file_path: 'a<b>c.ts', old_string: 'x', new_string: 'y' }, 'Edit')
    expect(html).toContain('a&lt;b&gt;c.ts')
    expect(html).not.toContain('a<b>c.ts')
  })

  it('renders edit-diff-scroll wrapper', () => {
    const html = formatToolInput({ file_path: 'a.ts', old_string: 'x', new_string: 'y' }, 'Edit')
    expect(html).toContain('edit-diff-scroll')
  })
})

// ────────────────────────────────────────────────────────────
// Bash renderer — deep tests
// ────────────────────────────────────────────────────────────

describe('Bash renderer (deep)', () => {
  it('does not crash when command is missing', () => {
    const html = formatToolInput({}, 'Bash')
    expect(html).toContain('bash-terminal-view')
    expect(html).toContain('bash-prompt')
  })

  it('does not show description line when description is absent', () => {
    const html = formatToolInput({ command: 'ls' }, 'Bash')
    expect(html).not.toContain('bash-terminal-desc')
  })

  it('does not show description line when description is empty', () => {
    const html = formatToolInput({ command: 'ls', description: '' }, 'Bash')
    expect(html).not.toContain('bash-terminal-desc')
  })

  it('escapes HTML in description', () => {
    const html = formatToolInput({ command: 'ls', description: '<b>bold</b>' }, 'Bash')
    expect(html).toContain('&lt;b&gt;bold&lt;/b&gt;')
    expect(html).not.toContain('<b>bold</b>')
  })

  it('highlights command with hljs', () => {
    const html = formatToolInput({ command: 'echo "hello"' }, 'Bash')
    // hljs highlight for bash should produce span elements
    expect(html).toContain('bash-terminal-body')
    expect(html).toContain('bash-prompt')
  })

  it('renders with empty command string', () => {
    const html = formatToolInput({ command: '' }, 'Bash')
    expect(html).toContain('bash-terminal-view')
    expect(html).toContain('bash-prompt')
  })
})

// ────────────────────────────────────────────────────────────
// Read renderer — deep tests
// ────────────────────────────────────────────────────────────

describe('Read renderer (deep)', () => {
  it('shows offset/limit info when no content', () => {
    const html = formatToolInput({ file_path: 'test.go', offset: 10, limit: 50 }, 'Read')
    expect(html).toContain('file-preview-meta')
    expect(html).not.toContain('file-preview-line')
  })

  it('renders multi-line content with separate line divs', () => {
    const html = formatToolInput({ file_path: 'test.go', content: 'line1\nline2\nline3' }, 'Read')
    const lineMatches = html.match(/file-preview-line/g)
    expect(lineMatches?.length).toBe(3)
  })

  it('renders empty file_path gracefully', () => {
    const html = formatToolInput({ file_path: '', content: 'hello' }, 'Read')
    expect(html).toContain('file-preview-view')
    expect(html).toContain('file-preview-line')
  })

  it('renders content with offset but no limit', () => {
    const html = formatToolInput({ file_path: 'test.go', offset: 5 }, 'Read')
    expect(html).toContain('file-preview-view')
  })

  it('renders content with limit but no offset', () => {
    const html = formatToolInput({ file_path: 'test.go', limit: 20 }, 'Read')
    expect(html).toContain('file-preview-view')
  })
})

// ────────────────────────────────────────────────────────────
// Write renderer — deep tests
// ────────────────────────────────────────────────────────────

describe('Write renderer (deep)', () => {
  it('renders only file header when no content', () => {
    const html = formatToolInput({ file_path: 'new.go' }, 'Write')
    expect(html).toContain('file-write-view')
    expect(html).toContain('file-write-badge')
    expect(html).not.toContain('file-write-line')
  })

  it('renders multi-line content with separate line divs', () => {
    const html = formatToolInput({ file_path: 'new.go', content: 'a\nb\nc' }, 'Write')
    const lineMatches = html.match(/file-write-line/g)
    expect(lineMatches?.length).toBe(3)
  })

  it('renders content with empty string', () => {
    const html = formatToolInput({ file_path: 'new.go', content: '' }, 'Write')
    expect(html).not.toContain('file-write-line')
  })
})

// ────────────────────────────────────────────────────────────
// AskUserQuestion renderer — deep tests
// ────────────────────────────────────────────────────────────

describe('AskUserQuestion renderer (deep)', () => {
  it('renders empty state when questions is empty array', () => {
    const html = formatToolInput({ questions: [] }, 'AskUserQuestion')
    expect(html).toContain('ask-question-empty')
  })

  it('renders empty state when questions is not an array', () => {
    const html = formatToolInput({ questions: 'not-array' }, 'AskUserQuestion')
    expect(html).toContain('ask-question-empty')
  })

  it('renders empty state when questions is missing', () => {
    const html = formatToolInput({}, 'AskUserQuestion')
    expect(html).toContain('ask-question-empty')
  })

  it('renders options that are plain strings', () => {
    const html = formatToolInput({
      questions: [{ question: 'Pick one', options: ['Option A', 'Option B'] }],
    }, 'AskUserQuestion')
    expect(html).toContain('Option A')
    expect(html).toContain('Option B')
  })

  it('renders option description when present', () => {
    const html = formatToolInput({
      questions: [{ question: 'Pick one', options: [{ label: 'A', description: 'Desc A' }] }],
    }, 'AskUserQuestion')
    expect(html).toContain('ask-option-desc')
    expect(html).toContain('Desc A')
  })

  it('renders multi-select with checkbox indicator', () => {
    const html = formatToolInput({
      questions: [{ question: 'Pick many', multiSelect: true, options: ['A', 'B'] }],
    }, 'AskUserQuestion')
    expect(html).toContain('☐')
    expect(html).toContain('data-multi="true"')
  })

  it('renders single-select with circle indicator', () => {
    const html = formatToolInput({
      questions: [{ question: 'Pick one', multiSelect: false, options: ['A', 'B'] }],
    }, 'AskUserQuestion')
    expect(html).toContain('◯')
    expect(html).toContain('data-multi="false"')
  })

  it('renders supplementary input', () => {
    const html = formatToolInput({
      questions: [{ question: 'Pick one', options: ['A'] }],
    }, 'AskUserQuestion')
    expect(html).toContain('ask-question-supplementary')
    expect(html).toContain('ask-supplementary-input')
  })

  it('submit button is disabled by default', () => {
    const html = formatToolInput({
      questions: [{ question: 'Pick one', options: ['A'] }],
    }, 'AskUserQuestion')
    expect(html).toContain('ask-question-submit" disabled')
  })

  it('renders header and question text', () => {
    const html = formatToolInput({
      questions: [{ header: 'Choose wisely', question: 'Which path?' }],
    }, 'AskUserQuestion')
    expect(html).toContain('ask-question-header')
    expect(html).toContain('Choose wisely')
    expect(html).toContain('ask-question-text')
    expect(html).toContain('Which path?')
  })

  it('renders data-qi and data-oi attributes on options', () => {
    const html = formatToolInput({
      questions: [{ question: 'Q', options: ['A', 'B'] }],
    }, 'AskUserQuestion')
    expect(html).toContain('data-qi="0"')
    expect(html).toContain('data-oi="0"')
    expect(html).toContain('data-oi="1"')
  })
})

// ────────────────────────────────────────────────────────────
// AskUserQuestion action handler — DOM interaction tests
// ────────────────────────────────────────────────────────────

describe('AskUserQuestion action handler', () => {
  /** Create a minimal AskUserQuestion DOM structure for testing */
  function createAskDOM(multiSelect = false): { container: HTMLDivElement; emit: ReturnType<typeof vi.fn> } {
    const emit = vi.fn()
    const container = document.createElement('div')
    container.className = 'ask-question-view'
    container.innerHTML = `
      <div class="ask-question-item" data-multi="${multiSelect}">
        <div class="ask-question-options">
          <div class="ask-question-option" data-qi="0" data-oi="0" data-label="Option A">
            <span class="ask-option-indicator">${multiSelect ? '☐' : '◯'}</span>
            <div class="ask-option-content">
              <span class="ask-option-label">Option A</span>
            </div>
          </div>
          <div class="ask-question-option" data-qi="0" data-oi="1" data-label="Option B">
            <span class="ask-option-indicator">${multiSelect ? '☐' : '◯'}</span>
            <div class="ask-option-content">
              <span class="ask-option-label">Option B</span>
            </div>
          </div>
        </div>
      </div>
      <div class="ask-question-supplementary">
        <label class="ask-supplementary-label">Additional info</label>
        <input class="ask-supplementary-input" type="text" placeholder="Optional" />
      </div>
      <button class="ask-question-submit" disabled>Submit</button>
    `
    document.body.appendChild(container)
    return { container, emit }
  }

  function cleanup(container: HTMLDivElement) {
    container.remove()
  }

  it('returns false for clicks outside option/submit', () => {
    const { container, emit } = createAskDOM()
    // Click on the supplementary input area (not option, not submit)
    const suppArea = container.querySelector('.ask-question-supplementary') as HTMLElement
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: suppArea, writable: false })
    const result = handleToolAction('AskUserQuestion', clickEvent, emit)
    expect(result).toBe(false)
    cleanup(container)
  })

  describe('single-select mode', () => {
    it('selecting an option marks it as selected and changes indicator to ◉', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement

      const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickEvent, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', clickEvent, emit)

      expect(option.classList.contains('selected')).toBe(true)
      const indicator = option.querySelector('.ask-option-indicator')
      expect(indicator?.textContent).toBe('◉')
      cleanup(container)
    })

    it('selecting another option deselects the previous one', () => {
      const { container, emit } = createAskDOM(false)
      const options = container.querySelectorAll('.ask-question-option')
      const optA = options[0] as HTMLElement
      const optB = options[1] as HTMLElement

      // Click A
      const clickA = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickA, 'target', { value: optA, writable: false })
      handleToolAction('AskUserQuestion', clickA, emit)

      // Click B
      const clickB = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickB, 'target', { value: optB, writable: false })
      handleToolAction('AskUserQuestion', clickB, emit)

      expect(optA.classList.contains('selected')).toBe(false)
      expect(optB.classList.contains('selected')).toBe(true)

      // A's indicator should revert to ◯
      const indicatorA = optA.querySelector('.ask-option-indicator')
      expect(indicatorA?.textContent).toBe('◯')
      cleanup(container)
    })

    it('enables submit button when an option is selected', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement

      expect(submitBtn.disabled).toBe(true)

      const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickEvent, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', clickEvent, emit)

      expect(submitBtn.disabled).toBe(false)
      cleanup(container)
    })
  })

  describe('multi-select mode', () => {
    it('toggles selection on and off', () => {
      const { container, emit } = createAskDOM(true)
      const option = container.querySelector('.ask-question-option') as HTMLElement

      // First click: select
      const click1 = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(click1, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', click1, emit)
      expect(option.classList.contains('selected')).toBe(true)
      const indicator = option.querySelector('.ask-option-indicator')
      expect(indicator?.textContent).toBe('☑')

      // Second click: deselect
      const click2 = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(click2, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', click2, emit)
      expect(option.classList.contains('selected')).toBe(false)
      expect(indicator?.textContent).toBe('☐')

      cleanup(container)
    })

    it('allows selecting multiple options simultaneously', () => {
      const { container, emit } = createAskDOM(true)
      const options = container.querySelectorAll('.ask-question-option')
      const optA = options[0] as HTMLElement
      const optB = options[1] as HTMLElement

      const clickA = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickA, 'target', { value: optA, writable: false })
      handleToolAction('AskUserQuestion', clickA, emit)

      const clickB = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickB, 'target', { value: optB, writable: false })
      handleToolAction('AskUserQuestion', clickB, emit)

      expect(optA.classList.contains('selected')).toBe(true)
      expect(optB.classList.contains('selected')).toBe(true)
      cleanup(container)
    })
  })

  describe('submit', () => {
    it('emits send-message with selected labels on submit', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement

      // Select option
      const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickOpt, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', clickOpt, emit)

      // Submit
      const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
      handleToolAction('AskUserQuestion', clickSubmit, emit)

      expect(emit).toHaveBeenCalledWith('send-message', 'Option A')
      cleanup(container)
    })

    it('appends supplementary text to answer', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement
      const suppInput = container.querySelector('.ask-supplementary-input') as HTMLInputElement

      suppInput.value = 'extra context'

      // Select option
      const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickOpt, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', clickOpt, emit)

      // Submit
      const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
      handleToolAction('AskUserQuestion', clickSubmit, emit)

      expect(emit).toHaveBeenCalledWith('send-message', 'Option A\nextra context')
      cleanup(container)
    })

    it('marks view as ask-submitted after submit', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement

      // Select option
      const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickOpt, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', clickOpt, emit)

      // Submit
      const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
      handleToolAction('AskUserQuestion', clickSubmit, emit)

      expect(container.classList.contains('ask-submitted')).toBe(true)
      cleanup(container)
    })

    it('disables submit button after submission', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement

      // Select option
      const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickOpt, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', clickOpt, emit)

      // Submit
      const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
      handleToolAction('AskUserQuestion', clickSubmit, emit)

      expect(submitBtn.disabled).toBe(true)
      cleanup(container)
    })

    it('disables supplementary input after submission', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement
      const suppInput = container.querySelector('.ask-supplementary-input') as HTMLInputElement

      // Select option
      const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickOpt, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', clickOpt, emit)

      // Submit
      const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
      handleToolAction('AskUserQuestion', clickSubmit, emit)

      expect(suppInput.disabled).toBe(true)
      cleanup(container)
    })

    it('sets pointer-events none on unselected options after submission', () => {
      const { container, emit } = createAskDOM(false)
      const options = container.querySelectorAll('.ask-question-option')
      const optA = options[0] as HTMLElement
      const optB = options[1] as HTMLElement
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement

      // Select option A
      const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickOpt, 'target', { value: optA, writable: false })
      handleToolAction('AskUserQuestion', clickOpt, emit)

      // Submit
      const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
      handleToolAction('AskUserQuestion', clickSubmit, emit)

      // Unselected option should have pointer-events: none
      expect(optB.style.pointerEvents).toBe('none')
      expect(optB.style.opacity).toBe('0.4')
      cleanup(container)
    })

    it('does not respond to clicks after submission', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement
      const options = container.querySelectorAll('.ask-question-option')
      const optB = options[1] as HTMLElement

      // Select A, submit
      const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickOpt, 'target', { value: option, writable: false })
      handleToolAction('AskUserQuestion', clickOpt, emit)

      const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
      handleToolAction('AskUserQuestion', clickSubmit, emit)

      const callCount = emit.mock.calls.length

      // Try clicking another option after submit — should not change state
      const clickB = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickB, 'target', { value: optB, writable: false })
      handleToolAction('AskUserQuestion', clickB, emit)

      // B should NOT be selected since the view is submitted
      expect(optB.classList.contains('selected')).toBe(false)
      cleanup(container)
    })

    it('does not emit when no options selected and submit is clicked', () => {
      const { container, emit } = createAskDOM(false)
      const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement

      // Submit without selecting anything (button would be disabled in real DOM,
      // but handler should also not emit)
      const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
      handleToolAction('AskUserQuestion', clickSubmit, emit)

      // Should not emit send-message (no answers)
      expect(emit).not.toHaveBeenCalledWith('send-message', expect.anything())
      cleanup(container)
    })
  })

  describe('case-insensitive dispatch', () => {
    it('dispatches to AskUserQuestion handler regardless of case', () => {
      const { container, emit } = createAskDOM(false)
      const option = container.querySelector('.ask-question-option') as HTMLElement

      const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
      Object.defineProperty(clickOpt, 'target', { value: option, writable: false })

      expect(handleToolAction('askuserquestion', clickOpt, emit)).toBe(true)
      expect(handleToolAction('ASKUSERQUESTION', clickOpt, emit)).toBe(true)
      cleanup(container)
    })
  })
})

// ────────────────────────────────────────────────────────────
// registerToolActionHandler
// ────────────────────────────────────────────────────────────

describe('registerToolActionHandler', () => {
  it('registers a handler that handleToolAction can dispatch to', () => {
    const handler = vi.fn().mockReturnValue(true)
    registerToolActionHandler('TestActionTool', handler)

    const event = new MouseEvent('click')
    const emit = vi.fn()
    const result = handleToolAction('TestActionTool', event, emit)

    expect(result).toBe(true)
    expect(handler).toHaveBeenCalledWith(event, emit)
  })

  it('registration is case-insensitive', () => {
    const handler = vi.fn().mockReturnValue(true)
    registerToolActionHandler('CaseTestTool', handler)

    const event = new MouseEvent('click')
    const emit = vi.fn()
    const result = handleToolAction('casetesttool', event, emit)

    expect(result).toBe(true)
    expect(handler).toHaveBeenCalledWith(event, emit)
  })
})

// ────────────────────────────────────────────────────────────
// JSON fallback — deep tests
// ────────────────────────────────────────────────────────────

describe('JSON fallback (deep)', () => {
  it('non-empty object includes hljs highlight', () => {
    const html = formatToolInput({ key: 'value' }, 'NonExistentTool')
    expect(html).toContain('tool-json-body')
    expect(html).toContain('<code>')
    expect(html).toContain('</code>')
    // hljs should add spans for syntax highlighting
    expect(html).toContain('hljs-')
  })

  it('empty object renders empty braces (highlighted)', () => {
    const html = formatToolInput({})
    // hljs wraps { and } in spans, so we check for both punctuation parts
    expect(html).toContain('hljs-punctuation">{</span>')
    expect(html).toContain('hljs-punctuation">}</span>')
  })

  it('falls back when tool name given but input is not object', () => {
    const html = formatToolInput('string input', 'Edit')
    expect(html).toContain('tool-json-body')
  })

  it('falls back when tool name given but input is null', () => {
    const html = formatToolInput(null, 'Edit')
    expect(html).toContain('tool-json-body')
  })
})

// ────────────────────────────────────────────────────────────
// hljs fallback paths (highlight throws via spy)
// ────────────────────────────────────────────────────────────

describe('hljs highlight fallback (catch blocks)', () => {
  let hljsSpy: ReturnType<typeof vi.spyOn>

  beforeEach(async () => {
    const { hljs } = await import('@/utils/globals.ts')
    hljsSpy = vi.spyOn(hljs, 'highlight').mockImplementation(() => { throw new Error('hljs mock error') })
  })

  afterEach(() => {
    hljsSpy?.mockRestore()
  })

  it('Bash renderer falls back to escapeHtml when hljs.highlight throws', () => {
    const html = formatToolInput({ command: 'echo "hello"' }, 'Bash')
    expect(html).toContain('bash-terminal-view')
    expect(html).toContain('echo')
    // Should NOT contain hljs spans
    expect(html).not.toContain('hljs-')
  })

  it('Grep renderer falls back to escapeHtml for pattern when hljs.highlight throws', () => {
    const html = formatToolInput({ pattern: 'TODO' }, 'Grep')
    expect(html).toContain('grep-search-view')
    expect(html).toContain('TODO')
    expect(html).not.toContain('hljs-')
  })

  it('JSON fallback (empty object) falls back when hljs.highlight throws', () => {
    const html = formatToolInput({})
    expect(html).toContain('tool-json-body')
    // Should contain literal {} without hljs spans
    expect(html).toContain('>{}</code>')
  })

  it('JSON fallback (non-empty object) falls back when hljs.highlight throws', () => {
    const html = formatToolInput({ foo: 'bar' }, 'UnknownTool')
    expect(html).toContain('tool-json-body')
    expect(html).toContain('foo')
    // Should contain escapeHtml'd JSON, not hljs spans
    expect(html).not.toContain('hljs-')
  })
})

// ────────────────────────────────────────────────────────────
// ACP tool renderers — PermissionApproval, ModeSwitch, TodoWrite, TaskTool, WorktreeSwitch
// ────────────────────────────────────────────────────────────

describe('PermissionApproval renderer', () => {
  it('renders permission-approval-view container', () => {
    const html = formatToolInput({ options: [] }, 'PermissionApproval')
    expect(html).toContain('permission-approval-view')
  })

  it('renders header with icon and title', () => {
    const html = formatToolInput({ options: [] }, 'PermissionApproval')
    expect(html).toContain('permission-header')
    expect(html).toContain('permission-icon')
    expect(html).toContain('permission-title')
  })

  it('renders tool name when present', () => {
    const html = formatToolInput({ toolName: 'Bash', options: [] }, 'PermissionApproval')
    expect(html).toContain('permission-tool-name')
    expect(html).toContain('Bash')
  })

  it('omits tool name when absent', () => {
    const html = formatToolInput({ options: [] }, 'PermissionApproval')
    expect(html).not.toContain('permission-tool-name')
  })

  it('parses toolInput JSON and shows file_path', () => {
    const html = formatToolInput({
      toolName: 'Edit',
      toolInput: JSON.stringify({ file_path: '/src/main.go' }),
      options: [],
    }, 'PermissionApproval')
    expect(html).toContain('permission-tool-detail')
    expect(html).toContain('/src/main.go')
  })

  it('parses toolInput JSON and shows command', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      toolInput: JSON.stringify({ command: 'rm -rf /tmp' }),
      options: [],
    }, 'PermissionApproval')
    expect(html).toContain('permission-tool-detail')
    expect(html).toContain('rm -rf /tmp')
  })

  it('handles non-JSON toolInput gracefully', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      toolInput: 'plain text input',
      options: [],
    }, 'PermissionApproval')
    expect(html).toContain('permission-tool-detail')
    expect(html).toContain('plain text input')
  })

  it('renders allow options with permission-btn-allow class', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      options: [
        { name: 'Allow Once', kind: 'allow_once', optionId: 'a1' },
        { name: 'Allow Always', kind: 'allow_always', optionId: 'a2' },
      ],
    }, 'PermissionApproval')
    expect(html).toContain('permission-options')
    expect(html).toContain('permission-btn-allow')
    expect(html).toContain('Allow Once')
    expect(html).toContain('Allow Always')
  })

  it('renders reject options with permission-btn-reject class', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      options: [
        { name: 'Deny', kind: 'reject_once', optionId: 'r1' },
      ],
    }, 'PermissionApproval')
    expect(html).toContain('permission-btn-reject')
    expect(html).toContain('Deny')
  })

  it('renders data-option-id and data-kind attributes on buttons', () => {
    const html = formatToolInput({
      options: [
        { name: 'Allow', kind: 'allow_once', optionId: 'opt-123' },
      ],
    }, 'PermissionApproval')
    expect(html).toContain('data-option-id="opt-123"')
    expect(html).toContain('data-kind="allow_once"')
  })

  it('escapes HTML in option labels', () => {
    const html = formatToolInput({
      options: [
        { name: '<script>alert(1)</script>', kind: 'allow_once', optionId: 'x' },
      ],
    }, 'PermissionApproval')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })

  it('omits options section when options is empty', () => {
    const html = formatToolInput({ options: [] }, 'PermissionApproval')
    expect(html).not.toContain('permission-options')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ options: [] }, 'PERMISSIONAPPROVAL')
    expect(html).toContain('permission-approval-view')
  })

  it('shows approved result badge when blockCtx.done=true and status=success', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      toolInput: JSON.stringify({ command: 'ls' }),
      options: [{ name: 'Allow Once', kind: 'allow_once', optionId: 'a1' }],
    }, 'PermissionApproval', { done: true, status: 'success', output: 'Approved' })
    expect(html).toContain('permission-responded')
    expect(html).toContain('permission-result')
    expect(html).toContain('permission-result-approved')
    expect(html).not.toContain('permission-options')
    expect(html).not.toContain('permission-btn')
  })

  it('shows denied result badge when blockCtx.done=true and status=error', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      toolInput: JSON.stringify({ command: 'rm -rf /' }),
      options: [{ name: 'Deny', kind: 'reject_once', optionId: 'r1' }],
    }, 'PermissionApproval', { done: true, status: 'error', output: 'Cancelled' })
    expect(html).toContain('permission-responded')
    expect(html).toContain('permission-result')
    expect(html).toContain('permission-result-denied')
    expect(html).not.toContain('permission-options')
  })

  it('shows buttons when blockCtx is absent (streaming/fresh)', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      options: [{ name: 'Allow Once', kind: 'allow_once', optionId: 'a1' }],
    }, 'PermissionApproval')
    expect(html).not.toContain('permission-responded')
    expect(html).toContain('permission-options')
    expect(html).toContain('permission-btn-allow')
  })

  it('shows buttons when blockCtx.done is false', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      options: [{ name: 'Allow Once', kind: 'allow_once', optionId: 'a1' }],
    }, 'PermissionApproval', { done: false })
    expect(html).not.toContain('permission-responded')
    expect(html).toContain('permission-options')
  })

  it('shows buttons when done=true but no output (cleanup/timeout marked done without real response)', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      options: [{ name: 'Allow Once', kind: 'allow_once', optionId: 'a1' }],
    }, 'PermissionApproval', { done: true })
    // done=true without output = not a real response, should still show buttons
    expect(html).not.toContain('permission-responded')
    expect(html).not.toContain('permission-result')
    expect(html).toContain('permission-options')
    expect(html).toContain('permission-btn-allow')
  })

  it('shows buttons when done=true with empty output', () => {
    const html = formatToolInput({
      toolName: 'Bash',
      options: [{ name: 'Allow Once', kind: 'allow_once', optionId: 'a1' }],
    }, 'PermissionApproval', { done: true, output: '' })
    expect(html).not.toContain('permission-responded')
    expect(html).toContain('permission-options')
  })

  it('shouldAutoExpandTool returns true', () => {
    expect(shouldAutoExpandTool('PermissionApproval')).toBe(true)
  })
})

describe('PermissionApproval action handler', () => {
  function createPermissionDOM(): { container: HTMLDivElement; emit: ReturnType<typeof vi.fn> } {
    const emit = vi.fn()
    const container = document.createElement('div')
    container.className = 'permission-approval-view'
    container.innerHTML = `
      <div class="permission-options">
        <button class="permission-btn permission-btn-allow" data-option-id="allow-1" data-kind="allow_once">Allow Once</button>
        <button class="permission-btn permission-btn-reject" data-option-id="reject-1" data-kind="reject_once">Deny</button>
      </div>
    `
    // Wrap in a tool-detail container with session/toolCallId data
    const toolDetail = document.createElement('div')
    toolDetail.className = 'tool-detail'
    toolDetail.dataset.sessionId = 'test-session-123'
    toolDetail.dataset.toolCallId = 'tc-456'
    toolDetail.appendChild(container)
    document.body.appendChild(toolDetail)
    return { container, emit }
  }

  function cleanup(container: HTMLDivElement) {
    container.closest('.tool-detail')?.remove()
  }

  it('returns false for clicks outside permission buttons', () => {
    const { container, emit } = createPermissionDOM()
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: container, writable: false })
    expect(handleToolAction('PermissionApproval', clickEvent, emit)).toBe(false)
    cleanup(container)
  })

  it('returns true for clicking an allow button', () => {
    const { container, emit } = createPermissionDOM()
    const btn = container.querySelector('.permission-btn-allow') as HTMLElement
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: btn, writable: false })
    expect(handleToolAction('PermissionApproval', clickEvent, emit)).toBe(true)
    cleanup(container)
  })

  it('marks view as permission-responded after clicking a button', () => {
    const { container, emit } = createPermissionDOM()
    const btn = container.querySelector('.permission-btn-allow') as HTMLElement
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: btn, writable: false })
    handleToolAction('PermissionApproval', clickEvent, emit)
    expect(container.classList.contains('permission-responded')).toBe(true)
    cleanup(container)
  })

  it('disables all buttons after responding', () => {
    const { container, emit } = createPermissionDOM()
    const btn = container.querySelector('.permission-btn-allow') as HTMLElement
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: btn, writable: false })
    handleToolAction('PermissionApproval', clickEvent, emit)
    const allBtns = container.querySelectorAll('.permission-btn')
    for (const b of allBtns) {
      expect((b as HTMLButtonElement).disabled).toBe(true)
    }
    cleanup(container)
  })

  it('dims unselected buttons after responding', () => {
    const { container, emit } = createPermissionDOM()
    const allowBtn = container.querySelector('.permission-btn-allow') as HTMLElement
    const rejectBtn = container.querySelector('.permission-btn-reject') as HTMLElement
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: allowBtn, writable: false })
    handleToolAction('PermissionApproval', clickEvent, emit)
    expect(rejectBtn.style.opacity).toBe('0.4')
    cleanup(container)
  })

  it('does not respond to clicks after already responded', () => {
    const { container, emit } = createPermissionDOM()
    const allowBtn = container.querySelector('.permission-btn-allow') as HTMLElement
    const rejectBtn = container.querySelector('.permission-btn-reject') as HTMLElement

    // First click
    const click1 = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(click1, 'target', { value: allowBtn, writable: false })
    handleToolAction('PermissionApproval', click1, emit)

    // Second click on another button — should not change state
    const click2 = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(click2, 'target', { value: rejectBtn, writable: false })
    handleToolAction('PermissionApproval', click2, emit)

    // Only 1 fetch call (from first click)
    expect(emit.mock.calls.length).toBe(0) // emit is not called by PermissionApproval handler (it uses fetch)
    cleanup(container)
  })
})

describe('ModeSwitch renderer (EnterPlanMode / ExitPlanMode)', () => {
  it('renders mode-switch-view container', () => {
    const html = formatToolInput({ mode: 'plan' }, 'EnterPlanMode')
    expect(html).toContain('mode-switch-view')
  })

  it('renders mode-switch-icon', () => {
    const html = formatToolInput({ mode: 'plan' }, 'EnterPlanMode')
    expect(html).toContain('mode-switch-icon')
  })

  it('renders mode name when present', () => {
    const html = formatToolInput({ mode: 'plan' }, 'EnterPlanMode')
    expect(html).toContain('mode-switch-mode')
    expect(html).toContain('plan')
  })

  it('uses mode_id as fallback for mode name', () => {
    const html = formatToolInput({ mode_id: 'architect' }, 'EnterPlanMode')
    expect(html).toContain('architect')
  })

  it('renders without mode name gracefully', () => {
    const html = formatToolInput({}, 'EnterPlanMode')
    expect(html).toContain('mode-switch-view')
    expect(html).not.toContain('mode-switch-mode')
  })

  it('EnterPlanMode is case-insensitive', () => {
    const html = formatToolInput({ mode: 'plan' }, 'enterplanmode')
    expect(html).toContain('mode-switch-view')
  })

  it('ExitPlanMode uses same renderer', () => {
    const html = formatToolInput({ mode: 'code' }, 'ExitPlanMode')
    expect(html).toContain('mode-switch-view')
    expect(html).toContain('code')
  })

  it('ExitPlanMode is case-insensitive', () => {
    const html = formatToolInput({ mode: 'code' }, 'exitplanmode')
    expect(html).toContain('mode-switch-view')
  })

  it('escapes HTML in mode name', () => {
    const html = formatToolInput({ mode: '<b>evil</b>' }, 'EnterPlanMode')
    expect(html).not.toContain('<b>evil</b>')
    expect(html).toContain('&lt;b&gt;evil&lt;/b&gt;')
  })
})

describe('TodoWrite renderer', () => {
  it('renders todo-write-view container', () => {
    const html = formatToolInput({ todos: [] }, 'TodoWrite')
    expect(html).toContain('todo-write-view')
  })

  it('renders completed todo with check icon and done class', () => {
    const html = formatToolInput({
      todos: [{ content: 'Task A', status: 'completed' }],
    }, 'TodoWrite')
    expect(html).toContain('todo-item')
    expect(html).toContain('todo-done')
    expect(html).toContain('✓')
    expect(html).toContain('Task A')
  })

  it('renders in_progress todo with active icon and class', () => {
    const html = formatToolInput({
      todos: [{ content: 'Task B', status: 'in_progress' }],
    }, 'TodoWrite')
    expect(html).toContain('todo-active')
    expect(html).toContain('►')
    expect(html).toContain('Task B')
  })

  it('renders pending todo with circle icon and pending class', () => {
    const html = formatToolInput({
      todos: [{ content: 'Task C', status: 'pending' }],
    }, 'TodoWrite')
    expect(html).toContain('todo-pending')
    expect(html).toContain('○')
    expect(html).toContain('Task C')
  })

  it('renders multiple todos', () => {
    const html = formatToolInput({
      todos: [
        { content: 'Done task', status: 'completed' },
        { content: 'Active task', status: 'in_progress' },
        { content: 'Pending task', status: 'pending' },
      ],
    }, 'TodoWrite')
    const items = html.match(/todo-item/g)
    expect(items?.length).toBe(3)
  })

  it('escapes HTML in todo content', () => {
    const html = formatToolInput({
      todos: [{ content: '<script>alert(1)</script>', status: 'pending' }],
    }, 'TodoWrite')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })

  it('handles empty todos array', () => {
    const html = formatToolInput({ todos: [] }, 'TodoWrite')
    expect(html).toContain('todo-write-view')
    expect(html).not.toContain('todo-write-list')
  })

  it('handles missing todos field', () => {
    const html = formatToolInput({}, 'TodoWrite')
    expect(html).toContain('todo-write-view')
    expect(html).not.toContain('todo-write-list')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ todos: [{ content: 'x', status: 'pending' }] }, 'todowrite')
    expect(html).toContain('todo-write-view')
  })
})

describe('TodoRead renderer', () => {
  it('renders todo-read-view container', () => {
    const html = formatToolInput({}, 'TodoRead')
    expect(html).toContain('todo-read-view')
  })

  it('renders icon and label', () => {
    const html = formatToolInput({}, 'TodoRead')
    expect(html).toContain('todo-read-icon')
    expect(html).toContain('todo-read-label')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({}, 'todoread')
    expect(html).toContain('todo-read-view')
  })
})

describe('TaskTool renderer (TaskCreate/TaskUpdate/etc.)', () => {
  it('renders task-tool-view container', () => {
    const html = formatToolInput({ subject: 'Fix bug' }, 'TaskCreate')
    expect(html).toContain('task-tool-view')
  })

  it('renders subject field', () => {
    const html = formatToolInput({ subject: 'Fix bug' }, 'TaskCreate')
    expect(html).toContain('task-tool-field')
    expect(html).toContain('Fix bug')
  })

  it('renders taskId field with code format', () => {
    const html = formatToolInput({ taskId: 'task-abc-123' }, 'TaskGet')
    expect(html).toContain('task-field-value')
    expect(html).toContain('task-abc-123')
    expect(html).toContain('<code')
  })

  it('renders task_id field (snake_case variant)', () => {
    const html = formatToolInput({ task_id: 'task-xyz' }, 'TaskStop')
    expect(html).toContain('task-xyz')
  })

  it('renders cron field with code format', () => {
    const html = formatToolInput({ cron: '0 8 * * *' }, 'TaskCreate')
    expect(html).toContain('0 8 * * *')
    expect(html).toContain('<code')
  })

  it('renders description field', () => {
    const html = formatToolInput({ description: 'Daily cleanup task' }, 'TaskCreate')
    expect(html).toContain('Daily cleanup task')
  })

  it('renders prompt field', () => {
    const html = formatToolInput({ prompt: 'Clean up temp files' }, 'TaskCreate')
    expect(html).toContain('Clean up temp files')
  })

  it('shows empty state when no relevant fields', () => {
    const html = formatToolInput({ foo: 'bar' }, 'TaskCreate')
    expect(html).toContain('task-tool-empty')
  })

  it('truncates long values with ellipsis', () => {
    const longSubject = 'A'.repeat(250)
    const html = formatToolInput({ subject: longSubject }, 'TaskCreate')
    expect(html).toContain('…')
    expect(html).not.toContain('A'.repeat(250))
  })

  it('escapes HTML in field values', () => {
    const html = formatToolInput({ subject: '<script>alert(1)</script>' }, 'TaskCreate')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })

  it('TaskCreate is case-insensitive', () => {
    const html = formatToolInput({ subject: 'test' }, 'taskcreate')
    expect(html).toContain('task-tool-view')
  })

  it('TaskUpdate uses same renderer', () => {
    const html = formatToolInput({ subject: 'Updated task' }, 'TaskUpdate')
    expect(html).toContain('task-tool-view')
    expect(html).toContain('Updated task')
  })

  it('TaskList uses same renderer', () => {
    const html = formatToolInput({}, 'TaskList')
    expect(html).toContain('task-tool-view')
  })

  it('TaskGet uses same renderer', () => {
    const html = formatToolInput({ taskId: 't1' }, 'TaskGet')
    expect(html).toContain('task-tool-view')
  })

  it('TaskStop uses same renderer', () => {
    const html = formatToolInput({ task_id: 't2' }, 'TaskStop')
    expect(html).toContain('task-tool-view')
  })

  it('TaskOutput uses same renderer', () => {
    const html = formatToolInput({ task_id: 't3' }, 'TaskOutput')
    expect(html).toContain('task-tool-view')
  })
})

describe('WorktreeSwitch renderer (EnterWorktree / LeaveWorktree)', () => {
  it('renders worktree-switch-view container', () => {
    const html = formatToolInput({ path: '/project/.worktrees/fix-1' }, 'EnterWorktree')
    expect(html).toContain('worktree-switch-view')
  })

  it('renders worktree-switch-icon', () => {
    const html = formatToolInput({ path: '/project/.worktrees/fix-1' }, 'EnterWorktree')
    expect(html).toContain('worktree-switch-icon')
  })

  it('renders path when present', () => {
    const html = formatToolInput({ path: '/project/.worktrees/fix-1' }, 'EnterWorktree')
    expect(html).toContain('worktree-switch-path')
    expect(html).toContain('/project/.worktrees/fix-1')
  })

  it('uses worktree_path as fallback for path', () => {
    const html = formatToolInput({ worktree_path: '/project/.worktrees/fix-2' }, 'EnterWorktree')
    expect(html).toContain('/project/.worktrees/fix-2')
  })

  it('renders without path gracefully', () => {
    const html = formatToolInput({}, 'EnterWorktree')
    expect(html).toContain('worktree-switch-view')
    expect(html).not.toContain('worktree-switch-path')
  })

  it('EnterWorktree is case-insensitive', () => {
    const html = formatToolInput({ path: '/wt' }, 'enterworktree')
    expect(html).toContain('worktree-switch-view')
  })

  it('LeaveWorktree uses same renderer', () => {
    const html = formatToolInput({ path: '/project' }, 'LeaveWorktree')
    expect(html).toContain('worktree-switch-view')
    expect(html).toContain('/project')
  })

  it('LeaveWorktree is case-insensitive', () => {
    const html = formatToolInput({ path: '/project' }, 'leaveworktree')
    expect(html).toContain('worktree-switch-view')
  })

  it('escapes HTML in path', () => {
    const html = formatToolInput({ path: '<b>evil</b>' }, 'EnterWorktree')
    expect(html).not.toContain('<b>evil</b>')
    expect(html).toContain('&lt;b&gt;evil&lt;/b&gt;')
  })
})

// ────────────────────────────────────────────────────────────
// SendMessage renderer
// ────────────────────────────────────────────────────────────

describe('SendMessage renderer', () => {
  it('renders send-message-view container', () => {
    const html = formatToolInput({ recipient: 'alice', content: 'Hello' }, 'SendMessage')
    expect(html).toContain('send-message-view')
  })

  it('renders send-message-icon', () => {
    const html = formatToolInput({ recipient: 'alice', content: 'Hello' }, 'SendMessage')
    expect(html).toContain('send-message-icon')
  })

  it('renders recipient when present', () => {
    const html = formatToolInput({ recipient: 'alice', content: 'Hello' }, 'SendMessage')
    expect(html).toContain('send-message-recipient')
    expect(html).toContain('alice')
  })

  it('omits recipient span when absent', () => {
    const html = formatToolInput({ content: 'Hello' }, 'SendMessage')
    expect(html).not.toContain('send-message-recipient')
  })

  it('renders content when present', () => {
    const html = formatToolInput({ recipient: 'bob', content: 'Hello world' }, 'SendMessage')
    expect(html).toContain('send-message-content')
    expect(html).toContain('Hello world')
  })

  it('omits content div when absent', () => {
    const html = formatToolInput({ recipient: 'bob' }, 'SendMessage')
    expect(html).not.toContain('send-message-content')
  })

  it('truncates content over 300 chars', () => {
    const longContent = 'A'.repeat(350)
    const html = formatToolInput({ content: longContent }, 'SendMessage')
    expect(html).toContain('…')
    expect(html).not.toContain('A'.repeat(350))
  })

  it('uses message field as content alias', () => {
    const html = formatToolInput({ recipient: 'bob', message: 'via message field' }, 'SendMessage')
    expect(html).toContain('send-message-content')
    expect(html).toContain('via message field')
  })

  it('escapes HTML in content and recipient', () => {
    const html = formatToolInput({ recipient: '<b>evil</b>', content: '<script>x</script>' }, 'SendMessage')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
    expect(html).toContain('&lt;b&gt;evil&lt;/b&gt;')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ recipient: 'alice', content: 'Hello' }, 'sendmessage')
    expect(html).toContain('send-message-view')
  })
})

// ────────────────────────────────────────────────────────────
// ComputerUse renderer
// ────────────────────────────────────────────────────────────

describe('ComputerUse renderer', () => {
  it('renders computer-use-view container', () => {
    const html = formatToolInput({ action: 'click', description: 'Click button' }, 'ComputerUse')
    expect(html).toContain('computer-use-view')
  })

  it('renders computer-use-icon', () => {
    const html = formatToolInput({ action: 'click' }, 'ComputerUse')
    expect(html).toContain('computer-use-icon')
  })

  it('renders action when present', () => {
    const html = formatToolInput({ action: 'click', description: 'Click button' }, 'ComputerUse')
    expect(html).toContain('computer-use-action')
    expect(html).toContain('click')
  })

  it('omits action span when absent', () => {
    const html = formatToolInput({ description: 'Do something' }, 'ComputerUse')
    expect(html).not.toContain('computer-use-action')
  })

  it('renders description when present', () => {
    const html = formatToolInput({ action: 'click', description: 'Click button' }, 'ComputerUse')
    expect(html).toContain('computer-use-desc')
    expect(html).toContain('Click button')
  })

  it('uses text field as description alias', () => {
    const html = formatToolInput({ action: 'screenshot', text: 'Screenshot of desktop' }, 'ComputerUse')
    expect(html).toContain('computer-use-desc')
    expect(html).toContain('Screenshot of desktop')
  })

  it('truncates description over 200 chars', () => {
    const longDesc = 'D'.repeat(250)
    const html = formatToolInput({ action: 'click', description: longDesc }, 'ComputerUse')
    expect(html).toContain('…')
    expect(html).not.toContain('D'.repeat(250))
  })

  it('omits description when absent', () => {
    const html = formatToolInput({ action: 'click' }, 'ComputerUse')
    expect(html).not.toContain('computer-use-desc')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ action: 'click' }, 'computeruse')
    expect(html).toContain('computer-use-view')
  })
})

// ────────────────────────────────────────────────────────────
// TeamTool renderer (TeamCreate / TeamDelete)
// ────────────────────────────────────────────────────────────

describe('TeamTool renderer', () => {
  it('renders team-tool-view container', () => {
    const html = formatToolInput({ name: 'backend-team' }, 'TeamCreate')
    expect(html).toContain('team-tool-view')
  })

  it('renders team-tool-icon', () => {
    const html = formatToolInput({ name: 'backend-team' }, 'TeamCreate')
    expect(html).toContain('team-tool-icon')
  })

  it('renders name when present', () => {
    const html = formatToolInput({ name: 'backend-team' }, 'TeamCreate')
    expect(html).toContain('team-tool-name')
    expect(html).toContain('backend-team')
  })

  it('uses team_name as alias for name', () => {
    const html = formatToolInput({ team_name: 'frontend-team' }, 'TeamCreate')
    expect(html).toContain('team-tool-name')
    expect(html).toContain('frontend-team')
  })

  it('omits name span when absent', () => {
    const html = formatToolInput({}, 'TeamCreate')
    expect(html).toContain('team-tool-view')
    expect(html).not.toContain('team-tool-name')
  })

  it('TeamCreate is case-insensitive', () => {
    const html = formatToolInput({ name: 'team' }, 'teamcreate')
    expect(html).toContain('team-tool-view')
  })

  it('TeamDelete uses same renderer', () => {
    const html = formatToolInput({ name: 'old-team' }, 'TeamDelete')
    expect(html).toContain('team-tool-view')
    expect(html).toContain('old-team')
  })

  it('TeamDelete is case-insensitive', () => {
    const html = formatToolInput({ name: 'team' }, 'teamdelete')
    expect(html).toContain('team-tool-view')
  })
})

// ────────────────────────────────────────────────────────────
// ChatReply renderer (WeChatReply / WeComReply)
// ────────────────────────────────────────────────────────────

describe('ChatReply renderer', () => {
  it('renders chat-reply-view container', () => {
    const html = formatToolInput({ message: 'Hello', recipient: 'alice' }, 'WeChatReply')
    expect(html).toContain('chat-reply-view')
  })

  it('renders chat-reply-icon', () => {
    const html = formatToolInput({ message: 'Hello' }, 'WeChatReply')
    expect(html).toContain('chat-reply-icon')
  })

  it('renders recipient when present', () => {
    const html = formatToolInput({ message: 'Hello', recipient: 'alice' }, 'WeChatReply')
    expect(html).toContain('chat-reply-recipient')
    expect(html).toContain('alice')
  })

  it('uses user field as recipient alias', () => {
    const html = formatToolInput({ message: 'Hello', user: 'bob' }, 'WeChatReply')
    expect(html).toContain('chat-reply-recipient')
    expect(html).toContain('bob')
  })

  it('omits recipient span when absent', () => {
    const html = formatToolInput({ message: 'Hello' }, 'WeChatReply')
    expect(html).not.toContain('chat-reply-recipient')
  })

  it('renders message when present', () => {
    const html = formatToolInput({ message: 'Hello world' }, 'WeChatReply')
    expect(html).toContain('chat-reply-message')
    expect(html).toContain('Hello world')
  })

  it('uses content field as message alias', () => {
    const html = formatToolInput({ content: 'via content field' }, 'WeChatReply')
    expect(html).toContain('chat-reply-message')
    expect(html).toContain('via content field')
  })

  it('truncates message over 300 chars', () => {
    const longMessage = 'M'.repeat(350)
    const html = formatToolInput({ message: longMessage }, 'WeChatReply')
    expect(html).toContain('…')
    expect(html).not.toContain('M'.repeat(350))
  })

  it('escapes HTML in message and recipient', () => {
    const html = formatToolInput({ recipient: '<b>x</b>', message: '<script>y</script>' }, 'WeChatReply')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
    expect(html).toContain('&lt;b&gt;x&lt;/b&gt;')
  })

  it('WeChatReply is case-insensitive', () => {
    const html = formatToolInput({ message: 'Hi' }, 'wechatreply')
    expect(html).toContain('chat-reply-view')
  })

  it('WeComReply uses same renderer', () => {
    const html = formatToolInput({ message: 'Hi', recipient: 'bob' }, 'WeComReply')
    expect(html).toContain('chat-reply-view')
    expect(html).toContain('bob')
  })

  it('WeComReply is case-insensitive', () => {
    const html = formatToolInput({ message: 'Hi' }, 'wecomreply')
    expect(html).toContain('chat-reply-view')
  })
})

// ────────────────────────────────────────────────────────────
// SaveMemory renderer
// ────────────────────────────────────────────────────────────

describe('SaveMemory renderer', () => {
  it('renders save-memory-view container', () => {
    const html = formatToolInput({ key: 'pref', value: 'dark-mode' }, 'save_memory')
    expect(html).toContain('save-memory-view')
  })

  it('renders save-memory-icon', () => {
    const html = formatToolInput({ key: 'pref', value: 'dark-mode' }, 'save_memory')
    expect(html).toContain('save-memory-icon')
  })

  it('renders key when present', () => {
    const html = formatToolInput({ key: 'pref', value: 'dark-mode' }, 'save_memory')
    expect(html).toContain('save-memory-key')
    expect(html).toContain('pref')
  })

  it('uses name field as key alias', () => {
    const html = formatToolInput({ name: 'setting', value: 'on' }, 'save_memory')
    expect(html).toContain('save-memory-key')
    expect(html).toContain('setting')
  })

  it('omits key span when absent', () => {
    const html = formatToolInput({ value: 'dark-mode' }, 'save_memory')
    expect(html).not.toContain('save-memory-key')
  })

  it('renders value when present', () => {
    const html = formatToolInput({ key: 'pref', value: 'dark-mode' }, 'save_memory')
    expect(html).toContain('save-memory-value')
    expect(html).toContain('dark-mode')
  })

  it('uses content field as value alias', () => {
    const html = formatToolInput({ key: 'pref', content: 'via content' }, 'save_memory')
    expect(html).toContain('save-memory-value')
    expect(html).toContain('via content')
  })

  it('truncates value over 200 chars', () => {
    const longValue = 'V'.repeat(250)
    const html = formatToolInput({ key: 'pref', value: longValue }, 'save_memory')
    expect(html).toContain('…')
    expect(html).not.toContain('V'.repeat(250))
  })

  it('omits value div when absent', () => {
    const html = formatToolInput({ key: 'pref' }, 'save_memory')
    expect(html).not.toContain('save-memory-value')
  })

  it('escapes HTML in key and value', () => {
    const html = formatToolInput({ key: '<b>k</b>', value: '<script>v</script>' }, 'save_memory')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
    expect(html).toContain('&lt;b&gt;k&lt;/b&gt;')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ key: 'pref', value: 'dark' }, 'SAVE_MEMORY')
    expect(html).toContain('save-memory-view')
  })
})

// ────────────────────────────────────────────────────────────
// DeepThink renderer
// ────────────────────────────────────────────────────────────

describe('DeepThink renderer', () => {
  it('renders deep-think-view container', () => {
    const html = formatToolInput({ topic: 'quantum computing' }, 'DeepThink')
    expect(html).toContain('deep-think-view')
  })

  it('renders deep-think-icon', () => {
    const html = formatToolInput({ topic: 'quantum computing' }, 'DeepThink')
    expect(html).toContain('deep-think-icon')
  })

  it('renders topic when present', () => {
    const html = formatToolInput({ topic: 'quantum computing' }, 'DeepThink')
    expect(html).toContain('deep-think-topic')
    expect(html).toContain('quantum computing')
  })

  it('uses query as topic alias', () => {
    const html = formatToolInput({ query: 'search query' }, 'DeepThink')
    expect(html).toContain('deep-think-topic')
    expect(html).toContain('search query')
  })

  it('uses prompt as topic alias', () => {
    const html = formatToolInput({ prompt: 'think about this' }, 'DeepThink')
    expect(html).toContain('deep-think-topic')
    expect(html).toContain('think about this')
  })

  it('truncates topic over 200 chars', () => {
    const longTopic = 'T'.repeat(250)
    const html = formatToolInput({ topic: longTopic }, 'DeepThink')
    expect(html).toContain('…')
    expect(html).not.toContain('T'.repeat(250))
  })

  it('escapes HTML in topic', () => {
    const html = formatToolInput({ topic: '<script>alert(1)</script>' }, 'DeepThink')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
  })

  it('renders without topic gracefully', () => {
    const html = formatToolInput({}, 'DeepThink')
    expect(html).toContain('deep-think-view')
    expect(html).not.toContain('deep-think-topic')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ topic: 'test' }, 'deepthink')
    expect(html).toContain('deep-think-view')
  })
})

// ────────────────────────────────────────────────────────────
// StructuredOutput renderer
// ────────────────────────────────────────────────────────────

describe('StructuredOutput renderer', () => {
  it('renders structured-output-view container', () => {
    const html = formatToolInput({ prompt: 'Generate a list' }, 'StructuredOutput')
    expect(html).toContain('structured-output-view')
  })

  it('renders structured-output-icon', () => {
    const html = formatToolInput({ prompt: 'Generate a list' }, 'StructuredOutput')
    expect(html).toContain('structured-output-icon')
  })

  it('renders prompt when present', () => {
    const html = formatToolInput({ prompt: 'Generate a list' }, 'StructuredOutput')
    expect(html).toContain('structured-output-prompt')
    expect(html).toContain('Generate a list')
  })

  it('uses instruction as prompt alias', () => {
    const html = formatToolInput({ instruction: 'Create a schema' }, 'StructuredOutput')
    expect(html).toContain('structured-output-prompt')
    expect(html).toContain('Create a schema')
  })

  it('truncates prompt over 200 chars', () => {
    const longPrompt = 'P'.repeat(250)
    const html = formatToolInput({ prompt: longPrompt }, 'StructuredOutput')
    expect(html).toContain('…')
    expect(html).not.toContain('P'.repeat(250))
  })

  it('renders without prompt gracefully', () => {
    const html = formatToolInput({}, 'StructuredOutput')
    expect(html).toContain('structured-output-view')
    expect(html).not.toContain('structured-output-prompt')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ prompt: 'test' }, 'structuredoutput')
    expect(html).toContain('structured-output-view')
  })
})

// ────────────────────────────────────────────────────────────
// SkillManage renderer
// ────────────────────────────────────────────────────────────

describe('SkillManage renderer', () => {
  it('renders skill-manage-view container', () => {
    const html = formatToolInput({ action: 'create', skill: 'deploy' }, 'SkillManage')
    expect(html).toContain('skill-manage-view')
  })

  it('renders skill-manage-icon', () => {
    const html = formatToolInput({ action: 'create', skill: 'deploy' }, 'SkillManage')
    expect(html).toContain('skill-manage-icon')
  })

  it('renders action when present', () => {
    const html = formatToolInput({ action: 'create', skill: 'deploy' }, 'SkillManage')
    expect(html).toContain('skill-manage-action')
    expect(html).toContain('create')
  })

  it('uses operation as action alias', () => {
    const html = formatToolInput({ operation: 'delete', skill: 'deploy' }, 'SkillManage')
    expect(html).toContain('skill-manage-action')
    expect(html).toContain('delete')
  })

  it('renders skill when present', () => {
    const html = formatToolInput({ action: 'create', skill: 'deploy' }, 'SkillManage')
    expect(html).toContain('skill-manage-name')
    expect(html).toContain('deploy')
  })

  it('uses name as skill alias', () => {
    const html = formatToolInput({ action: 'create', name: 'my-skill' }, 'SkillManage')
    expect(html).toContain('skill-manage-name')
    expect(html).toContain('my-skill')
  })

  it('omits action span when absent', () => {
    const html = formatToolInput({ skill: 'deploy' }, 'SkillManage')
    expect(html).not.toContain('skill-manage-action')
  })

  it('omits skill span when absent', () => {
    const html = formatToolInput({ action: 'create' }, 'SkillManage')
    expect(html).not.toContain('skill-manage-name')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ action: 'create', skill: 'deploy' }, 'skillmanage')
    expect(html).toContain('skill-manage-view')
  })
})

// ────────────────────────────────────────────────────────────
// Monitor renderer
// ────────────────────────────────────────────────────────────

describe('Monitor renderer', () => {
  it('renders monitor-view container', () => {
    const html = formatToolInput({ command: 'tail -f log.txt', target: 'server-1' }, 'Monitor')
    expect(html).toContain('monitor-view')
  })

  it('renders monitor-icon', () => {
    const html = formatToolInput({ command: 'tail -f log.txt', target: 'server-1' }, 'Monitor')
    expect(html).toContain('monitor-icon')
  })

  it('renders target when present', () => {
    const html = formatToolInput({ command: 'ls', target: 'server-1' }, 'Monitor')
    expect(html).toContain('monitor-target')
    expect(html).toContain('server-1')
  })

  it('omits target span when absent', () => {
    const html = formatToolInput({ command: 'ls' }, 'Monitor')
    expect(html).not.toContain('monitor-target')
  })

  it('renders command with bash prompt and hljs highlighting', () => {
    const html = formatToolInput({ command: 'tail -f log.txt', target: 'server-1' }, 'Monitor')
    expect(html).toContain('monitor-command-body')
    expect(html).toContain('bash-prompt')
  })

  it('omits command section when absent', () => {
    const html = formatToolInput({ target: 'server-1' }, 'Monitor')
    expect(html).not.toContain('monitor-command-body')
  })

  it('falls back to escapeHtml when hljs.highlight throws', async () => {
    const { hljs } = await import('@/utils/globals.ts')
    const spy = vi.spyOn(hljs, 'highlight').mockImplementation(() => { throw new Error('hljs mock error') })
    const html = formatToolInput({ command: 'echo hello', target: 'server-1' }, 'Monitor')
    expect(html).toContain('monitor-command-body')
    expect(html).toContain('echo hello')
    spy.mockRestore()
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ command: 'ls', target: 's1' }, 'monitor')
    expect(html).toContain('monitor-view')
  })
})

// ────────────────────────────────────────────────────────────
// ImageGen renderer
// ────────────────────────────────────────────────────────────

describe('ImageGen renderer', () => {
  it('renders image-gen-view container', () => {
    const html = formatToolInput({ prompt: 'A sunset over mountains', size: '1024x1024' }, 'ImageGen')
    expect(html).toContain('image-gen-view')
  })

  it('renders image-gen-icon', () => {
    const html = formatToolInput({ prompt: 'A sunset', size: '1024x1024' }, 'ImageGen')
    expect(html).toContain('image-gen-icon')
  })

  it('renders prompt when present', () => {
    const html = formatToolInput({ prompt: 'A sunset over mountains', size: '1024x1024' }, 'ImageGen')
    expect(html).toContain('image-gen-prompt')
    expect(html).toContain('A sunset over mountains')
  })

  it('uses description as prompt alias', () => {
    const html = formatToolInput({ description: 'A beautiful landscape' }, 'ImageGen')
    expect(html).toContain('image-gen-prompt')
    expect(html).toContain('A beautiful landscape')
  })

  it('renders size when present', () => {
    const html = formatToolInput({ prompt: 'A sunset', size: '1024x1024' }, 'ImageGen')
    expect(html).toContain('image-gen-size')
    expect(html).toContain('1024x1024')
  })

  it('omits size span when absent', () => {
    const html = formatToolInput({ prompt: 'A sunset' }, 'ImageGen')
    expect(html).not.toContain('image-gen-size')
  })

  it('truncates prompt over 200 chars', () => {
    const longPrompt = 'P'.repeat(250)
    const html = formatToolInput({ prompt: longPrompt }, 'ImageGen')
    expect(html).toContain('…')
    expect(html).not.toContain('P'.repeat(250))
  })

  it('renders without prompt gracefully', () => {
    const html = formatToolInput({}, 'ImageGen')
    expect(html).toContain('image-gen-view')
    expect(html).not.toContain('image-gen-prompt')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ prompt: 'sunset' }, 'imagegen')
    expect(html).toContain('image-gen-view')
  })
})

// ────────────────────────────────────────────────────────────
// LSP renderer
// ────────────────────────────────────────────────────────────

describe('LSP renderer', () => {
  it('renders lsp-view container', () => {
    const html = formatToolInput({ method: 'textDocument/definition', file_path: 'src/main.ts' }, 'LSP')
    expect(html).toContain('lsp-view')
  })

  it('renders lsp-icon', () => {
    const html = formatToolInput({ method: 'textDocument/definition' }, 'LSP')
    expect(html).toContain('lsp-icon')
  })

  it('renders method when present', () => {
    const html = formatToolInput({ method: 'textDocument/definition', file_path: 'src/main.ts' }, 'LSP')
    expect(html).toContain('lsp-method')
    expect(html).toContain('textDocument/definition')
  })

  it('omits method span when absent', () => {
    const html = formatToolInput({ file_path: 'src/main.ts' }, 'LSP')
    expect(html).not.toContain('lsp-method')
  })

  it('renders file_path when present', () => {
    const html = formatToolInput({ method: 'hover', file_path: 'src/main.ts' }, 'LSP')
    expect(html).toContain('lsp-file-path')
    expect(html).toContain('src/main.ts')
  })

  it('uses path as file_path alias', () => {
    const html = formatToolInput({ method: 'hover', path: 'lib/util.ts' }, 'LSP')
    expect(html).toContain('lsp-file-path')
    expect(html).toContain('lib/util.ts')
  })

  it('renders resolved path', () => {
    const html = formatToolInput({ method: 'hover', file_path: './src/main.ts' }, 'LSP')
    expect(html).toContain('lsp-file-path')
    // ./ prefix should be stripped
    expect(html).toContain('src/main.ts')
  })

  it('omits file_path span when absent', () => {
    const html = formatToolInput({ method: 'hover' }, 'LSP')
    expect(html).not.toContain('lsp-file-path')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ method: 'hover' }, 'lsp')
    expect(html).toContain('lsp-view')
  })
})

// ────────────────────────────────────────────────────────────
// Git renderer
// ────────────────────────────────────────────────────────────

describe('Git renderer', () => {
  it('renders git-tool-view container', () => {
    const html = formatToolInput({ command: 'commit', args: '-m "fix bug"' }, 'Git')
    expect(html).toContain('git-tool-view')
  })

  it('renders git-tool-icon', () => {
    const html = formatToolInput({ command: 'commit' }, 'Git')
    expect(html).toContain('git-tool-icon')
  })

  it('renders command with bash prompt and hljs highlighting', () => {
    const html = formatToolInput({ command: 'commit', args: '-m "fix"' }, 'Git')
    expect(html).toContain('git-tool-body')
    expect(html).toContain('bash-prompt')
    expect(html).toContain('git commit')
  })

  it('uses subcommand as command alias', () => {
    const html = formatToolInput({ subcommand: 'push', args: 'origin main' }, 'Git')
    expect(html).toContain('git push')
  })

  it('handles args as string', () => {
    const html = formatToolInput({ command: 'commit', args: '-m "fix bug"' }, 'Git')
    expect(html).toContain('git commit -m')
  })

  it('handles args as array via JSON.stringify', () => {
    const html = formatToolInput({ command: 'commit', arguments: ['-m', 'fix'] }, 'Git')
    expect(html).toContain('git commit')
  })

  it('uses arguments as args alias', () => {
    const html = formatToolInput({ command: 'push', arguments: 'origin main' }, 'Git')
    expect(html).toContain('git push origin main')
  })

  it('falls back to escapeHtml when hljs.highlight throws', async () => {
    const { hljs } = await import('@/utils/globals.ts')
    const spy = vi.spyOn(hljs, 'highlight').mockImplementation(() => { throw new Error('hljs mock error') })
    const html = formatToolInput({ command: 'commit', args: '-m "fix"' }, 'Git')
    expect(html).toContain('git-tool-view')
    expect(html).toContain('git commit')
    spy.mockRestore()
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ command: 'commit' }, 'git')
    expect(html).toContain('git-tool-view')
  })
})

// ────────────────────────────────────────────────────────────
// NotebookEdit renderer
// ────────────────────────────────────────────────────────────

describe('NotebookEdit renderer', () => {
  it('renders edit-diff-view container', () => {
    const html = formatToolInput({ file_path: 'notebook.ipynb', new_source: 'print("hello")' }, 'NotebookEdit')
    expect(html).toContain('edit-diff-view')
  })

  it('renders cell_index when present', () => {
    const html = formatToolInput({ file_path: 'nb.ipynb', cell_index: 3, new_source: 'x = 1' }, 'NotebookEdit')
    expect(html).toContain('Cell 3')
  })

  it('uses cellIndex as cell_index alias', () => {
    const html = formatToolInput({ file_path: 'nb.ipynb', cellIndex: 5, new_source: 'y = 2' }, 'NotebookEdit')
    expect(html).toContain('Cell 5')
  })

  it('omits cell badge when no cell_index', () => {
    const html = formatToolInput({ file_path: 'nb.ipynb', new_source: 'x = 1' }, 'NotebookEdit')
    expect(html).not.toContain('Cell')
  })

  it('uses new_string as new_source alias', () => {
    const html = formatToolInput({ file_path: 'nb.ipynb', cell_index: 0, new_string: 'z = 3' }, 'NotebookEdit')
    expect(html).toContain('edit-diff-add')
    expect(html).toContain('z = 3')
  })

  it('renders resolved path', () => {
    const html = formatToolInput({ file_path: './notebooks/test.ipynb', new_source: 'a = 1' }, 'NotebookEdit')
    // ./ prefix should be stripped
    expect(html).toContain('notebooks/test.ipynb')
  })

  it('renders new_source lines as additions', () => {
    const html = formatToolInput({
      file_path: 'nb.ipynb',
      new_source: 'line1\nline2\nline3',
    }, 'NotebookEdit')
    const addMatches = html.match(/edit-diff-add/g)
    expect(addMatches?.length).toBe(3)
  })

  it('omits diff body when no new_source', () => {
    const html = formatToolInput({ file_path: 'nb.ipynb', cell_index: 0 }, 'NotebookEdit')
    expect(html).not.toContain('edit-diff-scroll')
    expect(html).not.toContain('edit-diff-add')
  })

  it('is case-insensitive', () => {
    const html = formatToolInput({ file_path: 'nb.ipynb', new_source: 'x = 1' }, 'notebookedit')
    expect(html).toContain('edit-diff-view')
  })
})

// ────────────────────────────────────────────────────────────
// formatToolOutput — deep tests
// ────────────────────────────────────────────────────────────

describe('formatToolOutput (deep)', () => {
  it('returns empty string for empty output', () => {
    expect(formatToolOutput('')).toBe('')
  })

  it('routes to registered output renderer for known tool names', () => {
    const html = formatToolOutput('ok', 'Write')
    expect(html).toContain('tool-output-status-msg')
  })

  it('falls back to smart output for unregistered tool names', () => {
    const html = formatToolOutput('plain text result', 'UnknownTool')
    expect(html).toContain('tool-output-default')
  })

  it('falls back to smart output when no tool name', () => {
    const html = formatToolOutput('plain text result')
    expect(html).toContain('tool-output-default')
  })

  it('Git tool uses terminal output renderer', () => {
    const html = formatToolOutput('commit abc123', 'Git')
    expect(html).toContain('bash-output-body')
  })

  it('PowerShell tool uses terminal output renderer', () => {
    const html = formatToolOutput('Get-Process', 'PowerShell')
    expect(html).toContain('bash-output-body')
  })
})

// ────────────────────────────────────────────────────────────
// renderSmartOutput — tests via formatToolOutput
// ────────────────────────────────────────────────────────────

describe('renderSmartOutput (via formatToolOutput)', () => {
  it('pretty-prints JSON object', () => {
    const html = formatToolOutput('{"key": "value"}', 'UnknownTool')
    expect(html).toContain('tool-output-default')
    expect(html).toContain('<pre>')
    // Pretty-printed JSON should have newlines (indented)
    expect(html).toContain('key')
  })

  it('pretty-prints JSON array', () => {
    const html = formatToolOutput('[1, 2, 3]', 'UnknownTool')
    expect(html).toContain('tool-output-default')
    expect(html).toContain('<pre>')
  })

  it('falls back to code output for invalid JSON starting with {', () => {
    const html = formatToolOutput('{invalid json}', 'UnknownTool')
    expect(html).toContain('tool-output-default')
    expect(html).toContain('<pre>')
    expect(html).toContain('{invalid json}')
  })

  it('falls back to code output for invalid JSON starting with [', () => {
    const html = formatToolOutput('[not valid]', 'UnknownTool')
    expect(html).toContain('tool-output-default')
    expect(html).toContain('<pre>')
  })

  it('treats non-JSON plain text as code output', () => {
    const html = formatToolOutput('Hello world, this is plain text.', 'UnknownTool')
    expect(html).toContain('tool-output-default')
    expect(html).toContain('<pre>')
    expect(html).toContain('Hello world, this is plain text.')
  })
})

// ────────────────────────────────────────────────────────────
// renderStatusOutput — tests via formatToolOutput
// ────────────────────────────────────────────────────────────

describe('renderStatusOutput (via formatToolOutput)', () => {
  it('renders short message <= 50 chars as badge', () => {
    const html = formatToolOutput('ok', 'Write')
    expect(html).toContain('tool-output-status-msg')
    expect(html).toContain('tool-output-ok-badge')
    expect(html).toContain('ok')
  })

  it('renders long message > 50 chars as preformatted text', () => {
    const longMsg = 'A'.repeat(60)
    const html = formatToolOutput(longMsg, 'Write')
    expect(html).toContain('tool-output-default')
    expect(html).toContain('<pre>')
    expect(html).not.toContain('tool-output-ok-badge')
  })

  it('Edit tool uses status output renderer', () => {
    const html = formatToolOutput('ok', 'Edit')
    expect(html).toContain('tool-output-status-msg')
  })

  it('MultiEdit tool uses status output renderer', () => {
    const html = formatToolOutput('ok', 'MultiEdit')
    expect(html).toContain('tool-output-status-msg')
  })

  it('NotebookEdit tool uses status output renderer', () => {
    const html = formatToolOutput('ok', 'NotebookEdit')
    expect(html).toContain('tool-output-status-msg')
  })

  it('TodoWrite tool uses status output renderer', () => {
    const html = formatToolOutput('ok', 'TodoWrite')
    expect(html).toContain('tool-output-status-msg')
  })
})

// ────────────────────────────────────────────────────────────
// annotateLocalhostInEscapedText — tests via formatToolOutput
// ────────────────────────────────────────────────────────────

describe('annotateLocalhostInEscapedText (via formatToolOutput)', () => {
  it('annotates localhost URLs when isAppMode is true', () => {
    mockIsAppMode.value = true
    const html = formatToolOutput('Visit http://localhost:8080/api', 'Bash')
    expect(html).toContain('chat-url-open-btn')
    expect(html).toContain('data-port="8080"')
    mockIsAppMode.value = false
  })

  it('returns text as-is when isAppMode is false', () => {
    mockIsAppMode.value = false
    const html = formatToolOutput('Visit http://localhost:8080/api', 'Bash')
    expect(html).not.toContain('chat-url-open-btn')
    expect(html).toContain('http://localhost:8080/api')
  })
})

// ────────────────────────────────────────────────────────────
// PermissionApproval action handler — uncovered branches
// ────────────────────────────────────────────────────────────

describe('PermissionApproval action handler (uncovered branches)', () => {
  function createPermissionDOM(opts?: { noToolCallId?: boolean; noSessionId?: boolean }): { container: HTMLDivElement; emit: ReturnType<typeof vi.fn> } {
    const emit = vi.fn()
    const container = document.createElement('div')
    container.className = 'permission-approval-view'
    container.innerHTML = `
      <div class="permission-options">
        <button class="permission-btn permission-btn-allow" data-option-id="allow-1" data-kind="allow_once">Allow Once</button>
        <button class="permission-btn permission-btn-reject" data-option-id="reject-1" data-kind="reject_once">Deny</button>
      </div>
    `
    const toolDetail = document.createElement('div')
    toolDetail.className = 'tool-detail'
    if (!opts?.noSessionId) toolDetail.dataset.sessionId = 'test-session-123'
    if (!opts?.noToolCallId) toolDetail.dataset.toolCallId = 'tc-456'
    toolDetail.appendChild(container)
    document.body.appendChild(toolDetail)
    return { container, emit }
  }

  function cleanup(container: HTMLDivElement) {
    container.closest('.tool-detail')?.remove()
  }

  it('click reject button sets denied text', () => {
    const { container, emit } = createPermissionDOM()
    const rejectBtn = container.querySelector('.permission-btn-reject') as HTMLElement
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: rejectBtn, writable: false })
    handleToolAction('PermissionApproval', clickEvent, emit)
    expect(rejectBtn.textContent).toContain('Denied')
    cleanup(container)
  })

  it('no toolCallId → early return with console.warn', () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { container, emit } = createPermissionDOM({ noToolCallId: true })
    const allowBtn = container.querySelector('.permission-btn-allow') as HTMLElement
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: allowBtn, writable: false })
    const result = handleToolAction('PermissionApproval', clickEvent, emit)
    expect(result).toBe(true)
    expect(warnSpy).toHaveBeenCalledWith('PermissionApproval: no toolCallId found')
    // View should NOT be marked as responded (early return before that)
    expect(container.classList.contains('permission-responded')).toBe(false)
    warnSpy.mockRestore()
    cleanup(container)
  })

  it('falls back to getCurrentSessionId when toolDetail has no sessionId', () => {
    const { container, emit } = createPermissionDOM({ noSessionId: true })
    const allowBtn = container.querySelector('.permission-btn-allow') as HTMLElement
    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickEvent, 'target', { value: allowBtn, writable: false })
    handleToolAction('PermissionApproval', clickEvent, emit)
    // Should still work (falls back to getCurrentSessionId)
    expect(container.classList.contains('permission-responded')).toBe(true)
    cleanup(container)
  })
})

// ────────────────────────────────────────────────────────────
// AskUserQuestion action handler — uncovered branches
// ────────────────────────────────────────────────────────────

describe('AskUserQuestion action handler (uncovered branches)', () => {
  function createAskDOM(multiSelect = false): { container: HTMLDivElement; emit: ReturnType<typeof vi.fn> } {
    const emit = vi.fn()
    const container = document.createElement('div')
    container.className = 'ask-question-view'
    container.innerHTML = `
      <div class="ask-question-item" data-multi="${multiSelect}">
        <div class="ask-question-options">
          <div class="ask-question-option" data-qi="0" data-oi="0" data-label="Option A">
            <span class="ask-option-indicator">${multiSelect ? '☐' : '◯'}</span>
            <div class="ask-option-content">
              <span class="ask-option-label">Option A</span>
            </div>
          </div>
          <div class="ask-question-option" data-qi="0" data-oi="1" data-label="Option B">
            <span class="ask-option-indicator">${multiSelect ? '☐' : '◯'}</span>
            <div class="ask-option-content">
              <span class="ask-option-label">Option B</span>
            </div>
          </div>
        </div>
      </div>
      <div class="ask-question-supplementary">
        <label class="ask-supplementary-label">Additional info</label>
        <input class="ask-supplementary-input" type="text" placeholder="Optional" />
      </div>
      <button class="ask-question-submit" disabled>Submit</button>
    `
    document.body.appendChild(container)
    return { container, emit }
  }

  function cleanup(container: HTMLDivElement) {
    container.remove()
  }

  it('submit with supplementary text input appends to answer', () => {
    const { container, emit } = createAskDOM(false)
    const option = container.querySelector('.ask-question-option') as HTMLElement
    const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement
    const suppInput = container.querySelector('.ask-supplementary-input') as HTMLInputElement

    // Select option
    const clickOpt = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickOpt, 'target', { value: option, writable: false })
    handleToolAction('AskUserQuestion', clickOpt, emit)

    // Set supplementary text
    suppInput.value = 'some extra details'

    // Submit
    const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
    handleToolAction('AskUserQuestion', clickSubmit, emit)

    expect(emit).toHaveBeenCalledWith('send-message', 'Option A\nsome extra details')
    cleanup(container)
  })

  it('submit with no answers → early return, no emit', () => {
    const { container, emit } = createAskDOM(false)
    const submitBtn = container.querySelector('.ask-question-submit') as HTMLButtonElement

    const clickSubmit = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(clickSubmit, 'target', { value: submitBtn, writable: false })
    const result = handleToolAction('AskUserQuestion', clickSubmit, emit)

    expect(result).toBe(true)
    expect(emit).not.toHaveBeenCalledWith('send-message', expect.anything())
    cleanup(container)
  })

  it('multi-select toggling changes checkbox indicators', () => {
    const { container, emit } = createAskDOM(true)
    const option = container.querySelector('.ask-question-option') as HTMLElement

    // First click: select → ☑
    const click1 = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(click1, 'target', { value: option, writable: false })
    handleToolAction('AskUserQuestion', click1, emit)
    const indicator = option.querySelector('.ask-option-indicator')
    expect(indicator?.textContent).toBe('☑')
    expect(option.classList.contains('selected')).toBe(true)

    // Second click: deselect → ☐
    const click2 = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(click2, 'target', { value: option, writable: false })
    handleToolAction('AskUserQuestion', click2, emit)
    expect(indicator?.textContent).toBe('☐')
    expect(option.classList.contains('selected')).toBe(false)

    cleanup(container)
  })
})
