import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TaskChatCard from '@/components/chat/TaskChatCard.vue'

// ── Mocks ──

// The event-label helpers resolve through the app i18n singleton, so the module
// has to exist. The mock echoes keys so assertions target the mapping rather
// than the translated string (matching forgeEventLabels.test.ts).
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        contentBlocks: {
          loading: 'Loading...',
          scheduledTaskCreated: 'Task created',
          taskDeleted: 'Task deleted',
          frequency: 'Frequency',
          executor: 'Executor',
          repeat: 'Repeat',
          status: 'Status',
          lastRun: 'Last run',
          nextRun: 'Next run',
          statusActive: 'Enabled',
          statusPaused: 'Disabled',
          statusCompleted: 'Completed',
          statusExecutions: '{count} executions',
        },
      },
      task: {
        overview: {
          eventPausedNote: 'This task is disabled, so no event will trigger a run',
        },
        form: {
          eventTypes: 'Events to watch',
          eventTypesNone: 'No events configured',
          eventKindIssue: 'Issues',
          eventKindPr: 'Pull requests',
          eventKindRepo: 'Repository pipelines',
          eventOpened: 'Opened',
          eventClosed: 'Closed',
          eventMerged: 'Merged',
        },
      },
    },
  },
})

const AgentIconStub = { name: 'AgentIcon', template: '<span class="agent-stub" />' }
const LucideStub = { template: '<span class="lucide-stub" />' }

function mountCard(props: Record<string, unknown> = {}) {
  return mount(TaskChatCard, {
    props: {
      task: null,
      loading: false,
      deleted: false,
      getAgentBackend: () => 'acp',
      getAgentName: () => 'test-agent',
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: {
        AgentIcon: AgentIconStub,
        AlertTriangle: LucideStub,
        Archive: LucideStub,
        ChevronRight: LucideStub,
        Clock: LucideStub,
        Zap: LucideStub,
      },
    },
  })
}

const cronTask = {
  id: 7,
  name: 'Nightly build',
  status: 'active',
  triggerMode: 'cron',
  cronExpr: '0 0 * * *',
  agentId: 'a1',
  repeatMode: 'once',
  maxRuns: 1,
  runCount: 0,
  prompt: 'Build the project and report failures',
  lastRunAt: '2026-09-16T10:00:00Z',
  nextRunAt: '2026-09-17T10:00:00Z',
}

const eventTask = {
  id: 41,
  name: 'Review new PRs',
  status: 'active',
  triggerMode: 'event',
  eventTypes: 'pr.opened',
  cronExpr: '',
  agentId: 'a1',
  // The backend leaves these inert for event tasks; the card must not render
  // them as if they described the trigger.
  repeatMode: 'unlimited',
  maxRuns: 0,
  runCount: 3,
  prompt: 'Review the pull request and leave comments',
  lastRunAt: '',
  nextRunAt: '',
}

describe('TaskChatCard', () => {
  describe('cron tasks (unchanged behaviour)', () => {
    it('renders the schedule, repeat mode and next run', () => {
      const wrapper = mountCard({ task: cronTask })
      const rows = wrapper.findAll('.stask-row').map(r => r.text())
      expect(rows.some(r => r.includes('Frequency'))).toBe(true)
      expect(rows.some(r => r.includes('Repeat'))).toBe(true)
      expect(rows.some(r => r.includes('Next run'))).toBe(true)
      expect(wrapper.text()).toContain('Nightly build')
    })

    it('does not render the event subscription row', () => {
      const wrapper = mountCard({ task: cronTask })
      expect(wrapper.findAll('.stask-chip')).toHaveLength(0)
      expect(wrapper.text()).not.toContain('Events to watch')
    })
  })

  describe('event tasks', () => {
    it('shows the subscribed events instead of a blank cron line and an inert repeat mode', () => {
      const wrapper = mountCard({ task: eventTask })
      const rows = wrapper.findAll('.stask-row').map(r => r.text())

      // The subscription is what actually fires the task.
      expect(rows.some(r => r.includes('Events to watch'))).toBe(true)
      expect(wrapper.find('.stask-chip').text()).toContain('Opened')

      // Frequency/repeat/next-run describe a schedule an event task does not
      // have: humanizeCron('') renders blank and repeatLabel would fabricate
      // "unlimited". They must be absent, not empty.
      expect(rows.some(r => r.includes('Frequency'))).toBe(false)
      expect(rows.some(r => r.includes('Repeat'))).toBe(false)
      expect(rows.some(r => r.includes('Next run'))).toBe(false)
    })

    it('groups a multi-event subscription into one chip per event', () => {
      const wrapper = mountCard({ task: { ...eventTask, eventTypes: 'issue.opened,pr.merged' } })
      const chips = wrapper.findAll('.stask-chip')
      expect(chips).toHaveLength(2)
      expect(chips[0].text()).toContain('Opened')
      expect(chips[1].text()).toContain('Merged')
    })

    it('warns that no events are configured rather than rendering an empty row', () => {
      const wrapper = mountCard({ task: { ...eventTask, eventTypes: '' } })
      expect(wrapper.findAll('.stask-chip')).toHaveLength(0)
      expect(wrapper.find('.stask-chips-none').text()).toBe('No events configured')
    })

    it('carries the event accent so the trigger type is distinguishable at a glance', () => {
      expect(mountCard({ task: eventTask }).classes()).toContain('is-event')
      expect(mountCard({ task: cronTask }).classes()).not.toContain('is-event')
    })
  })

  describe('header', () => {
    // The header badge repeated the body's status row, which carries strictly
    // more (the execution count). Keeping both said the same thing twice, so
    // the badge is gone — and the body row must stay the surviving source.
    it('renders no status badge, but keeps the body status row', () => {
      const wrapper = mountCard({ task: cronTask })
      expect(wrapper.find('.stask-status-badge').exists()).toBe(false)
      expect(wrapper.find('.stask-status-dot').exists()).toBe(true)
      expect(wrapper.text()).toContain('Enabled')
    })
  })

  describe('execution count', () => {
    // The count is the only place a stopped task reports how often it ran, so
    // it must survive the task being disabled or exhausted.
    it('keeps the execution count visible once the task is disabled', () => {
      const wrapper = mountCard({ task: { ...cronTask, status: 'paused', runCount: 4 } })
      expect(wrapper.text()).toContain('Disabled')
      expect(wrapper.text()).toContain('4 executions')
    })

    it('keeps the execution count visible once the task is completed', () => {
      const wrapper = mountCard({ task: { ...cronTask, status: 'completed', runCount: 2 } })
      expect(wrapper.text()).toContain('Completed')
      expect(wrapper.text()).toContain('2 executions')
    })

    it('shows progress toward the run limit for a bounded task', () => {
      const wrapper = mountCard({ task: { ...cronTask, repeatMode: 'limited', maxRuns: 5, runCount: 2 } })
      expect(wrapper.find('.stask-progress').text()).toBe('(2/5)')
    })

    // An unlimited task never advances toward a limit, and maxRuns is 0 there —
    // printing the ratio would render the nonsense "(12/1)".
    it('omits the progress ratio for an unlimited task', () => {
      const wrapper = mountCard({ task: { ...cronTask, repeatMode: 'unlimited', maxRuns: 0, runCount: 12 } })
      expect(wrapper.find('.stask-progress').exists()).toBe(false)
      // The repeat row itself stays: only the ratio is dropped.
      const rows = wrapper.findAll('.stask-row').map(r => r.text())
      expect(rows.some(r => r.includes('Repeat'))).toBe(true)
    })
  })

  describe('prompt preview', () => {
    // Every other row describes mechanics; the prompt is the only place the card
    // says what a run actually does.
    it('shows the prompt, stripped of markdown', () => {
      const wrapper = mountCard({ task: { ...cronTask, prompt: 'Check **CI** and `report`' } })
      expect(wrapper.find('.stask-prompt').text()).toBe('Check CI and report')
    })

    it('hides the row when the task carries no prompt', () => {
      const wrapper = mountCard({ task: { ...cronTask, prompt: '' } })
      expect(wrapper.find('.stask-prompt').exists()).toBe(false)
    })

    it('renders for an event task too', () => {
      expect(mountCard({ task: eventTask }).find('.stask-prompt').exists()).toBe(true)
    })
  })

  describe('disabled event task warning', () => {
    // A disabled event task is silently inert: the trigger requires
    // status=active, so nothing fires and nothing reports it. The card must say
    // so rather than leaving a grey status dot to imply it.
    it('warns when an event task is disabled', () => {
      const wrapper = mountCard({ task: { ...eventTask, status: 'paused' } })
      expect(wrapper.find('.stask-warn').exists()).toBe(true)
      expect(wrapper.text()).toContain('no event will trigger a run')
    })

    it('does not warn while the event task is enabled', () => {
      expect(mountCard({ task: eventTask }).find('.stask-warn').exists()).toBe(false)
    })

    // A paused cron task simply has no next run — its own row already shows
    // that, so the event-specific warning would misdescribe it.
    it('does not warn for a paused cron task', () => {
      expect(mountCard({ task: { ...cronTask, status: 'paused' } }).find('.stask-warn').exists()).toBe(false)
    })
  })

  describe('loading / deleted / absent task', () => {
    it('renders the created fallback while loading and no body', () => {
      const wrapper = mountCard({ task: null, loading: true })
      expect(wrapper.text()).toContain('Loading...')
      expect(wrapper.find('.stask-body').exists()).toBe(false)
    })

    it('renders a deleted card as inert', () => {
      const wrapper = mountCard({ task: eventTask, deleted: true })
      expect(wrapper.classes()).toContain('deleted')
      expect(wrapper.text()).toContain('Task deleted')
      expect(wrapper.find('.stask-body').exists()).toBe(false)
    })

    it('does not emit select while the card is not actionable', async () => {
      const loading = mountCard({ task: null, loading: true })
      await loading.trigger('click')
      expect(loading.emitted('select')).toBeFalsy()

      const deleted = mountCard({ task: eventTask, deleted: true })
      await deleted.trigger('click')
      expect(deleted.emitted('select')).toBeFalsy()
    })

    it('emits select once the task resolved', async () => {
      const wrapper = mountCard({ task: eventTask })
      await wrapper.trigger('click')
      expect(wrapper.emitted('select')).toHaveLength(1)
    })
  })

  describe('affordance', () => {
    // The whole card is the click target, so a footer "view details" row only
    // repeated what clicking anywhere already does. Both trigger modes dropped
    // it; this pins that so it cannot be reintroduced on one branch only.
    it.each([
      ['cron', cronTask],
      ['event', eventTask],
    ])('does not render a footer view-details row (%s task)', (_label, task) => {
      const wrapper = mountCard({ task })
      expect(wrapper.find('.stask-view-btn').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('View detail')
    })
  })

  describe('quote source identity', () => {
    // Selecting the card's title is how a task gets quoted, so the card must
    // expose the task id as a machine key: a name is not addressable, and both
    // the jump handler and the AI prompt need the id.
    it('exposes the task id and a labelled source', () => {
      const wrapper = mountCard({ task: cronTask })

      expect(wrapper.attributes('data-quote-task-id')).toBe('7')
      expect(wrapper.attributes('data-quote-source')).toBe('Nightly build (#7)')
    })

    // A card that has not resolved its task has no id to offer. Claiming one
    // would make a quote jump to a task that does not exist.
    it('exposes no task id while loading', () => {
      const wrapper = mountCard({ task: null, loading: true })

      expect(wrapper.attributes('data-quote-task-id')).toBe('')
      expect(wrapper.attributes('data-quote-source')).toBe('')
    })

    it('exposes no task id for a deleted task', () => {
      const wrapper = mountCard({ task: cronTask, deleted: true })

      expect(wrapper.attributes('data-quote-task-id')).toBe('')
    })
  })
})
