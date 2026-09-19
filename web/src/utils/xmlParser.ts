/**
 * XML parsing utilities for structured AI output.
 *
 * Handles the <clawbench-ask-question> XML tag format. All parsing is delegated to
 * `@/utils/askQuestion.ts`, the canonical implementation shared with the Go
 * backend (`internal/askquestion`). This module keeps the historical export
 * names so existing call sites and tests are unaffected.
 */

import { parseItems, type AskItem, type AskOption } from '@/utils/askQuestion.ts'

// ────────────────────────────────────────────────────────────
// ask-question XML parsing
// ────────────────────────────────────────────────────────────

export type { AskOption, AskItem }

export interface AskQuestionData {
  questions: AskItem[]
}

/**
 * Parse <clawbench-ask-question> XML content into structured data.
 * Returns null if the payload is invalid or contains no renderable question.
 *
 * The content may include the <clawbench-ask-question> wrapper or be a bare payload.
 */
export function parseAskQuestionXML(rawContent: string): AskQuestionData | null {
  const trimmed = rawContent.trim()
  if (trimmed === '') return null

  // A bare payload (no wrapper) is the common case here: callers pass the
  // inner content detected by detectAskQuestion.
  const items = parseItems(trimmed)
  if (items.length === 0) return null
  return { questions: items }
}
