/**
 * pickUnspokenReply — which assistant reply, if any, still needs reading out.
 *
 * Auto-speech normally fires from the terminal `done` event of a turn. That
 * event is not guaranteed to arrive: a WS reconnect mid-turn leaves the hub
 * with no subscriber and the frame is dropped, so the turn ends through a
 * session-state update or a history reload instead. Continuous conversation is
 * the one place where "every reply gets spoken" is the whole point, so it asks
 * this question again after the turn has settled — with nothing playing.
 *
 * Only the NEWEST assistant message is considered. If it was already handed to
 * TTS there is nothing pending: walking further back would replay an older
 * reply the user has long moved past, which is worse than saying nothing.
 *
 * A leaf module (it only imports `extractSpeakableText`) so it can be unit
 * tested without mounting the chat panel.
 */
import { extractSpeakableText } from '@/utils/speakableText'

export interface PendingReply {
  /** Message id, as a string — the key auto-speech uses. */
  id: string
  text: string
}

interface ReplyCandidate {
  role?: string
  id?: string | number
  blocks?: Array<Record<string, unknown>>
  /**
   * Backend-side summary. A long reply that was summarized has its `content`
   * replaced with `{"blocks":[]}`, so the summary is the only text left in the
   * history row — speaking it is better than silence (TTS summarizes long text
   * anyway).
   */
  summary?: string | null
}

export function pickUnspokenReply(
  messages: ReadonlyArray<ReplyCandidate>,
  spoken: ReadonlySet<string>,
): PendingReply | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i]
    if (msg?.role !== 'assistant') continue
    const id = msg.id == null ? '' : String(msg.id)
    // An assistant bubble without an id cannot be keyed, so it can neither be
    // spoken (auto-speech keys by id) nor be remembered as spoken.
    if (!id) return null
    if (spoken.has(id)) return null
    const text = extractSpeakableText(msg.blocks || []) ||
      (typeof msg.summary === 'string' ? msg.summary.trim() : '')
    return text ? { id, text } : null
  }
  return null
}
