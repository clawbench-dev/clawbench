import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TaskFormPage from '../TaskFormPage.vue'

// The real composable returns `form` as a ref. The template auto-unwraps it
// (`form.triggerMode`) while the script reads `form.value.agentId`, so the mock
// must be a genuine ref to satisfy both.
vi.mock('@/composables/useTaskForm', () => ({
  useTaskForm: () => ({
    form: ref({
      name: 't', prompt: 'p', agentId: '', cronExpr: '',
      triggerMode: 'event', eventTypes: '', eventRepo: '',
      repeatMode: 'unlimited', maxRuns: 0,
    }),
    errors: ref({}),
    formError: ref(''),
    saving: ref(false),
    submit: vi.fn(),
    init: vi.fn(),
  }),
}))

vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('@/utils/forgeApi')
  return { ...actual, fetchForgeBinding: vi.fn(async () => ({ binding: null })) }
})

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { cancel: 'Cancel', save: 'Save' },
      task: {
        form: {
          name: 'Name', prompt: 'Prompt', agent: 'Agent', triggerMode: 'Trigger',
          triggerCron: 'Schedule', triggerEvent: 'Event',
          eventTypes: 'Events to watch', eventTypesRequired: 'pick one',
          eventKindIssue: 'Issues', eventKindPr: 'Pull requests',
          eventOpened: 'Opened', eventClosed: 'Closed', eventMerged: 'Merged',
          eventReopened: 'Reopened', eventCommented: 'Commented', eventPipeline: 'Pipeline finished',
          eventRepo: 'Repo', eventRepoAny: 'Any', eventRepoHint: '',
          eventContextHeader: 'Context', varEventType: 'event', varRepo: 'repo',
          varItem: 'item', varTitle: 'title', varUrl: 'url', varAuthor: 'author',
          varState: 'state', varCommentBody: 'comment', varPipelineStatus: 'ps', varPipelineUrl: 'pu',
          repeatMode: 'Repeat', presets: {},
        },
      },
    },
  },
})

function mountForm() {
  return mount(TaskFormPage, {
    props: { mode: 'create' },
    global: { plugins: [i18n], stubs: { RefreshButton: true } },
  })
}

describe('TaskFormPage event type grouping', () => {
  it('groups events under Issues and Pull requests', () => {
    const wrapper = mountForm()
    const groups = wrapper.findAll('.event-type-group')
    expect(groups.length, 'two kind groups').toBe(2)
    const labels = wrapper.findAll('.event-type-group-label').map(g => g.text())
    expect(labels).toEqual(['Issues', 'Pull requests'])
  })

  it('emits kind-scoped keys so issue and PR events are independent', () => {
    const wrapper = mountForm()
    const values = wrapper.findAll('.event-type-group input[type=checkbox]')
      .map(i => (i.element as HTMLInputElement).value)
    // The whole point of the split: the same transition appears once per kind.
    expect(values).toContain('issue.opened')
    expect(values).toContain('pr.opened')
    // A bare "opened" must not be offered: it would match both kinds again.
    expect(values).not.toContain('opened')
  })

  it('offers merge and pipeline only for pull requests', () => {
    const wrapper = mountForm()
    const values = wrapper.findAll('.event-type-group input[type=checkbox]')
      .map(i => (i.element as HTMLInputElement).value)
    // An issue has no merge and no CI, so these would never fire.
    expect(values).toContain('pr.merged')
    expect(values).toContain('pr.pipeline_done')
    expect(values).not.toContain('issue.merged')
    expect(values).not.toContain('issue.pipeline_done')
  })

  it('gives Issues fewer options than Pull requests', () => {
    const wrapper = mountForm()
    const groups = wrapper.findAll('.event-type-group')
    const issueCount = groups[0].findAll('input[type=checkbox]').length
    const prCount = groups[1].findAll('input[type=checkbox]').length
    expect(issueCount).toBe(4)
    expect(prCount).toBe(6)
  })
})
