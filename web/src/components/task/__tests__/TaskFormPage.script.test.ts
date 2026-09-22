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
  script: '', scriptTimeout: 0,
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
          scriptPlaceholder: 'Optional shell script',
          scriptTimeout: 'Script timeout (seconds)',
          scriptTimeoutInvalid: 'bad timeout',
          scriptHint: 'Runs before the AI with the project path as cwd.',
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
    formRef.value.scriptTimeout = 0
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

  it('renders the helper text explaining the three script behaviours', () => {
    // The hint sits below BOTH fields (script + timeout), so it is looked up
    // across the form rather than inside the textarea's own group.
    const wrapper = mountForm()
    const hints = wrapper.findAll('.form-hint').map(h => h.text())
    expect(hints).toContain('Runs before the AI with the project path as cwd.')
  })

  it('sets the script textarea in the monospace face', () => {
    // A shell script is code; the shared textarea style is proportional, so the
    // section must opt into the mono face explicitly.
    const wrapper = mountForm()
    const textarea = scriptSection(wrapper)!.find('textarea')
    expect(textarea.classes()).toContain('script-textarea')
  })
})
