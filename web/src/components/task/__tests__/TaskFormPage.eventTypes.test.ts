import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TaskFormPage from '../TaskFormPage.vue'
import { resetForgeBindingState } from '@/composables/useForgeBinding'

// The real composable returns `form` as a ref. The template auto-unwraps it
// (`form.triggerMode`) while the script reads `form.value.agentId`, so the mock
// must be a genuine ref to satisfy both.
// Exposed so individual tests can seed a stored subscription (e.g. a legacy
// task) before mounting.
const formRef = ref({
  name: 't', prompt: 'p', agentId: '', cronExpr: '',
  triggerMode: 'event', eventTypes: '',
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

// The form resolves the project's forge binding to display the watched
// repository. Defaults to unbound; individual tests override it.
const { mockFetchForgeBinding } = vi.hoisted(() => ({
  mockFetchForgeBinding: vi.fn(),
}))
vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('@/utils/forgeApi')
  return { ...actual, fetchForgeBinding: mockFetchForgeBinding }
})

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { cancel: 'Cancel', save: 'Save', loading: 'Loading…' },
      task: {
        form: {
          name: 'Name', prompt: 'Prompt', agent: 'Agent', triggerMode: 'Trigger',
          triggerCron: 'Schedule', triggerEvent: 'Event',
          eventTypes: 'Events to watch', eventTypesRequired: 'pick one',
          eventKindIssue: 'Issues', eventKindPr: 'Pull requests', eventKindRepo: 'Repository pipelines',
          eventOpened: 'Opened', eventClosed: 'Closed', eventMerged: 'Merged',
          eventReopened: 'Reopened', eventCommented: 'Commented', eventPipeline: 'Pipeline finished',
          eventRepo: 'Repo', eventRepoHint: 'Watched repo',
          eventRepoUnbound: 'No repository bound',
          eventRepoUnboundWarn: 'Bind a repository or this task will never fire',
          eventContextHeader: 'Context', varEventType: 'event', varRepo: 'repo',
          varItem: 'item', varTitle: 'title', varUrl: 'url', varAuthor: 'author',
          varState: 'state', varCommentBody: 'comment', varPipelineStatus: 'ps', varPipelineUrl: 'pu',
          varActorIsSelf: 'self',
          varPrevState: 'prev', varBody: 'body', varLabels: 'labels', varAssignees: 'assignees',
          varDraft: 'draft', varSourceBranch: 'branch', varMergedAt: 'mergedAt',
          varCreatedAt: 'createdAt', varUpdatedAt: 'updatedAt', varCommentCount: 'commentCount',
          varCommentId: 'commentId', varPipelineNumber: 'pn', varPipelineRef: 'pr',
          varPipelineSha: 'psha', varPipelineTrigger: 'ptr', varPipelineDuration: 'pd',
          varPipelineLinkedPrs: 'plpr',
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
  beforeEach(() => {
    formRef.value.eventTypes = ''
    mockFetchForgeBinding.mockReset()
    mockFetchForgeBinding.mockResolvedValue({ binding: null })
  })

  it('groups events under Issues, Pull requests and Repository pipelines', () => {
    const wrapper = mountForm()
    const groups = wrapper.findAll('.event-type-group')
    expect(groups.length, 'two kind groups plus the repository-level group').toBe(3)
    const labels = wrapper.findAll('.event-type-group-label').map(g => g.text())
    expect(labels).toEqual(['Issues', 'Pull requests', 'Repository pipelines'])
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

  it('offers pipeline_done as a bare repository-level key', () => {
    // A pipeline is triggered by a push, a tag or a schedule — none of which is
    // an issue or a PR — so it is offered in its own group under the BARE key,
    // which is also how the backend matches it.
    const wrapper = mountForm()
    const values = wrapper.findAll('.event-type-group input[type=checkbox]')
      .map(i => (i.element as HTMLInputElement).value)
    expect(values).toContain('pipeline_done')
    // The kind-scoped spellings must NOT be offered: an issue has no CI, and a
    // PR-scoped pipeline would imply a run belongs to a change request.
    expect(values).not.toContain('pr.pipeline_done')
    expect(values).not.toContain('issue.pipeline_done')
    expect(values).not.toContain('pipeline.pipeline_done')
  })

  it('renders the repository pipeline group last', () => {
    const wrapper = mountForm()
    const groups = wrapper.findAll('.event-type-group')
    const repoGroup = groups[groups.length - 1]
    expect(repoGroup.find('.event-type-group-label').text()).toBe('Repository pipelines')
    const values = repoGroup.findAll('input[type=checkbox]').map(i => (i.element as HTMLInputElement).value)
    expect(values).toEqual(['pipeline_done'])
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

// The watched repository is not configurable — an event task always watches the
// project's binding. The form therefore shows it rather than offering a choice.
describe('TaskFormPage watched repository', () => {
  beforeEach(() => {
    formRef.value.eventTypes = ''
    mockFetchForgeBinding.mockReset()
    // The binding singleton (and its TTL cache) survives between tests; reset it
    // so each case performs its own lookup, as a real project switch would.
    resetForgeBindingState()
  })

  it('shows the project-bound repository as read-only text, with no selector', async () => {
    mockFetchForgeBinding.mockResolvedValue({
      binding: { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets' },
    })
    const wrapper = mountForm()

    await vi.waitFor(() => {
      expect(wrapper.find('.event-repo-readonly').text()).toContain('acme/widgets')
    })
    // There is nothing to pick: no dropdown for the repo scope.
    expect(wrapper.find('.event-repo-readonly select').exists()).toBe(false)
    expect(wrapper.find('.form-warning').exists()).toBe(false)
  })

  it('warns when the project has no binding, without blocking the save', async () => {
    mockFetchForgeBinding.mockResolvedValue({ binding: null })
    const wrapper = mountForm()

    await vi.waitFor(() => {
      expect(wrapper.find('.event-repo-readonly').text()).toContain('No repository bound')
    })
    // Soft warning only: the save button stays enabled so the task can still be
    // created (the user may bind a repository afterwards).
    expect(wrapper.find('.form-warning').text()).toContain('never fire')
    const save = wrapper.find('button.primary-btn, button[type=submit]')
    if (save.exists()) expect(save.attributes('disabled')).toBeUndefined()
  })

  // Regression: the repository row read `boundRepoLabel || unbound` with the
  // label starting at '', so an in-flight lookup rendered the "no repository
  // bound — this task will never fire" warning. Users saw a red warning for a
  // project that was in fact bound, for as long as the request took.
  it('does not warn while the binding lookup is still in flight', async () => {
    let release: (v: unknown) => void = () => {}
    mockFetchForgeBinding.mockReturnValue(new Promise(resolve => { release = resolve }))

    const wrapper = mountForm()
    await wrapper.vm.$nextTick()

    // The lookup has not answered yet: no unbound claim, no warning.
    expect(wrapper.find('.form-warning').exists()).toBe(false)
    expect(wrapper.find('.event-repo-readonly').text()).not.toContain('No repository bound')

    // Once it answers "bound", the real repository appears and still no warning.
    release({ binding: { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets' } })
    await vi.waitFor(() => {
      expect(wrapper.find('.event-repo-readonly').text()).toContain('acme/widgets')
    })
    expect(wrapper.find('.form-warning').exists()).toBe(false)
  })

  // ── Event context block ──
  // The block is the user's only documentation of what the task will receive, so
  // it must advertise the newly-added variables for the subscriptions that can
  // actually yield them.
  describe('event context block', () => {
    // selectedEventTypes is a computed over the form ref, so the block only
    // re-renders after a tick.
    const blockText = async (eventTypes: string) => {
      formRef.value.eventTypes = eventTypes
      await wrapper.vm.$nextTick()
      return wrapper.text()
    }
    let wrapper: ReturnType<typeof mountForm>

    beforeEach(() => { wrapper = mountForm() })

    // With no event selected the task has no trigger, so nothing is injected.
    // Showing the block (or a heading with no rows) would document a payload
    // that can never arrive.
    it('hides the block entirely until an event is selected', async () => {
      await blockText('')
      expect(wrapper.find('.event-context-block').exists()).toBe(false)
      // Not merely empty — the label and hint go too.
      expect(wrapper.text()).not.toContain('Context')
    })

    it('shows the block once an event is selected', async () => {
      await blockText('pr.opened')
      expect(wrapper.find('.event-context-block').exists()).toBe(true)
      expect(wrapper.text()).toContain('{{EVENT_TYPE}}')
    })

    it('lists the item-scoped variables for a PR subscription', async () => {
      const text = await blockText('pr.opened')
      for (const placeholder of [
        '{{BODY}}', '{{LABELS}}', '{{ASSIGNEES}}', '{{SOURCE_BRANCH}}', '{{PREV_STATE}}',
        '{{DRAFT}}', '{{MERGED_AT}}', '{{CREATED_AT}}', '{{UPDATED_AT}}', '{{COMMENT_COUNT}}',
      ]) {
        expect(text, `missing ${placeholder}`).toContain(placeholder)
      }
    })

    it('does not list pipeline variables for an issue/PR subscription', async () => {
      const text = await blockText('pr.opened')
      expect(text).not.toContain('{{PIPELINE_')
      expect(text).not.toContain('{{COMMENT_BODY}}')
    })

    // The regression: the block used to compare a kind-scoped subscription key
    // ("pr.commented") against a bare scoped transition, so the comment
    // variables never appeared for a real (kind-scoped) subscription.
    it('lists the comment variables for a kind-scoped commented subscription', async () => {
      const text = await blockText('pr.commented')
      expect(text).toContain('{{COMMENT_BODY}}')
      expect(text).toContain('{{COMMENT_ID}}')
    })

    it('lists the pipeline variables for a pipeline subscription', async () => {
      const text = await blockText('pipeline_done')
      expect(text).toContain('{{PIPELINE_STATUS}}')
      expect(text).toContain('{{PIPELINE_REF}}')
      expect(text).toContain('{{PIPELINE_SHA}}')
      expect(text).toContain('{{ACTOR_IS_SELF}}')
    })

    it('omits every item-scoped variable for a pipeline-only subscription', async () => {
      const text = await blockText('pipeline_done')
      expect(text).not.toContain('{{ITEM_TYPE')
      expect(text).not.toContain('{{BODY}}')
      expect(text).not.toContain('{{SOURCE_BRANCH}}')
      expect(text).not.toContain('{{MERGED_AT}}')
    })

    it('hides PREV_STATE for a commented-only subscription', async () => {
      // Its previous state equals the current one, so the line would be noise.
      expect(await blockText('pr.commented')).not.toContain('{{PREV_STATE}}')
    })
  })
})
