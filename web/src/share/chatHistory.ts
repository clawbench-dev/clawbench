/**
 * Catching a (re)joining viewer up on the chat it missed.
 *
 * Chat rides the control channel and is never stored on the viewer's side, so a
 * viewer that drops out and comes back starts from an empty panel — the lines
 * it saw before are gone even though the publisher still has them. The
 * publisher therefore replays its in-memory transcript on request.
 *
 * Kept as pure functions (no Vue, no i18n): the publisher and the viewer agree
 * on one merge rule, and the framing rules can be tested without a peer
 * connection.
 */
import {
  CHAT_HISTORY_FRAME_CHARS,
  CHAT_HISTORY_MAX,
  LIVE_CHAT_MAX,
  type ChatWire,
} from './liveControl'
import type { ChatMessage } from './LiveChatPanel.vue'

/**
 * Keep only the newest `LIVE_CHAT_MAX` lines.
 *
 * One place decides the bound so the three ways a line can arrive — the local
 * echo, a relayed live frame, and a replay merged after a rejoin — cannot drift
 * apart. `slice(-n)` returns the same array when it is already short enough, so
 * callers can use the result unconditionally.
 */
export function trimChat(messages: ChatMessage[]): ChatMessage[] {
  return messages.length > LIVE_CHAT_MAX ? messages.slice(-LIVE_CHAT_MAX) : messages
}

/** Append one line, dropping the oldest once the transcript is full. */
export function appendChatMessage(existing: ChatMessage[], message: ChatMessage): ChatMessage[] {
  return trimChat([...existing, message])
}

/** The wire shape of one rendered line. `self` is per-recipient, so it stays here. */
export function toChatWire(m: ChatMessage): ChatWire {
  return { id: m.id, from: m.from ?? '', name: m.name, text: m.text, at: m.at }
}

/**
 * Split the tail of a transcript into frames that fit the control channel.
 *
 * Two bounds apply at once: the last `CHAT_HISTORY_MAX` lines only, and frames
 * under `CHAT_HISTORY_FRAME_CHARS` — a single line may already be 2000 chars, so
 * a fixed line count could still produce a frame the parser rejects.
 */
export function chunkChatHistory(messages: ChatMessage[]): ChatWire[][] {
  const wire = messages.slice(-CHAT_HISTORY_MAX).map(toChatWire)
  const frames: ChatWire[][] = []
  let batch: ChatWire[] = []
  let chars = 0
  for (const m of wire) {
    const size = JSON.stringify(m).length
    if (batch.length > 0 && chars + size > CHAT_HISTORY_FRAME_CHARS) {
      frames.push(batch)
      batch = []
      chars = 0
    }
    batch.push(m)
    chars += size
  }
  if (batch.length > 0) frames.push(batch)
  return frames
}

/**
 * Fold replayed lines into what the viewer already has.
 *
 * Deduplicated by frame id: a line sent live while the replay was in flight
 * would otherwise appear twice, and the frames may arrive in any order. `at`
 * ordering (with the id as tie-break) restores the conversation order that the
 * live list already had.
 */
export function mergeChatHistory(
  existing: ChatMessage[],
  incoming: ChatWire[],
  selfId: string,
  fallbackName: (from: string) => string,
): ChatMessage[] {
  const seen = new Set(existing.map((m) => m.id))
  const added: ChatMessage[] = []
  for (const w of incoming) {
    if (!w.id || seen.has(w.id)) continue
    seen.add(w.id)
    added.push({
      id: w.id,
      from: w.from,
      name: w.name || fallbackName(w.from),
      text: w.text,
      at: w.at,
      self: w.from !== '' && w.from === selfId,
    })
  }
  // Same array when nothing is new: the panel watches `messages.length`, and a
  // fresh array on every empty replay would re-render for nothing.
  if (added.length === 0) return existing
  return trimChat(
    [...existing, ...added].sort((a, b) => a.at - b.at || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)),
  )
}
