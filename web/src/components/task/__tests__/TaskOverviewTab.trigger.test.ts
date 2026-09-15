import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskOverviewTab from '../TaskOverviewTab.vue'
import { resetForgeBindingState } from '@/composables/useForgeBinding'

// ── Mocks ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// The event-label helper resolves through the app i18n instance.
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
    template: `<svg :data-icon="${name}" />`,
  })
  return {
    ChevronDown: stub('ChevronDown'),
    Clock: stub('Clock'),
    CalendarClock: stub('CalendarClock'),
    MessageSquare: stub('MessageSquare'),
    Zap: stub('Zap'),
    Braces: stub('Braces'),
    AlertTriangle: stub('AlertTriangle'),
  }
})

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdown: (md: string) => ({ html: `<p>${md}</p>`, detectedPaths: [], detectedSHAs: [] }),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgentBackend: () => 'acp', getAgentName: () => 'test-agent' }),
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', template: '<span class="agent-icon-stub" />' },
}))

vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({
    verifyFilePaths: vi.fn(),
    openFilePath: vi.fn(),
    readLineTargetFromEl: () => ({ filePath: null }),
  }),
}))

vi.mock('@/composables/useCommitHashAnnotation', () => ({
  verifyCommitHashes: vi.fn(),
}))

vi.mock('@/composables/useLocalhostAnnotation', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: () => false }),
}))

vi.mock('@/composables/useCodeBlockHeader', () => ({
  handleCodeBlockClick: () => false,
  handleTableBlockClick: () => false,
}))

vi.mock('@/composables/useCodeLinkPreview', () => ({
  useCodeLinkPreview: () => ({
    enabled: { value: false },
    isTouchDevice: () => false,
    handleClick: vi.fn(),
    close: vi.fn(),
  }),
  handleVerifiedFilePathClick: () => false,
}))

vi.mock('@/components/file/CodeLinkPreview.vue', () => ({
  default: { name: 'CodeLinkPreview', template: '<div class="code-link-preview-stub" />' },
}))

vi.mock('@/stores/app', () => ({
  store: {
    state: { projectRoot: '/home/user/project', homeDir: '/home/user' },
    selectFile: vi.fn(),
    navigateToDir: vi.fn(),
  },
}))

vi.mock('@/utils/format', () => ({
  humanizeCron: (cron: string) => `cron:${cron}`,
  repeatLabel: () => 'repeat',
  formatDateTime: (t: string) => `time:${t}`,
  formatDateTimeWithYear: (t: string) => `year:${t}`,
}))

// Binding lookup feeds the "watched repository" row; mock it so the tests are
// deterministic and do not touch the network.
const { mockFetchBinding } = vi.hoisted(() => ({ mockFetchBinding: vi.fn() }))
vi.mock('@/utils/forgeApi', () => ({
  fetchForgeBinding: mockFetchBinding,
}))

const cronTask = {
  id: 1,
  name: 'Daily backup',
  agentId: 'acp',
  status: 'active',
  triggerMode: 'cron',
  cronExpr: '0 2 * * *',
  repeatMode: 'unlimited',
  maxRuns: 0,
  runCount: 3,
  runningCount: 0,
  unreadCount: 0,
  nextRunAt: '2026-08-23T02:00:00Z',
  prompt: 'Backup the database every day.',
}

const eventTask = {
  id: 2,
  name: 'On new PR',
  agentId: 'acp',
  status: 'active',
  triggerMode: 'event',
  cronExpr: '',
  eventTypes: 'pr.merged,issue.opened',
  repeatMode: 'unlimited',
  maxRuns: 0,
  runCount: 5,
  runningCount: 0,
  unreadCount: 0,
  prompt: 'Review the merged PR.',
}

describe('TaskOverviewTab trigger card branching', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // The binding singleton (and its TTL cache) survives between tests; reset it
    // so each case performs its own lookup, as a real project switch would.
    resetForgeBindingState()
    mockFetchBinding.mockResolvedValue({ binding: null })
  })

  it('renders the schedule card for a cron task', () => {
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...cronTask } } })
    // The cron expression is the tell-tale of the schedule card.
    expect(wrapper.text()).toContain('cron:0 2 * * *')
    // The event card's repository row must not appear.
    expect(wrapper.find('.event-chips').exists()).toBe(false)
  })

  it('renders the event card for an event task, not the schedule card', () => {
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...eventTask } } })
    expect(wrapper.find('.event-chips').exists()).toBe(true)
    // No cron expression is stored for an event task, so the blank cron line
    // that the old single-card layout produced must be gone.
    expect(wrapper.text()).not.toContain('cron:')
  })

  it('lists each subscribed event under its item kind', () => {
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...eventTask } } })
    const chips = wrapper.findAll('.event-chip').map(c => c.text())
    // Groups appear in first-seen order, so pr.merged precedes issue.opened here.
    expect(chips.sort()).toEqual(['task.form.eventMerged', 'task.form.eventOpened'])
    // Issue and PR chips carry distinct kind classes so they can be tinted apart.
    expect(wrapper.find('.event-chip.kind-issue').exists()).toBe(true)
    expect(wrapper.find('.event-chip.kind-pr').exists()).toBe(true)
  })

  it('shows the injected event context with sample values, not raw placeholders', () => {
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...eventTask } } })
    const preview = wrapper.find('.event-context-preview')
    expect(preview.exists()).toBe(true)
    // Sample title is an illustrative value; the literal {{TITLE}} placeholder
    // must never be shown here.
    expect(preview.text()).toContain('task.overview.eventSampleTitle')
    expect(preview.text()).not.toContain('{{TITLE}}')
  })

  it('omits the comment row when no comment event is subscribed', () => {
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...eventTask } } })
    expect(wrapper.find('.event-context-preview').text()).not.toContain('task.form.varCommentBody')
  })

  it('includes the comment row when a comment event is subscribed', () => {
    const task = { ...eventTask, eventTypes: 'issue.commented' }
    const wrapper = mount(TaskOverviewTab, { props: { task } })
    expect(wrapper.find('.event-context-preview').text()).toContain('task.form.varCommentBody')
  })

  it('samples an issue item when only issues are subscribed', () => {
    const task = { ...eventTask, eventTypes: 'issue.opened' }
    const wrapper = mount(TaskOverviewTab, { props: { task } })
    const preview = wrapper.find('.event-context-preview').text()
    // The sample must match the subscribed kind, not always assume a PR.
    expect(preview).toContain('issue #123')
    expect(preview).toContain('/issues/123')
    expect(preview).not.toContain('pr #123')
  })

  it('samples a PR item when PRs are subscribed', () => {
    const task = { ...eventTask, eventTypes: 'pr.merged' }
    const wrapper = mount(TaskOverviewTab, { props: { task } })
    const preview = wrapper.find('.event-context-preview').text()
    expect(preview).toContain('pr #123')
    expect(preview).toContain('/pull/123')
  })

  it('never leaks the unbound label into the sample URL', async () => {
    mockFetchBinding.mockResolvedValue({ binding: null })
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...eventTask } } })
    await vi.waitFor(() => {
      expect(wrapper.find('.event-context-preview').text()).toContain('owner/repo')
    })
    // The URL row must use the placeholder repo, not the translated prose label.
    expect(wrapper.find('.event-context-preview').text()).not.toContain('task.form.eventRepoUnbound/pull')
  })

  it('warns when a paused event task will never fire', () => {
    const task = { ...eventTask, status: 'paused' }
    const wrapper = mount(TaskOverviewTab, { props: { task } })
    expect(wrapper.find('.event-paused-note').exists()).toBe(true)
  })

  it('does not warn for an active event task', () => {
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...eventTask } } })
    expect(wrapper.find('.event-paused-note').exists()).toBe(false)
  })

  // Every event task watches its project's binding, so the card resolves it.
  it('shows the project-bound repository', async () => {
    mockFetchBinding.mockResolvedValue({
      binding: { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' },
    })
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...eventTask } } })
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('acme/widgets')
    })
  })

  // An unbound project can host an event task but it will never fire; the card
  // states that rather than showing a fabricated repository.
  it('shows the unbound label when the project has no binding', async () => {
    mockFetchBinding.mockResolvedValue({ binding: null })
    const wrapper = mount(TaskOverviewTab, { props: { task: { ...eventTask } } })
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('task.form.eventRepoUnbound')
    })
  })
})
