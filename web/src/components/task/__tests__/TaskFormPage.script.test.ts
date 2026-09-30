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

// The script editor mounts real CodeMirror, which is heavyweight and unrelated
// to the form contract under test. Stub it so the tests can assert on the
// field's presence/props without a real editor instance.
vi.mock('@/components/task/TaskScriptEditor.vue', () => ({
  default: {
    name: 'TaskScriptEditor',
    props: ['modelValue', 'language', 'placeholder', 'disabled'],
    template: '<div class="script-editor-stub" :data-placeholder="placeholder"></div>',
  },
}))

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
          script: 'Gating script',
          scriptEnabled: 'Enable gating script',
          scriptHint: 'Runs before the agent',
          scriptPlaceholder: 'Enter a shell script',
          scriptGuideTitle: 'About the gating script',
          scriptGuideIntro: 'Exits 0 to run the agent.',
          scriptGuideVarsTitle: 'The prompt can reference:',
          scriptVarCode: 'Exit code',
          scriptVarStdout: 'Script stdout',
          scriptVarStderr: 'Script stderr',
          scriptVarOutput: 'Script output',
          scriptTimeout: 'Script timeout (seconds)',
          scriptTimeoutInvalid: 'bad timeout',
        },
      },
    },
  },
})

function mountForm(props: Record<string, unknown> = {}) {
  return mount(TaskFormPage, {
    props: { mode: 'create', ...props },
    global: { plugins: [i18n], stubs: { RefreshButton: true } },
  })
}

/** The gating-script switch row, located by its label text. */
function switchRow(wrapper: ReturnType<typeof mountForm>) {
  return wrapper.findAll('.script-switch-row')[0]
}

/** The script editor field group, located by its label. */
function scriptSection(wrapper: ReturnType<typeof mountForm>) {
  return wrapper.findAll('.form-group').find(g => g.find('.form-label').exists()
    && g.find('.form-label').text() === 'Gating script')
}

describe('TaskFormPage gating script section', () => {
  beforeEach(() => {
    formRef.value.triggerMode = 'cron'
    formRef.value.script = ''
    formRef.value.scriptTimeout = ''
    mockFetchForgeBinding.mockReset()
    mockFetchForgeBinding.mockResolvedValue({ binding: null })
    resetForgeBindingState()
  })

  it('hides the script config by default for a task without a script', () => {
    // The switch is off unless the stored task has a script, so a fresh form
    // shows no script configuration at all.
    const wrapper = mountForm()
    expect(switchRow(wrapper), 'the gate switch must render').toBeTruthy()
    expect(scriptSection(wrapper), 'the script editor must stay hidden while the switch is off').toBeFalsy()
    expect(wrapper.text()).not.toContain('Script timeout (seconds)')
  })

  it('reveals the script editor and timeout when the switch is enabled', async () => {
    const wrapper = mountForm()
    await switchRow(wrapper).find('input[type="checkbox"]').setValue(true)
    const section = scriptSection(wrapper)
    expect(section, 'the cron form must offer a gating script once enabled').toBeTruthy()
    expect(section!.find('.script-editor-stub').exists()).toBe(true)
    expect(wrapper.text()).toContain('Script timeout (seconds)')
  })

  it('turns the gate on when editing a task that already has a script', async () => {
    const wrapper = mountForm({ mode: 'edit', task: { script: 'test -f x', cronExpr: '0 9 * * *' } })
    // The switch is flipped in onMounted, so the DOM needs a tick to patch.
    await wrapper.vm.$nextTick()
    expect(scriptSection(wrapper), 'a stored script must not be silently dropped').toBeTruthy()
  })

  it('clears the script when the switch is turned off', async () => {
    // A disabled gate must not leave a stale script behind to run later.
    const wrapper = mountForm({ mode: 'edit', task: { script: 'echo hi', cronExpr: '0 9 * * *' } })
    await wrapper.vm.$nextTick()
    await switchRow(wrapper).find('input[type="checkbox"]').setValue(false)
    expect(formRef.value.script).toBe('')
  })

  it('restores the script when the switch is turned back on (accidental toggle)', async () => {
    // The switch sits one stray click away from the editor. Off→on must give
    // the text back rather than silently destroying it (WARN-901).
    // `init` is mocked out here, so seed the field the way a real edit does.
    formRef.value.script = 'echo hi'
    const wrapper = mountForm({ mode: 'edit', task: { script: 'echo hi', cronExpr: '0 9 * * *' } })
    await wrapper.vm.$nextTick()
    const toggle = switchRow(wrapper).find('input[type="checkbox"]')

    await toggle.setValue(false)
    expect(formRef.value.script).toBe('')

    await toggle.setValue(true)
    expect(formRef.value.script, 'the stashed script must come back').toBe('echo hi')
  })

  it('does not clobber text typed after the toggle while restoring', async () => {
    // Restore only fills an empty field, so it can never overwrite an edit the
    // user made between the two toggles.
    formRef.value.script = 'echo hi'
    const wrapper = mountForm({ mode: 'edit', task: { script: 'echo hi', cronExpr: '0 9 * * *' } })
    await wrapper.vm.$nextTick()
    const toggle = switchRow(wrapper).find('input[type="checkbox"]')

    await toggle.setValue(false)
    formRef.value.script = 'new content'
    await toggle.setValue(true)

    expect(formRef.value.script).toBe('new content')
  })

  it('stashes the empty string when the switch is off with nothing to lose', async () => {
    // No prior script: toggling off then on must leave the field empty rather
    // than resurrect stale text from an earlier edit.
    const wrapper = mountForm()
    const toggle = switchRow(wrapper).find('input[type="checkbox"]')
    await toggle.setValue(true)
    await toggle.setValue(false)
    await toggle.setValue(true)
    expect(formRef.value.script).toBe('')
  })

  it('hides the script field for an event task', async () => {
    // A script is a cron-task precondition: an event task's prompt comes from
    // the injected event context, so the field would be inert.
    formRef.value.triggerMode = 'event'
    const wrapper = mountForm()
    await wrapper.vm.$nextTick()

    expect(switchRow(wrapper), 'the gate switch is cron-only').toBeFalsy()
    expect(scriptSection(wrapper)).toBeFalsy()
    expect(wrapper.text()).not.toContain('Script timeout (seconds)')
  })

  it('shows the 300s default as a placeholder on an empty timeout input', async () => {
    const wrapper = mountForm({ mode: 'edit', task: { script: 'echo hi', cronExpr: '0 9 * * *' } })
    await wrapper.vm.$nextTick()
    const timeout = wrapper.findAll('.form-input').find(i => i.attributes('type') === 'number')!
    expect((timeout.element as HTMLInputElement).value).toBe('')
    expect(timeout.attributes('placeholder')).toBe('300')
  })

  it('documents the template variables in a dedicated guide panel', async () => {
    // The four substitution variables are the feature's contract. They belong
    // in a persistent, styleable panel — not the editor placeholder, which
    // disappears on the first keystroke.
    const wrapper = mountForm({ mode: 'edit', task: { script: 'echo hi', cronExpr: '0 9 * * *' } })
    await wrapper.vm.$nextTick()
    const guide = wrapper.find('.script-guide')
    expect(guide.exists(), 'the guide panel must render once the gate is on').toBe(true)
    expect(guide.text()).toContain('About the gating script')

    const tokens = wrapper.findAll('.script-guide-token').map(t => t.text())
    expect(tokens).toEqual(['{{code}}', '{{stdout}}', '{{stderr}}', '{{output}}'])
  })

  it('keeps the placeholder to a short hint', async () => {
    // The explanation moved out, so the placeholder must no longer be a wall
    // of text repeating the guide panel.
    const wrapper = mountForm({ mode: 'edit', task: { script: 'echo hi', cronExpr: '0 9 * * *' } })
    await wrapper.vm.$nextTick()
    const placeholder = scriptSection(wrapper)!.find('.script-editor-stub').attributes('data-placeholder')!
    expect(placeholder).not.toContain('{{')
  })

  it('hides the guide panel along with the rest of the script config', () => {
    const wrapper = mountForm()
    expect(wrapper.find('.script-guide').exists()).toBe(false)
  })
})
