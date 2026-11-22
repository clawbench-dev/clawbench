import { describe, it, expect, vi, afterEach } from 'vitest'
import { pushEscapeLayer, resetEscapeLayersForTest } from '../escapeStack'

function pressEscape(): KeyboardEvent {
  const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
  window.dispatchEvent(event)
  return event
}

afterEach(() => resetEscapeLayersForTest())

describe('escapeStack', () => {
  it('closes only the newest layer', () => {
    const bottom = vi.fn()
    const top = vi.fn()
    const popBottom = pushEscapeLayer(bottom)
    const popTop = pushEscapeLayer(top)

    pressEscape()
    expect(top).toHaveBeenCalledTimes(1)
    expect(bottom).not.toHaveBeenCalled()

    popTop()
    pressEscape()
    expect(bottom).toHaveBeenCalledTimes(1)

    popBottom()
    pressEscape()
    expect(bottom).toHaveBeenCalledTimes(1)
  })

  it('ignores Escape that another handler already claimed', () => {
    const close = vi.fn()
    pushEscapeLayer(close)
    const swallow = (event: KeyboardEvent) => {
      if (event.key === 'Escape') event.preventDefault()
    }
    window.addEventListener('keydown', swallow, { capture: true })
    try {
      pressEscape()
    } finally {
      window.removeEventListener('keydown', swallow, { capture: true })
    }
    expect(close).not.toHaveBeenCalled()
  })

  it('tolerates a disposer being called twice', () => {
    const close = vi.fn()
    const pop = pushEscapeLayer(close)
    pop()
    pop()
    pressEscape()
    expect(close).not.toHaveBeenCalled()
  })
})
