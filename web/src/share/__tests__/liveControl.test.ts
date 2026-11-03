import { describe, expect, it } from 'vitest'
import {
  CHAT_MAX_CHARS,
  TEXT_MAX_CHARS,
  contentRectOf,
  fromNormalized,
  parseControlMessage,
  toInjectorEvent,
  toNormalized,
  undoStroke,
  type InkStroke,
} from '../liveControl'

describe('parseControlMessage', () => {
  it('rejects malformed frames instead of throwing', () => {
    expect(parseControlMessage('')).toBeNull()
    expect(parseControlMessage('not json')).toBeNull()
    expect(parseControlMessage('null')).toBeNull()
    expect(parseControlMessage('{"t":"nope"}')).toBeNull()
    // A laser without coordinates would paint at (0,0) — worse than dropping.
    expect(parseControlMessage('{"t":"laser","x":0.5}')).toBeNull()
  })

  it('clamps coordinates into the content rect', () => {
    const m = parseControlMessage('{"t":"laser","x":1.4,"y":-0.2,"on":true}')
    expect(m).toEqual({ t: 'laser', x: 1, y: 0, on: true, from: undefined })
  })

  it('defaults an ink phase to move and sanitises colour and width', () => {
    const m = parseControlMessage('{"t":"ink","x":0.2,"y":0.3,"color":"javascript:alert(1)","width":99}')
    expect(m).toMatchObject({ t: 'ink', phase: 'move', color: '#ff3b30', width: 24 })
  })

  it('drops empty chat lines', () => {
    expect(parseControlMessage('{"t":"chat","text":"   "}')).toBeNull()
    expect(parseControlMessage('{"t":"chat","text":"hi"}')).toMatchObject({ t: 'chat', text: 'hi' })
  })

  it('truncates an over-long chat line instead of rejecting it', () => {
    const long = 'a'.repeat(5000)
    const m = parseControlMessage(JSON.stringify({ t: 'chat', text: long }))
    expect(m).toMatchObject({ t: 'chat' })
    expect((m as { text: string }).text).toHaveLength(2000)
  })

  it('parses a replayed transcript and keeps only the usable lines', () => {
    const m = parseControlMessage(
      JSON.stringify({
        t: 'chat-history',
        messages: [
          { id: 'a', from: 'v1', name: '小明', text: '早', at: 1000 },
          // No id: nothing can dedupe it, so a replayed line would duplicate
          // the live one it came from.
          { from: 'v1', name: '小明', text: 'no id', at: 1001 },
          { id: 'c', from: '', name: '', text: '  ', at: 1002 },
          { id: 'd', from: 'host', name: '主持人', text: '开会了', at: 1003 },
        ],
      }),
    )
    expect(m).toEqual({
      t: 'chat-history',
      messages: [
        { id: 'a', from: 'v1', name: '小明', text: '早', at: 1000 },
        { id: 'd', from: 'host', name: '主持人', text: '开会了', at: 1003 },
      ],
    })
  })

  it('drops a history frame with no usable lines', () => {
    expect(parseControlMessage('{"t":"chat-history","messages":[]}')).toBeNull()
    expect(parseControlMessage('{"t":"chat-history","messages":"nope"}')).toBeNull()
    expect(parseControlMessage('{"t":"chat-history","messages":[{"text":"x"}]}')).toBeNull()
  })

  it('parses the history request', () => {
    expect(parseControlMessage('{"t":"chat-history-req"}')).toEqual({ t: 'chat-history-req' })
  })

  it('keeps a whole IME commit and only caps it at the injector limit', () => {
    // 32 was the old limit and silently cut the tail of an IME commit (a
    // pinyin/speech input that lands 40 characters at once). The Go injector
    // accepts 512, so the wire format must not be stricter than it.
    const commit = '一'.repeat(39)
    const short = parseControlMessage(JSON.stringify({ t: 'ctrl-ev', kind: 'text', text: commit }))
    expect((short as { text: string }).text).toBe(commit)

    const long = parseControlMessage(
      JSON.stringify({ t: 'ctrl-ev', kind: 'text', text: 'a'.repeat(513) }),
    )
    expect((long as { text: string }).text).toHaveLength(TEXT_MAX_CHARS)
    expect(TEXT_MAX_CHARS).toBe(512)
    // Chat lines have their own, much larger budget; the two must not be
    // confused with each other.
    expect(TEXT_MAX_CHARS).toBeLessThan(CHAT_MAX_CHARS)
  })

  it('parses a control release', () => {
    expect(parseControlMessage('{"t":"ctrl-release"}')).toEqual({ t: 'ctrl-release' })
  })

  it('drops a pointer event with no coordinates', () => {
    // Missing coordinates would be injected at the host's (0,0).
    expect(parseControlMessage('{"t":"ctrl-ev","kind":"move"}')).toBeNull()
    expect(parseControlMessage('{"t":"ctrl-ev","kind":"down"}')).toBeNull()
    // Wheel and text do not need them.
    expect(parseControlMessage('{"t":"ctrl-ev","kind":"wheel","delta":120}')).not.toBeNull()
  })

  it('accepts only the known control-event kinds', () => {
    expect(parseControlMessage('{"t":"ctrl-ev","kind":"teleport"}')).toBeNull()
    expect(parseControlMessage('{"t":"ctrl-ev","kind":"move","x":0.5,"y":0.5}')).toMatchObject({
      kind: 'move',
    })
  })

  it('parses the grant frame both ways', () => {
    expect(parseControlMessage('{"t":"ctrl-grant","viewerId":"v1","granted":true}')).toEqual({
      t: 'ctrl-grant',
      viewerId: 'v1',
      granted: true,
    })
    expect(parseControlMessage('{"t":"ctrl-grant","viewerId":"v1"}')).toMatchObject({ granted: false })
  })
})

describe('coordinate mapping', () => {
  it('accounts for the letterbox of a 16:9 picture in a 4:3 element', () => {
    // 800x600 element showing a 1920x1080 picture: scale = min(0.416, 0.555)
    const rect = contentRectOf(
      { clientWidth: 800, clientHeight: 600, videoWidth: 1920, videoHeight: 1080 },
      { left: 100, top: 50 },
    )
    expect(rect.width).toBeCloseTo(800)
    expect(rect.height).toBeCloseTo(450)
    // 75px of black bar above and below.
    expect(rect.top).toBeCloseTo(125)
    expect(rect.left).toBeCloseTo(100)
  })

  it('round-trips a point through normalisation', () => {
    const rect = contentRectOf(
      { clientWidth: 800, clientHeight: 600, videoWidth: 1920, videoHeight: 1080 },
      { left: 100, top: 50 },
    )
    const n = toNormalized(500, 350, rect)
    const back = fromNormalized(n.x, n.y, rect)
    expect(back.clientX).toBeCloseTo(500)
    expect(back.clientY).toBeCloseTo(350)
  })

  it('maps the same normalised point to different pixels on different screens', () => {
    const desktop = contentRectOf(
      { clientWidth: 1920, clientHeight: 1080, videoWidth: 1920, videoHeight: 1080 },
      { left: 0, top: 0 },
    )
    const phone = contentRectOf(
      { clientWidth: 390, clientHeight: 844, videoWidth: 1920, videoHeight: 1080 },
      { left: 0, top: 0 },
    )
    const a = fromNormalized(0.3, 0.7, desktop)
    const b = fromNormalized(0.3, 0.7, phone)
    // Same place in the picture, wildly different pixels — this is exactly why
    // the wire format carries 0..1 and not client coordinates.
    expect(a.clientX).toBeCloseTo(576)
    expect(b.clientX).toBeCloseTo(117)
    expect(a.clientY).not.toBeCloseTo(b.clientY)
  })

  it('treats an element with no picture yet as the whole box', () => {
    const rect = contentRectOf({ clientWidth: 400, clientHeight: 300 }, { left: 10, top: 20 })
    expect(rect).toEqual({ left: 10, top: 20, width: 400, height: 300 })
  })
})

describe('undoStroke', () => {
  it('drops the last stroke and leaves the array untouched', () => {
    const strokes: InkStroke[] = [
      { color: '#fff', width: 2, points: [{ x: 0, y: 0 }] },
      { color: '#fff', width: 2, points: [{ x: 1, y: 1 }] },
    ]
    const next = undoStroke(strokes)
    expect(next).toHaveLength(1)
    expect(strokes).toHaveLength(2)
  })

  it('is a no-op on an empty list', () => {
    expect(undoStroke([])).toEqual([])
  })
})

describe('toInjectorEvent', () => {
  it('strips the envelope fields the injector rejects', () => {
    // The helper decodes strictly, so a frame that still carries `t`/`from`
    // fails with "unknown field" — which is how the whole feature was dead.
    const payload = toInjectorEvent({
      t: 'ctrl-ev',
      kind: 'move',
      x: 0.5,
      y: 0.5,
      from: 'v1',
    })
    expect(payload).not.toBeNull()
    expect(Object.keys(payload!).sort()).toEqual(
      ['button', 'delta', 'key', 'kind', 'modifiers', 'text', 'x', 'y'].sort(),
    )
    expect(JSON.stringify(payload)).not.toContain('"t"')
    expect(JSON.stringify(payload)).not.toContain('from')
  })

  it('returns null for frames that are not control events', () => {
    expect(toInjectorEvent({ t: 'chat', id: '1', from: 'v', name: 'v', text: 'x', at: 0 })).toBeNull()
    expect(toInjectorEvent({ t: 'ctrl-req' })).toBeNull()
  })
})
