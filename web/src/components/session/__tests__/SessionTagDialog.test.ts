import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import SessionTagDialog from '@/components/session/SessionTagDialog.vue'

const { mockStore, mockDialogHolder, mockGet, mockDelete, mockPatch } = vi.hoisted(() => ({
  mockStore: { state: { sessionListVersion: 0 } },
  mockDialogHolder: { confirm: null as null | ((m: string, o?: any) => Promise<boolean>) },
  mockGet: vi.fn(),
  mockDelete: vi.fn(),
  mockPatch: vi.fn(),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    // Include params so assertions can verify interpolation reached the key.
    useI18n: () => ({ t: (key: string, params?: any) => (params ? `${key}:${JSON.stringify(params)}` : key) }),
  }
})
vi.mock('@/stores/app', () => ({ store: mockStore }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))
vi.mock('@/utils/api', () => ({
  apiGet: mockGet,
  apiDelete: mockDelete,
  apiPatch: mockPatch,
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: (m: string, o?: any) => mockDialogHolder.confirm!(m, o),
  }),
}))
// ModalDialog teleports to body; stub it to a plain wrapper that forwards the
// default + footer slots so the dialog body is inspectable in-place.
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    props: ['open', 'title', 'zIndex'],
    template: '<div class="modal-stub" v-if="open"><slot /><slot name="footer" /></div>',
  },
}))

function tagFixtures() {
  return {
    tags: [
      { name: 'bug', scope: 'project', count: 2 },
      { name: 'shared', scope: 'global', count: 1 },
    ],
  }
}

describe('SessionTagDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockResolvedValue(tagFixtures())
    mockDelete.mockResolvedValue({ ok: true })
    mockPatch.mockResolvedValue({ ok: true })
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    mockStore.state.sessionListVersion = 0
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  async function mountDialog(props = {}) {
    return mount(SessionTagDialog, {
      props: { open: true, sessionId: 's1', initialTags: [], ...props },
    })
  }

  it('loads candidates on open', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    expect(mockGet).toHaveBeenCalledWith('/api/ai/session/tags')
    expect(wrapper.vm.candidates.length).toBe(2)
  })

  it('seeds selection from initialTags', async () => {
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()
    expect(wrapper.vm.isSelected('bug')).toBe(true)
    expect(wrapper.vm.isSelected('shared')).toBe(false)
  })

  it('toggles a candidate on and off', async () => {
    const wrapper = await mountDialog()
    await flushPromises()

    wrapper.vm.toggleTag('bug')
    expect(wrapper.vm.selected).toEqual(['bug'])
    wrapper.vm.toggleTag('bug')
    expect(wrapper.vm.selected).toEqual([])
  })

  it('toggles by clicking the chip itself, and reports pressed state', async () => {
    // The chip is the toggle now. It replaced a checkbox, which had supplied
    // keyboard access and the checked semantics for free — aria-pressed is what
    // carries that over, so it must track the selection both ways.
    const wrapper = await mountDialog()
    await flushPromises()

    const chip = wrapper.find('.st-chip')
    expect(chip.attributes('aria-pressed')).toBe('false')

    await chip.trigger('click')
    expect(wrapper.vm.selected).toEqual(['bug'])
    expect(wrapper.find('.st-chip').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('.st-chip').classes()).toContain('active')

    await wrapper.find('.st-chip').trigger('click')
    expect(wrapper.vm.selected).toEqual([])
    expect(wrapper.find('.st-chip').attributes('aria-pressed')).toBe('false')
  })

  it('keeps the delete button a sibling of the chip, never a child', async () => {
    // Nested <button> is invalid HTML and the parser would break the nesting,
    // so the two must be siblings inside the cell.
    const wrapper = await mountDialog()
    await flushPromises()

    const cell = wrapper.find('.st-candidate')
    expect(cell.find('.st-chip').exists()).toBe(true)
    expect(cell.find('.st-delete-btn').exists()).toBe(true)
    expect(cell.find('.st-chip .st-delete-btn').exists()).toBe(false)
  })

  it('marks a just-created tag as pending', async () => {
    mockGet.mockResolvedValue({ tags: [] })
    const wrapper = await mountDialog()
    await flushPromises()

    await wrapper.find('.st-input').setValue('fresh')
    await wrapper.find('.st-add-btn').trigger('click')
    await nextTick()

    // Dashed border is what tells the user it has not been saved yet — and why
    // deleting it skips the confirmation.
    expect(wrapper.find('.st-candidate').classes()).toContain('pending')
  })

  it('leaves an already-registered tag unmarked', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    for (const cell of wrapper.findAll('.st-candidate')) {
      expect(cell.classes()).not.toContain('pending')
    }
  })

  it('gives each chip its own readable label colour for the filled state', async () => {
    // Filling with the tag's accent means the label must be chosen per tag: a
    // fixed white scores as low as 1.67:1 on the pale palette entries. Both
    // theme variants are handed to CSS, which picks one via [data-theme-base].
    const wrapper = await mountDialog()
    await flushPromises()

    const styles = wrapper.findAll('.st-chip').map(c => c.attributes('style') || '')
    for (const style of styles) {
      expect(style).toContain('--st-chip-text-light')
      expect(style).toContain('--st-chip-text-dark')
      expect(style).toMatch(/--st-chip-text-light:\s*#(000000|ffffff)/)
    }
    // Distinct tags are not forced to the same label colour.
    expect(styles.length).toBe(2)
  })

  it('saves the selection as name+scope pairs', async () => {
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    wrapper.vm.toggleTag('shared')
    await wrapper.vm.save()

    expect(mockPatch).toHaveBeenCalledWith(
      '/api/ai/session/update?session_id=s1',
      { tags: [{ name: 'bug', scope: 'project' }, { name: 'shared', scope: 'global' }] }
    )
    expect(wrapper.emitted('saved')).toBeTruthy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('bumps sessionListVersion so other rows refresh', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    wrapper.vm.toggleTag('bug')
    await wrapper.vm.save()
    expect(mockStore.state.sessionListVersion).toBe(1)
  })

  it('does not save when the session id is missing', async () => {
    const wrapper = await mountDialog({ sessionId: '' })
    await flushPromises()
    wrapper.vm.toggleTag('bug')
    await wrapper.vm.save()
    expect(mockPatch).not.toHaveBeenCalled()
  })

  it('keeps the dialog open when saving fails', async () => {
    mockPatch.mockRejectedValue(new Error('boom'))
    const wrapper = await mountDialog()
    await flushPromises()
    wrapper.vm.toggleTag('bug')
    await wrapper.vm.save()
    // A failed save must not look successful: no close, no refresh.
    expect(wrapper.emitted('close')).toBeFalsy()
    expect(mockStore.state.sessionListVersion).toBe(0)
  })

  it('addNewTag creates a candidate and selects it', async () => {
    const wrapper = await mountDialog()
    await flushPromises()

    wrapper.vm.newTagName = '  fresh  '
    wrapper.vm.newTagScope = 'global'
    wrapper.vm.addNewTag()

    expect(wrapper.vm.newTagName).toBe('')
    expect(wrapper.vm.selected).toEqual(['fresh'])
    const created = wrapper.vm.candidates.find((c: any) => c.name === 'fresh')
    expect(created.scope).toBe('global')
  })

  it('addNewTag selects an existing candidate instead of duplicating it', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    const before = wrapper.vm.candidates.length

    wrapper.vm.newTagName = 'bug'
    wrapper.vm.addNewTag()

    expect(wrapper.vm.candidates.length).toBe(before)
    expect(wrapper.vm.selected).toEqual(['bug'])
  })

  it('addNewTag collapses internal whitespace like the backend', async () => {
    const wrapper = await mountDialog()
    await flushPromises()

    // The backend normalizes with strings.Fields + join, so "needs   review"
    // and "needs review" are the SAME tag. If the dialog kept the raw spacing,
    // the chip would silently change text after saving.
    wrapper.vm.newTagName = '  needs   review  '
    wrapper.vm.addNewTag()

    expect(wrapper.vm.selected).toEqual(['needs review'])
    expect(wrapper.vm.candidates.some((c: any) => c.name === 'needs review')).toBe(true)
  })

  it('addNewTag folds case like the backend', async () => {
    const wrapper = await mountDialog()
    await flushPromises()

    // The backend folds tag names to lowercase, so "Bug" IS "bug". Without the
    // same fold here the dialog would create a second, near-identical chip.
    wrapper.vm.newTagName = 'Bug'
    wrapper.vm.addNewTag()

    expect(wrapper.vm.selected).toEqual(['bug'])
    expect(wrapper.vm.candidates.some((c: any) => c.name === 'bug')).toBe(true)
    expect(wrapper.vm.candidates.some((c: any) => c.name === 'Bug')).toBe(false)
  })

  it('addNewTag does not duplicate an existing tag via a case variant', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    const before = wrapper.vm.candidates.length

    // 'bug' already exists as a candidate (fixture).
    wrapper.vm.newTagName = 'BUG'
    wrapper.vm.addNewTag()

    expect(wrapper.vm.candidates.length).toBe(before)
    expect(wrapper.vm.selected).toEqual(['bug'])
  })

  it('addNewTag ignores a blank name', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    wrapper.vm.newTagName = '   '
    wrapper.vm.addNewTag()
    expect(wrapper.vm.selected).toEqual([])
    expect(wrapper.vm.canAdd).toBe(false)
  })

  it('an existing tag keeps its own scope on save', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    // Picker set to global, but 'bug' already exists as a project tag — the
    // backend would refuse the re-scope anyway, so the payload must not ask.
    wrapper.vm.newTagScope = 'global'
    wrapper.vm.toggleTag('bug')
    await wrapper.vm.save()

    expect(mockPatch).toHaveBeenCalledWith(
      '/api/ai/session/update?session_id=s1',
      { tags: [{ name: 'bug', scope: 'project' }] }
    )
  })

  it('requestDelete confirms, deletes, and unselects the tag', async () => {
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.vm.requestDelete({ name: 'bug', scope: 'project' })
    await flushPromises()

    expect(mockDialogHolder.confirm).toHaveBeenCalled()
    expect(mockDelete).toHaveBeenCalledWith('/api/ai/session/tags?name=bug&scope=project')
    expect(wrapper.vm.candidates.some((c: any) => c.name === 'bug')).toBe(false)
    expect(wrapper.vm.selected).toEqual([])
    // Deleting affects every session carrying the label, not just this one.
    expect(mockStore.state.sessionListVersion).toBe(1)
  })

  it('requestDelete does nothing when the user cancels', async () => {
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(false)
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.vm.requestDelete({ name: 'bug', scope: 'project' })

    expect(mockDelete).not.toHaveBeenCalled()
    expect(wrapper.vm.selected).toEqual(['bug'])
  })

  it('URL-encodes the tag name on delete', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    await wrapper.vm.requestDelete({ name: 'needs review', scope: 'project' })
    await flushPromises()
    expect(mockDelete).toHaveBeenCalledWith('/api/ai/session/tags?name=needs%20review&scope=project')
  })

  it('keeps the tag when deletion fails', async () => {
    mockDelete.mockRejectedValue(new Error('nope'))
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.vm.requestDelete({ name: 'bug', scope: 'project' })
    await flushPromises()

    expect(wrapper.vm.candidates.some((c: any) => c.name === 'bug')).toBe(true)
    expect(mockStore.state.sessionListVersion).toBe(0)
  })

  it('resets transient input each time it reopens', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    wrapper.vm.newTagName = 'leftover'
    wrapper.vm.newTagScope = 'global'

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(wrapper.vm.newTagName).toBe('')
    expect(wrapper.vm.newTagScope).toBe('project')
  })

  it('re-seeds the selection from initialTags on reopen', async () => {
    const wrapper = await mountDialog({ initialTags: [] })
    await flushPromises()
    wrapper.vm.toggleTag('bug')

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ initialTags: ['shared'], open: true })
    await flushPromises()

    // Stale selection from the previous session must not leak into the new one.
    expect(wrapper.vm.selected).toEqual(['shared'])
  })

  it('marks global tags with a globe icon instead of a text badge', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    await nextTick()

    // A text badge would have to fit inside a 120px grid cell alongside the
    // name and the delete button; an icon costs no width. The accessible name
    // still has to say "global" — an unlabelled icon reads as nothing.
    const marks = wrapper.findAll('.st-chip-scope')
    expect(marks.length).toBe(1)
    expect(marks[0].attributes('aria-label')).toBe('sessionTags.scopeGlobal')
    // role="img" is what makes the label announced at all — a bare <svg> is
    // exposed as a generic graphic and drops its aria-label.
    expect(marks[0].attributes('role')).toBe('img')
    expect(wrapper.findAll('.st-candidate').length).toBe(2)
  })

  it('shows the global hint only when the picker is global', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    expect(wrapper.find('.st-scope-hint').exists()).toBe(false)

    wrapper.vm.newTagScope = 'global'
    await nextTick()
    expect(wrapper.find('.st-scope-hint').exists()).toBe(true)
  })

  it('surfaces a load failure without crashing', async () => {
    mockGet.mockRejectedValue(new Error('offline'))
    const wrapper = await mountDialog()
    await flushPromises()
    expect(wrapper.vm.candidates).toEqual([])
    expect(wrapper.vm.loading).toBe(false)
  })
  // The global reset zeroes padding/margin on every element (web/css/base.css),
  // so a control that declares only layout (flex/min-width) renders as bare UA
  // chrome — no border, no background, no hit area. That is exactly how this
  // dialog shipped once: the input read as plain text and the create button as
  // a text link, while the chips above (which do declare a surface) looked
  // right. jsdom does not cascade CSS, so assert on the declared source rules.
  describe('form controls declare their own surface', () => {
    const src = readFileSync(resolve(process.cwd(), 'src/components/session/SessionTagDialog.vue'), 'utf8')
    const ruleFor = (selector: string) => {
      // Match ".selector { ... }" and take the declaration block.
      const m = new RegExp(`\\${selector}\\s*\\{([^}]*)\\}`).exec(src)
      return m ? m[1] : ''
    }

    it.each([
      ['.st-input', 'the text field'],
      ['.st-scope-select', 'the scope dropdown'],
    ])('%s (%s) declares border, background and padding', (selector) => {
      const rule = ruleFor(selector)
      expect(rule, `${selector} rule must exist`).not.toBe('')
      expect(rule).toMatch(/border:/)
      expect(rule).toMatch(/background:/)
      expect(rule).toMatch(/padding:/)
    })

    it('.st-add-btn declares a button surface, not just layout', () => {
      const rule = ruleFor('.st-add-btn')
      expect(rule).not.toBe('')
      // A <button> with no border declaration keeps the UA border, which reads
      // as an unstyled element next to the bordered input.
      expect(rule).toMatch(/border:/)
      expect(rule).toMatch(/background:/)
      expect(rule).toMatch(/padding:/)
    })

    it('.st-chip declares a button surface, not just layout', () => {
      const rule = ruleFor('.st-chip')
      expect(rule).not.toBe('')
      // A <button> with no border declaration keeps the UA border, which reads
      // as an unstyled element next to the input.
      expect(rule).toMatch(/border:/)
      expect(rule).toMatch(/background:/)
      expect(rule).toMatch(/padding:/)
    })

    it('.session-tags-dialog insets its content from the card edge', () => {
      // The shared .modal-body is deliberately padding-free (modal-card.css) —
      // it only supplies the scroll container. A dialog that does not inset its
      // own root renders flush against the card border, which is how this one
      // shipped once.
      const rule = ruleFor('.session-tags-dialog')
      expect(rule).not.toBe('')
      expect(rule).toMatch(/padding:/)
    })
  })

  describe('candidate grid', () => {
    const src = readFileSync(resolve(process.cwd(), 'src/components/session/SessionTagDialog.vue'), 'utf8')
    const decls = src.replace(/\/\*[\s\S]*?\*\//g, '')
    const ruleFor = (selector: string) => {
      const m = new RegExp(`\\${selector}\\s*\\{([^}]*)\\}`).exec(decls)
      return m ? m[1] : ''
    }

    it('flows the candidates at their natural width', () => {
      // Several tags per row, each only as wide as its own label. An even-width
      // grid (minmax(120px, 1fr)) was tried and rejected: it stretched short
      // tags like "bug" out to a fixed column, so every chip was mostly empty
      // padding. The filter bar sizes chips to content too.
      const list = ruleFor('.st-candidate-list')
      expect(list).not.toBe('')
      expect(list).toMatch(/display:\s*flex/)
      expect(list).toMatch(/flex-wrap:\s*wrap/)
      expect(list).not.toMatch(/display:\s*grid/)

      // The cell must shrink-wrap the chip rather than claim a column width, and
      // the chip must not grow to fill one.
      const cell = ruleFor('.st-candidate')
      expect(cell).toMatch(/display:\s*inline-flex/)
      const chip = ruleFor('.st-chip')
      expect(chip).toMatch(/display:\s*inline-flex/)
      expect(chip).not.toMatch(/flex:\s*1\b/)
      expect(chip).not.toMatch(/flex-grow:\s*[1-9]/)
    })

    it('still caps a very long name at the row width', () => {
      // Content sizing must not let a long name push the chip past the dialog.
      // The cell caps at 100% and the name span ellipsises inside it.
      const cell = ruleFor('.st-candidate')
      expect(cell).toMatch(/max-width:\s*100%/)
      const name = ruleFor('.st-chip-name')
      expect(name).toMatch(/text-overflow:\s*ellipsis/)
      expect(name).toMatch(/overflow:\s*hidden/)
    })

    it('gives the cell no surface of its own', () => {
      // The old row tint (selected background + border) was removed: with one
      // tag per cell a tinted row is a block whose shape fights the pill inside
      // it, and the filled chip already states the selection.
      const rule = ruleFor('.st-candidate')
      expect(rule).not.toBe('')
      expect(rule).toMatch(/position:\s*relative/)
      expect(rule).not.toMatch(/background/)
      expect(rule).not.toMatch(/border/)
    })

    it('fills the selected chip with the tag accent', () => {
      // Same treatment as the filter bar's active chip, so one tag looks the
      // same in both places. The label colour must come from the per-tag
      // readable value rather than a fixed white (1.67:1 on pale accents).
      const rule = ruleFor('.st-chip.active')
      expect(rule).not.toBe('')
      expect(rule).toMatch(/background:\s*var\(--tag-accent\)/)
      expect(rule).toMatch(/color:\s*var\(--st-chip-text-light\)/)
      const darkRule = /\[data-theme-base="dark"\]\s*\.st-chip\.active\s*\{([^}]*)\}/.exec(decls)
      expect(darkRule, 'dark themes must use their own label colour').not.toBeNull()
      expect(darkRule![1]).toMatch(/color:\s*var\(--st-chip-text-dark\)/)
    })

    it('overlays the delete button instead of nesting it in the chip', () => {
      // A <button> inside a <button> is invalid HTML; the parser would break the
      // nesting. So it is a sibling, positioned over the chip's right edge.
      const rule = ruleFor('.st-delete-btn')
      expect(rule).not.toBe('')
      expect(rule).toMatch(/position:\s*absolute/)
      expect(rule).toMatch(/right:/)
    })

    it('keeps the delete button permanently visible', () => {
      // It must not be hidden behind a hover reveal: a hover-only control is
      // undiscoverable, and touch devices have no hover at all.
      const rule = ruleFor('.st-delete-btn')
      expect(rule).not.toMatch(/opacity:\s*0/)
      expect(rule).not.toMatch(/display:\s*none/)
      expect(rule).not.toMatch(/visibility:\s*hidden/)
      // No rule anywhere may hide it pending hover either.
      expect(decls).not.toMatch(/\.st-candidate:hover\s+\.st-delete-btn[^{]*\{[^}]*opacity/)
    })

    it('reserves room in the chip for the overlaid button', () => {
      // The button is permanently visible and overlays the chip's right edge, so
      // the chip must reserve at least that much padding or a long label runs
      // underneath the icon. Button = right:3px + width:18px => 21px.
      const chip = ruleFor('.st-chip')
      const btn = ruleFor('.st-delete-btn')
      // padding is the 4-value shorthand (top right bottom left); take the right.
      const shorthand = /padding:\s*([^;]+);/.exec(chip)?.[1]?.trim()
      expect(shorthand, 'the chip must declare padding').toBeTruthy()
      const right = shorthand!.split(/\s+(?![^(]*\))/)[1]
      // Accept a bare token or calc(token + Npx) — the chip needs a little slack
      // over the token value, since absolute offsets are measured from the
      // padding box and --space-8 (20px) alone is a pixel short of 21px.
      const m = /var\(--space-(\d+)\)(?:\s*\+\s*(\d+)px)?/.exec(right)
      expect(m, `right padding should be a space token, got "${right}"`).not.toBeNull()
      const SPACE_PX: Record<string, number> = { '6': 12, '7': 16, '8': 20 }
      const base = SPACE_PX[m![1]]
      expect(base, `unmapped --space-${m![1]}`).toBeDefined()
      const padRightPx = base + Number(m![2] || 0)
      const inset = Number(/right:\s*(\d+)px/.exec(btn)?.[1])
      const width = Number(/width:\s*(\d+)px/.exec(btn)?.[1])
      expect(inset + width).toBeLessThanOrEqual(padRightPx)
    })
  })

  describe('pending tag', () => {
    const src = readFileSync(resolve(process.cwd(), 'src/components/session/SessionTagDialog.vue'), 'utf8')
    const decls = src.replace(/\/\*[\s\S]*?\*\//g, '')

    it('marks a not-yet-saved tag with a dashed border', () => {
      // A tag typed in this dialog has no server-side definition yet, and
      // deleting it skips the confirmation for that reason. Dashed states that
      // without costing width or introducing another colour.
      const m = /\.st-candidate\.pending\s+\.st-chip\s*\{([^}]*)\}/.exec(decls)
      expect(m, 'the pending rule must exist').not.toBeNull()
      expect(m![1]).toMatch(/border-style:\s*dashed/)
    })
  })

  // Regression: the 创建 button was the only way to commit a typed name, so
  // typing a name and pressing 确定 saved nothing — the field was cleared and
  // the tag silently vanished, with no error shown.
  it('saves a name typed in the input even when 创建 was never pressed', async () => {
    mockGet.mockResolvedValue({ tags: [] })
    mockPatch.mockResolvedValue({})
    const wrapper = await mountDialog()
    await flushPromises()

    await wrapper.find('.st-input').setValue('  Needs   Review  ')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(mockPatch).toHaveBeenCalledTimes(1)
    // Normalized the same way the backend does (collapse + lowercase).
    expect(mockPatch.mock.calls[0][1]).toEqual({
      tags: [{ name: 'needs review', scope: 'project' }],
    })
  })

  it('keeps checked tags AND the pending input name when saving', async () => {
    mockGet.mockResolvedValue({ tags: [{ name: 'bug', scope: 'project', count: 1 }] })
    mockPatch.mockResolvedValue({})
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.find('.st-input').setValue('urgent')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    // The pending name must be additive, not a replacement for the selection.
    expect(mockPatch.mock.calls[0][1]).toEqual({
      tags: [
        { name: 'bug', scope: 'project' },
        { name: 'urgent', scope: 'project' },
      ],
    })
  })

  it('does not duplicate the pending name when it is already selected', async () => {
    mockGet.mockResolvedValue({ tags: [] })
    mockPatch.mockResolvedValue({})
    const wrapper = await mountDialog()
    await flushPromises()

    await wrapper.find('.st-input').setValue('bug')
    await wrapper.find('.st-add-btn').trigger('click')  // commit → now selected
    await wrapper.find('.st-input').setValue('bug')     // retype the same name
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(mockPatch.mock.calls[0][1]).toEqual({
      tags: [{ name: 'bug', scope: 'project' }],
    })
  })

  it('sends an empty tag list when nothing is selected and the input is blank', async () => {
    mockGet.mockResolvedValue({ tags: [{ name: 'bug', scope: 'project', count: 1 }] })
    mockPatch.mockResolvedValue({})
    const wrapper = await mountDialog({ initialTags: [] })
    await flushPromises()

    await wrapper.find('.st-input').setValue('   ')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    // Whitespace-only input is not a tag; clearing the selection still works.
    expect(mockPatch.mock.calls[0][1]).toEqual({ tags: [] })
  })

  // Regression: a tag created in the dialog has no server-side definition until
  // the session is saved, so deleting it issued a DELETE that always failed and
  // the catch left the row in place — the trash button looked dead.
  it('removing a just-created tag drops it locally without calling the server', async () => {
    mockGet.mockResolvedValue({ tags: [] })
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    const wrapper = await mountDialog()
    await flushPromises()

    await wrapper.find('.st-input').setValue('brandnew')
    await wrapper.find('.st-add-btn').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.st-candidate').length).toBe(1)

    await wrapper.find('.st-delete-btn').trigger('click')
    await flushPromises()

    // Nothing to delete server-side; the request would 400.
    expect(mockDelete).not.toHaveBeenCalled()
    // And no confirmation — undoing a local creation affects nothing else.
    expect(mockDialogHolder.confirm).not.toHaveBeenCalled()
    expect(wrapper.findAll('.st-candidate').length).toBe(0)
    expect(wrapper.vm.selected).toEqual([])
  })

  it('removing a just-created tag also clears it from the pending save payload', async () => {
    mockGet.mockResolvedValue({ tags: [] })
    mockPatch.mockResolvedValue({})
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    const wrapper = await mountDialog()
    await flushPromises()

    await wrapper.find('.st-input').setValue('brandnew')
    await wrapper.find('.st-add-btn').trigger('click')
    await flushPromises()
    await wrapper.find('.st-delete-btn').trigger('click')
    await flushPromises()

    // No server round-trip: the tag never existed outside this dialog.
    expect(mockDelete).not.toHaveBeenCalled()

    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()
    // The discarded tag must not sneak back in via the save payload.
    expect(mockPatch.mock.calls[0][1]).toEqual({ tags: [] })
  })

  it('still deletes an existing tag through the server, after confirming', async () => {
    mockGet.mockResolvedValue({ tags: [{ name: 'bug', scope: 'project', count: 2 }] })
    mockDelete.mockResolvedValue({ ok: true })
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.find('.st-delete-btn').trigger('click')
    await flushPromises()

    // An existing definition is shared, so it keeps the confirm + server delete.
    expect(mockDialogHolder.confirm).toHaveBeenCalled()
    expect(mockDelete).toHaveBeenCalledTimes(1)
    expect(mockDelete.mock.calls[0][0]).toContain('name=bug')
    expect(wrapper.findAll('.st-candidate').length).toBe(0)
  })

  it('keeps an existing tag when the delete confirmation is declined', async () => {
    mockGet.mockResolvedValue({ tags: [{ name: 'bug', scope: 'project', count: 2 }] })
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(false)
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.find('.st-delete-btn').trigger('click')
    await flushPromises()

    expect(mockDelete).not.toHaveBeenCalled()
    expect(wrapper.findAll('.st-candidate').length).toBe(1)
  })

  it('re-adding a name after removing the pending tag still works', async () => {
    mockGet.mockResolvedValue({ tags: [] })
    mockPatch.mockResolvedValue({})
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    const wrapper = await mountDialog()
    await flushPromises()

    await wrapper.find('.st-input').setValue('brandnew')
    await wrapper.find('.st-add-btn').trigger('click')
    await flushPromises()
    await wrapper.find('.st-delete-btn').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.st-candidate').length).toBe(0)

    // Creating it again must behave like a fresh creation, not a no-op.
    await wrapper.find('.st-input').setValue('brandnew')
    await wrapper.find('.st-add-btn').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.st-candidate').length).toBe(1)

    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()
    expect(mockPatch.mock.calls[0][1]).toEqual({
      tags: [{ name: 'brandnew', scope: 'project' }],
    })
  })

  // Every catch here used to only appLog.e, so a failed save left the dialog
  // open and unchanged with no explanation — indistinguishable from the click
  // doing nothing. The row is what makes the failure visible and retryable.
  describe('failures are visible to the user', () => {
    it('shows an error row when tags fail to load', async () => {
      mockGet.mockRejectedValue(new Error('offline'))
      const wrapper = await mountDialog()
      await flushPromises()

      expect(wrapper.find('.st-error').exists()).toBe(true)
      expect(wrapper.find('.st-error').text()).toBe('sessionTags.loadFailed')
    })

    it('shows an error row when saving fails, and keeps the dialog open', async () => {
      mockGet.mockResolvedValue({ tags: [] })
      mockPatch.mockRejectedValue(new Error('boom'))
      const wrapper = await mountDialog()
      await flushPromises()

      await wrapper.find('.st-input').setValue('urgent')
      await wrapper.find('.fbtn-primary').trigger('click')
      await flushPromises()

      expect(wrapper.find('.st-error').text()).toBe('sessionTags.saveFailed')
      // Still open, edits intact, so the user can retry.
      expect(wrapper.find('.modal-stub').exists()).toBe(true)
      expect(wrapper.emitted('close')).toBeUndefined()
      expect(wrapper.vm.saving).toBe(false)
    })

    it('clears the error row and closes once a retry succeeds', async () => {
      mockGet.mockResolvedValue({ tags: [] })
      const wrapper = await mountDialog()
      await flushPromises()

      mockPatch.mockRejectedValueOnce(new Error('boom'))
      await wrapper.find('.st-input').setValue('urgent')
      await wrapper.find('.fbtn-primary').trigger('click')
      await flushPromises()
      expect(wrapper.find('.st-error').exists()).toBe(true)

      mockPatch.mockResolvedValue({})
      await wrapper.find('.fbtn-primary').trigger('click')
      await flushPromises()

      expect(wrapper.find('.st-error').exists()).toBe(false)
      expect(wrapper.emitted('close')).toBeTruthy()
    })

    it('shows an error row when deleting an existing tag fails', async () => {
      mockGet.mockResolvedValue({ tags: [{ name: 'bug', scope: 'project', count: 2 }] })
      mockDelete.mockRejectedValue(new Error('boom'))
      mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
      const wrapper = await mountDialog({ initialTags: ['bug'] })
      await flushPromises()

      await wrapper.find('.st-delete-btn').trigger('click')
      await flushPromises()

      expect(wrapper.find('.st-error').text()).toBe('sessionTags.deleteFailed')
      // The row stays: the definition was not actually removed.
      expect(wrapper.findAll('.st-candidate').length).toBe(1)
    })

    it('does not leave a stale error row after a later success', async () => {
      mockGet.mockRejectedValueOnce(new Error('offline'))
      const wrapper = await mountDialog()
      await flushPromises()
      expect(wrapper.find('.st-error').exists()).toBe(true)

      // Reopening reloads; the previous failure must not linger.
      await wrapper.setProps({ open: false })
      mockGet.mockResolvedValue({ tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      await wrapper.setProps({ open: true })
      await flushPromises()

      expect(wrapper.find('.st-error').exists()).toBe(false)
    })
  })
})
