// Custom rendering for tool_use block details in chat messages.
// All backends normalize tool names and input field names in their parsers,
// so we can assume canonical field names here: file_path, command, old_string, etc.

import { hljs } from './globals.ts'
import { escapeHtml } from './html.ts'
import { detectLang, highlightLine } from './diff.ts'
import { resolveFilePath, fileOpenButtonHtml } from '@/composables/useFilePathAnnotation.ts'
import { localhostOpenButtonHtml } from '@/composables/useLocalhostAnnotation.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { appLog } from '@/utils/appLog'

const TAG = 'renderToolDetail'
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
  const workdir = input.workdir || input.dir_path || ''
  const timeout = input.timeout
  const runInBackground = input.run_in_background || input.is_background

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

  html += '</div>'

  // Tags row: workdir, timeout, background
  const tags: string[] = []
  if (workdir) tags.push(escapeHtml(workdir))
  if (timeout) tags.push(`timeout ${timeout}ms`)
  if (runInBackground) tags.push('background')
  if (tags.length > 0) {
    html += '<div class="bash-tags-row">'
    for (const tag of tags) {
      html += `<span class="grep-mode-tag">${tag}</span>`
    }
    html += '</div>'
  }

  html += '</div>'

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
  const globFilter = input.glob || input.include_pattern || input.include || ''
  const caseInsensitive = input['-i'] || input.ignoreCase || input.case_sensitive === false
  const contextLines = input.context || ''
  const afterLines = input['-A'] || input.after || ''
  const beforeLines = input['-B'] || input.before || ''

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

  // Tags row: output mode, case-insensitive, glob filter
  const tags: string[] = []
  if (outputMode) tags.push(escapeHtml(outputMode))
  if (caseInsensitive) tags.push('-i')
  if (globFilter) tags.push(escapeHtml(globFilter))
  if (contextLines) tags.push(`-C ${contextLines}`)
  if (afterLines && !contextLines) tags.push(`-A ${afterLines}`)
  if (beforeLines && !contextLines) tags.push(`-B ${beforeLines}`)
  if (tags.length > 0) {
    html += '<div class="grep-tags-row">'
    for (const tag of tags) {
      html += `<span class="grep-mode-tag">${tag}</span>`
    }
    html += '</div>'
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
  const caseSensitive = input.case_sensitive

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

  // Case-sensitive tag
  if (caseSensitive === true || caseSensitive === false) {
    html += '<div class="glob-tags-row">'
    html += `<span class="grep-mode-tag">${caseSensitive ? 'case-sensitive' : 'case-insensitive'}</span>`
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
  const allowedDomains = input.allowed_domains as string[] | undefined
  const blockedDomains = input.blocked_domains as string[] | undefined
  const topic = input.topic || ''

  let html = '<div class="web-search-view">'
  html += '<div class="web-search-query">'
  html += '<span class="web-search-icon">🔍</span>'
  html += `<span class="web-search-text">${escapeHtml(query)}</span>`
  html += '</div>'

  // Tags row: topic, allowed/blocked domains
  const tags: string[] = []
  if (topic) tags.push(escapeHtml(topic))
  if (allowedDomains && allowedDomains.length > 0) tags.push(`${allowedDomains.length} allowed`)
  if (blockedDomains && blockedDomains.length > 0) tags.push(`${blockedDomains.length} blocked`)
  if (tags.length > 0) {
    html += '<div class="web-search-tags-row">'
    for (const tag of tags) {
      html += `<span class="grep-mode-tag">${tag}</span>`
    }
    html += '</div>'
  }

  html += '</div>'
  return html
}

/**
 * Render WebFetch tool input as a URL fetch view.
 * Shows the URL (clickable) and optional prompt.
 */
function renderWebFetch(input: Record<string, any>): string {
  const url = input.url || input.prompt || ''
  const format = input.format || ''
  const timeout = input.timeout

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

  // Tags: format, timeout
  const tags: string[] = []
  if (format) tags.push(escapeHtml(format))
  if (timeout) tags.push(`timeout ${timeout}ms`)
  if (tags.length > 0) {
    html += '<div class="web-fetch-tags-row">'
    for (const tag of tags) {
      html += `<span class="grep-mode-tag">${tag}</span>`
    }
    html += '</div>'
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
function renderPermissionApproval(input: Record<string, any>, blockCtx?: ToolBlockCtx): string {
  const options = Array.isArray(input.options) ? input.options : []
  const toolName = input.toolName || ''
  const toolInput = input.toolInput || ''
  const isAutoApproved = input.autoApproved === true
  const isDone = blockCtx?.done
  // Require explicit output to consider this genuinely responded.
  // done=true without output can happen when cleanup/timeout marks the block done
  // without a real user response — that's a "pending" state, not "approved".
  const hasRealResult = isDone && blockCtx?.output
  const isApproved = hasRealResult && blockCtx?.status !== 'error'

  let html = '<div class="permission-approval-view'

  // Only mark as responded when we have a real result from user action
  if (hasRealResult) {
    html += ' permission-responded'
  }
  if (isAutoApproved) {
    html += ' permission-auto-approved'
  }

  html += '">'

  // Header
  html += '<div class="permission-header">'
  if (isAutoApproved) {
    html += `<span class="permission-icon">✅</span>`
    html += `<span class="permission-title">${escapeHtml(gt('tool.permission.autoApprovedTitle'))}</span>`
  } else {
    html += `<span class="permission-icon">⚠️</span>`
    html += `<span class="permission-title">${escapeHtml(gt('tool.permission.title'))}</span>`
  }
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

  // Option buttons / result
  if (hasRealResult) {
    // Already responded — show result badge instead of buttons
    if (isApproved) {
      html += `<div class="permission-result permission-result-approved">${escapeHtml(gt('tool.permission.approved'))}</div>`
    } else {
      html += `<div class="permission-result permission-result-denied">${escapeHtml(gt('tool.permission.denied'))}</div>`
    }
  } else if (isAutoApproved) {
    // Auto-approved but SSE result not yet arrived — show auto-approved badge
    html += `<div class="permission-result permission-result-auto-approved">${escapeHtml(gt('tool.permission.autoApproved'))}</div>`
  } else if (options.length > 0) {
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
 * Render LS tool input as a directory listing view.
 * Shows the directory path (clickable).
 */
function renderLSView(input: Record<string, any>): string {
  const path = input.path || input.dir_path || ''

  let html = '<div class="ls-dir-view">'
  html += '<div class="ls-dir-header">'
  html += `<span class="ls-dir-icon">📂</span>`

  if (path) {
    const projectRoot = store.state.projectRoot || ''
    const homeDir = store.state.homeDir || ''
    const resolvedPath = resolveFilePath(path, projectRoot, homeDir)
    const displayPath = resolvedPath || path.replace(/^\.\//, '')
    html += `<span class="ls-dir-path">${escapeHtml(displayPath)}</span>`
    if (resolvedPath) {
      html += fileOpenButtonHtml(resolvedPath)
    }
  } else {
    html += `<span class="ls-dir-path">${escapeHtml(gt('tool.ls.currentDir'))}</span>`
  }

  html += '</div></div>'
  return html
}

/**
 * Render TodoWrite tool input as a structured task list.
 * Shows todo items with their status.
 */
function renderTodoWrite(input: Record<string, any>): string {
  const todos = Array.isArray(input.todos) ? input.todos : []

  let html = '<div class="todo-write-view">'

  if (todos.length > 0) {
    html += '<div class="todo-write-list">'
    for (const todo of todos) {
      const content = todo.content || ''
      const status = todo.status || ''
      const isActive = status === 'in_progress'
      const isDone = status === 'completed'
      let icon = '○'
      let cls = 'todo-pending'
      if (isDone) { icon = '✓'; cls = 'todo-done' }
      else if (isActive) { icon = '►'; cls = 'todo-active' }
      html += `<div class="todo-item ${cls}">`
      html += `<span class="todo-icon">${icon}</span>`
      html += `<span class="todo-content">${escapeHtml(content)}</span>`
      html += '</div>'
    }
    html += '</div>'
  }

  html += '</div>'
  return html
}

/**
 * Render TodoRead tool input as a task read view.
 */
function renderTodoRead(_input: Record<string, any>): string {
  let html = '<div class="todo-read-view">'
  html += `<span class="todo-read-icon">📋</span>`
  html += `<span class="todo-read-label">${escapeHtml(gt('tool.todoRead.label'))}</span>`
  html += '</div>'
  return html
}

/**
 * Render a Task management tool input with a key-value summary.
 * Used for TaskCreate, TaskUpdate, TaskList, TaskGet, TaskStop, TaskOutput.
 */
function renderTaskTool(input: Record<string, any>): string {
  let html = '<div class="task-tool-view">'

  // Show the most relevant fields based on what's present
  const fields: { key: string; label: string; format?: 'code' | 'text' }[] = [
    { key: 'subject', label: gt('tool.task.subject') },
    { key: 'description', label: gt('tool.task.description') },
    { key: 'taskId', label: 'ID', format: 'code' },
    { key: 'task_id', label: 'ID', format: 'code' },
    { key: 'name', label: gt('tool.task.name') },
    { key: 'cron', label: gt('tool.task.cron'), format: 'code' },
    { key: 'prompt', label: gt('tool.task.prompt') },
    { key: 'agent', label: gt('tool.task.agent') },
    { key: 'agent_id', label: gt('tool.task.agent') },
    { key: 'status', label: gt('tool.task.status') },
    { key: 'owner', label: gt('tool.task.owner') },
    { key: 'activeForm', label: gt('tool.task.activeForm') },
  ]

  let hasContent = false
  for (const f of fields) {
    const val = input[f.key]
    if (val !== undefined && val !== null && val !== '') {
      hasContent = true
      const display = typeof val === 'string' ? val : JSON.stringify(val)
      const truncated = display.length > 200 ? display.substring(0, 200) + '…' : display
      html += '<div class="task-tool-field">'
      html += `<span class="task-field-label">${escapeHtml(f.label)}</span>`
      if (f.format === 'code') {
        html += `<code class="task-field-value">${escapeHtml(truncated)}</code>`
      } else {
        html += `<span class="task-field-value">${escapeHtml(truncated)}</span>`
      }
      html += '</div>'
    }
  }

  if (!hasContent) {
    html += `<div class="task-tool-empty">${escapeHtml(gt('tool.task.noDetails'))}</div>`
  }

  html += '</div>'
  return html
}

/**
 * Render mode switch tools (EnterPlanMode/ExitPlanMode) as a simple badge view.
 */
function renderModeSwitch(input: Record<string, any>): string {
  let html = '<div class="mode-switch-view">'
  html += `<span class="mode-switch-icon">🔄</span>`
  const mode = input.mode || input.mode_id || ''
  if (mode) {
    html += `<span class="mode-switch-mode">${escapeHtml(mode)}</span>`
  }
  html += '</div>'
  return html
}

/**
 * Render worktree switch tools (EnterWorktree/LeaveWorktree) as a path view.
 */
function renderWorktreeSwitch(input: Record<string, any>): string {
  const path = input.path || input.worktree_path || ''
  let html = '<div class="worktree-switch-view">'
  html += `<span class="worktree-switch-icon">🌳</span>`
  if (path) {
    const projectRoot = store.state.projectRoot || ''
    const homeDir = store.state.homeDir || ''
    const resolvedPath = resolveFilePath(path, projectRoot, homeDir)
    const displayPath = resolvedPath || path.replace(/^\.\//, '')
    html += `<span class="worktree-switch-path">${escapeHtml(displayPath)}</span>`
    if (resolvedPath) {
      html += fileOpenButtonHtml(resolvedPath)
    }
  }
  html += '</div>'
  return html
}

/**
 * Render SendMessage tool input as a message card.
 */
function renderSendMessage(input: Record<string, any>): string {
  const recipient = input.recipient || ''
  const content = input.content || input.message || ''

  let html = '<div class="send-message-view">'
  html += '<div class="send-message-header">'
  html += '<span class="send-message-icon">💬</span>'
  if (recipient) {
    html += `<span class="send-message-recipient">${escapeHtml(gt('tool.sendMessage.to'))} ${escapeHtml(recipient)}</span>`
  }
  html += '</div>'
  if (content) {
    const truncated = content.length > 300 ? content.substring(0, 300) + '…' : content
    html += `<div class="send-message-content">${escapeHtml(truncated)}</div>`
  }
  html += '</div>'
  return html
}

/**
 * Render ComputerUse tool input as a computer action view.
 */
function renderComputerUse(input: Record<string, any>): string {
  const action = input.action || ''
  const description = input.description || input.text || ''

  let html = '<div class="computer-use-view">'
  html += '<div class="computer-use-header">'
  html += '<span class="computer-use-icon">🖥️</span>'
  if (action) {
    html += `<span class="computer-use-action">${escapeHtml(action)}</span>`
  }
  html += '</div>'
  if (description) {
    const truncated = description.length > 200 ? description.substring(0, 200) + '…' : description
    html += `<div class="computer-use-desc">${escapeHtml(truncated)}</div>`
  }
  html += '</div>'
  return html
}

/**
 * Render team management tools (TeamCreate/TeamDelete) as a simple card.
 */
function renderTeamTool(input: Record<string, any>): string {
  const name = input.name || input.team_name || ''

  let html = '<div class="team-tool-view">'
  html += '<span class="team-tool-icon">👥</span>'
  if (name) {
    html += `<span class="team-tool-name">${escapeHtml(name)}</span>`
  }
  html += '</div>'
  return html
}

/**
 * Render chat reply tools (WeChatReply/WeComReply) as a message card.
 */
function renderChatReply(input: Record<string, any>): string {
  const message = input.message || input.content || ''
  const recipient = input.recipient || input.user || ''

  let html = '<div class="chat-reply-view">'
  html += '<div class="chat-reply-header">'
  html += '<span class="chat-reply-icon">💬</span>'
  if (recipient) {
    html += `<span class="chat-reply-recipient">${escapeHtml(recipient)}</span>`
  }
  html += '</div>'
  if (message) {
    const truncated = message.length > 300 ? message.substring(0, 300) + '…' : message
    html += `<div class="chat-reply-message">${escapeHtml(truncated)}</div>`
  }
  html += '</div>'
  return html
}

/**
 * Render save_memory tool input as a key-value card.
 */
function renderSaveMemory(input: Record<string, any>): string {
  const key = input.key || input.name || ''
  const value = input.value || input.content || ''

  let html = '<div class="save-memory-view">'
  html += '<span class="save-memory-icon">💾</span>'
  if (key) {
    html += `<span class="save-memory-key">${escapeHtml(key)}</span>`
  }
  if (value) {
    const truncated = value.length > 200 ? value.substring(0, 200) + '…' : value
    html += `<div class="save-memory-value">${escapeHtml(truncated)}</div>`
  }
  html += '</div>'
  return html
}

/**
 * Render DeepThink tool input as a thinking indicator.
 */
function renderDeepThink(input: Record<string, any>): string {
  const topic = input.topic || input.query || input.prompt || ''

  let html = '<div class="deep-think-view">'
  html += '<span class="deep-think-icon">🧠</span>'
  if (topic) {
    const truncated = topic.length > 200 ? topic.substring(0, 200) + '…' : topic
    html += `<span class="deep-think-topic">${escapeHtml(truncated)}</span>`
  }
  html += '</div>'
  return html
}

/**
 * Render StructuredOutput tool input as a schema preview.
 */
function renderStructuredOutput(input: Record<string, any>): string {
  const prompt = input.prompt || input.instruction || ''

  let html = '<div class="structured-output-view">'
  html += '<span class="structured-output-icon">📋</span>'
  if (prompt) {
    const truncated = prompt.length > 200 ? prompt.substring(0, 200) + '…' : prompt
    html += `<span class="structured-output-prompt">${escapeHtml(truncated)}</span>`
  }
  html += '</div>'
  return html
}

/**
 * Render SkillManage tool input as a skill management card.
 */
function renderSkillManage(input: Record<string, any>): string {
  const action = input.action || input.operation || ''
  const skill = input.skill || input.name || ''

  let html = '<div class="skill-manage-view">'
  html += '<span class="skill-manage-icon">⚡</span>'
  if (action) {
    html += `<span class="skill-manage-action">${escapeHtml(action)}</span>`
  }
  if (skill) {
    html += `<span class="skill-manage-name">${escapeHtml(skill)}</span>`
  }
  html += '</div>'
  return html
}

/**
 * Render Monitor tool input as a monitor view.
 */
function renderMonitor(input: Record<string, any>): string {
  const command = input.command || ''
  const target = input.target || ''

  let html = '<div class="monitor-view">'
  html += '<span class="monitor-icon">📡</span>'
  if (target) {
    html += `<span class="monitor-target">${escapeHtml(target)}</span>`
  }
  if (command) {
    html += '<div class="monitor-command-body">'
    html += '<span class="bash-prompt">$</span>'
    try {
      html += hljs.highlight(command, { language: 'bash', ignoreIllegals: true }).value
    } catch {
      html += escapeHtml(command)
    }
    html += '</div>'
  }
  html += '</div>'
  return html
}

/**
 * Render ImageGen tool input as an image generation card.
 */
function renderImageGen(input: Record<string, any>): string {
  const prompt = input.prompt || input.description || ''
  const size = input.size || ''

  let html = '<div class="image-gen-view">'
  html += '<span class="image-gen-icon">🎨</span>'
  if (prompt) {
    const truncated = prompt.length > 200 ? prompt.substring(0, 200) + '…' : prompt
    html += `<span class="image-gen-prompt">${escapeHtml(truncated)}</span>`
  }
  if (size) {
    html += `<span class="image-gen-size">${escapeHtml(size)}</span>`
  }
  html += '</div>'
  return html
}

/**
 * Render LSP tool input as a language service card.
 */
function renderLSP(input: Record<string, any>): string {
  const method = input.method || ''
  const filePath = input.file_path || input.path || ''

  let html = '<div class="lsp-view">'
  html += '<span class="lsp-icon">🔮</span>'
  if (method) {
    html += `<span class="lsp-method">${escapeHtml(method)}</span>`
  }
  if (filePath) {
    const projectRoot = store.state.projectRoot || ''
    const homeDir = store.state.homeDir || ''
    const resolvedPath = resolveFilePath(filePath, projectRoot, homeDir)
    const displayPath = resolvedPath || filePath.replace(/^\.\//, '')
    html += `<span class="lsp-file-path">${escapeHtml(displayPath)}</span>`
    if (resolvedPath) {
      html += fileOpenButtonHtml(resolvedPath)
    }
  }
  html += '</div>'
  return html
}

/**
 * Render Git tool input as a git command card.
 */
function renderGit(input: Record<string, any>): string {
  const command = input.command || input.subcommand || ''
  const args = input.args || input.arguments || ''

  let html = '<div class="git-tool-view">'
  html += '<span class="git-tool-icon">🔀</span>'
  html += '<div class="git-tool-body">'
  html += '<span class="bash-prompt">$</span>'
  const fullCmd = `git ${command} ${typeof args === 'string' ? args : JSON.stringify(args)}`.trim()
  try {
    html += hljs.highlight(fullCmd, { language: 'bash', ignoreIllegals: true }).value
  } catch {
    html += escapeHtml(fullCmd)
  }
  html += '</div></div>'
  return html
}

/**
 * Render NotebookEdit tool input — same as Edit with cell context.
 */
function renderNotebookEdit(input: Record<string, any>): string {
  // NotebookEdit has same diff structure as Edit plus cell info
  const filePath = input.file_path || ''
  const cellIndex = input.cell_index ?? input.cellIndex ?? ''
  const newStr = input.new_source || input.new_string || ''

  const projectRoot = store.state.projectRoot || ''
  const homeDir = store.state.homeDir || ''
  const resolvedPath = resolveFilePath(filePath, projectRoot, homeDir)
  const displayPath = resolvedPath || filePath.replace(/^\.\//, '')
  const lang = detectLang(filePath)

  let html = '<div class="edit-diff-view">'
  html += '<div class="tool-file-header">'
  html += `<span class="tool-file-path">${escapeHtml(displayPath)}</span>`
  if (resolvedPath) {
    html += fileOpenButtonHtml(resolvedPath)
  }
  if (cellIndex !== '' && cellIndex !== undefined) {
    html += `<span class="edit-diff-replace-all">Cell ${escapeHtml(String(cellIndex))}</span>`
  }
  html += '</div>'

  if (newStr) {
    html += '<div class="edit-diff-scroll"><div class="edit-diff-body">'
    const lines = newStr.split('\n')
    for (const line of lines) {
      html += `<div class="edit-diff-add">${highlightLine(line, lang)}</div>`
    }
    html += '</div></div>'
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

/** Extra block-level context passed to tool renderers that need it (e.g. PermissionApproval). */
export interface ToolBlockCtx {
  done?: boolean
  status?: string
  output?: string
}

export type ToolRenderer = (input: Record<string, any>, blockCtx?: ToolBlockCtx) => string

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
export function formatToolInput(input: any, toolName?: string, blockCtx?: ToolBlockCtx): string {
  if (toolName) {
    const renderer = TOOL_RENDERERS[toolName.toLowerCase()]
    if (renderer && input && typeof input === 'object') {
      return renderer(input, blockCtx)
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

// ── Tool output renderer registry ──

type ToolOutputRenderer = (output: string) => string
const TOOL_OUTPUT_RENDERERS: Record<string, ToolOutputRenderer> = {}

/**
 * Register an output renderer for a tool type.
 * Tool names are matched case-insensitively.
 */
function registerToolOutputRenderer(toolName: string, renderer: ToolOutputRenderer) {
  TOOL_OUTPUT_RENDERERS[toolName.toLowerCase()] = renderer
}

/**
 * Render tool output as terminal-style output (Bash, Git, PowerShell, etc.).
 * Escapes HTML, annotates localhost URLs, wraps in terminal-styled <pre>.
 */
function renderTerminalOutput(output: string): string {
  const escaped = escapeHtml(output)
  const annotated = annotateLocalhostInEscapedText(escaped)
  return `<div class="bash-output-body"><pre>${annotated}</pre></div>`
}

/**
 * Render tool output as syntax-highlighted code (Read output is file contents).
 * Detects language from the tool input's file_path when available.
 */
function renderCodeOutput(output: string): string {
  const escaped = escapeHtml(output)
  const annotated = annotateLocalhostInEscapedText(escaped)
  return `<div class="tool-output-default"><pre>${annotated}</pre></div>`
}

/**
 * Render a simple success/error status message.
 * For tools that just return "ok" or short status strings.
 */
function renderStatusOutput(output: string): string {
  const trimmed = output.trim()
  // Short status messages get a badge treatment
  if (trimmed.length <= 50) {
    const escaped = escapeHtml(trimmed)
    return `<div class="tool-output-status-msg"><span class="tool-output-ok-badge">${escaped}</span></div>`
  }
  // Longer output falls back to preformatted text
  return renderCodeOutput(output)
}

/**
 * Try to parse output as JSON and pretty-print it.
 * If parsing fails, treat as plain text.
 */
function renderSmartOutput(output: string): string {
  const trimmed = output.trim()
  // Try JSON parse + pretty print
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      const parsed = JSON.parse(trimmed)
      const pretty = JSON.stringify(parsed, null, 2)
      const escaped = escapeHtml(pretty)
      return `<div class="tool-output-default"><pre>${escaped}</pre></div>`
    } catch {
      // Not valid JSON, treat as plain text
    }
  }
  return renderCodeOutput(output)
}

/**
 * Format tool execution output for display in the expanded tool detail area.
 * Renders output text with appropriate styling based on tool type.
 * Uses the tool output renderer registry for type-specific formatting.
 * Falls back to smart output (JSON pretty-print or plain text) for unregistered tools.
 */
export function formatToolOutput(output: string, toolName?: string): string {
  if (!output) return ''
  // Check for a registered output renderer
  if (toolName) {
    const renderer = TOOL_OUTPUT_RENDERERS[toolName.toLowerCase()]
    if (renderer) {
      return renderer(output)
    }
  }
  // Fallback: smart output (detect JSON vs plain text)
  return renderSmartOutput(output)
}

// ── Tool output registrations ──

// Terminal-style output (command output)
registerToolOutputRenderer('bash', renderTerminalOutput)
registerToolOutputRenderer('git', renderTerminalOutput)
registerToolOutputRenderer('powershell', renderTerminalOutput)

// Code/file content output
registerToolOutputRenderer('read', renderCodeOutput)

// Status-style output (success/error messages)
registerToolOutputRenderer('write', renderStatusOutput)
registerToolOutputRenderer('edit', renderStatusOutput)
registerToolOutputRenderer('multiedit', renderStatusOutput)
registerToolOutputRenderer('notebookedit', renderStatusOutput)

// Formatted list output
registerToolOutputRenderer('grep', renderCodeOutput)
registerToolOutputRenderer('glob', renderCodeOutput)
registerToolOutputRenderer('ls', renderCodeOutput)

// Web output
registerToolOutputRenderer('websearch', renderCodeOutput)
registerToolOutputRenderer('webfetch', renderCodeOutput)

// Agent/communication output
registerToolOutputRenderer('agent', renderCodeOutput)
registerToolOutputRenderer('sendmessage', renderCodeOutput)

// Search/indexing tools
registerToolOutputRenderer('lsp', renderCodeOutput)
registerToolOutputRenderer('monitor', renderCodeOutput)

// Skill/task output
registerToolOutputRenderer('skill', renderCodeOutput)
registerToolOutputRenderer('skillmanage', renderCodeOutput)
registerToolOutputRenderer('todowrite', renderStatusOutput)
registerToolOutputRenderer('todoread', renderCodeOutput)

// ── Tool registrations ──

// Core file/code tools
registerToolRenderer('Edit', renderEditDiff)
registerToolRenderer('Bash', renderBashTerminal)
registerToolRenderer('Read', renderReadPreview)
registerToolRenderer('Write', renderWritePreview)
registerToolRenderer('NotebookEdit', renderNotebookEdit)
registerToolRenderer('MultiEdit', renderEditDiff)       // same diff view as Edit

// Search tools
registerToolRenderer('Grep', renderGrepSearch)
registerToolRenderer('Glob', renderGlobPattern)
registerToolRenderer('LS', renderLSView)

// Web tools
registerToolRenderer('WebSearch', renderWebSearch)
registerToolRenderer('WebFetch', renderWebFetch)

// Agent/communication tools
registerToolRenderer('Agent', renderAgentCall)
registerToolRenderer('SendMessage', renderSendMessage)
registerToolRenderer('ComputerUse', renderComputerUse)
registerToolRenderer('TeamCreate', renderTeamTool)
registerToolRenderer('TeamDelete', renderTeamTool)

// Skill/task tools
registerToolRenderer('Skill', renderSkillCall)
registerToolRenderer('SkillManage', renderSkillManage)
registerToolRenderer('TodoWrite', renderTodoWrite)
registerToolRenderer('TodoRead', renderTodoRead)
registerToolRenderer('Task', renderAgentCall)       // ACP generic Task → same as Agent
registerToolRenderer('TaskCreate', renderTaskTool)
registerToolRenderer('TaskUpdate', renderTaskTool)
registerToolRenderer('TaskList', renderTaskTool)
registerToolRenderer('TaskGet', renderTaskTool)
registerToolRenderer('TaskStop', renderTaskTool)
registerToolRenderer('TaskOutput', renderTaskTool)

// Mode/worktree tools
registerToolRenderer('EnterPlanMode', renderModeSwitch)
registerToolRenderer('ExitPlanMode', renderModeSwitch)
registerToolRenderer('EnterWorktree', renderWorktreeSwitch)
registerToolRenderer('LeaveWorktree', renderWorktreeSwitch)

// Chat reply tools
registerToolRenderer('WeChatReply', renderChatReply)
registerToolRenderer('WeComReply', renderChatReply)

// Specialized tools
registerToolRenderer('AskUserQuestion', renderAskUserQuestion)
registerToolRenderer('PermissionApproval', renderPermissionApproval)
registerToolRenderer('save_memory', renderSaveMemory)
registerToolRenderer('DeepThink', renderDeepThink)
registerToolRenderer('StructuredOutput', renderStructuredOutput)
registerToolRenderer('ImageGen', renderImageGen)
registerToolRenderer('Monitor', renderMonitor)
registerToolRenderer('LSP', renderLSP)
registerToolRenderer('Git', renderGit)

// Terminal alias
registerToolRenderer('PowerShell', renderBashTerminal)

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
    appLog.e(TAG, 'permission respond failed:', e)
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
      const multiSelect = (optionEl.closest('.ask-question-item') as HTMLElement | null)?.dataset.multi === 'true'

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

registerToolActionHandler('PermissionApproval', (event, _emit) => {
  const target = event.target as HTMLElement

  // Button click
  const btn = target.closest('.permission-btn') as HTMLElement | null
  if (btn) {
    event.stopPropagation()
    event.preventDefault()

    const view = btn.closest('.permission-approval-view')
    if (!view || view.classList.contains('permission-responded') || view.classList.contains('permission-auto-approved')) {
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
      appLog.w(TAG, 'PermissionApproval: no toolCallId found')
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
