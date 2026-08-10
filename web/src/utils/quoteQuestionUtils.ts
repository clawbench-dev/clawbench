/**
 * Pure functions extracted from useQuoteQuestion composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 */

/**
 * Get the closest Element matching a selector from a node.
 * The node may be a Text node, so we use parentElement first.
 */
export function closestElement(node: Node | null, selector: string): HTMLElement | null {
  if (!node) return null
  const el = (node instanceof HTMLElement ? node : node.parentElement)
  return el?.closest?.(selector) ?? null
}

/**
 * Get line numbers from a selection range inside a code preview.
 * Walks up from anchor/focus nodes to find .code-line[data-line] elements.
 */
export function getLineInfo(selection: Selection): { startLine: number; endLine: number } {
  const anchor = closestElement(selection.anchorNode, '.code-line')
  const focus = closestElement(selection.focusNode, '.code-line')
  if (!anchor || !focus) return { startLine: 0, endLine: 0 }

  const anchorLine = parseInt(anchor.getAttribute('data-line') || '0')
  const focusLine = parseInt(focus.getAttribute('data-line') || '0')
  return {
    startLine: Math.min(anchorLine, focusLine),
    endLine: Math.max(anchorLine, focusLine),
  }
}

/**
 * Get the file path and language from the container element.
 */
export function getFileInfo(container: HTMLElement): { filePath: string; language: string } {
  const codePreview = container.closest('.raw-content-pre')
  if (codePreview) {
    const filePath = codePreview.getAttribute('data-file-path') || ''
    const language = codePreview.getAttribute('data-language') || ''
    return { filePath, language }
  }
  const markdownBody = container.closest('.markdown-body')
  if (markdownBody) {
    const filePath = markdownBody.getAttribute('data-file-path') || ''
    return { filePath, language: '' }
  }
  const officeBody = container.closest('.office-preview-body')
  if (officeBody) {
    const filePath = officeBody.getAttribute('data-file-path') || ''
    return { filePath, language: '' }
  }
  return { filePath: '', language: '' }
}

/**
 * Truncate quote text to a maximum length, appending an ellipsis if truncated.
 * Extracted from QuoteQuestionBar.vue's fullQuoteText computed.
 */
export function truncateQuoteText(text: string, maxLen = 150): string {
  return text.length > maxLen ? text.slice(0, maxLen) + '…' : text
}

/**
 * Check if the input text is non-empty after trimming.
 * Extracted from QuoteQuestionBar.vue's canSend computed.
 */
export function canSendInput(inputText: string): boolean {
  return inputText.trim().length > 0
}

/**
 * Strip the project root prefix from an absolute in-project path so it can be
 * opened as a project-relative file. Returns the path unchanged when it is not
 * under the given root (e.g. external paths starting with '/').
 */
export function relativizeProjectPath(filePath: string, projectRoot: string): string {
  if (!filePath) return filePath
  if (projectRoot && filePath.startsWith(projectRoot + '/')) {
    return filePath.slice(projectRoot.length + 1)
  }
  return filePath
}

/**
 * Build a message that embeds quoted code as a fenced code block.
 * The code block includes language prefix, file path, and optional line range
 * so the AI can identify the source context precisely.
 */
export function buildQuoteMessage(
  userMessage: string,
  text: string,
  filePath: string,
  language: string,
  startLine: number,
  endLine: number,
): string {
  const langPrefix = language ? `${language}:` : ':'
  let lineSuffix = ''
  if (startLine && endLine && startLine !== endLine) {
    lineSuffix = `:${startLine}-${endLine}`
  } else if (startLine) {
    lineSuffix = `:${startLine}`
  }
  return `${userMessage.trim()}\n\n\`\`\`${langPrefix}${filePath}${lineSuffix}\n${text}\n\`\`\``
}

export interface QuoteMessageItem {
  text: string
  filePath: string
  language: string
  startLine: number
  endLine: number
  note?: string
}

function buildQuoteBlock(quote: QuoteMessageItem): string {
  const langPrefix = quote.language ? `${quote.language}:` : ':'
  let lineSuffix = ''
  if (quote.startLine && quote.endLine && quote.startLine !== quote.endLine) {
    lineSuffix = `:${quote.startLine}-${quote.endLine}`
  } else if (quote.startLine) {
    lineSuffix = `:${quote.startLine}`
  }
  return `\`\`\`${langPrefix}${quote.filePath}${lineSuffix}\n${quote.text}\n\`\`\``
}

/** Build one prompt from an optional overall question and ordered quoted selections. */
export function buildMultiQuoteMessage(userMessage: string, quotes: QuoteMessageItem[]): string {
  const parts: string[] = []
  const prompt = userMessage.trim()
  if (prompt) parts.push(prompt)

  for (const quote of quotes) {
    const note = quote.note?.trim()
    if (note) parts.push(note)
    parts.push(buildQuoteBlock(quote))
  }

  return parts.join('\n\n')
}
