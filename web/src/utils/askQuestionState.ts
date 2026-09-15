// ── Per-ask-card answer state ──
//
// The AskUserQuestion card is rendered as an HTML *string* through `v-html`
// (see renderToolDetail.ts renderAskUserQuestion). That means the option
// selection, the supplementary text, and the submitted flag used to live ONLY
// in the live DOM nodes — any re-render that changed the string (a loadHistory
// reload, a merged-card branch flip, a remount of the message list) replaced
// the subtree and silently discarded the user's input.
//
// This store hoists that state out of the DOM so a re-render can restore it.
//
// Module-level (NOT component-local) on purpose, for the same reason as
// chatDraftStore.ts: an SPA project switch changes the root `:key="projectKey"`
// binding and makes Vue destroy and rebuild the whole `.app-container` subtree
// — including every ContentBlocks instance. A component-local Map would be
// garbage-collected with it, dropping answers the user had already selected.
//
// In-memory only: a page reload starts with no answers, matching chatDraftStore.

/** Selected option labels per question index, plus the free-text note and the
 *  submitted flag. All three are the card's user-visible answer state. */
export interface AskAnswerState {
  /** question index (as rendered in data-qi) → selected option labels. */
  selected: Record<string, string[]>
  /** Free-text "supplementary info" field content. */
  supplementary: string
  /** Whether the user already answered this card. */
  submitted: boolean
  /**
   * Which terminal path answered the card. The two render differently: a
   * submit keeps the Submit button visible as "Submitted" and dims the options
   * the user did NOT pick, while "Recommend" hides the Submit button entirely
   * (no option was picked, so there is nothing to dim). Without recording the
   * path a rebuild would have to guess, and a reverted recommend card would
   * come back with its Submit button still hidden.
   *
   * Only meaningful while `submitted` is true — cleared on revert.
   */
  viaRecommend: boolean
}

/** A partial update; omitted keys keep their current value. */
export type AskAnswerPatch = Partial<AskAnswerState>

const answers = new Map<string, AskAnswerState>()

/**
 * Build the store key for one ask card.
 *
 * Scoped by session so that destroying a session can sweep its cards in bulk
 * (`clearAskStatesByPrefix`). `kind` distinguishes the four places a card is
 * rendered — they are NOT interchangeable, because the same questions are
 * re-aggregated differently in each (see the call sites in ContentBlocks.vue):
 *   - 'tool'    a single tool card showing one block's questions
 *   - 'msg'     the merged card holding every question of one message
 *   - 'text'    a text block carrying an <ask-question> tag (pre-Finalize)
 *   - 'summary' the summary-view merged card
 */
export function askCardKey(sessionId: string, kind: string, id: string): string {
  return `${sessionId || 'no-session'}|${kind}:${id}`
}

/** Prefix matching every ask card of a session — for bulk cleanup. */
export function askSessionPrefix(sessionId: string): string {
  return `${sessionId || 'no-session'}|`
}

/** Whether any card of this session carries state (cheap guard for hot paths). */
export function hasAskStatesForPrefix(prefix: string): boolean {
  if (!prefix) return false
  for (const key of answers.keys()) {
    if (key.startsWith(prefix)) return true
  }
  return false
}

/** Whether a state carries no user input at all (and can be dropped). */
function isEmpty(state: AskAnswerState): boolean {
  return (
    !state.submitted &&
    state.supplementary === '' &&
    Object.values(state.selected).every(labels => labels.length === 0)
  )
}

/** Read a card's answer state; undefined when the user has not touched it. */
export function getAskState(key: string): AskAnswerState | undefined {
  if (!key) return undefined
  return answers.get(key)
}

/**
 * Merge a patch into a card's answer state. A patch that leaves the state
 * empty (no selection, no text, not submitted) removes the entry entirely so
 * the Map does not accumulate untouched cards.
 */
export function patchAskState(key: string, patch: AskAnswerPatch): void {
  if (!key) return
  const prev = answers.get(key)
  const submitted = patch.submitted ?? prev?.submitted ?? false
  const next: AskAnswerState = {
    selected: patch.selected ?? prev?.selected ?? {},
    supplementary: patch.supplementary ?? prev?.supplementary ?? '',
    submitted,
    // `viaRecommend` describes a terminal look, so it is only meaningful while
    // submitted. Forcing it false here makes the invariant structural: no
    // caller can leave a stale flag behind and have a later rebuild re-apply
    // the recommend styling to a card that is answerable again.
    viaRecommend: submitted ? (patch.viaRecommend ?? prev?.viaRecommend ?? false) : false,
  }
  if (isEmpty(next)) answers.delete(key)
  else answers.set(key, next)
}

/** Drop a card's answer state (e.g. it was answered and the reply was sent). */
export function clearAskState(key: string): void {
  if (key) answers.delete(key)
}

/**
 * Drop every card whose key starts with `prefix`. Used when a session is
 * archived/destroyed — ask cards are keyed per card (far more entries than the
 * per-session chat drafts), so they must be swept in bulk rather than leaked.
 */
export function clearAskStatesByPrefix(prefix: string): void {
  if (!prefix) return
  for (const key of answers.keys()) {
    if (key.startsWith(prefix)) answers.delete(key)
  }
}

/** @internal Reset all answer state — for tests only. */
export function _resetAskStatesForTesting(): void {
  answers.clear()
}

/** @internal Number of tracked cards — for tests only. */
export function _askStateCountForTesting(): number {
  return answers.size
}
