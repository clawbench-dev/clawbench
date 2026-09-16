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
        },
      },
      task: {
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
})
