/**
 * The control channel that rides alongside the media.
 *
 * Everything here is *not* media: laser pointer, ink strokes, chat, presence,
 * remote-control events. It travels over an RTCDataChannel on the same
 * PeerConnection as the picture, which matters for two reasons:
 *
 *  1. it needs no server round trip (latency is the same as the picture's);
 *  2. it is ordered relative to nothing else — a pointer update must never wait
 *     behind a video keyframe.
 *
 * Coordinates are ALWAYS normalised to the shared content (0..1), never pixels:
 * two viewers see the same screen at different sizes, with different
 * letterboxing, and a pixel coordinate from one is meaningless in the other.
 * `contentRectOf` / `toNormalized` are the only sanctioned way to convert.
 */

/** One chat line on the wire — the live `chat` frame and the history replay
 *  carry the same shape, so a replayed line renders exactly like a live one. */
export interface ChatWire {
  id: string
  from: string
  name: string
  text: string
  at: number
}

/** Messages the publisher and viewers exchange over the control channel. */
export type ControlMessage =
  /** Transient pointer. `on:false` means "hide". */
  | { t: 'laser'; x: number; y: number; on: boolean; from?: string }
  /** One freehand stroke. `phase` splits it so the receiver can draw live. */
  | { t: 'ink'; phase: 'start' | 'move' | 'end'; x: number; y: number; color: string; width: number; from?: string }
  | { t: 'ink-clear' }
  | { t: 'ink-undo' }
  | { t: 'chat'; id: string; from: string; name: string; text: string; at: number }
  /**
   * Publisher → viewer: the transcript so far, replayed to a viewer that just
   * joined (or rejoined). Split across frames when long; the receiver merges by
   * `id`, so the order the frames arrive in does not matter.
   */
  | { t: 'chat-history'; messages: ChatWire[] }
  /**
   * Viewer → publisher: "I am on the channel, send me what I missed." Sent by
   * the viewer once its data channel opens — the only moment it can be sure the
   * publisher's side of the same channel is usable.
   */
  | { t: 'chat-history-req'; from?: string }
  /** A viewer's window became visible/hidden. */
  | { t: 'presence'; viewerId: string; visible: boolean; from?: string }
  /** Audio level 0..1 for the speaking indicator (publisher → viewers). */
  | { t: 'talk'; viewerId: string; level: number }
  | { t: 'ctrl-req'; from?: string }
  /** The viewer gave control back (its own release button or Escape). */
  | { t: 'ctrl-release'; from?: string }
  | { t: 'ctrl-grant'; viewerId: string; granted: boolean }
  | {
      t: 'ctrl-ev'
      kind: 'move' | 'down' | 'up' | 'wheel' | 'key' | 'text'
      x?: number
      y?: number
      button?: number
      key?: string
      delta?: number
      /** Literal characters to type (kind=text). */
      text?: string
      /** Held during the event: ctrl / shift / alt / meta. */
      modifiers?: string[]
      from?: string
    }

/** Everything a viewer is allowed to send; the publisher relays these. */
export const VIEWER_MESSAGE_TYPES = new Set([
  'laser',
  'ink',
  'ink-clear',
  'ink-undo',
  'chat',
  'chat-history-req',
  'presence',
  'ctrl-req',
  'ctrl-release',
  'ctrl-ev',
])

/** Upper bound for one chat line. A control channel is not a file transfer. */
export const CHAT_MAX_CHARS = 2000

/**
 * How many chat lines a late joiner is caught up on.
 *
 * A meeting is not an archive: the replay exists so someone who dropped out for
 * a minute does not come back to an empty panel. The whole transcript is in the
 * database for the record view; pushing hundreds of lines through the data
 * channel on every join would compete with the picture.
 */
export const CHAT_HISTORY_MAX = 200

/**
 * Character budget for one history frame.
 *
 * `parseControlMessage` refuses frames over 64 KiB, and one line can be
 * `CHAT_MAX_CHARS` long, so a fixed message count is not a safe bound — the
 * sender splits by accumulated length instead.
 */
export const CHAT_HISTORY_FRAME_CHARS = 24 * 1024

/**
 * Upper bound for one `kind=text` frame, in characters.
 *
 * Must stay in step with `maxTextRunes` in internal/inputinject/event.go (512):
 * the injector truncates there, so a shorter limit here silently eats the tail
 * of what the viewer typed — an IME commit ("一次上屏 40 字") used to arrive as
 * 32 characters. Change one side, change the other.
 */
export const TEXT_MAX_CHARS = 512

function clamp01(n: number): number {
  return n < 0 ? 0 : n > 1 ? 1 : n
}

/**
 * One replayed chat line.
 *
 * Deliberately stricter than the live `chat` frame: a history frame is a list,
 * so one junk entry must not poison the batch — bad entries are dropped and the
 * rest still land.
 */
function parseChatWire(raw: unknown): ChatWire | null {
  if (!raw || typeof raw !== 'object') return null
  const m = raw as Record<string, unknown>
  if (typeof m.text !== 'string' || !m.text.trim()) return null
  if (typeof m.id !== 'string' || !m.id) return null
  return {
    id: m.id,
    from: typeof m.from === 'string' ? m.from : '',
    name: typeof m.name === 'string' ? m.name : '',
    text: m.text.slice(0, CHAT_MAX_CHARS),
    at: typeof m.at === 'number' ? m.at : 0,
  }
}

/**
 * Parses one wire frame.
 *
 * Returns null for anything malformed: a control channel is fed by peers, and a
 * bad frame must be dropped, not thrown into the render path.
 */
export function parseControlMessage(raw: string): ControlMessage | null {
  if (!raw || raw.length > 64 * 1024) return null
  let data: unknown
  try {
    data = JSON.parse(raw)
  } catch {
    return null
  }
  if (!data || typeof data !== 'object') return null
  const m = data as Record<string, unknown>
  const type = m.t

  switch (type) {
    case 'laser': {
      if (typeof m.x !== 'number' || typeof m.y !== 'number') return null
      return {
        t: 'laser',
        x: clamp01(m.x),
        y: clamp01(m.y),
        on: m.on !== false,
        from: typeof m.from === 'string' ? m.from : undefined,
      }
    }
    case 'ink': {
      if (typeof m.x !== 'number' || typeof m.y !== 'number') return null
      const phase = m.phase === 'start' || m.phase === 'end' ? m.phase : 'move'
      const color = typeof m.color === 'string' && /^#[0-9a-f]{3,8}$/i.test(m.color) ? m.color : '#ff3b30'
      const width = typeof m.width === 'number' ? Math.min(24, Math.max(1, m.width)) : 3
      return {
        t: 'ink',
        phase,
        x: clamp01(m.x),
        y: clamp01(m.y),
        color,
        width,
        from: typeof m.from === 'string' ? m.from : undefined,
      }
    }
    case 'ink-clear':
      return { t: 'ink-clear' }
    case 'ink-undo':
      return { t: 'ink-undo' }
    case 'chat': {
      if (typeof m.text !== 'string' || !m.text.trim()) return null
      const text = m.text.slice(0, CHAT_MAX_CHARS)
      return {
        t: 'chat',
        id: typeof m.id === 'string' ? m.id : String(Date.now()),
        from: typeof m.from === 'string' ? m.from : '',
        name: typeof m.name === 'string' ? m.name : '',
        text,
        at: typeof m.at === 'number' ? m.at : Date.now(),
      }
    }
    case 'chat-history-req':
      return { t: 'chat-history-req' }
    case 'chat-history': {
      if (!Array.isArray(m.messages)) return null
      const messages: ChatWire[] = []
      for (const item of m.messages.slice(0, CHAT_HISTORY_MAX)) {
        const parsed = parseChatWire(item)
        if (parsed) messages.push(parsed)
      }
      // An empty frame is not an error worth rendering, but it also carries
      // nothing: drop it so the caller never re-renders on a no-op.
      if (messages.length === 0) return null
      return { t: 'chat-history', messages }
    }
    case 'presence':
      return {
        t: 'presence',
        viewerId: typeof m.viewerId === 'string' ? m.viewerId : '',
        visible: m.visible !== false,
      }
    case 'talk':
      if (typeof m.level !== 'number') return null
      return {
        t: 'talk',
        viewerId: typeof m.viewerId === 'string' ? m.viewerId : '',
        level: clamp01(m.level),
      }
    case 'ctrl-req':
      return { t: 'ctrl-req' }
    case 'ctrl-release':
      return { t: 'ctrl-release' }
    case 'ctrl-grant':
      return {
        t: 'ctrl-grant',
        viewerId: typeof m.viewerId === 'string' ? m.viewerId : '',
        granted: m.granted === true,
      }
    case 'ctrl-ev': {
      const kind = m.kind
      if (
        kind !== 'move' &&
        kind !== 'down' &&
        kind !== 'up' &&
        kind !== 'wheel' &&
        kind !== 'key' &&
        kind !== 'text'
      ) {
        return null
      }
      // A pointer event without coordinates would be injected at (0,0) —
      // the top-left corner of the host's screen. Drop it instead.
      if (kind !== 'wheel' && kind !== 'key' && kind !== 'text') {
        if (typeof m.x !== 'number' || typeof m.y !== 'number') return null
      }
      return {
        t: 'ctrl-ev',
        kind,
        x: typeof m.x === 'number' ? clamp01(m.x) : undefined,
        y: typeof m.y === 'number' ? clamp01(m.y) : undefined,
        button: typeof m.button === 'number' ? m.button : undefined,
        key: typeof m.key === 'string' ? m.key.slice(0, 32) : undefined,
        delta: typeof m.delta === 'number' ? Math.max(-1000, Math.min(1000, m.delta)) : undefined,
        text: typeof m.text === 'string' ? m.text.slice(0, TEXT_MAX_CHARS) : undefined,
        modifiers: Array.isArray(m.modifiers)
          ? m.modifiers.filter((x): x is string => typeof x === 'string').slice(0, 4)
          : undefined,
      }
    }
    default:
      return null
  }
}

/** Rectangle of the actual picture inside an element, in client pixels. */
export interface ContentRect {
  left: number
  top: number
  width: number
  height: number
}

/**
 * Computes where the picture really is inside a `object-fit: contain` element.
 *
 * The letterbox is the whole reason normalised coordinates exist: a viewer on a
 * phone and a viewer on an ultrawide see the same screen with different black
 * bars, so a raw client coordinate would land in a different place on each.
 */
export function contentRectOf(
  element: { clientWidth: number; clientHeight: number; videoWidth?: number; videoHeight?: number },
  origin: { left: number; top: number },
): ContentRect {
  const elW = element.clientWidth
  const elH = element.clientHeight
  const srcW = element.videoWidth ?? 0
  const srcH = element.videoHeight ?? 0
  if (!elW || !elH || !srcW || !srcH) {
    // No picture yet: treat the whole element as the content so early strokes
    // are still in the same coordinate space as later ones.
    return { left: origin.left, top: origin.top, width: elW || 1, height: elH || 1 }
  }
  const scale = Math.min(elW / srcW, elH / srcH)
  const width = srcW * scale
  const height = srcH * scale
  return {
    left: origin.left + (elW - width) / 2,
    top: origin.top + (elH - height) / 2,
    width,
    height,
  }
}

/** Client pixels → normalised (0..1) coordinates inside the content rect. */
export function toNormalized(
  clientX: number,
  clientY: number,
  rect: ContentRect,
): { x: number; y: number } {
  return {
    x: clamp01((clientX - rect.left) / (rect.width || 1)),
    y: clamp01((clientY - rect.top) / (rect.height || 1)),
  }
}

/** Normalised coordinates → client pixels inside the content rect. */
export function fromNormalized(
  x: number,
  y: number,
  rect: ContentRect,
): { clientX: number; clientY: number } {
  return {
    clientX: rect.left + clamp01(x) * rect.width,
    clientY: rect.top + clamp01(y) * rect.height,
  }
}

/** One stroke being drawn locally (also used to replay a received stroke). */
export interface InkStroke {
  color: string
  width: number
  points: Array<{ x: number; y: number }>
}

/** Drops the last stroke; returns a new array so Vue notices the change. */
export function undoStroke(strokes: InkStroke[]): InkStroke[] {
  return strokes.length ? strokes.slice(0, -1) : strokes
}

/**
 * Narrows a control frame to the fields the desktop injector accepts.
 *
 * The helper decodes strictly (unknown fields are an error), so passing the
 * whole frame — which also carries `t` and `from` — makes every event fail. This
 * whitelist is the single place that decides what reaches `SendInput`.
 */
export function toInjectorEvent(msg: ControlMessage): {
  kind: 'move' | 'down' | 'up' | 'wheel' | 'key' | 'text'
  x?: number
  y?: number
  button?: number
  key?: string
  delta?: number
  text?: string
  modifiers?: string[]
} | null {
  if (msg.t !== 'ctrl-ev') return null
  return {
    kind: msg.kind,
    x: msg.x,
    y: msg.y,
    button: msg.button,
    key: msg.key,
    delta: msg.delta,
    text: msg.text,
    modifiers: msg.modifiers,
  }
}
