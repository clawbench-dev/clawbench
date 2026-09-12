/**
 * XML parsing utilities for structured AI output.
 *
 * Handles the <ask-question> XML tag format.
 * Uses DOMParser for robust parsing of nested XML structures.
 * All data is in child element text nodes (no attributes) so that
 * if parsing fails, content remains human-readable.
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
 * Parse <ask-question> XML content into structured data.
 * Returns null if the XML is invalid or contains no <item> elements.
 */
export function parseAskQuestionXML(rawContent: string): AskQuestionData | null {
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


