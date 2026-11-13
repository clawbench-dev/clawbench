import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import {
  createVoiceTarget,
  findVoiceTarget,
  registerVoiceHost,
  removeVoiceText,
  textBeforeTarget,
  writeVoiceText,
  type VoiceTargetHost,
} from './voiceTarget'

function textarea(value: string, start = value.length, end = start): HTMLTextAreaElement {
  const el = document.createElement('textarea')
  el.value = value
  document.body.appendChild(el)
  el.focus()
  el.setSelectionRange(start, end)
  return el
}

describe('voiceTarget', () => {
  beforeEach(() => { document.body.innerHTML = '' })
  afterEach(() => { document.body.innerHTML = '' })

  it('writes at the caret and leaves the text on both sides alone', () => {
    const el = textarea('前后', 1)
    const t = createVoiceTarget(el)!
    writeVoiceText(t, '中间')
    expect(el.value).toBe('前中间后')
    // The caret follows the transcript, so a second utterance continues after it.
    expect(el.selectionStart).toBe(3)
  })

  it('replaces the transcript on every partial instead of appending', () => {
    const el = textarea('', 0)
    const t = createVoiceTarget(el)!
    writeVoiceText(t, '你好')
    writeVoiceText(t, '你好世界')
    expect(el.value).toBe('你好世界')
  })

  it('replaces a selection with the first write', () => {
    const el = textarea('keep this', 5, 9) // "this"
    const t = createVoiceTarget(el)!
    writeVoiceText(t, 'that')
    expect(el.value).toBe('keep that')
  })

  it('puts the replaced selection back on cancel', () => {
    const el = textarea('keep this', 5, 9)
    const t = createVoiceTarget(el)!
    writeVoiceText(t, 'that')
    removeVoiceText(t)
    expect(el.value).toBe('keep this')
    expect(el.selectionStart).toBe(5)
  })

  it('re-anchors when the field was cleared under it (message sent)', () => {
    const el = textarea('发送前的话', 5)
    const t = createVoiceTarget(el)!
    writeVoiceText(t, '听写')
    expect(el.value).toBe('发送前的话听写')

    el.value = ''
    writeVoiceText(t, '新的听写')
    expect(el.value).toBe('新的听写')
  })

  it('does not delete the user\'s own edit when cancelling', () => {
    const el = textarea('abc', 3)
    const t = createVoiceTarget(el)!
    writeVoiceText(t, '听写')
    // The user edited the dictated text itself — it is theirs now, so cancel
    // must not splice the range out from under them.
    el.value = 'abc听'
    removeVoiceText(t)
    expect(el.value).toBe('abc听')
  })

  it('dispatches an input event so v-model sees the change', () => {
    const el = textarea('', 0)
    const seen: string[] = []
    el.addEventListener('input', () => seen.push(el.value))
    writeVoiceText(createVoiceTarget(el)!, '你好')
    expect(seen).toEqual(['你好'])
  })

  it('refuses password, read-only and disabled fields', () => {
    const pwd = document.createElement('input')
    pwd.type = 'password'
    document.body.appendChild(pwd)

    const ro = textarea('x')
    ro.readOnly = true

    const disabled = textarea('y')
    disabled.disabled = true

    expect(createVoiceTarget(pwd)).toBeNull()
    expect(createVoiceTarget(ro)).toBeNull()
    expect(createVoiceTarget(disabled)).toBeNull()
  })

  it('refuses number inputs (no selection API) and the terminal', () => {
    const num = document.createElement('input')
    num.type = 'number'
    document.body.appendChild(num)
    expect(createVoiceTarget(num)).toBeNull()

    const term = document.createElement('div')
    term.className = 'xterm'
    const inner = document.createElement('textarea')
    term.appendChild(inner)
    document.body.appendChild(term)
    inner.focus()
    expect(findVoiceTarget()).toBeNull()
  })

  it('refuses anything marked data-voice-ignore', () => {
    const el = textarea('x')
    el.setAttribute('data-voice-ignore', '')
    expect(createVoiceTarget(el)).toBeNull()
  })

  it('finds the focused field', () => {
    const el = textarea('hello', 5)
    expect(findVoiceTarget()?.el).toBe(el)
  })

  it('reports the text in front of the caret as recognition context', () => {
    const el = textarea('前中间后', 3)
    expect(textBeforeTarget(createVoiceTarget(el)!)).toBe('前中间')
  })

  it('caps the recognition context so a whole file is never uploaded', () => {
    const el = textarea(`${'x'.repeat(5000)}尾巴`, 5002)
    expect(textBeforeTarget(createVoiceTarget(el)!)).toHaveLength(200)
  })

  /**
   * CodeMirror and friends: focus sits on a contenteditable child, so the only
   * way in is a host registered for the editor box. This fake stands in for
   * that editor — the point is that the caret arithmetic works against it.
   */
  function registeredEditor(initial: string, caret = initial.length) {
    const container = document.createElement('div')
    container.className = 'cm-editor'
    const content = document.createElement('div')
    // jsdom does not implement the `contentEditable` property, so the attribute
    // has to be set explicitly for the div to be focusable here.
    content.setAttribute('contenteditable', 'true')
    container.appendChild(content)
    document.body.appendChild(container)
    let doc = initial
    let sel = { from: caret, to: caret }
    const host: VoiceTargetHost = {
      read: () => doc,
      range: () => sel,
      focused: () => document.activeElement === content,
      replace: (from, to, text) => {
        doc = doc.slice(0, from) + text + doc.slice(to)
        sel = { from: from + text.length, to: from + text.length }
        return true
      },
      select: (from, to) => { sel = { from, to } },
      alive: () => container.isConnected,
    }
    const unregister = registerVoiceHost(container, () => host)
    return { container, content, unregister, doc: () => doc, selection: () => sel }
  }

  it('writes into a registered editor at its caret, and cancels back out', () => {
    const editor = registeredEditor('const a = 1\n')
    const t = createVoiceTarget(editor.content)!
    // The editor box is the anchor (the pill must not hug the text flow).
    expect(t.el).toBe(editor.container)

    writeVoiceText(t, '// 注释')
    expect(editor.doc()).toBe('const a = 1\n// 注释')

    // Streaming partials rewrite the same range instead of appending.
    writeVoiceText(t, '// 注释完成')
    expect(editor.doc()).toBe('const a = 1\n// 注释完成')

    removeVoiceText(t)
    expect(editor.doc()).toBe('const a = 1\n')
    expect(editor.selection()).toEqual({ from: 12, to: 12 })
  })

  it('finds a registered editor from the focused node inside it', () => {
    const editor = registeredEditor('abc', 1)
    editor.content.focus()
    expect(findVoiceTarget()?.el).toBe(editor.container)
  })

  it('ignores a registered editor that reports itself unavailable', () => {
    const container = document.createElement('div')
    const content = document.createElement('div')
    content.setAttribute('contenteditable', 'true')
    container.appendChild(content)
    document.body.appendChild(container)
    // Read-only / disabled: the factory answers null.
    registerVoiceHost(container, () => null)
    expect(createVoiceTarget(content)).toBeNull()
  })

  it('stops resolving once the editor unregisters', () => {
    const editor = registeredEditor('abc')
    expect(createVoiceTarget(editor.content)).not.toBeNull()
    editor.unregister()
    expect(createVoiceTarget(editor.content)).toBeNull()
  })
})
