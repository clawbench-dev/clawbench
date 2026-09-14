import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskEventCard from '../TaskEventCard.vue'
import { resetForgeBindingState } from '@/composables/useForgeBinding'

// ── Mocks ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// forgeEventLabels resolves through the app i18n singleton (i18n.global.t), so
// a bare vue-i18n mock leaves createI18n undefined and importing @/i18n throws.
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  },
}))

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({
    name,
    props: { size: Number },
    template: '<svg />',
  })
  return {
    Zap: stub('Zap'),
    Braces: stub('Braces'),
    AlertTriangle: stub('AlertTriangle'),
  }
})

vi.mock('@/utils/format', () => ({
  formatDateTime: (t: string) => `time:${t}`,
  formatDateTimeWithYear: (t: string) => `year:${t}`,
}))

const { mockFetchBinding } = vi.hoisted(() => ({ mockFetchBinding: vi.fn() }))
vi.mock('@/utils/forgeApi', () => ({
  fetchForgeBinding: mockFetchBinding,
}))

const eventTask = {
  id: 2,
  name: 'On new PR',
  agentId: 'acp',
  status: 'active',
  triggerMode: 'event',
  eventTypes: 'pr.opened',
  runCount: 0,
  unreadCount: 0,
  prompt: 'Review the PR.',
}

function mountCard(overrides: Record<string, unknown> = {}) {
  return mount(TaskEventCard, { props: { task: { ...eventTask, ...overrides } } })
}

describe('TaskEventCard manual-run explanation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // The binding singleton (and its TTL cache) survives between tests; reset it
    // so each case performs its own lookup, as a real project switch would.
    resetForgeBindingState()
    mockFetchBinding.mockResolvedValue({ binding: null })
  })

  // The detail page hides the Run button on an event task. Without a note the
  // button is simply missing and the user has no way to learn why.
  it('explains why an event task cannot be run manually', () => {
    expect(mountCard().text()).toContain('task.overview.eventNoManualRun')
  })

  // The note is a property of event tasks, not of their enabled/disabled state:
  // it must survive on a paused task, where the paused warning is shown too.
  it('keeps the explanation on a paused event task alongside the paused warning', () => {
    const wrapper = mountCard({ status: 'paused' })
    expect(wrapper.find('.event-paused-note').exists()).toBe(true)
    expect(wrapper.text()).toContain('task.overview.eventNoManualRun')
  })
})

// The card resolves the project's bound repository for the "watched repository"
// row. It must show the real binding, and must not claim the project is unbound
// before the lookup has answered.
describe('TaskEventCard watched repository', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetForgeBindingState()
  })

  it('shows the project-bound repository', async () => {
    mockFetchBinding.mockResolvedValue({
      binding: { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets' },
    })
    const wrapper = mountCard()
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('acme/widgets')
    })
  })

  it('shows the unbound label when the project has no binding', async () => {
    mockFetchBinding.mockResolvedValue({ binding: null })
    const wrapper = mountCard()
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('task.form.eventRepoUnbound')
    })
  })

  // Regression: the row read `boundRepoLabel || t('eventRepoUnbound')`, and the
  // label started at '', so an in-flight lookup claimed the project was unbound.
  it('does not claim the project is unbound while the lookup is in flight', async () => {
    let release: (v: unknown) => void = () => {}
    mockFetchBinding.mockReturnValue(new Promise(resolve => { release = resolve }))

    const wrapper = mountCard()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).not.toContain('task.form.eventRepoUnbound')
    expect(wrapper.text()).toContain('common.loading')

    release({ binding: { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets' } })
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('acme/widgets')
    })
    expect(wrapper.text()).not.toContain('task.form.eventRepoUnbound')
  })
})

describe('TaskEventCard event-context sample rows', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetForgeBindingState()
    mockFetchBinding.mockResolvedValue({ binding: null })
  })

  // A pipeline subscription carries the "repo" pseudo-kind, which is neither
  // "issue" nor "pr". sampleKind used to fall through to 'pr', so every pipeline
  // task advertised a "pr #123" sample — a payload it can never receive.
  it('does not show a pr item sample for a pipeline-only subscription', () => {
    const text = mountCard({ eventTypes: 'pipeline_done' }).text()
    expect(text).not.toContain('pr #123')
    // And the fabricated issue/PR link must be gone too.
    expect(text).not.toContain('/pull/123')
    expect(text).not.toContain('/issues/123')
    // The pipeline-specific rows are still there.
    expect(text).toContain('task.form.varPipelineStatus')
  })

  it('still shows the issue sample for an issue-only subscription', () => {
    const text = mountCard({ eventTypes: 'issue.opened' }).text()
    expect(text).toContain('issue #123')
    expect(text).toContain('/issues/123')
  })

  it('still shows the pr sample for a pr-only subscription', () => {
    const text = mountCard({ eventTypes: 'pr.opened' }).text()
    expect(text).toContain('pr #123')
    expect(text).toContain('/pull/123')
  })

  // A mixed subscription does have a real item, so it keeps the item sample.
  it('shows the item sample when a pipeline is combined with a PR', () => {
    const text = mountCard({ eventTypes: 'pr.opened,pipeline_done' }).text()
    expect(text).toContain('pr #123')
    expect(text).toContain('task.form.varPipelineStatus')
  })
})
