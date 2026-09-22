import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TaskFormPage from '../TaskFormPage.vue'
import { resetForgeBindingState } from '@/composables/useForgeBinding'

// The real composable returns `form` as a genuine ref: the template
// auto-unwraps it while the script reads `form.value`. The mock must satisfy
// both, so it exposes an actual ref (same pattern as the event-types test).
const formRef = ref({
  name: 't', prompt: 'p', agentId: '', cronExpr: '',
  triggerMode: 'cron', eventTypes: '',
  repeatMode: 'unlimited', maxRuns: 0,
  // The real form leaves this empty; the 300s default is a placeholder.
  script: '', scriptTimeout: '',
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
          script: 'Custom script',
          scriptPlaceholder: 'Optional shell script. If it exits 0 with no output, the AI is skipped.',
          scriptTimeout: 'Script timeout (seconds)',
          scriptTimeoutInvalid: 'bad timeout',
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

/** The script textarea, located by its label so the test does not depend on
 *  the field order in the form. */
function scriptSection(wrapper: ReturnType<typeof mountForm>) {
  return wrapper.findAll('.form-group').find(g => g.find('.form-label').exists()
    && g.find('.form-label').text() === 'Custom script')
}

describe('TaskFormPage custom script section', () => {
  beforeEach(() => {
    formRef.value.triggerMode = 'cron'
    formRef.value.script = ''
    formRef.value.scriptTimeout = ''
    mockFetchForgeBinding.mockReset()
    mockFetchForgeBinding.mockResolvedValue({ binding: null })
    resetForgeBindingState()
  })

  it('renders the script field and timeout for a cron task', () => {
    const wrapper = mountForm()
    const section = scriptSection(wrapper)
    expect(section, 'the cron form must offer a custom script').toBeTruthy()
    expect(section!.find('textarea').exists()).toBe(true)
    // The timeout is a numeric input, not free text.
    const timeout = wrapper.findAll('.form-input').find(i => i.attributes('type') === 'number')
    expect(timeout, 'the cron form must offer a script timeout').toBeTruthy()
    expect(wrapper.text()).toContain('Script timeout (seconds)')
  })

  it('hides the script field for an event task', async () => {
    // A script is a cron-task precondition: an event task's prompt comes from
    // the injected event context, so the field would be inert.
    formRef.value.triggerMode = 'event'
    const wrapper = mountForm()
    await wrapper.vm.$nextTick()

    expect(scriptSection(wrapper)).toBeFalsy()
    expect(wrapper.text()).not.toContain('Script timeout (seconds)')
  })

  it('shows the 300s default as a placeholder on an empty timeout input', () => {
    // The field is empty by default and reads as 300 via the placeholder —
    // rendering a literal 0 would read as "no timeout".
    const wrapper = mountForm()
    const timeout = wrapper.findAll('.form-input').find(i => i.attributes('type') === 'number')!
    expect((timeout.element as HTMLInputElement).value).toBe('')
    expect(timeout.attributes('placeholder')).toBe('300')
  })

  it('carries the skip explanation in the script textarea placeholder', () => {
    // The skip semantics moved out of the (now removed) long hint paragraph
    // and into the placeholder, so the placeholder must mention the skip.
    const wrapper = mountForm()
    const textarea = scriptSection(wrapper)!.find('textarea')
    expect(textarea.attributes('placeholder')).toContain('the AI is skipped')
  })

  it('no longer renders the verbose script hint paragraph', () => {
    // The long helper text (which sat under the timeout input) was removed as
    // too verbose. Its i18n key is gone, so the timeout's form-group must not
    // render any .form-hint, and the old copy must be absent from the form.
    const wrapper = mountForm()
    const timeoutGroup = wrapper.findAll('.form-group').find(g => g.find('.form-label').exists()
      && g.find('.form-label').text() === 'Script timeout (seconds)')
    expect(timeoutGroup, 'the timeout field must still render').toBeTruthy()
    expect(timeoutGroup!.findAll('.form-hint')).toHaveLength(0)
  })

  it('sets the script textarea in the monospace face', () => {
    // A shell script is code; the shared textarea style is proportional, so the
    // section must opt into the mono face explicitly.
    const wrapper = mountForm()
    const textarea = scriptSection(wrapper)!.find('textarea')
    expect(textarea.classes()).toContain('script-textarea')
  })
})
