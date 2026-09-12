import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TaskFormPage from '../TaskFormPage.vue'

// The real composable returns `form` as a ref. The template auto-unwraps it
// (`form.triggerMode`) while the script reads `form.value.agentId`, so the mock
// must be a genuine ref to satisfy both.
// Exposed so individual tests can seed a stored subscription (e.g. a legacy
// task) before mounting.
const formRef = ref({
  name: 't', prompt: 'p', agentId: '', cronExpr: '',
  triggerMode: 'event', eventTypes: '', eventRepo: '',
  repeatMode: 'unlimited', maxRuns: 0,
})

vi.mock('@/composables/useTaskForm', () => ({
  useTaskForm: () => ({
    form: formRef,
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
  beforeEach(() => { formRef.value.eventTypes = '' })

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

  it('offers merge only for pull requests', () => {
    const wrapper = mountForm()
    const values = wrapper.findAll('.event-type-group input[type=checkbox]')
      .map(i => (i.element as HTMLInputElement).value)
    // An issue has no merge, so issue.merged would never fire.
    expect(values).toContain('pr.merged')
    expect(values).not.toContain('issue.merged')
  })

  it('does not offer pipeline_done, which nothing can trigger yet', () => {
    // No code path derives a pipeline event, so offering it would create a
    // subscription that can never fire.
    const wrapper = mountForm()
    const values = wrapper.findAll('.event-type-group input[type=checkbox]')
      .map(i => (i.element as HTMLInputElement).value)
    expect(values).not.toContain('pr.pipeline_done')
    expect(values).not.toContain('issue.pipeline_done')
  })

  it('gives Issues fewer options than Pull requests', () => {
    const wrapper = mountForm()
    const groups = wrapper.findAll('.event-type-group')
    const issueCount = groups[0].findAll('input[type=checkbox]').length
    const prCount = groups[1].findAll('input[type=checkbox]').length
    expect(issueCount).toBe(4)
    expect(prCount).toBe(5)
  })

  // ── Legacy (pre-split) subscriptions ──

  function checkedValues(wrapper: ReturnType<typeof mountForm>) {
    return wrapper.findAll('.event-type-group input[type=checkbox]')
      .filter(i => (i.element as HTMLInputElement).checked)
      .map(i => (i.element as HTMLInputElement).value)
  }

  it('shows a legacy bare key as checked under every kind it matches', () => {
    // Regression: a task stored as "opened" showed ZERO boxes ticked, because
    // the model held bare keys while the checkboxes use scoped values.
    formRef.value.eventTypes = 'opened'
    const wrapper = mountForm()
    // The backend treats bare "opened" as "either kind", so both must be shown.
    expect(checkedValues(wrapper)).toEqual(['issue.opened', 'pr.opened'])
  })

  it('maps a bare key that only one kind supports to just that kind', () => {
    formRef.value.eventTypes = 'merged'
    const wrapper = mountForm()
    expect(checkedValues(wrapper)).toEqual(['pr.merged'])
  })

  it('does not silently drop a legacy subscription when another box is toggled', async () => {
    // Regression: the old setter wrote vals.join(','), so ticking one box
    // rewrote "opened,closed" to just the new value and lost the rest.
    formRef.value.eventTypes = 'opened,closed'
    const wrapper = mountForm()
    const boxes = wrapper.findAll('.event-type-group input[type=checkbox]')
    // Tick issue.commented on top of the expanded legacy selection. Use
    // setValue so Vue's v-model actually observes the change.
    const commented = boxes.find(i => (i.element as HTMLInputElement).value === 'issue.commented')!
    await commented.setValue(true)
    const stored = formRef.value.eventTypes.split(',').map((x: string) => x.trim()).sort()
    expect(stored).toContain('issue.commented')
    // Every originally-subscribed transition survives.
    expect(stored).toContain('issue.closed')
    expect(stored).toContain('pr.closed')
  })

  it('preserves an unrecognized stored key it cannot render', async () => {
    // A retired/unknown key has no checkbox; an edit must not erase it.
    formRef.value.eventTypes = 'opened,some_future_event'
    const wrapper = mountForm()
    const boxes = wrapper.findAll('.event-type-group input[type=checkbox]')
    const commented = boxes.find(i => (i.element as HTMLInputElement).value === 'issue.commented')!
    await commented.setValue(true)
    expect(formRef.value.eventTypes).toContain('some_future_event')
  })
})
