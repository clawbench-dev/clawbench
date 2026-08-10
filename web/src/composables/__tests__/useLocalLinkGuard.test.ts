import { describe, expect, it, vi, afterEach } from 'vitest'
import { initLocalLinkGuard } from '@/composables/useLocalLinkGuard'

describe('initLocalLinkGuard', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  function appendLink(href: string) {
    const a = document.createElement('a')
    a.setAttribute('href', href)
    a.textContent = 'link'
    document.body.appendChild(a)
    return a
  }
  function fireClick(target: Element, opts?: MouseEventInit) {
    const e = new MouseEvent('click', { bubbles: true, cancelable: true, ...opts })
    Object.defineProperty(e, 'target', { value: target, writable: false })
    document.dispatchEvent(e)
    return e
  }

  it('intercepts an unhandled relative link and opens it in-app', () => {
    const onOpen = vi.fn()
    const stop = initLocalLinkGuard(onOpen)
    const a = appendLink('src/main.go')

    fireClick(a)
    expect(onOpen).toHaveBeenCalledWith('src/main.go')

    stop()
  })

  it('intercepts a file:// link', () => {
    const onOpen = vi.fn()
    const stop = initLocalLinkGuard(onOpen)
    const a = appendLink('file:///workspace/src/main.go#L10-L20')

    fireClick(a)
    expect(onOpen).toHaveBeenCalledWith('file:///workspace/src/main.go#L10-L20')

    stop()
  })

  it('does not intercept links already defaultPrevented by a site handler', () => {
    const onOpen = vi.fn()
    const stop = initLocalLinkGuard(onOpen)
    const a = appendLink('src/main.go')

    fireClick(a, {})
    // Re-fire with defaultPrevented true to simulate a prior site handler.
    const e = new MouseEvent('click', { bubbles: true, cancelable: true })
    Object.defineProperty(e, 'target', { value: a, writable: false })
    e.preventDefault()
    document.dispatchEvent(e)
    expect(onOpen).toHaveBeenCalledTimes(1)

    stop()
  })

  it('does not intercept external links or anchors', () => {
    const onOpen = vi.fn()
    const stop = initLocalLinkGuard(onOpen)

    fireClick(appendLink('https://example.com'))
    fireClick(appendLink('#section'))
    fireClick(appendLink('mailto:x@y.com'))
    expect(onOpen).not.toHaveBeenCalled()

    stop()
  })

  it('does not intercept download links or backend endpoints', () => {
    const onOpen = vi.fn()
    const stop = initLocalLinkGuard(onOpen)

    const dl = appendLink('/api/local-file/src/main.go?download=1')
    dl.setAttribute('download', 'main.go')
    fireClick(dl)

    fireClick(appendLink('/api/local-file/src/main.go'))
    fireClick(appendLink('/api/some/endpoint'))
    expect(onOpen).not.toHaveBeenCalled()

    stop()
  })

  it('does not intercept modified (ctrl/cmd/middle) clicks', () => {
    const onOpen = vi.fn()
    const stop = initLocalLinkGuard(onOpen)
    const a = appendLink('src/main.go')

    fireClick(a, { ctrlKey: true })
    fireClick(a, { button: 1 })
    expect(onOpen).not.toHaveBeenCalled()

    stop()
  })

  it('stop() removes the listener', () => {
    const onOpen = vi.fn()
    const stop = initLocalLinkGuard(onOpen)
    const a = appendLink('src/main.go')

    stop()
    fireClick(a)
    expect(onOpen).not.toHaveBeenCalled()
  })
})
