// Custom rendering for tool_use block details in chat messages.
// All backends normalize tool names and input field names in their parsers,
// so we can assume canonical field names here: file_path, command, old_string, etc.

import { hljs } from './globals.ts'
import { escapeHtml } from './html.ts'
import { detectLang, highlightLine } from './diff.ts'
import { resolveFilePath, fileOpenButtonHtml } from '@/composables/useFilePathAnnotation.ts'
import { localhostOpenButtonHtml } from '@/composables/useLocalhostAnnotation.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { gt } from '@/composables/useLocale'
import { store } from '@/stores/app.ts'
import { renderMarkdown } from '@/composables/useMarkdownRenderer.ts'
import { getSessionId } from '@/composables/useSessionIdentity.ts'

// ────────────────────────────────────────────────────────────
// Tool renderer functions
// ────────────────────────────────────────────────────────────

/**
 * Render Edit tool input as a diff view.
 * Shows old_string lines in red, new_string lines in green.
 * No line numbers, no +/- prefix — color-only distinction.
 * File path is clickable to open the file.
 */
function renderEditDiff(input: Record<string, any>): string {
  const filePath = input.file_path || ''
  const oldStr = input.old_string || ''
  const newStr = input.new_string || ''
  const replaceAll = input.replace_all

  // Resolve file path for click-to-open
  const projectRoot = store.state.projectRoot || ''
  const homeDir = store.state.homeDir || ''
  const resolvedPath = resolveFilePath(filePath, projectRoot, homeDir)
  const displayPath = resolvedPath || filePath.replace(/^\.\//, '')

  // Detect language for syntax highlighting
  const lang = detectLang(filePath)

  // Build header
  let header = '<div class="tool-file-header">'
  header += `<span class="tool-file-path">${escapeHtml(displayPath)}</span>`
  if (resolvedPath) {
    header += fileOpenButtonHtml(resolvedPath)
  }
  if (replaceAll) {
    header += '<span class="edit-diff-replace-all" title="' + escapeHtml(gt('tool.edit.replaceAllTitle')) + '">' + escapeHtml(gt('tool.edit.replaceAll')) + '</span>'
  }
  header += '</div>'

  // Build diff body (scroll wrapper + inner content)
  let body = '<div class="edit-diff-scroll"><div class="edit-diff-body">'

  // Old lines (red)
  if (oldStr) {
    const oldLines = oldStr.split('\n')
    for (const line of oldLines) {
      body += `<div class="edit-diff-del">${highlightLine(line, lang)}</div>`
    }
  }

  // New lines (green)
  if (newStr) {
    const newLines = newStr.split('\n')
    for (const line of newLines) {
      body += `<div class="edit-diff-add">${highlightLine(line, lang)}</div>`
    }
  }

  body += '</div></div>'

  return `<div class="edit-diff-view">${header}${body}</div>`
}

/**
 * Render Bash tool input as a terminal-style view.
 * Shows description (if any) and command with $ prefix.
 */
function renderBashTerminal(input: Record<string, any>): string {
  const command = input.command || ''
  const description = input.description || ''

  let html = '<div class="bash-terminal-view">'

  if (description) {
    html += `<div class="bash-terminal-desc">${escapeHtml(description)}</div>`
  }

  html += '<div class="bash-terminal-body">'
  html += '<span class="bash-prompt">$</span>'

  // Highlight command as bash
  if (command) {
    try {
      html += hljs.highlight(command, { language: 'bash', ignoreIllegals: true }).value
    } catch {
      html += escapeHtml(command)
    }
  }

  html += '</div></div>'

  return html
}

/**
 * Build a clickable file path header used by Read/Write/Edit views.
 */
function filePathHeader(input: Record<string, any>, extraBadge = ''): string {
  const filePath = input.file_path || ''
  const projectRoot = store.state.projectRoot || ''
  const homeDir = store.state.homeDir || ''
  const resolvedPath = resolveFilePath(filePath, projectRoot, homeDir)
  const displayPath = resolvedPath || filePath.replace(/^\.\//, '')

  let html = '<div class="tool-file-header">'
  html += `<span class="tool-file-path">${escapeHtml(displayPath)}</span>`
  if (resolvedPath) {
    html += fileOpenButtonHtml(resolvedPath)
  }
  if (extraBadge) html += extraBadge
  html += '</div>'
  return html
}

/**
 * Render Read tool input as a file preview view.
 * Shows clickable file path + syntax-highlighted content preview.
 */
function renderReadPreview(input: Record<string, any>): string {
  const filePath = input.file_path || ''
  const lang = detectLang(filePath)

  let html = '<div class="file-preview-view">'
  html += filePathHeader(input)

  // Content preview body
  html += '<div class="file-preview-body">'
  const content = input.content || ''
  if (content) {
    const lines = content.split('\n')
    for (const line of lines) {
      html += `<div class="file-preview-line">${highlightLine(line, lang)}</div>`
    }
  } else {
    // No content field — show offset/limit info if present
    const parts: string[] = []
    if (input.offset) parts.push(gt('tool.read.fromLine', { offset: input.offset }))
    if (input.limit) parts.push(gt('tool.read.readLines', { limit: input.limit }))
    if (parts.length > 0) {
      html += `<div class="file-preview-meta">${parts.join(gt('common.listSeparator'))}</div>`
    }
  }
  html += '</div></div>'

  return html
}

/**
 * Render Write tool input as a file write view.
 * Shows clickable file path + syntax-highlighted content to write.
 */
function renderWritePreview(input: Record<string, any>): string {
  const filePath = input.file_path || ''
  const lang = detectLang(filePath)

  let html = '<div class="file-write-view">'
  html += filePathHeader(input, `<span class="file-write-badge">${gt('tool.write.badge')}</span>`)

  html += '<div class="file-write-body">'
  const content = input.content || ''
  if (content) {
    const lines = content.split('\n')
    for (const line of lines) {
      html += `<div class="file-write-line">${highlightLine(line, lang)}</div>`
    }
  }
  html += '</div></div>'

  return html
}

/**
 * Render AskUserQuestion tool input as an interactive question card.
 * Shows question header, question text, and selectable option buttons.
 * Clicking an option is handled by the AskUserQuestion action handler
 * registered at the bottom of this file.
 */
function renderAskUserQuestion(input: Record<string, any>): string {
  const questions = input.questions
  if (!Array.isArray(questions) || questions.length === 0) {
    return `<div class="ask-question-view"><div class="ask-question-empty">${gt('tool.askUser.noQuestions')}</div></div>`
  }

  let html = '<div class="ask-question-view">'

  for (let qi = 0; qi < questions.length; qi++) {
    const q = questions[qi]
    const header = q.header || ''
    const question = q.question || ''
    const multiSelect = !!q.multiSelect
    const options = Array.isArray(q.options) ? q.options : []

    html += `<div class="ask-question-item" data-multi="${multiSelect}">`

    if (header) {
      html += `<div class="ask-question-header">${escapeHtml(header)}</div>`
    }
    if (question) {
      html += `<div class="ask-question-text">${escapeHtml(question)}</div>`
    }

    if (options.length > 0) {
      html += '<div class="ask-question-options">'
      for (let oi = 0; oi < options.length; oi++) {
        const opt = options[oi]
        const label = typeof opt === 'string' ? opt : (opt.label || '')
        const desc = typeof opt === 'object' ? (opt.description || '') : ''
        html += `<div class="ask-question-option" data-qi="${qi}" data-oi="${oi}" data-label="${escapeHtml(label)}">`
        html += `<span class="ask-option-indicator">${multiSelect ? '☐' : '◯'}</span>`
        html += '<div class="ask-option-content">'
        html += `<span class="ask-option-label">${escapeHtml(label)}</span>`
        if (desc) {
          html += `<span class="ask-option-desc">${escapeHtml(desc)}</span>`
        }
        html += '</div>'
        html += '</div>'
      }
      html += '</div>'
    }

    html += '</div>'
  }

  html += '<div class="ask-question-supplementary">'
  html += `<label class="ask-supplementary-label">${escapeHtml(gt('tool.askUser.supplementary'))}</label>`
  html += `<input class="ask-supplementary-input" type="text" placeholder="${escapeHtml(gt('tool.askUser.supplementaryPlaceholder'))}" />`
  html += '</div>'

  html += `<button class="ask-question-submit" disabled>${gt('tool.askUser.submit')}</button>`
  html += '</div>'

  return html
}

/**
 * Render Grep tool input as a search view.
 * Shows search pattern (highlighted) + search path + output_mode tag.
 */
function renderGrepSearch(input: Record<string, any>): string {
  const pattern = input.pattern || ''
  const path = input.path || ''
  const outputMode = input.output_mode || ''

  let html = '<div class="grep-search-view">'

  // Pattern line
  html += '<div class="grep-pattern-row">'
  html += `<span class="grep-label">${escapeHtml(gt('tool.grep.pattern'))}</span>`
  try {
    html += `<span class="grep-pattern-text">${hljs.highlight(pattern, { language: 'bash', ignoreIllegals: true }).value}</span>`
  } catch {
    html += `<span class="grep-pattern-text">${escapeHtml(pattern)}</span>`
  }
  html += '</div>'

  // Path line
  if (path) {
    const projectRoot = store.state.projectRoot || ''
    const homeDir = store.state.homeDir || ''
    const resolvedPath = resolveFilePath(path, projectRoot, homeDir)
    const displayPath = resolvedPath || path.replace(/^\.\//, '')
    html += '<div class="grep-path-row">'
    html += `<span class="grep-label">${escapeHtml(gt('tool.grep.path'))}</span>`
    html += `<span class="grep-path-text">${escapeHtml(displayPath)}</span>`
    if (resolvedPath) {
      html += fileOpenButtonHtml(resolvedPath)
    }
    html += '</div>'
  }

  // Output mode tag
  if (outputMode) {
    html += `<span class="grep-mode-tag">${escapeHtml(outputMode)}</span>`
  }

  html += '</div>'
  return html
}

/**
 * Render Glob tool input as a file pattern view.
 * Shows glob pattern + search directory.
 */
function renderGlobPattern(input: Record<string, any>): string {
  const pattern = input.pattern || ''
  const path = input.path || ''

  let html = '<div class="glob-pattern-view">'

  // Pattern line
  html += '<div class="glob-pattern-row">'
  html += `<span class="glob-label">${escapeHtml(gt('tool.glob.pattern'))}</span>`
  html += `<span class="glob-pattern-text">${escapeHtml(pattern)}</span>`
  html += '</div>'

  // Path line
  if (path) {
    const projectRoot = store.state.projectRoot || ''
    const homeDir = store.state.homeDir || ''
    const resolvedPath = resolveFilePath(path, projectRoot, homeDir)
    const displayPath = resolvedPath || path.replace(/^\.\//, '')
    html += '<div class="glob-path-row">'
    html += `<span class="glob-label">${escapeHtml(gt('tool.glob.path'))}</span>`
    html += `<span class="glob-path-text">${escapeHtml(displayPath)}</span>`
    if (resolvedPath) {
      html += fileOpenButtonHtml(resolvedPath)
    }
    html += '</div>'
  }

  html += '</div>'
  return html
}

/**
 * Render WebSearch tool input as a search query view.
 * Shows the search query text.
 */
function renderWebSearch(input: Record<string, any>): string {
  const query = input.query || ''

  let html = '<div class="web-search-view">'
  html += '<div class="web-search-query">'
  html += '<span class="web-search-icon">🔍</span>'
  html += `<span class="web-search-text">${escapeHtml(query)}</span>`
  html += '</div>'
  html += '</div>'
  return html
}

/**
 * Render WebFetch tool input as a URL fetch view.
 * Shows the URL (clickable) and optional prompt.
 */
function renderWebFetch(input: Record<string, any>): string {
  const url = input.url || input.prompt || ''

  let html = '<div class="web-fetch-view">'

  // URL line
  if (url) {
    html += '<div class="web-fetch-url-row">'
    html += `<span class="web-fetch-label">${escapeHtml(gt('tool.webFetch.url'))}</span>`
    // Determine if it looks like a URL
    const isUrl = /^https?:\/\//i.test(url)
    if (isUrl) {
      html += `<a class="web-fetch-link" href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(url)}</a>`
    } else {
      html += `<span class="web-fetch-text">${escapeHtml(url)}</span>`
    }
    html += '</div>'
  }

  // Prompt (if present and different from url)
  const prompt = input.prompt && input.url ? input.prompt : ''
  if (prompt) {
    html += `<div class="web-fetch-prompt">${escapeHtml(prompt)}</div>`
  }

  html += '</div>'
  return html
}

/**
 * Render Agent tool input as a sub-agent call view.
 * Shows agent type badge + description + full markdown-rendered prompt.
 */
function renderAgentCall(input: Record<string, any>): string {
  const description = input.description || ''
  const prompt = input.prompt || ''
  const subagentType = input.subagent_type || input.mode || ''

  let html = '<div class="agent-call-view">'

  // Type badge + description
  html += '<div class="agent-call-header">'
  if (subagentType) {
    html += `<span class="agent-type-badge">${escapeHtml(subagentType)}</span>`
  }
  if (description) {
    html += `<span class="agent-call-desc">${escapeHtml(description)}</span>`
  }
  html += '</div>'

  // Prompt (full content, markdown rendered)
  if (prompt) {
    const rendered = renderMarkdown(prompt, {
      sanitize: true,
      renderKatex: false,
      renderMermaid: false,
      wrapTables: false,
    })
    html += `<div class="agent-call-prompt">${rendered}</div>`
  }

  html += '</div>'
  return html
}

/**
 * Render Skill tool input as a skill call view.
 * Shows skill name + optional arguments (full content).
 */
function renderSkillCall(input: Record<string, any>): string {
  const skill = input.skill || input.command || ''
  const args = input.args || input.arguments || ''

  let html = '<div class="skill-call-view">'

  // Skill name
  html += '<div class="skill-call-header">'
  html += '<span class="skill-call-icon">✨</span>'
  html += `<span class="skill-call-name">${escapeHtml(skill)}</span>`
  html += '</div>'

  // Arguments (if present, full content)
  if (args) {
    const argStr = typeof args === 'string' ? args : JSON.stringify(args, null, 2)
    html += `<div class="skill-call-args">${escapeHtml(argStr)}</div>`
  }

  html += '</div>'
  return html
}

/**
 * Render PermissionApproval tool input as an interactive permission card.
 * Shows tool name + description, permission options as buttons.
 * Clicking an option calls POST /api/ai/permission/respond.
 */
function renderPermissionApproval(input: Record<string, any>): string {
  const options = Array.isArray(input.options) ? input.options : []
  const toolName = input.toolName || ''
  const toolInput = input.toolInput || ''

  let html = '<div class="permission-approval-view">'

  // Header
  html += '<div class="permission-header">'
  html += `<span class="permission-icon">⚠️</span>`
  html += `<span class="permission-title">${escapeHtml(gt('tool.permission.title'))}</span>`
  html += '</div>'

  // Tool description
  if (toolName) {
    html += `<div class="permission-tool-name">${escapeHtml(toolName)}</div>`
  }
  if (toolInput) {
    try {
      const parsed = JSON.parse(toolInput)
      const filePath = parsed.file_path || parsed.path || ''
      const command = parsed.command || ''
      if (filePath) {
        html += `<div class="permission-tool-detail"><span class="permission-detail-label">${escapeHtml(gt('tool.permission.file'))}</span><code>${escapeHtml(filePath)}</code></div>`
      }
      if (command) {
        html += `<div class="permission-tool-detail"><span class="permission-detail-label">${escapeHtml(gt('tool.permission.command'))}</span><code>${escapeHtml(command)}</code></div>`
      }
    } catch {
      // Not JSON, show as-is
      html += `<div class="permission-tool-detail"><code>${escapeHtml(toolInput.substring(0, 200))}</code></div>`
    }
  }

  // Option buttons
  if (options.length > 0) {
    html += '<div class="permission-options">'
    for (let i = 0; i < options.length; i++) {
      const opt = options[i]
      const label = opt.name || ''
      const kind = opt.kind || ''
      const optionId = opt.optionId || ''
      let btnClass = 'permission-btn'
      if (kind === 'allow_once' || kind === 'allow_always') {
        btnClass += ' permission-btn-allow'
      } else {
        btnClass += ' permission-btn-reject'
      }
      html += `<button class="${btnClass}" data-option-id="${escapeHtml(String(optionId))}" data-kind="${escapeHtml(kind)}">${escapeHtml(label)}</button>`
    }
    html += '</div>'
  }

  html += '</div>'
  return html
}

/**
 * Render input as JSON (the fallback for unregistered tools).
 */
function renderJsonFallback(input: any): string {
  if (!input || (typeof input === 'object' && Object.keys(input).length === 0)) {
    try {
      const highlighted = hljs.highlight('{}', { language: 'json' }).value
      return `<div class="tool-json-body"><code>${highlighted}</code></div>`
    } catch {
      return '<div class="tool-json-body"><code>{}</code></div>'
    }
  }
  try {
    const json = JSON.stringify(input, null, 2)
    const highlighted = hljs.highlight(json, { language: 'json' }).value
    return `<div class="tool-json-body"><code>${highlighted}</code></div>`
  } catch {
    return `<div class="tool-json-body"><code>${escapeHtml(JSON.stringify(input, null, 2))}</code></div>`
  }
}

// ────────────────────────────────────────────────────────────
// Tool registries (renderer + action handler + auto-expand)
// ────────────────────────────────────────────────────────────
// Three parallel registries for tool customization:
//   TOOL_RENDERERS       — specialized HTML rendering for tool detail area
//   TOOL_ACTION_HANDLERS — interactive click handling inside v-html content
//   TOOL_AUTO_EXPAND     — tools whose detail area should auto-expand
//
// All lookups are case-insensitive. New tools register once;
// no changes needed in generic components (ContentBlocks, ChatPanel).

export type ToolRenderer = (input: Record<string, any>) => string

export type ToolActionHandler = (
  event: Event,
  emit: (type: string, payload?: any) => void
) => boolean

const TOOL_RENDERERS: Record<string, ToolRenderer> = {}
const TOOL_ACTION_HANDLERS: Record<string, ToolActionHandler> = {}
const TOOL_AUTO_EXPAND: Set<string> = new Set()

/**
 * Register a renderer for a tool type.
 * Tool names are matched case-insensitively.
 */
export function registerToolRenderer(toolName: string, renderer: ToolRenderer) {
  TOOL_RENDERERS[toolName.toLowerCase()] = renderer
}

/**
 * Register an action handler for a tool type.
 * Tool names are matched case-insensitively.
 */
export function registerToolActionHandler(toolName: string, handler: ToolActionHandler) {
  TOOL_ACTION_HANDLERS[toolName.toLowerCase()] = handler
}

/**
 * Dispatch a click event to the registered tool action handler.
 * Returns true if a handler consumed the event, false otherwise.
 */
export function handleToolAction(toolName: string, event: Event, emit: (type: string, payload?: any) => void): boolean {
  const handler = TOOL_ACTION_HANDLERS[toolName.toLowerCase()]
  if (!handler) return false
  return handler(event, emit)
}

/**
 * Check if a tool type should auto-expand its detail area
 * (bypass the normal click-to-expand toggle).
 */
export function shouldAutoExpandTool(toolName: string): boolean {
  return TOOL_AUTO_EXPAND.has(toolName.toLowerCase())
}

/**
 * Format tool_use input for display in the expanded tool detail area.
 * Looks up the tool name in the renderer registry; falls back to JSON.
 */
export function formatToolInput(input: any, toolName?: string): string {
  if (toolName) {
    const renderer = TOOL_RENDERERS[toolName.toLowerCase()]
    if (renderer && input && typeof input === 'object') {
      return renderer(input)
    }
  }
  return renderJsonFallback(input)
}

// ── Tool result output formatting ──

/**
 * Annotate localhost URLs in already-escaped text (e.g. tool output inside <pre>).
 * Unlike annotateLocalhostUrls() which operates on full HTML with block protection,
 * this works on plain escaped text — matching bare URLs and wrapping them
 * with <a> tags + open buttons.
 * Only runs in App mode (same gate as the main annotation composable).
 */
function annotateLocalhostInEscapedText(text: string): string {
  const { isAppMode } = useAppMode()
  if (!isAppMode.value) return text

  // Match localhost URLs in escapeHtml'd text.
  // URL characters (letters, digits, :/.-?) are not changed by escapeHtml,
  // but & becomes &amp; in query strings, and = stays as-is.
  // Path group matches anything that isn't whitespace, quotes, or closing brackets.
  const re = /https?:\/\/(?:localhost|127\.0\.0\.1):(\d+)(\/[^\s"'>)\]]*)?/gi
  return text.replace(re, (url, portStr) => {
    const port = parseInt(portStr)
    if (port <= 0 || port > 65535) return url
    const protocol = url.startsWith('https') ? 'https' : 'http'
    // Un-escape the URL for data attributes (escapeHtml turned & into &amp;, etc.)
    const rawUrl = url.replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"')
    // In <pre> context, wrap URL in <a> and append the open button
    const linkHtml = `<a href="${escapeHtml(rawUrl)}" target="_blank" rel="noopener">${url}</a>`
    return `${linkHtml}${localhostOpenButtonHtml(port, protocol, rawUrl)}`
  })
}

/**
 * Format tool execution output for display in the expanded tool detail area.
 * Renders output text with appropriate styling based on tool type.
 * In App mode, localhost URLs in the output are annotated with open buttons.
 */
export function formatToolOutput(output: string, toolName?: string): string {
  if (!output) return ''
  // For Bash tool: render as terminal output
  if (toolName?.toLowerCase() === 'bash') {
    const escaped = escapeHtml(output)
    const annotated = annotateLocalhostInEscapedText(escaped)
    return `<div class="bash-output-body"><pre>${annotated}</pre></div>`
  }
  // Default: render as preformatted text
  const escaped = escapeHtml(output)
  const annotated = annotateLocalhostInEscapedText(escaped)
  return `<div class="tool-output-default"><pre>${annotated}</pre></div>`
}

// ── Tool registrations ──

registerToolRenderer('Edit', renderEditDiff)
registerToolRenderer('Bash', renderBashTerminal)
registerToolRenderer('Read', renderReadPreview)
registerToolRenderer('Write', renderWritePreview)
registerToolRenderer('AskUserQuestion', renderAskUserQuestion)
registerToolRenderer('Grep', renderGrepSearch)
registerToolRenderer('Glob', renderGlobPattern)
registerToolRenderer('WebSearch', renderWebSearch)
registerToolRenderer('WebFetch', renderWebFetch)
registerToolRenderer('Agent', renderAgentCall)
registerToolRenderer('Skill', renderSkillCall)
registerToolRenderer('PermissionApproval', renderPermissionApproval)

TOOL_AUTO_EXPAND.add('askuserquestion')
TOOL_AUTO_EXPAND.add('permissionapproval')

// ── AskUserQuestion action handler ──

// Helper: fetch current session ID from the global singleton
function getCurrentSessionId(): string {
  return getSessionId()
}

// Helper: call the permission respond API
async function respondPermission(sessionId: string, toolCallId: string, optionId: string, cancelled: boolean): Promise<boolean> {
  try {
    const resp = await fetch('/api/ai/permission/respond', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId, toolCallId, optionId, cancelled }),
    })
    return resp.ok
  } catch (e) {
    console.error('permission respond failed:', e)
    return false
  }
}

function updateAskSubmitState(view: Element) {
  const items = view.querySelectorAll('.ask-question-item')
  let allAnswered = true
  for (const item of items) {
    if (!item.querySelector('.ask-question-option.selected')) {
      allAnswered = false
      break
    }
  }
  const submitBtn = view.querySelector('.ask-question-submit') as HTMLButtonElement | null
  if (submitBtn) {
    submitBtn.disabled = !allAnswered
  }
}

registerToolActionHandler('AskUserQuestion', (event, emit) => {
  const target = event.target as HTMLElement

  // Option click
  const optionEl = target.closest('.ask-question-option') as HTMLElement | null
  if (optionEl) {
    event.stopPropagation()
    event.preventDefault()
    const view = optionEl.closest('.ask-question-view')
    if (view && !view.classList.contains('ask-submitted')) {
      const multiSelect = optionEl.closest('.ask-question-item')?.dataset.multi === 'true'

      if (multiSelect) {
        optionEl.classList.toggle('selected')
        const indicator = optionEl.querySelector('.ask-option-indicator')
        if (indicator) indicator.textContent = optionEl.classList.contains('selected') ? '☑' : '☐'
      } else {
        const siblings = optionEl.parentElement!.querySelectorAll('.ask-question-option')
        for (const s of siblings) {
          s.classList.remove('selected')
          const ind = s.querySelector('.ask-option-indicator')
          if (ind) ind.textContent = '◯'
        }
        optionEl.classList.add('selected')
        const indicator = optionEl.querySelector('.ask-option-indicator')
        if (indicator) indicator.textContent = '◉'
      }

      updateAskSubmitState(view)
    }
    return true
  }

  // Submit click
  const submitBtn = target.closest('.ask-question-submit') as HTMLElement | null
  if (submitBtn) {
    event.stopPropagation()
    event.preventDefault()
    const view = submitBtn.closest('.ask-question-view')
    if (view && !view.classList.contains('ask-submitted')) {
      const answers: string[] = []
      const items = view.querySelectorAll('.ask-question-item')
      for (const item of items) {
        const selected = item.querySelectorAll('.ask-question-option.selected')
        const labels = [...selected].map(el => (el as HTMLElement).dataset.label)
        if (labels.length > 0) {
          answers.push(labels.join(', '))
        }
      }
      if (answers.length === 0) return true

      // Append supplementary text if provided
      const supplementaryInput = view.querySelector('.ask-supplementary-input') as HTMLInputElement | null
      const supplementaryText = supplementaryInput?.value?.trim()
      if (supplementaryText) {
        answers.push(supplementaryText)
      }

      // Mark as submitted
      view.classList.add('ask-submitted')
      const allOptions = view.querySelectorAll('.ask-question-option')
      for (const opt of allOptions) {
        ;(opt as HTMLElement).style.pointerEvents = 'none'
        if (!opt.classList.contains('selected')) {
          ;(opt as HTMLElement).style.opacity = '0.4'
        }
      }
      if (supplementaryInput) {
        supplementaryInput.disabled = true
        supplementaryInput.style.opacity = '0.6'
      }
      submitBtn.textContent = gt('tool.askUser.submitted')
      ;(submitBtn as HTMLButtonElement).disabled = true

      emit('send-message', answers.join('\n'))
    }
    return true
  }

  // Not an AskUserQuestion-specific click — fall through
  return false
})

// ── PermissionApproval action handler ──

registerToolActionHandler('PermissionApproval', (event, emit) => {
  const target = event.target as HTMLElement

  // Button click
  const btn = target.closest('.permission-btn') as HTMLElement | null
  if (btn) {
    event.stopPropagation()
    event.preventDefault()

    const view = btn.closest('.permission-approval-view')
    if (!view || view.classList.contains('permission-responded')) {
      return true
    }

    const optionId = btn.dataset.optionId || ''
    const kind = btn.dataset.kind || ''
    const cancelled = kind === 'reject_once' || kind === 'reject_always'

    // Extract sessionId and toolCallId from the tool-detail container's data attributes
    const toolDetail = btn.closest('.tool-detail') as HTMLElement | null
    const sessionId = toolDetail?.dataset?.sessionId || getCurrentSessionId()
    const toolCallId = toolDetail?.dataset?.toolCallId || ''

    if (!toolCallId) {
      console.warn('PermissionApproval: no toolCallId found')
      return true
    }

    // Mark as responded
    view.classList.add('permission-responded')
    const allBtns = view.querySelectorAll('.permission-btn')
    for (const b of allBtns) {
      ;(b as HTMLButtonElement).disabled = true
      if (b !== btn) {
        ;(b as HTMLElement).style.opacity = '0.4'
      }
    }

    // Show feedback
    if (cancelled) {
      btn.textContent = gt('tool.permission.denied')
    } else {
      btn.textContent = gt('tool.permission.approved')
    }

    // Call the API
    respondPermission(sessionId, toolCallId, optionId, cancelled)

    return true
  }

  // Not a PermissionApproval-specific click — fall through
  return false
})
