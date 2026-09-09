import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskOverviewTab from '../TaskOverviewTab.vue'

// ── Mocks ──
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
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
    MessageSquare: stub('MessageSquare'),
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

const { mockOpenFilePath } = vi.hoisted(() => ({ mockOpenFilePath: vi.fn() }))

vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({ verifyFilePaths: vi.fn(), openFilePath: mockOpenFilePath }),
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

const baseTask = {
  id: 1,
  name: 'Daily backup',
  agentId: 'acp',
  status: 'active',
  cronExpr: '0 2 * * *',
  repeatMode: 'unlimited',
  maxRuns: 0,
  runCount: 3,
  runningCount: 0,
  unreadCount: 0,
  nextRunAt: '2026-08-23T02:00:00Z',
  prompt: 'Backup the database every day.',
}

describe('TaskOverviewTab prompt collapse', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // jsdom does not implement offsetParent/layout, so isVisible() is unreliable.
  // Assert the v-show inline style directly instead.
  const bodyStyle = (wrapper: ReturnType<typeof mount>) =>
    wrapper.find('.prompt-body').attributes('style') || ''

  it('renders prompt collapsed by default', () => {
    const wrapper = mount(TaskOverviewTab, {
      props: { task: { ...baseTask } },
    })
    expect(wrapper.find('.prompt-body').exists()).toBe(true)
    expect(bodyStyle(wrapper)).toContain('display: none')
    expect(wrapper.find('.prompt-chevron-collapsed').exists()).toBe(true)
  })

  it('expands prompt body when title is clicked', async () => {
    const wrapper = mount(TaskOverviewTab, {
      props: { task: { ...baseTask } },
    })
    await wrapper.find('.prompt-card-title').trigger('click')
    expect(bodyStyle(wrapper)).not.toContain('display: none')
    expect(wrapper.find('.prompt-chevron-collapsed').exists()).toBe(false)
  })

  it('toggles prompt collapse on subsequent clicks', async () => {
    const wrapper = mount(TaskOverviewTab, {
      props: { task: { ...baseTask } },
    })
    const title = wrapper.find('.prompt-card-title')
    await title.trigger('click') // expand
    expect(bodyStyle(wrapper)).not.toContain('display: none')
    await title.trigger('click') // collapse
    expect(bodyStyle(wrapper)).toContain('display: none')
  })

  it('no longer renders the action bar (moved to TaskDetailPage bottom bar)', () => {
    const wrapper = mount(TaskOverviewTab, {
      props: { task: { ...baseTask, runCount: 5 } },
    })
    // The action bar (with the old "history" button) was moved out of the overview
    expect(wrapper.find('.overview-actions').exists()).toBe(false)
    expect(wrapper.find('.action-btn').exists()).toBe(false)
  })

  it('calls openFilePath when chat-file-open-btn is clicked', async () => {
    const wrapper = mount(TaskOverviewTab, {
      props: {
        task: {
          ...baseTask,
          prompt: '<button class="chat-file-open-btn" data-file-path="src/main.ts" data-line-start="10" data-line-end="20">Open</button>',
        },
      },
    })
    const btn = wrapper.find('.chat-file-open-btn')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(mockOpenFilePath).toHaveBeenCalledWith('src/main.ts', 10, 20, 'task')
  })

  it('calls openFilePath when dir chat-file-path is clicked', async () => {
    const wrapper = mount(TaskOverviewTab, {
      props: {
        task: {
          ...baseTask,
          prompt: '<span class="chat-file-path" data-file-path="src/components" data-path-type="dir">components</span>',
        },
      },
    })
    const dirEl = wrapper.find('.chat-file-path')
    expect(dirEl.exists()).toBe(true)
    await dirEl.trigger('click')
    expect(mockOpenFilePath).toHaveBeenCalledWith('src/components', undefined, undefined, 'task')
  })
})

