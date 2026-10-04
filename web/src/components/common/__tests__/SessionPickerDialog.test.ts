import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const { mockCoalescedJson } = vi.hoisted(() => ({
  mockCoalescedJson: vi.fn(),
}))
vi.mock('@/utils/inflightGet.ts', () => ({
  coalescedJson: mockCoalescedJson,
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    getAgentBackend: (id: string) => (id === 'a-claude' ? 'claude' : 'codebuddy'),
    getAgentName: (id: string) => (id === 'a-claude' ? 'Claude' : 'CodeBuddy'),
    getAgentAvatar: () => '',
  }),
}))

// runningSessions / runningSessionsVersion are module-level refs exported by
// useSessionIdentity, and currentSessionId is a ref on the composable's return.
// The mock must hand back REAL refs: the component relies on template
// auto-unwrap and on the version ref to re-render, neither of which a plain
// `{ value }` object provides.
const holders = vi.hoisted(() => ({
  refs: null as null | { running: any; runningVersion: any; currentSessionId: any },
}))
vi.mock('@/composables/useSessionIdentity.ts', async () => {
  const { ref } = await import('vue')
  const running = ref(new Set<string>())
  const runningVersion = ref(0)
  const currentSessionId = ref('')
  holders.refs = { running, runningVersion, currentSessionId }
  return {
    useSessionIdentity: () => ({ currentSessionId, runningSessionsVersion: runningVersion }),
    runningSessions: running,
    runningSessionsVersion: runningVersion,
  }
})

/** Live running Set (a ref). */
const runningRef = () => holders.refs!.running
/** Live running-version ref. */
const runningVersionRef = () => holders.refs!.runningVersion
/** Live current-session-id ref. */
const currentIdRef = () => holders.refs!.currentSessionId

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    props: ['open', 'title'],
    template: '<div class="bottom-sheet-stub"><slot name="header" /><slot /></div>',
  },
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'size'], template: '<span class="agent-icon-stub" />' },
}))

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div class="loading-stub" />' },
}))

import SessionPickerDialog from '@/components/common/SessionPickerDialog.vue'
import { readWebFile } from '@/testUtils/readWebFile'

const SESSIONS = [
  { id: 's-1', title: 'First session', agentId: 'a-codebuddy' },
  { id: 's-2', title: 'Second session', agentId: 'a-claude' },
  { id: 's-3', title: '', agentId: 'a-codebuddy' },
]

function mountPicker(props: Record<string, unknown> = {}) {
  return mount(SessionPickerDialog, { props: { open: true, ...props } })
}

describe('SessionPickerDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    runningRef().value = new Set<string>()
    runningVersionRef().value = 0
    currentIdRef().value = ''
    mockCoalescedJson.mockResolvedValue({ sessions: SESSIONS.map(s => ({ ...s })) })
  })

  it('loads and renders the current project sessions', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    expect(mockCoalescedJson).toHaveBeenCalledWith('/api/ai/sessions')
    const rows = wrapper.findAll('.sp-row:not(.sp-row-create)')
    expect(rows.length).toBe(3)
    expect(wrapper.text()).toContain('First session')
    expect(wrapper.text()).toContain('Second session')
  })

  it('falls back to the unnamed label for a session with no title', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.text()).toContain('session.unnamed')
  })

  it('always offers the create-session row, and it is last', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    const rows = wrapper.findAll('.sp-row')
    expect(rows[rows.length - 1].classes()).toContain('sp-row-create')
  })

  it('shows a trailing spinner for a running session, with no status text', async () => {
    runningRef().value = new Set(['s-2'])
    const wrapper = mountPicker()
    await flushPromises()

    const running = wrapper.findAll('.sp-row').filter(r => r.classes().includes('sp-row-running'))
    expect(running.length).toBe(1)
    expect(running[0].text()).toContain('Second session')
    // The spinner is the whole cue — the label was removed on request.
    expect(running[0].find('.sp-run-spinner').exists()).toBe(true)
    expect(running[0].text()).not.toContain('session.executing')

    // The left side stays icon-only: no running dot before the agent icon.
    expect(wrapper.find('.sp-run-dot').exists()).toBe(false)
    // Order on the trailing edge: spinner first, then the goto button (which is
    // the row's last child, flush right).
    const children = Array.from(running[0].element.children)
    const spinnerIdx = children.findIndex(c => c.classList.contains('sp-run-spinner'))
    const gotoIdx = children.findIndex(c => c.classList.contains('sp-goto'))
    expect(spinnerIdx).toBeGreaterThan(-1)
    expect(gotoIdx).toBe(children.length - 1)
    expect(spinnerIdx).toBeLessThan(gotoIdx)
  })

  it('does not render a spinner for idle sessions', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.findAll('.sp-run-spinner').length).toBe(0)
  })

  // ── Row separators ──
  // Rows are divided by a hairline. jsdom does not apply scoped CSS, so the
  // rules themselves are asserted at source level; here we only pin the DOM
  // shape the `:first-child` rule depends on: the first row IS the list's first
  // element child (no separator above it), and the create row is last.

  it('keeps the first row as the list\'s first child and create as the last', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    const list = wrapper.find('.session-picker-list').element
    expect(list.children[0].classList.contains('sp-row')).toBe(true)
    expect(list.children[0].classList.contains('sp-row-create')).toBe(false)
    const last = list.children[list.children.length - 1]
    expect(last.classList.contains('sp-row-create')).toBe(true)
  })

  // ── Vertical rhythm ──
  // "Too cramped" was the complaint, and the fix is row HEIGHT (32 → 40), not
  // gutters: the list itself must stay flush so the rows run edge to edge. Both
  // halves are asserted, because re-adding a gutter would waste the height the
  // rows just gained. jsdom has no layout engine, so this is a source-level
  // assertion like the separator test below.

  it('carries the vertical rhythm in the row height, not in list gutters', () => {
    const src = readWebFile('src/components/common/SessionPickerDialog.vue')
    const css = src.slice(src.indexOf('<style'))
    // A bare 32px here was the cramped value; pin the floor, not the exact
    // number, so a later bump to 44 is not rejected.
    const row = css.match(/(?:^|\n)\.sp-row\s*\{[^}]*\}/)?.[0]
    expect(row, '.sp-row should exist').toBeTruthy()
    const minHeight = row!.match(/min-height:\s*(\d+)px/)?.[1]
    expect(Number(minHeight)).toBeGreaterThanOrEqual(40)
    // The list is flush top and bottom — no vertical padding of any form.
    const list = css.match(/(?:^|\n)\.session-picker-list\s*\{[^}]*\}/)?.[0]
    expect(list, '.session-picker-list should exist').toBeTruthy()
    expect(list).toMatch(/padding:\s*0;/)
    expect(list, 'a vertical gutter reopens the cramped read').not.toMatch(
      /padding:\s*(?:var\(--space-\d+\)|\d+px)\s+0/,
    )
  })

  it('styles the current chip so it reads as a chip, not bare text', () => {
    // jsdom does not apply the component's scoped CSS, so an unstyled chip
    // (correct DOM, no rule) would pass every behavioural test above while
    // rendering as a stray word glued to the title.
    const src = readWebFile('src/components/common/SessionPickerDialog.vue')
    const css = src.slice(src.indexOf('<style'))
    const rule = css.match(/(?:^|\n)\.sp-current-chip\s*\{[^}]*\}/)?.[0]
    expect(rule, '.sp-current-chip should exist').toBeTruthy()
    expect(rule).toMatch(/color:\s*var\(--accent-color/)
    expect(rule).toMatch(/background:\s*color-mix\(in srgb, var\(--accent-color\)/)
  })

  it('draws the row separator as a top border, suppressed on the first row', () => {
    // jsdom does not apply the component's scoped CSS, so assert the rules
    // themselves — a silent drop would leave the list undivided.
    const src = readWebFile('src/components/common/SessionPickerDialog.vue')
    const css = src.slice(src.indexOf('<style'))
    expect(css).toMatch(/\.sp-row\s*\{[^}]*border-top:\s*1px solid var\(--border-color/)
    // Without this the header's bottom border and the first row's top border
    // stack into a visibly heavier double line.
    expect(css).toMatch(/\.sp-row:first-child\s*\{\s*border-top:\s*none/)
  })

  it('labels the create row as 新会话', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    const create = wrapper.find('.sp-row-create')
    // quoteBar.newSession is the "新会话" wording (session.newSession, used by
    // the session-list header, stays "新建会话").
    expect(create.text()).toContain('quoteBar.newSession')
    expect(create.text()).not.toContain('session.newSession')
  })

  // ── Current-session label ──
  // The tint + rail alone said "this row is special" but not WHY. The row is
  // simultaneously the session you are in and a destination you can pick, so
  // the chip is what disambiguates it from a merely highlighted row.

  it('labels the currently-open session row with the current chip', async () => {
    currentIdRef().value = 's-2'
    const wrapper = mountPicker()
    await flushPromises()

    const chips = wrapper.findAll('.sp-current-chip')
    expect(chips.length).toBe(1)
    expect(chips[0].text()).toBe('quoteBar.current')
    // It must be on the row that owns the current id, not merely the first row.
    expect(chips[0].element.closest('.sp-row')!.textContent).toContain('Second session')
  })

  it('renders no current chip when there is no open session', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.findAll('.sp-current-chip').length).toBe(0)
  })

  it('keeps the current chip out of the create row', async () => {
    currentIdRef().value = 's-1'
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.find('.sp-row-create .sp-current-chip').exists()).toBe(false)
  })

  it('marks and pins the currently-open session to the top', async () => {
    currentIdRef().value = 's-3'
    const wrapper = mountPicker()
    await flushPromises()

    const rows = wrapper.findAll('.sp-row:not(.sp-row-create)')
    expect(rows[0].classes()).toContain('sp-row-current')
    // s-3 was last in the server order; the current one must lead.
    expect(rows[0].text()).toContain('session.unnamed')
  })

  it('leaves the server order alone when the current session is already first', async () => {
    currentIdRef().value = 's-1'
    const wrapper = mountPicker()
    await flushPromises()

    const rows = wrapper.findAll('.sp-row:not(.sp-row-create)')
    expect(rows.map(r => r.text())).toEqual([
      expect.stringContaining('First session'),
      expect.stringContaining('Second session'),
      expect.stringContaining('session.unnamed'),
    ])
  })

  it('emits select with the session id and closes on click', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.findAll('.sp-row:not(.sp-row-create)')[1].trigger('click')

    expect(wrapper.emitted('select')).toBeTruthy()
    expect(wrapper.emitted('select')![0]).toEqual(['s-2'])
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // ── "Add and open" button ──
  // The row click adds WITHOUT leaving; the trailing button adds AND opens.

  it('renders a goto button on every session row', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.findAll('.sp-row:not(.sp-row-create) .sp-goto').length).toBe(3)
    // The create row now carries its own goto button too — same pair.
    expect(wrapper.find('.sp-row-create .sp-goto').exists()).toBe(true)
  })

  it('emits create-and-open when the create row\'s arrow is clicked', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.sp-row-create .sp-goto').trigger('click')

    expect(wrapper.emitted('create-and-open')).toBeTruthy()
    // Must not also fire the plain create (the arrow is the "and open" half).
    expect(wrapper.emitted('create')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('the create row click still emits plain create (no open)', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.sp-row-create .sp-title').trigger('click')

    expect(wrapper.emitted('create')).toBeTruthy()
    expect(wrapper.emitted('create-and-open')).toBeFalsy()
  })

  it('the create row arrow is labelled for assistive tech', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    const btn = wrapper.find('.sp-row-create .sp-goto')
    expect(btn.attributes('aria-label')).toBe('quoteBar.createAndOpen')
    expect(btn.attributes('title')).toBe('quoteBar.createAndOpen')
  })

  it('emits select-and-open (not select) when the goto button is clicked', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.findAll('.sp-goto')[1].trigger('click')

    expect(wrapper.emitted('select-and-open')).toBeTruthy()
    expect(wrapper.emitted('select-and-open')![0]).toEqual(['s-2'])
    // The two actions must stay distinguishable — a click must not fire both.
    expect(wrapper.emitted('select')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('the goto click does not also trigger the row click', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    // @click.stop on the button is what keeps this from bubbling into the row.
    await wrapper.findAll('.sp-goto')[0].trigger('click')
    expect(wrapper.emitted('select-and-open')!.length).toBe(1)
    expect(wrapper.emitted('select')).toBeFalsy()
  })

  it('goto buttons are labelled for assistive tech', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    const btn = wrapper.findAll('.sp-goto')[0]
    expect(btn.attributes('aria-label')).toBe('quoteBar.addAndOpen')
    expect(btn.attributes('title')).toBe('quoteBar.addAndOpen')
  })

  it('emits create (not select) when the create row is clicked', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.sp-row-create').trigger('click')

    expect(wrapper.emitted('create')).toBeTruthy()
    expect(wrapper.emitted('select')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits close when the sheet requests it', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.findComponent({ name: 'BottomSheet' }).vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('shows the empty state when the project has no sessions', async () => {
    mockCoalescedJson.mockResolvedValue({ sessions: [] })
    const wrapper = mountPicker()
    await flushPromises()

    expect(wrapper.text()).toContain('session.noSessions')
    // The create row must still be reachable with no existing sessions.
    expect(wrapper.find('.sp-row-create').exists()).toBe(true)
  })

  it('degrades to the empty state when the fetch fails', async () => {
    mockCoalescedJson.mockRejectedValue(new Error('network down'))
    const wrapper = mountPicker()
    await flushPromises()

    expect(wrapper.text()).toContain('session.noSessions')
    expect(wrapper.find('.sp-row-create').exists()).toBe(true)
  })

  it('does not fetch while closed, and fetches when opened', async () => {
    mockCoalescedJson.mockClear()
    mountPicker({ open: false })
    await flushPromises()
    expect(mockCoalescedJson).not.toHaveBeenCalled()

    mockCoalescedJson.mockClear()
    mountPicker({ open: true })
    await flushPromises()
    expect(mockCoalescedJson).toHaveBeenCalled()
  })

  it('ArrowDown + Enter confirms the highlighted session', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    function key(k: string) {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }))
    }
    key('ArrowDown')
    key('ArrowDown')
    key('Enter')
    await flushPromises()

    expect(wrapper.emitted('select')![0]).toEqual(['s-2'])
  })

  it('keyboard nav reaches the create row past the sessions', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    function key(k: string) {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }))
    }
    // Three sessions, then the create row is the 4th item.
    key('ArrowDown')
    key('ArrowDown')
    key('ArrowDown')
    key('ArrowDown')
    key('Enter')
    await flushPromises()

    expect(wrapper.emitted('create')).toBeTruthy()
    expect(wrapper.emitted('select')).toBeFalsy()
  })

  it('ArrowUp from an unset index highlights the LAST item (create row)', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()

    expect(wrapper.emitted('create')).toBeTruthy()
  })

  it('reloads the list each time it is reopened', async () => {
    const wrapper = mountPicker({ open: true })
    await flushPromises()
    expect(mockCoalescedJson).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(mockCoalescedJson).toHaveBeenCalledTimes(2)
  })

  it('reflects a session that starts running after the list was loaded', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.findAll('.sp-row-running').length).toBe(0)

    // The snapshot was taken on open; live running state must still land.
    runningRef().value = new Set(['s-1'])
    runningVersionRef().value++
    await flushPromises()

    expect(wrapper.findAll('.sp-row-running').length).toBe(1)
  })
})
