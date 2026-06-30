import { describe, expect, it, vi } from 'vitest'
import { annotateCodeBlockHeaders, handleCodeBlockClick } from '@/composables/useCodeBlockHeader'

// Mock clipboard utility
vi.mock('@/utils/clipboard.ts', () => ({
  copyText: vi.fn(),
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// --- annotateCodeBlockHeaders ---

describe('annotateCodeBlockHeaders', () => {
  it('wraps a <pre><code> block with language class', () => {
    const html = '<pre><code class="language-go">fmt.Println("hello")</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('code-block-wrapper')
    expect(result).toContain('code-block-header')
    expect(result).toContain('code-block-lang')
    expect(result).toContain('language-go')
    expect(result).toContain('data-action="copy"')
    expect(result).toContain('data-action="wrap"')
  })

  it('extracts language label from class="language-X"', () => {
    const html = '<pre><code class="language-python">print(1)</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('>python<')
  })

  it('leaves language label empty for untagged code blocks', () => {
    const html = '<pre><code>no language</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('code-block-lang')
  })

  it('skips mermaid blocks', () => {
    const html = '<pre class="mermaid">graph TD; A-->B</pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).not.toContain('code-block-wrapper')
  })

  it('is idempotent — does not double-wrap', () => {
    const html = '<pre><code class="language-js">x</code></pre>'
    const once = annotateCodeBlockHeaders(html)
    const twice = annotateCodeBlockHeaders(once)
    // Count occurrences of code-block-wrapper
    const countOnce = (once.match(/code-block-wrapper/g) || []).length
    const countTwice = (twice.match(/code-block-wrapper/g) || []).length
    expect(countTwice).toBe(countOnce)
  })

  it('handles multiple code blocks', () => {
    const html = '<pre><code class="language-go">a</code></pre><p>text</p><pre><code class="language-rust">b</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    const wrappers = (result.match(/code-block-wrapper/g) || []).length
    expect(wrappers).toBe(2)
  })

  it('returns input unchanged for empty string', () => {
    expect(annotateCodeBlockHeaders('')).toBe('')
  })

  it('returns input unchanged when no <pre><code> exists', () => {
    const html = '<p>Hello world</p>'
    expect(annotateCodeBlockHeaders(html)).toBe(html)
  })

  it('skips <pre> without <code> child', () => {
    const html = '<pre>plain text</pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).not.toContain('code-block-wrapper')
  })
})

// --- handleCodeBlockClick ---

describe('handleCodeBlockClick', () => {
  function createClickEvent(target: HTMLElement): MouseEvent {
    return { target, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent
  }

  it('returns false for non-code-block clicks', () => {
    const div = document.createElement('div')
    const event = createClickEvent(div)
    expect(handleCodeBlockClick(event)).toBe(false)
  })

  it('handles copy button click', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper'
    const header = document.createElement('div')
    header.className = 'code-block-header'
    const copyBtn = document.createElement('button')
    copyBtn.className = 'code-block-copy-btn'
    copyBtn.setAttribute('data-action', 'copy')
    copyBtn.innerHTML = '<svg></svg>'
    header.appendChild(copyBtn)
    const pre = document.createElement('pre')
    const code = document.createElement('code')
    code.textContent = 'hello world'
    pre.appendChild(code)
    wrapper.appendChild(header)
    wrapper.appendChild(pre)
    document.body.appendChild(wrapper)

    const event = createClickEvent(copyBtn)
    const result = handleCodeBlockClick(event)
    expect(result).toBe(true)
    expect(copyBtn.classList.contains('is-copied')).toBe(true)

    wrapper.remove()
  })

  it('handles wrap toggle click', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper'
    const header = document.createElement('div')
    header.className = 'code-block-header'
    const wrapBtn = document.createElement('button')
    wrapBtn.className = 'code-block-wrap-btn'
    wrapBtn.setAttribute('data-action', 'wrap')
    header.appendChild(wrapBtn)
    const pre = document.createElement('pre')
    wrapper.appendChild(header)
    wrapper.appendChild(pre)
    document.body.appendChild(wrapper)

    const event = createClickEvent(wrapBtn)
    const result = handleCodeBlockClick(event)
    expect(result).toBe(true)
    expect(wrapper.classList.contains('word-wrap')).toBe(true)
    expect(wrapBtn.classList.contains('is-wrapped')).toBe(true)

    // Toggle back
    handleCodeBlockClick(createClickEvent(wrapBtn))
    expect(wrapper.classList.contains('word-wrap')).toBe(false)

    wrapper.remove()
  })

  it('returns true and does nothing if wrapper has no <pre>', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper'
    const copyBtn = document.createElement('button')
    copyBtn.className = 'code-block-copy-btn'
    copyBtn.setAttribute('data-action', 'copy')
    wrapper.appendChild(copyBtn)
    document.body.appendChild(wrapper)

    const event = createClickEvent(copyBtn)
    expect(handleCodeBlockClick(event)).toBe(true)

    wrapper.remove()
  })

  it('skips copy when already showing "is-copied" feedback', async () => {
    const { copyText } = await import('@/utils/clipboard.ts')
    vi.mocked(copyText).mockClear()
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper'
    const header = document.createElement('div')
    const copyBtn = document.createElement('button')
    copyBtn.className = 'code-block-copy-btn is-copied'
    copyBtn.setAttribute('data-action', 'copy')
    header.appendChild(copyBtn)
    const pre = document.createElement('pre')
    const code = document.createElement('code')
    code.textContent = 'test'
    pre.appendChild(code)
    wrapper.appendChild(header)
    wrapper.appendChild(pre)
    document.body.appendChild(wrapper)

    const event = createClickEvent(copyBtn)
    handleCodeBlockClick(event)
    expect(copyText).not.toHaveBeenCalled()

    wrapper.remove()
  })
})
