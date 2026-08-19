// Custom rendering for tool_use block details in chat messages.
// All backends normalize tool names and input field names in their parsers,
// so we can assume canonical field names here: file_path, command, old_string, etc.

import { highlightCode } from './globals.ts'
import { escapeHtml } from './html.ts'
import { detectLang, highlightLine } from './diff.ts'
import { resolveFilePath, fileOpenButtonHtml } from '@/composables/useFilePathAnnotation.ts'
import { localhostOpenButtonHtml, parseLocalhostUrl } from '@/composables/useLocalhostAnnotation.ts'
import { useAppMode } from '@/composables/useAppMode.ts'
import { appLog } from '@/utils/appLog'

const TAG = 'renderToolDetail'
import { gt } from '@/composables/useLocale'
import { store } from '@/stores/app.ts'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import { getSessionId } from '@/composables/useSessionIdentity.ts'
import { copyText } from '@/utils/clipboard.ts'

// ────────────────────────────────────────────────────────────
// Shared SVG icons for tool content headers
// ────────────────────────────────────────────────────────────

const COPY_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>'

const WRAP_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M3 6h18"/><path d="M3 12h15a3 3 0 1 1 0 6h-3"/><path d="M18 15l-3 3 3 3"/><path d="M3 18h7"/></svg>'

// Exported for reuse by ToolDetailDrawer.vue (avoid duplicating SVG constants)
export { COPY_ICON_SVG, WRAP_ICON_SVG }

// ────────────────────────────────────────────────────────────
// Type helpers for tool input
// ────────────────────────────────────────────────────────────

/** Type for tool input JSON objects — values are unknown at rest, use str()/num()/bool() to narrow. */
type ToolInput = Record<string, unknown>

/** Extract a string from unknown tool input value, defaulting to '' */
function str(val: unknown, def = ''): string {
  return typeof val === 'string' ? val : def
}

/** Extract a number from unknown tool input value, defaulting to undefined */
function num(val: unknown): number | undefined {
  return typeof val === 'number' ? val : undefined
}

/**
 * Coerce a 1-based line number from tool input: accepts numbers and numeric
 * strings (some CLIs serialize JSON numbers as strings). Returns undefined for
 * non-numeric, fractional, negative, or zero values — matching the codebase's
 * convention that 0 means "no line" (fileOpenButtonHtml/extractLineInfo).
 */
function lineNum(val: unknown): number | undefined {
  const n = typeof val === 'number' ? val : typeof val === 'string' && val.trim() !== '' ? Number(val) : NaN
  return Number.isInteger(n) && n >= 1 ? n : undefined
}

// ────────────────────────────────────────────────────────────
// Tool renderer functions
// ────────────────────────────────────────────────────────────

/**
 * Build a content header bar with copy + wrap toggle buttons.
 * Follows the same pattern as code-block/table-block headers in useCodeBlockHeader.
 * @param contentSelector CSS selector for the scrollable content element (to find text for copy)
 */
function toolContentHeaderHtml(contentSelector: string): string {
  let html = '<div class="tool-content-header">'
  html += '<span class="tool-content-header-actions">'
  html += `<button class="tool-content-copy-btn" data-action="copy" data-content-selector="${contentSelector}" type="button" title="${escapeHtml(gt('common.copy'))}" aria-label="${escapeHtml(gt('common.copy'))}">${COPY_ICON_SVG}</button>`
  html += `<button class="tool-content-wrap-btn is-wrapped" data-action="wrap" data-content-selector="${contentSelector}" type="button" title="${escapeHtml(gt('toolDetailBlock.wrapOn'))}" aria-label="${escapeHtml(gt('toolDetailBlock.wrapOn'))}">${WRAP_ICON_SVG}</button>`
  html += '</span>'
  html += '</div>'
  return html
}

/**
 * Render Edit tool input as a diff view.
 * Shows old_string lines in red, new_string lines in green.
 * No line numbers, no +/- prefix — color-only distinction.
 * File path is clickable to open the file.
 * Includes content header with copy + wrap toggle.
 */
function renderEditDiff(input: ToolInput): string {
  const filePath = str(input.file_path)
  const oldStr = str(input.old_string)
  const newStr = str(input.new_string)
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

  return `<div class="edit-diff-view tool-content-wrap word-wrap">${header}${toolContentHeaderHtml('.edit-diff-scroll')}${body}</div>`
}

/**
 * Render Bash tool input as a terminal-style view.
 * Shows description (if any) and command with $ prefix.
 */
function renderBashTerminal(input: ToolInput): string {
  const command = str(input.command)
  const description = str(input.description)
  const workdir = str(input.workdir) || str(input.dir_path)
  const timeout = num(input.timeout)
  const runInBackground = input.run_in_background || input.is_background

  let html = '<div class="bash-terminal-view tool-content-wrap word-wrap">'

  if (description) {
    html += `<div class="bash-terminal-desc">${escapeHtml(description)}</div>`
  }

  // Reuse the tool content header (copy + wrap toggle) shared with Edit/Read/Write/JSON views
  html += toolContentHeaderHtml('.bash-command')

  html += '<div class="bash-terminal-body">'
  html += '<span class="bash-prompt">$</span>'

  // Highlight command as bash, wrapped so copy targets only the command (not the $ prompt)
  if (command) {
    html += '<span class="bash-command">'
    try {
      html += highlightCode(command, 'bash')
    } catch {
      html += escapeHtml(command)
    }
    html += '</span>'
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
 * Optionally appends a `:start-end` line range suffix and passes it to the open
 * button so clicking jumps to that location (annotation-style path:80-100).
 */
function filePathHeader(input: ToolInput, extraBadge = '', lineStart?: number, lineEnd?: number): string {
  const filePath = str(input.file_path)
  const projectRoot = store.state.projectRoot || ''
  const homeDir = store.state.homeDir || ''
  const resolvedPath = resolveFilePath(filePath, projectRoot, homeDir)
  const displayPath = resolvedPath || filePath.replace(/^\.\//, '')
  const lineSuffix = lineStart ? `:${lineStart}${lineEnd ? `-${lineEnd}` : ''}` : ''

  let html = '<div class="tool-file-header">'
  html += `<span class="tool-file-path">${escapeHtml(displayPath + lineSuffix)}</span>`
  if (resolvedPath) {
    html += fileOpenButtonHtml(resolvedPath, lineStart, lineEnd)
  }
  if (extraBadge) html += extraBadge
  html += '</div>'
  return html
}

/**
 * Render Read tool input as a file preview view.
 * Shows clickable file path + syntax-highlighted content preview.
 * Includes content header with copy + wrap toggle when content is present.
 */
function renderReadPreview(input: ToolInput): string {
  const filePath = str(input.file_path)
  const lang = detectLang(filePath)
  const content = str(input.content)
  const hasContent = !!content

  const offset = lineNum(input.offset)
  const limit = lineNum(input.limit)
  const lineStart = offset
  const lineEnd = offset && limit ? offset + limit : undefined

  let html = '<div class="file-preview-view'
  if (hasContent) html += ' tool-content-wrap word-wrap'
  html += '">'
  html += filePathHeader(input, '', lineStart, lineEnd)

  // Content header (copy + wrap toggle) — only when there is content to copy
  if (hasContent) {
    html += toolContentHeaderHtml('.file-preview-body')
  }

  // Content preview body
  html += '<div class="file-preview-body">'
  if (content) {
    const lines = content.split('\n')
    for (const line of lines) {
      html += `<div class="file-preview-line">${highlightLine(line, lang)}</div>`
    }
  }
  html += '</div></div>'

  return html
}

/**
 * Render Write tool input as a file write view.
 * Shows clickable file path + syntax-highlighted content to write.
 * Includes content header with copy + wrap toggle.
 */
function renderWritePreview(input: ToolInput): string {
  const filePath = str(input.file_path)
  const lang = detectLang(filePath)

  let html = '<div class="file-write-view tool-content-wrap word-wrap">'
  html += filePathHeader(input, `<span class="file-write-badge">${gt('tool.write.badge')}</span>`)
  html += toolContentHeaderHtml('.file-write-body')

  html += '<div class="file-write-body">'
  const content = str(input.content)
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
function renderAskUserQuestion(input: ToolInput): string {
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

  html += '<div class="ask-question-actions">'
  html += `<button class="ask-question-recommend">${gt('tool.askUser.recommend')}</button>`
  html += `<button class="ask-question-submit" disabled>${gt('tool.askUser.submit')}</button>`
  html += '</div>'
  html += '</div>'

  return html
}

/**
 * Render Grep tool input as a search view.
 * Shows search pattern (highlighted) + search path + output_mode tag.
 */
function renderGrepSearch(input: ToolInput): string {
  const pattern = str(input.pattern)
  const path = str(input.path)
  const outputMode = str(input.output_mode)
  const globFilter = str(input.glob) || str(input.include_pattern) || str(input.include)
  const caseInsensitive = input['-i'] || input.ignoreCase || input.case_sensitive === false
  const contextLines = str(input.context)
  const afterLines = str(input['-A']) || str(input.after)
  const beforeLines = str(input['-B']) || str(input.before)

  let html = '<div class="grep-search-view">'

  // Pattern line
  html += '<div class="grep-pattern-row">'
  html += `<span class="grep-label">${escapeHtml(gt('tool.grep.pattern'))}</span>`
  try {
    html += `<span class="grep-pattern-text">${highlightCode(pattern, 'bash')}</span>`
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
function renderGlobPattern(input: ToolInput): string {
  const pattern = str(input.pattern)
  const path = str(input.path)
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
function renderWebSearch(input: ToolInput): string {
  const query = str(input.query)
  const allowedDomains = Array.isArray(input.allowed_domains) ? input.allowed_domains as string[] : undefined
  const blockedDomains = Array.isArray(input.blocked_domains) ? input.blocked_domains as string[] : undefined
  const topic = str(input.topic)

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
function renderWebFetch(input: ToolInput): string {
  const url = str(input.url) || str(input.prompt)
  const format = str(input.format)
  const timeout = num(input.timeout)

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
  const prompt = input.prompt && input.url ? str(input.prompt) : ''
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
function renderAgentCall(input: ToolInput): string {
  const description = str(input.description)
  const prompt = str(input.prompt)
  const subagentType = str(input.subagent_type) || str(input.mode)

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
    const rendered = renderMarkdownHtml(prompt, {
      sanitize: true,
      skipEnhancements: true,
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
function renderSkillCall(input: ToolInput): string {
  const skill = str(input.skill) || str(input.command)
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
function renderPermissionApproval(input: ToolInput, blockCtx?: ToolBlockCtx): string {
  const options = Array.isArray(input.options) ? input.options as Array<Record<string, unknown>> : []
  const toolName = str(input.toolName)
  const toolInput = str(input.toolInput)
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
      const parsed = JSON.parse(toolInput) as Record<string, unknown>
      const filePath = str(parsed.file_path) || str(parsed.path)
      const command = str(parsed.command)
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
      const label = str(opt.name)
      const kind = str(opt.kind)
      const optionId = str(opt.optionId)
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
function renderLSView(input: ToolInput): string {
  const path = str(input.path) || str(input.dir_path)

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
function renderTodoWrite(input: ToolInput): string {
  const todos = Array.isArray(input.todos) ? input.todos as Array<Record<string, unknown>> : []

  let html = '<div class="todo-write-view">'

  if (todos.length > 0) {
    html += '<div class="todo-write-list">'
    for (const todo of todos) {
      const content = str(todo.content)
      const status = str(todo.status)
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
function renderTodoRead(_input: ToolInput): string {
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
function renderTaskTool(input: ToolInput): string {
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
function renderModeSwitch(input: ToolInput): string {
  let html = '<div class="mode-switch-view">'
  html += `<span class="mode-switch-icon">🔄</span>`
  const mode = str(input.mode) || str(input.mode_id)
  if (mode) {
    html += `<span class="mode-switch-mode">${escapeHtml(mode)}</span>`
  }
  html += '</div>'
  return html
}

/**
 * Render worktree switch tools (EnterWorktree/LeaveWorktree) as a path view.
 */
function renderWorktreeSwitch(input: ToolInput): string {
  const path = str(input.path) || str(input.worktree_path)
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
function renderSendMessage(input: ToolInput): string {
  const recipient = str(input.recipient)
  const content = str(input.content) || str(input.message)

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
function renderComputerUse(input: ToolInput): string {
  const action = str(input.action)
  const description = str(input.description) || str(input.text)

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
function renderTeamTool(input: ToolInput): string {
  const name = str(input.name) || str(input.team_name)

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
function renderChatReply(input: ToolInput): string {
  const message = str(input.message) || str(input.content)
  const recipient = str(input.recipient) || str(input.user)

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
function renderSaveMemory(input: ToolInput): string {
  const key = str(input.key) || str(input.name)
  const value = str(input.value) || str(input.content)

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
 * Render DeepThink tool input with agent-call layout.
 * Shows subagent_type badge + description + markdown-rendered prompt.
 * Falls back to topic/query for backward compatibility with older messages.
 */
function renderDeepThink(input: ToolInput): string {
  const description = str(input.description) || str(input.topic)
  const prompt = str(input.prompt) || str(input.query)
  const subagentType = str(input.subagent_type) || str(input.mode)

  let html = '<div class="deep-think-view">'

  if (!description && !prompt) {
    html += '<div class="deep-think-thinking">🧠 Thinking...</div>'
  } else {
    html += '<div class="agent-call-header">'
    if (subagentType) {
      html += `<span class="agent-type-badge">${escapeHtml(subagentType)}</span>`
    }
    if (description) {
      html += `<span class="agent-call-desc">${escapeHtml(description)}</span>`
    }
    html += '</div>'

    if (prompt) {
      const rendered = renderMarkdownHtml(prompt, { sanitize: true, skipEnhancements: true, wrapTables: false })
      html += `<div class="agent-call-prompt">${rendered}</div>`
    }
  }

  html += '</div>'
  return html
}

/**
 * Render DeepThink tool output — strips <task>/<task_result> XML wrappers
 * and renders the markdown content from the sub-agent.
 */
function renderDeepThinkOutput(output: string): string {
  const result = output
    .replace(/^<task_result[^>]*>\n?/, '')
    .replace(/\n?<\/task_result>$/, '')
    .replace(/^<task[^>]*>\n?/, '')
    .replace(/\n?<\/task>$/, '')
    .trim()
  return renderMarkdownHtml(result, { sanitize: true, skipEnhancements: true, wrapTables: false })
}

/**
 * Render StructuredOutput tool input as a schema preview.
 */
function renderStructuredOutput(input: ToolInput): string {
  const prompt = str(input.prompt) || str(input.instruction)

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
function renderSkillManage(input: ToolInput): string {
  const action = str(input.action) || str(input.operation)
  const skill = str(input.skill) || str(input.name)

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
function renderMonitor(input: ToolInput): string {
  const command = str(input.command)
  const target = str(input.target)

  let html = '<div class="monitor-view">'
  html += '<span class="monitor-icon">📡</span>'
  if (target) {
    html += `<span class="monitor-target">${escapeHtml(target)}</span>`
  }
  if (command) {
    html += '<div class="monitor-command-body">'
    html += '<span class="bash-prompt">$</span>'
    try {
      html += highlightCode(command, 'bash')
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
function renderImageGen(input: ToolInput): string {
  const prompt = str(input.prompt) || str(input.description)
  const size = str(input.size)

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
function renderLSP(input: ToolInput): string {
  const method = str(input.method)
  const filePath = str(input.file_path) || str(input.path)

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
function renderGit(input: ToolInput): string {
  const command = str(input.command) || str(input.subcommand)
  const args = input.args || input.arguments || ''

  let html = '<div class="git-tool-view">'
  html += '<span class="git-tool-icon">🔀</span>'
  html += '<div class="git-tool-body">'
  html += '<span class="bash-prompt">$</span>'
  const fullCmd = `git ${command} ${typeof args === 'string' ? args : JSON.stringify(args)}`.trim()
  try {
    html += highlightCode(fullCmd, 'bash')
  } catch {
    html += escapeHtml(fullCmd)
  }
  html += '</div></div>'
  return html
}

/**
 * Render NotebookEdit tool input — same as Edit with cell context.
 * Includes content header with copy + wrap toggle.
 */
function renderNotebookEdit(input: ToolInput): string {
  // NotebookEdit has same diff structure as Edit plus cell info
  const filePath = str(input.file_path)
  const cellIndex = input.cell_index ?? input.cellIndex ?? ''
  const newStr = str(input.new_source) || str(input.new_string)

  const projectRoot = store.state.projectRoot || ''
  const homeDir = store.state.homeDir || ''
  const resolvedPath = resolveFilePath(filePath, projectRoot, homeDir)
  const displayPath = resolvedPath || filePath.replace(/^\.\//, '')
  const lang = detectLang(filePath)

  let html = '<div class="edit-diff-view tool-content-wrap word-wrap">'
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
    html += toolContentHeaderHtml('.edit-diff-scroll')
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
 * Includes content header with copy + wrap toggle.
 */
function renderJsonFallback(input: unknown): string {
  if (!input || (typeof input === 'object' && !Array.isArray(input) && Object.keys(input as Record<string, unknown>).length === 0)) {
    try {
      const highlighted = highlightCode('{}', 'json')
      return `<div class="tool-json-body tool-content-wrap word-wrap">${toolContentHeaderHtml('.tool-json-body code')}<code>${highlighted}</code></div>`
    } catch {
      return '<div class="tool-json-body tool-content-wrap word-wrap"><code>{}</code></div>'
    }
  }
  try {
    const json = JSON.stringify(input, null, 2)
    const highlighted = highlightCode(json, 'json')
    return `<div class="tool-json-body tool-content-wrap word-wrap">${toolContentHeaderHtml('.tool-json-body code')}<code>${highlighted}</code></div>`
  } catch {
    return `<div class="tool-json-body tool-content-wrap word-wrap">${toolContentHeaderHtml('.tool-json-body code')}<code>${escapeHtml(JSON.stringify(input, null, 2))}</code></div>`
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

export type ToolRenderer = (input: ToolInput, blockCtx?: ToolBlockCtx) => string

export type ToolActionHandler = (
  event: Event,
  emit: (type: string, payload?: unknown) => void
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
export function handleToolAction(toolName: string, event: Event, emit: (type: string, payload?: unknown) => void): boolean {
  const handler = TOOL_ACTION_HANDLERS[toolName.toLowerCase()]
  if (!handler) return false
  return handler(event, emit)
}

/**
 * Handle tool content header button clicks (copy + wrap toggle).
 * Works on the tool-content-wrap elements added by renderEditDiff,
 * renderWritePreview, renderReadPreview, and renderNotebookEdit.
 *
 * Call from ToolDetailDrawer's handleBodyClick before other handlers.
 * @returns true if the click was handled (caller should return early)
 */
export function handleToolContentHeaderClick(event: MouseEvent): boolean {
  const target = event.target as HTMLElement
  const btn = target.closest('.tool-content-copy-btn, .tool-content-wrap-btn') as HTMLElement | null
  if (!btn) return false

  event.preventDefault()
  event.stopPropagation()

  const wrapper = btn.closest('.tool-content-wrap') as HTMLElement | null
  if (!wrapper) return true

  const action = btn.getAttribute('data-action')

  if (action === 'copy') {
    if (btn.classList.contains('is-copied')) return true // already showing feedback
    // Find the content element to extract text from
    const contentSelector = btn.getAttribute('data-content-selector') || ''
    const contentEl = contentSelector ? wrapper.querySelector(contentSelector) : wrapper
    const text = contentEl?.textContent || ''
    copyText(text)
    // Show "Copied!" on the button briefly
    const originalTitle = btn.getAttribute('title') || ''
    const originalAriaLabel = btn.getAttribute('aria-label') || ''
    btn.innerHTML = `<span class="tool-content-copied-text">${gt('common.copied')}</span>`
    btn.classList.add('is-copied')
    btn.setAttribute('title', gt('common.copied'))
    btn.setAttribute('aria-label', gt('common.copied'))
    setTimeout(() => {
      btn.innerHTML = COPY_ICON_SVG
      btn.classList.remove('is-copied')
      btn.setAttribute('title', originalTitle)
      btn.setAttribute('aria-label', originalAriaLabel)
    }, 1500)
  } else if (action === 'wrap') {
    wrapper.classList.toggle('word-wrap')
    btn.classList.toggle('is-wrapped')
    const isWrapped = wrapper.classList.contains('word-wrap')
    btn.setAttribute('title', isWrapped ? gt('toolDetailBlock.wrapOn') : gt('toolDetailBlock.wrapOff'))
    btn.setAttribute('aria-label', isWrapped ? gt('toolDetailBlock.wrapOn') : gt('toolDetailBlock.wrapOff'))
  }

  return true
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
export function formatToolInput(input: unknown, toolName?: string, blockCtx?: ToolBlockCtx): string {
  if (toolName) {
    const renderer = TOOL_RENDERERS[toolName.toLowerCase()]
    if (renderer && input && typeof input === 'object') {
      return renderer(input as ToolInput, blockCtx)
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
    // Un-escape the URL for data attributes (escapeHtml turned & into &amp;, etc.)
    const rawUrl = url.replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"')
    const parsed = parseLocalhostUrl(rawUrl)
    const protocol = parsed?.protocol || (rawUrl.startsWith('https') ? 'https' : 'http')
    const path = parsed?.path || ''
    // In <pre> context, wrap URL in <a> and append the open button
    const linkHtml = `<a href="${escapeHtml(rawUrl)}" target="_blank" rel="noopener">${url}</a>`
    return `${linkHtml}${localhostOpenButtonHtml(port, protocol, rawUrl, path)}`
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
  return `<div class="tool-output-content"><pre>${annotated}</pre></div>`
}

/**
 * Render tool output as syntax-highlighted code (Read output is file contents).
 * Detects language from the tool input's file_path when available.
 */
function renderCodeOutput(output: string): string {
  const escaped = escapeHtml(output)
  const annotated = annotateLocalhostInEscapedText(escaped)
  return `<div class="tool-output-content"><pre>${annotated}</pre></div>`
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
      return `<div class="tool-output-content"><pre>${escaped}</pre></div>`
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
registerToolOutputRenderer('deepthink', renderDeepThinkOutput)

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

export function updateAskSubmitState(view: Element) {
  const items = view.querySelectorAll('.ask-question-item')
  let allAnswered = true
  for (const item of items) {
    if (!item.querySelector('.ask-question-option.selected')) {
      allAnswered = false
      break
    }
  }
  const supplementaryInput = view.querySelector('.ask-supplementary-input') as HTMLInputElement | null
  const hasSupplementary = !!supplementaryInput?.value?.trim()
  const submitBtn = view.querySelector('.ask-question-submit') as HTMLButtonElement | null
  if (submitBtn) {
    submitBtn.disabled = !allAnswered && !hasSupplementary
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

  // Recommend click
  const recommendBtn = target.closest('.ask-question-recommend') as HTMLElement | null
  if (recommendBtn) {
    event.stopPropagation()
    event.preventDefault()
    const view = recommendBtn.closest('.ask-question-view')
    if (view && !view.classList.contains('ask-submitted')) {
      // Mark as submitted
      view.classList.add('ask-submitted')
      const allOptions = view.querySelectorAll('.ask-question-option')
      for (const opt of allOptions) {
        ;(opt as HTMLElement).style.pointerEvents = 'none'
      }
      const submitBtn = view.querySelector('.ask-question-submit') as HTMLButtonElement | null
      if (submitBtn) {
        submitBtn.disabled = true
        submitBtn.style.display = 'none'
      }
      const supplementaryInput = view.querySelector('.ask-supplementary-input') as HTMLInputElement | null
      if (supplementaryInput) {
        supplementaryInput.disabled = true
        supplementaryInput.style.opacity = '0.6'
      }
      recommendBtn.textContent = gt('tool.askUser.recommended')
      recommendBtn.style.pointerEvents = 'none'

      emit('send-message', '你推荐我选哪个？')
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
      // Append supplementary text if provided
      const supplementaryInput = view.querySelector('.ask-supplementary-input') as HTMLInputElement | null
      const supplementaryText = supplementaryInput?.value?.trim()
      if (supplementaryText) {
        answers.push(supplementaryText)
      }
      if (answers.length === 0) return true

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
