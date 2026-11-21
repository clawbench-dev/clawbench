import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { nextTick, ref } from 'vue'

/**
 * A real ref, not a plain object: the directive watches it so `draggable`
 * follows a resize/rotation, and a non-reactive value would never fire.
 */
const layout = vi.hoisted(() => ({ isWideScreen: null as unknown as { value: boolean } }))
vi.mock('@/composables/useWideScreenLayout', async () => {
  const { ref } = await import('vue')
  const isWideScreen = ref(true)
  layout.isWideScreen = isWideScreen
  return { getWideScreenState: () => ({ isWideScreen }) }
})

import { ChatDragDirective, type ChatDragBinding } from '../chatDrag'
import { ATTACH_DRAG_MIME, cleanupDragGhost, readAttachDragData } from '@/utils/attachDrag'
import { QUOTE_DRAG_MIME, readQuoteDragData, type QuoteDragPayload } from '@/utils/quoteDrag'

function mockDataTransfer(): DataTransfer {
  const store: Record<string, string> = {}
  const types: string[] = []
  return {
    setData(type: string, value: string) {
      store[type] = value
      if (!types.includes(type)) types.push(type)
    },
    getData(type: string) {
      return store[type] ?? ''
    },
    setDragImage: () => {},
    effectAllowed: 'all',
    get types() {
      return Object.freeze([...types])
    },
  } as unknown as DataTransfer
}

function dragEvent(type: string, dt: DataTransfer): Event {
  const event = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'dataTransfer', { value: dt })
  return event
}

const QUOTE: QuoteDragPayload = {
  text: '',
  filePath: 'acme/widgets#12',
  language: 'issue',
  startLine: 0,
  endLine: 0,
  sourceKind: 'url',
  url: 'https://example.test/acme/widgets/issues/12',
}

function mountDrag(binding: ChatDragBinding): HTMLElement {
  const el = document.createElement('div')
  document.body.appendChild(el)
  ChatDragDirective.mounted!(el, { value: binding } as never)
  return el
}

describe('v-chat-drag', () => {
  beforeEach(() => {
    layout.isWideScreen.value = true
  })

  afterEach(() => {
    cleanupDragGhost()
    document.body.innerHTML = ''
  })

  it('starts a quote drag with the payload and cleans the ghost on dragend', () => {
    const el = mountDrag({ kind: 'quote', payload: QUOTE })
    expect(el.draggable).toBe(true)

    const dt = mockDataTransfer()
    el.dispatchEvent(dragEvent('dragstart', dt))

    expect(readQuoteDragData(dt)).toEqual(QUOTE)
    // The shared ghost is a real DOM element (Chrome cannot snapshot canvas).
    expect(document.querySelector('[data-attach-ghost]')).not.toBeNull()

    el.dispatchEvent(dragEvent('dragend', dt))
    expect(document.querySelector('[data-attach-ghost]')).toBeNull()
  })

  it('starts an attach drag with the file range', () => {
    const el = mountDrag({ kind: 'attach', path: 'docs/a.md', name: 'a.md', startLine: 10, endLine: 24 })
    const dt = mockDataTransfer()
    el.dispatchEvent(dragEvent('dragstart', dt))

    expect(readAttachDragData(dt)).toEqual({
      path: 'docs/a.md',
      isDir: false,
      startLine: 10,
      endLine: 24,
    })
  })

  it('reads a getter payload at dragstart, so a v-for row drags its current data', () => {
    let current = QUOTE
    const el = mountDrag({ kind: 'quote', payload: () => current })

    const first = mockDataTransfer()
    el.dispatchEvent(dragEvent('dragstart', first))
    expect(readQuoteDragData(first)).toEqual(QUOTE)

    current = { ...QUOTE, filePath: 'acme/widgets#13' }
    const second = mockDataTransfer()
    el.dispatchEvent(dragEvent('dragstart', second))
    expect(readQuoteDragData(second)?.filePath).toBe('acme/widgets#13')
  })

  it('disables the drag on a narrow screen and cancels the gesture', async () => {
    const el = mountDrag({ kind: 'quote', payload: QUOTE })

    layout.isWideScreen.value = false
    await nextTick()
    expect(el.draggable).toBe(false)

    const dt = mockDataTransfer()
    const event = dragEvent('dragstart', dt)
    el.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    expect(dt.getData(QUOTE_DRAG_MIME)).toBe('')
  })

  it('keeps the drag on a narrow screen when allowNarrow is set', async () => {
    const el = mountDrag({ kind: 'quote', payload: QUOTE, allowNarrow: true })
    layout.isWideScreen.value = false
    await nextTick()

    expect(el.draggable).toBe(true)
    const dt = mockDataTransfer()
    el.dispatchEvent(dragEvent('dragstart', dt))
    expect(readQuoteDragData(dt)).toEqual(QUOTE)
  })

  it('cancels when the payload is unusable', () => {
    const el = mountDrag({ kind: 'quote', payload: null })
    const event = dragEvent('dragstart', mockDataTransfer())
    el.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
  })

  it('cancels an attach drag with an empty path', () => {
    const el = mountDrag({ kind: 'attach', path: '' })
    const event = dragEvent('dragstart', mockDataTransfer())
    el.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
  })

  it('picks up a changed binding on update', () => {
    const el = mountDrag({ kind: 'quote', payload: QUOTE })
    ChatDragDirective.updated!(el, {
      value: { kind: 'attach', path: 'src/b.ts' },
    } as never)

    const dt = mockDataTransfer()
    el.dispatchEvent(dragEvent('dragstart', dt))
    expect(readAttachDragData(dt)?.path).toBe('src/b.ts')
  })

  it('disables the drag when the binding goes away', () => {
    const el = mountDrag({ kind: 'quote', payload: QUOTE })
    ChatDragDirective.updated!(el, { value: null } as never)

    expect(el.draggable).toBe(false)
    const event = dragEvent('dragstart', mockDataTransfer())
    el.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
  })

  it('tolerates an unmount without a mounted state', () => {
    const el = document.createElement('div')
    expect(() => ChatDragDirective.unmounted!(el, {} as never)).not.toThrow()
  })

  it('removes its listeners on unmount', () => {
    const el = mountDrag({ kind: 'quote', payload: QUOTE })
    ChatDragDirective.unmounted!(el, {} as never)

    const dt = mockDataTransfer()
    el.dispatchEvent(dragEvent('dragstart', dt))
    expect(dt.getData(QUOTE_DRAG_MIME)).toBe('')
  })

  it('cleans the ghost when the row is removed mid-drag', () => {
    const el = mountDrag({ kind: 'quote', payload: QUOTE })
    el.dispatchEvent(dragEvent('dragstart', mockDataTransfer()))
    expect(document.querySelector('[data-attach-ghost]')).not.toBeNull()

    // A removed row never receives dragend.
    ChatDragDirective.unmounted!(el, {} as never)
    expect(document.querySelector('[data-attach-ghost]')).toBeNull()
  })

  it('writes the attach MIME the drop handlers read', () => {
    const el = mountDrag({ kind: 'attach', path: 'a.txt' })
    const dt = mockDataTransfer()
    el.dispatchEvent(dragEvent('dragstart', dt))
    expect(dt.types).toContain(ATTACH_DRAG_MIME)
  })
})
