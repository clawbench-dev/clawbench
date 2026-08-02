/**
 * XML/JSON parsing utilities for structured AI output.
 *
 * Handles <ask-question> XML tag format (with JSON content support).
 * Uses DOMParser for robust parsing of nested XML structures.
 * All data is in child element text nodes (no attributes) so that
 * if parsing fails, content remains human-readable.
 * JSON format is also supported as a fallback inside <ask-question> tags.
 */

// ────────────────────────────────────────────────────────────
// ask-question XML parsing
// ────────────────────────────────────────────────────────────

export interface AskOption {
  label: string
  description?: string
}

export interface AskItem {
  header: string
  multiSelect: boolean
  question: string
  options: AskOption[]
}

export interface AskQuestionData {
  questions: AskItem[]
}

/**
 * Parse JSON-format ask-question content into structured data.
 * JSON format: { "questions": [{ "question", "header", "multiSelect", "options": [{ "label", "description" }] }] }
 * Returns null if JSON is invalid or doesn't match the expected schema.
 */
export function parseAskQuestionJSON(rawContent: string): AskQuestionData | null {
  try {
    const data = JSON.parse(rawContent.trim())
    if (!data || !Array.isArray(data.questions) || data.questions.length === 0) {
      return null
    }

    const questions: AskItem[] = []
    for (const item of data.questions) {
      if (!item.question || !Array.isArray(item.options) || item.options.length === 0) {
        continue
      }

      const options: AskOption[] = []
      for (const opt of item.options) {
        if (opt.label) {
          options.push(opt.description ? { label: opt.label, description: opt.description } : { label: opt.label })
        }
      }

      if (options.length > 0) {
        questions.push({
          header: item.header || '',
          multiSelect: item.multiSelect === true,
          question: item.question,
          options,
        })
      }
    }

    return questions.length > 0 ? { questions } : null
  } catch {
    return null
  }
}

/**
 * Parse <ask-question> XML or JSON content into structured data.
 * Tries XML parsing first, falls back to JSON if XML fails.
 * Returns null if neither format produces valid data.
 */
export function parseAskQuestionXML(rawContent: string): AskQuestionData | null {
  // Try XML first
  const xmlResult = parseAskQuestionXMLOnly(rawContent)
  if (xmlResult) return xmlResult

  // Fall back to JSON
  return parseAskQuestionJSON(rawContent)
}

/**
 * XML-only parsing of <ask-question> content.
 * Returns null if XML is invalid or contains no <item> elements.
 */
function parseAskQuestionXMLOnly(rawContent: string): AskQuestionData | null {
  try {
    const xmlStr = rawContent.trim()
    const parser = new DOMParser()

    // Try parsing as-is first (content may already include <ask-question> wrapper)
    let doc = parser.parseFromString(xmlStr, 'text/xml')
    let parseError = doc.querySelector('parsererror')

    // If parse error, try wrapping in <ask-question> root
    if (parseError || doc.querySelectorAll('item').length === 0) {
      // Also try wrapping in a root element (multiple <item> siblings need a parent)
      const wrapped = `<root>${xmlStr}</root>`
      doc = parser.parseFromString(wrapped, 'text/xml')
      parseError = doc.querySelector('parsererror')
      if (parseError) return null
    }

    const items = doc.querySelectorAll('item')
    if (items.length === 0) return null

    const questions: AskItem[] = []
    items.forEach(item => {
      const header = item.querySelector('header')?.textContent?.trim() || ''
      const multiSelectText = item.querySelector('multi-select')?.textContent?.trim()?.toLowerCase()
      const multiSelect = multiSelectText === 'true'
      const question = item.querySelector('question')?.textContent?.trim() || ''

      const options: AskOption[] = []
      item.querySelectorAll('option').forEach(opt => {
        const label = opt.querySelector('label')?.textContent?.trim() || ''
        const description = opt.querySelector('description')?.textContent?.trim()
        if (label) {
          options.push(description ? { label, description } : { label })
        }
      })

      if (question && options.length > 0) {
        questions.push({ header, multiSelect, question, options })
      }
    })

    if (questions.length === 0) return null
    return { questions }
  } catch {
    return null
  }
}


