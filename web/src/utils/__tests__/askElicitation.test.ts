import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { formatToolInput, handleToolAction } from '@/utils/renderToolDetail.ts'
import { useToast } from '@/composables/useToast'
import { gt } from '@/composables/useLocale'
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

function mount(input: any, opts: { done?: boolean; status?: string } = {}): { root: HTMLDivElement; view: HTMLElement; emit: any } {
  const root = document.createElement('div')
  root.className = 'tool-detail'
  root.dataset.sessionId = 'sess-1'
  const view = document.createElement('div')
  view.innerHTML = formatToolInput(input, 'AskUserQuestion', { done: !!opts.done, status: opts.status })
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
    // The expired-question toast is a module singleton with a 4s timer; clear
    // it or it leaks into the next test (and keeps vitest's worker alive).
    useToast().dismiss()
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

  it('drops the shared note on a multi-question form and refuses a partial submit', () => {
    // The agent wants one value per schema property, so the note has no property
    // to live in. It used to be rendered anyway: typing in it unlocked Submit
    // with the other questions unanswered, the form went out half-filled, and
    // the agent reported the missing answers as "Unanswered" and asked the same
    // questions again — an endless loop on multi-question cards.
    const twoQuestions = {
      elicitationId: 'elic_5_5',
      questions: [
        { key: 'q0', header: '部署方式', customKey: 'q0_custom', options: [{ label: 'Docker', value: 'docker' }] },
        { key: 'q1', header: '要日志吗', customKey: 'q1_custom', options: [{ label: '要', value: 'yes' }, { label: '不要', value: 'no' }] },
      ],
    }
    const { view, emit } = mount(twoQuestions)

    expect(view.querySelector('.ask-supplementary-input')).toBeNull()

    const items = [...view.querySelectorAll('.ask-question-item')] as HTMLElement[]
    const submit = view.querySelector('.ask-question-submit') as HTMLButtonElement

    click(items[0].querySelector('.ask-question-option') as HTMLElement, emit)
    // One of two answered: Submit must stay locked, or the form goes out with
    // q1 missing and the agent asks everything again.
    expect(submit.disabled).toBe(true)

    click(items[1].querySelector('.ask-question-option') as HTMLElement, emit)
    expect(submit.disabled).toBe(false)

    click(submit, emit)
    const body = JSON.parse(elicitationCall()[1].body)
    expect(body.content).toEqual({ q0: 'docker', q1: 'yes' })
  })

  it('keeps the note on a multi-question card once the request is gone (chat-message exit)', () => {
    // The chat-message path appends the note to the message, so it is delivered
    // there — only the live RPC form has nowhere to put it.
    const { view } = mount({
      elicitationId: 'elic_6_6',
      questions: [
        { key: 'q0', options: [{ label: 'A', value: 'a' }] },
        { key: 'q1', options: [{ label: 'B', value: 'b' }] },
      ],
    }, { done: true, status: 'error' })

    expect(view.querySelector('.ask-supplementary-input')).not.toBeNull()
    expect(view.querySelector('.ask-question-submit')).not.toBeNull()
  })

  it('refuses to send a note as the answer of a multiple-choice field', () => {
    // OpenCode (and MCP servers) validate the answer against the field's
    // options: "Invalid option for form field: choice". A note typed on such a
    // card used to unlock Submit and go out as the value, so every attempt was
    // rejected with a 400 and the same form came back.
    const { view, emit } = mount({
      elicitationId: 'elic_7_7',
      questions: [
        {
          key: 'choice',
          question: 'Allow OpenCode to search the web?',
          options: [
            { label: 'Allow search via Exa', value: 'allow' },
            { label: 'Disable web search', value: 'disable' },
          ],
        },
      ],
    })
    // Nowhere to put free text: the box is not offered at all.
    expect(view.querySelector('.ask-supplementary-input')).toBeNull()

    const submit = view.querySelector('.ask-question-submit') as HTMLButtonElement
    expect(submit.disabled).toBe(true)
    click(submit, emit)
    expect(emit).not.toHaveBeenCalled()
    expect((fetch as any).mock.calls.some((c: any[]) => c[0] === '/api/ai/elicitation/respond')).toBe(false)

    // Picking one of the offered values still works.
    const option = view.querySelector('.ask-question-option') as HTMLElement
    click(option, emit)
    expect(submit.disabled).toBe(false)
    click(submit, emit)
    expect(JSON.parse(elicitationCall()[1].body).content).toEqual({ choice: 'allow' })
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

  it('has no shared note box when several questions could claim it', () => {
    // One box cannot be attributed to one of N questions; guessing would send
    // the user's sentence to the wrong property, so the box is not offered at
    // all (see the partial-submit test above).
    const { view, emit } = mount({
      elicitationId: 'elic_3_3',
      questions: [
        { key: 'q0', question: 'A?', customKey: 'q0_custom', options: [{ label: 'a', value: 'a' }] },
        { key: 'q1', question: 'B?', customKey: 'q1_custom', options: [{ label: 'b', value: 'b' }] },
      ],
    })
    expect(view.querySelector('.ask-supplementary-input')).toBeNull()

    const items = [...view.querySelectorAll('.ask-question-item')] as HTMLElement[]
    const submit = view.querySelector('.ask-question-submit') as HTMLElement

    click(items[0].querySelector('.ask-question-option') as HTMLElement, emit)
    click(items[1].querySelector('.ask-question-option') as HTMLElement, emit)
    click(submit, emit)

    const body = JSON.parse(elicitationCall()[1].body)
    expect(body.content).toEqual({ q0: 'a', q1: 'b' })
  })

  it('reverts the card when the respond call fails', async () => {
    ;(fetch as any).mockResolvedValue({ ok: false, status: 500 })
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
    const { view, emit } = mount(singleQuestionInput, { done: true, status: 'success' })
    expect(view.dataset.readonly).toBe('1')
    expect(view.querySelector('.ask-question-submit')).toBeNull()
    expect(view.querySelector('.ask-supplementary-input')).toBeNull()

    // Clicks must not resurrect a selection on a finished request.
    const option = view.querySelector('.ask-question-option') as HTMLElement
    click(option, emit)
    expect(option.classList.contains('selected')).toBe(false)
  })

  it('keeps a timed-out elicitation answerable and sends the answer as a chat message', () => {
    // The 30-minute backstop answers "unanswered" and the agent moves on, so the
    // RPC is gone — but the user must still be able to pick an option later.
    // The answer then travels as an ordinary chat message (a new turn).
    const { view, emit } = mount(singleQuestionInput, { done: true, status: 'error' })
    expect(view.dataset.readonly).toBeUndefined()
    expect(view.dataset.elicitationLive).toBeUndefined()
    const submit = view.querySelector('.ask-question-submit') as HTMLElement
    expect(submit).not.toBeNull()

    const option = view.querySelector('.ask-question-option') as HTMLElement
    click(option, emit)
    expect(option.classList.contains('selected')).toBe(true)

    click(submit, emit)
    expect(emit).toHaveBeenCalledWith('send-message', '- Docker')
    // No RPC attempt: the request is known to be gone.
    expect((fetch as any).mock.calls.some((c: any[]) => c[0] === '/api/ai/elicitation/respond')).toBe(false)
    expect(view.classList.contains('ask-submitted')).toBe(true)
  })

  it('keeps an interrupted elicitation answerable (turn ended before any answer)', () => {
    const { view } = mount(singleQuestionInput, { done: true, status: 'interrupted' })
    expect(view.dataset.readonly).toBeUndefined()
    expect(view.querySelector('.ask-question-submit')).not.toBeNull()
    expect(view.querySelector('.ask-question-recommend')).not.toBeNull()
  })

  it('never turns a gone request into a chat message (no extra queued message)', async () => {
    // The card can still look live when the request is already gone: OpenCode
    // cancels its own question (it has a timeout of its own) or the question was
    // answered on another client. The answer can no longer reach that request,
    // and sending it as a chat message queued it behind the still-running turn —
    // the user then got a queued message they never composed. The card goes back
    // to answerable, keeps the selection, and the user is told why.
    ;(fetch as any).mockResolvedValue({ ok: false, status: 404 })
    const toast = useToast()
    const { view, emit } = mount(singleQuestionInput)
    const option = view.querySelector('.ask-question-option') as HTMLElement
    const submit = view.querySelector('.ask-question-submit') as HTMLElement

    click(option, emit)
    click(submit, emit)
    await new Promise(r => setTimeout(r, 0))

    expect(emit).not.toHaveBeenCalled()
    expect(view.classList.contains('ask-submitted')).toBe(false)
    expect(option.classList.contains('selected')).toBe(true)
    expect(toast.visible.value).toBe(true)
    expect(toast.message.value).toBe(gt('tool.askUser.expired'))
  })
})
