import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { formatToolInput, handleToolAction } from '@/utils/renderToolDetail.ts'
import '@/i18n'

// A live ACP elicitation reuses the AskUserQuestion card but must answer through
// POST /api/ai/elicitation/respond instead of `send-message`: the agent is
// blocked on the request, so a chat message would queue behind the very turn it
// is meant to unblock (issue #633).

const singleQuestionInput = {
  elicitationId: 'elic_1_1',
  questions: [
    {
      key: 'q0',
      header: '部署方式',
      question: '你想怎么部署？',
      customKey: 'q0_custom',
      options: [
        { label: 'Docker', value: 'docker', description: '容器部署' },
        { label: '二进制', value: 'binary' },
      ],
    },
  ],
}

function mount(input: any, done = false): { root: HTMLDivElement; view: HTMLElement; emit: any } {
  const root = document.createElement('div')
  root.className = 'tool-detail'
  root.dataset.sessionId = 'sess-1'
  const view = document.createElement('div')
  view.innerHTML = formatToolInput(input, 'AskUserQuestion', { done })
  root.appendChild(view.firstElementChild as HTMLElement)
  document.body.appendChild(root)
  return { root, view: root.firstElementChild as HTMLElement, emit: vi.fn() as any }
}

/** The one fetch call the card makes; other code may also use fetch. */
function elicitationCall(): [string, any] {
  const call = (fetch as any).mock.calls.find((c: any[]) => c[0] === '/api/ai/elicitation/respond')
  if (!call) throw new Error('no POST /api/ai/elicitation/respond')
  return call
}

function click(target: Element, emit: any) {
  const ev = new MouseEvent('click', { bubbles: true, cancelable: true })
  Object.defineProperty(ev, 'target', { value: target, writable: false })
  return handleToolAction('AskUserQuestion', ev, emit)
}

describe('elicitation ask card', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
  })
  afterEach(() => {
    document.body.innerHTML = ''
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('carries the elicitation id, field keys and option values on the DOM', () => {
    const { view } = mount(singleQuestionInput)

    expect(view.dataset.elicitationId).toBe('elic_1_1')
    const item = view.querySelector('.ask-question-item') as HTMLElement
    expect(item.dataset.key).toBe('q0')
    expect(item.dataset.customKey).toBe('q0_custom')

    const options = [...view.querySelectorAll('.ask-question-option')] as HTMLElement[]
    expect(options.map(o => o.dataset.value)).toEqual(['docker', 'binary'])
    // The label stays what the user reads; the value is what the agent gets.
    expect(options[0].dataset.label).toBe('Docker')
  })

  it('does not offer Recommend on a live elicitation card', () => {
    // Recommend sends a chat message, which is exactly what a blocked turn
    // cannot accept.
    const { view } = mount(singleQuestionInput)
    expect(view.querySelector('.ask-question-recommend')).toBeNull()
    expect(view.querySelector('.ask-question-submit')).not.toBeNull()
  })

  it('posts the selection to the elicitation endpoint instead of sending a message', async () => {
    const { view, emit } = mount(singleQuestionInput)
    const option = view.querySelector('.ask-question-option') as HTMLElement
    const submit = view.querySelector('.ask-question-submit') as HTMLElement

    click(option, emit)
    click(submit, emit)

    expect(emit).not.toHaveBeenCalled()
    const call = elicitationCall()
    expect(JSON.parse(call[1].body)).toEqual({
      sessionId: 'sess-1',
      elicitationId: 'elic_1_1',
      content: { q0: 'docker' },
    })
    // Optimistic terminal state: the card must not accept a second answer.
    expect(view.classList.contains('ask-submitted')).toBe(true)
  })

  it('sends the visible note as the "type your own answer" field for a single question', () => {
    const { view, emit } = mount(singleQuestionInput)
    const option = view.querySelector('.ask-question-option') as HTMLElement
    const note = view.querySelector('.ask-supplementary-input') as HTMLTextAreaElement
    const submit = view.querySelector('.ask-question-submit') as HTMLElement

    click(option, emit)
    note.value = '  用 nix 部署  '
    click(submit, emit)

    const body = JSON.parse(elicitationCall()[1].body)
    expect(body.content).toEqual({ q0: 'docker', q0_custom: '用 nix 部署' })
  })

  it('uses the note as the answer for a single free-text question', () => {
    // A schema property with no options is a free-text field: there is no
    // companion key, so the note IS the answer. Without this the Submit button
    // would be enabled and do nothing while the agent stayed blocked.
    const { view, emit } = mount({
      elicitationId: 'elic_4_4',
      questions: [{ key: 'name', header: '你的名字', question: '请输入' }],
    })
    const note = view.querySelector('.ask-supplementary-input') as HTMLTextAreaElement
    const submit = view.querySelector('.ask-question-submit') as HTMLElement

    note.value = ' 泷泽 '
    click(submit, emit)

    const body = JSON.parse(elicitationCall()[1].body)
    expect(body.content).toEqual({ name: '泷泽' })
  })

  it('keeps every selection of a multi-select question as an array', () => {
    const { view, emit } = mount({
      elicitationId: 'elic_2_2',
      questions: [
        {
          key: 'q0',
          question: '要开哪些',
          multiSelect: true,
          options: [
            { label: '日志', value: 'logs' },
            { label: '指标', value: 'metrics' },
          ],
        },
      ],
    })
    const options = [...view.querySelectorAll('.ask-question-option')] as HTMLElement[]
    const submit = view.querySelector('.ask-question-submit') as HTMLElement

    click(options[0], emit)
    click(options[1], emit)
    click(submit, emit)

    const body = JSON.parse(elicitationCall()[1].body)
    expect(body.content).toEqual({ q0: ['logs', 'metrics'] })
  })

  it('ignores the shared note when several questions could claim it', () => {
    // One box cannot be attributed to one of N questions; guessing would send
    // the user's sentence to the wrong property.
    const { view, emit } = mount({
      elicitationId: 'elic_3_3',
      questions: [
        { key: 'q0', question: 'A?', customKey: 'q0_custom', options: [{ label: 'a', value: 'a' }] },
        { key: 'q1', question: 'B?', customKey: 'q1_custom', options: [{ label: 'b', value: 'b' }] },
      ],
    })
    const items = [...view.querySelectorAll('.ask-question-item')] as HTMLElement[]
    const note = view.querySelector('.ask-supplementary-input') as HTMLTextAreaElement
    const submit = view.querySelector('.ask-question-submit') as HTMLElement

    click(items[0].querySelector('.ask-question-option') as HTMLElement, emit)
    click(items[1].querySelector('.ask-question-option') as HTMLElement, emit)
    note.value = '补充一句'
    click(submit, emit)

    const body = JSON.parse(elicitationCall()[1].body)
    expect(body.content).toEqual({ q0: 'a', q1: 'b' })
  })

  it('reverts the card when the request is gone (stale card after reload)', async () => {
    ;(fetch as any).mockResolvedValue({ ok: false })
    const { view, emit } = mount(singleQuestionInput)
    const option = view.querySelector('.ask-question-option') as HTMLElement
    const submit = view.querySelector('.ask-question-submit') as HTMLElement

    click(option, emit)
    click(submit, emit)

    // Wait for the rejected POST to settle.
    await Promise.resolve()
    await Promise.resolve()

    expect(view.classList.contains('ask-submitted')).toBe(false)
  })

  it('renders an answered elicitation as a record with no buttons', () => {
    const { view, emit } = mount(singleQuestionInput, true)
    expect(view.dataset.readonly).toBe('1')
    expect(view.querySelector('.ask-question-submit')).toBeNull()
    expect(view.querySelector('.ask-supplementary-input')).toBeNull()

    // Clicks must not resurrect a selection on a finished request.
    const option = view.querySelector('.ask-question-option') as HTMLElement
    click(option, emit)
    expect(option.classList.contains('selected')).toBe(false)
  })
})
