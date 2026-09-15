import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ForgePipelineDetail from '@/components/forge/ForgePipelineDetail.vue'

const detailState = {
  run: { value: null as unknown },
  jobs: { value: [] as unknown[] },
  loading: { value: false },
  error: { value: null as unknown },
  open: vi.fn(),
  close: vi.fn(),
}

vi.mock('@/composables/useForge', () => ({
  useForgePipelineDetail: () => detailState,
}))

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'en',
    messages: {
      en: {
        forge: {
          loading: 'Loading',
          error: { generic: 'Failed', auth: 'Auth', rateLimit: 'Rate', network: 'Net' },
          pipeline: {
            status: { success: 'Success', failure: 'Failed', running: 'Running', cancelled: 'Cancelled', skipped: 'Skipped', unknown: 'Unknown' },
            emptyJobs: 'No job information',
            noPlatform: 'No pipelines on this platform',
            loadFailed: 'Failed to load',
            jobName: 'Job', jobStage: 'Stage', jobStatus: 'Status', jobDuration: 'Duration',
            runNumber: 'Run', event: 'Trigger', duration: 'Duration', jobs: 'Jobs',
            linkedPrs: 'Linked pull request',
            openPr: 'Open pull request #{number}',
            openRun: 'Open in browser', quote: 'Quote in chat',
            detail: { back: 'Back' },
          },
        },
      },
    },
  })
}

const globalOpts = {
  plugins: [makeI18n()],
  stubs: { LoadingIndicator: true },
}

function run(overrides: Record<string, unknown> = {}) {
  return {
    platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets',
    id: 42, name: 'CI', number: 7, status: 'failure', ref: 'main',
    sha: 'a91957a858320c0e17f3a0eca7cfacbff50ea29a', event: 'push',
    actor: 'octocat', url: 'https://ci/run/42',
    createdAt: '2026-09-14T10:00:00Z', updatedAt: '2026-09-14T10:05:00Z',
    durationSeconds: 245, slug: 'acme/widgets',
    ...overrides,
  }
}

describe('ForgePipelineDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    detailState.run.value = null
    detailState.jobs.value = []
    detailState.loading.value = false
    detailState.error.value = null
  })

  it('loads the requested run on mount', () => {
    mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    expect(detailState.open).toHaveBeenCalledWith(42)
  })

  it('renders the run metadata and the job table', async () => {
    detailState.run.value = run()
    detailState.jobs.value = [
      { id: 1, name: 'build', status: 'success', url: 'u', durationSeconds: 30 },
      { id: 2, name: 'test', status: 'failure', url: 'u', stage: 'test', failureReason: 'script_failure' },
    ]
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.forge-detail-title').text()).toBe('CI')
    // The run number, ref, actor and status all describe the run.
    const meta = wrapper.find('.forge-detail-meta').text()
    expect(meta).toContain('#7')
    expect(meta).toContain('main')
    expect(meta).toContain('octocat')
    expect(meta).toContain('Failed')

    // The job table is the reason the view exists: a run's failure is located
    // by which job failed.
    const rows = wrapper.findAll('.forge-pipeline-job-row')
    // One header row plus two jobs.
    expect(rows).toHaveLength(3)
    expect(rows[1].text()).toContain('build')
    expect(rows[2].text()).toContain('test')
    // GitLab's failure_reason is surfaced so the user knows how it failed.
    expect(rows[2].text()).toContain('script_failure')
  })

  it('shows a placeholder when the platform reports no stage', async () => {
    // GitHub has no stage concept, so the column must degrade gracefully rather
    // than rendering an empty cell.
    detailState.run.value = run()
    detailState.jobs.value = [{ id: 1, name: 'build', status: 'success', url: 'u' }]
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    const jobRow = wrapper.findAll('.forge-pipeline-job-row')[1]
    expect(jobRow.find('.col-stage').text()).toBe('—')
  })

  it('explains an empty job list instead of showing a bare table', async () => {
    detailState.run.value = run()
    detailState.jobs.value = []
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.forge-pipeline-jobs-empty').text()).toBe('No job information')
  })

  it('renders a duration only when the platform reported one', async () => {
    detailState.run.value = run({ durationSeconds: 245 })
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.forge-pipeline-duration').text()).toContain('4m 5s')

    // GitHub's list endpoint reports no duration; the line must be absent
    // rather than showing "0s".
    detailState.run.value = run({ durationSeconds: undefined })
    const wrapper2 = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper2.vm.$nextTick()
    expect(wrapper2.find('.forge-pipeline-duration').exists()).toBe(false)
  })

  it('distinguishes "platform has no CI" from a generic failure', async () => {
    detailState.error.value = { message: 'nope', code: 'ForgeNoPipelines' }
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.forge-error-title').text()).toBe('No pipelines on this platform')

    detailState.error.value = { message: 'boom', code: 'ForgeError' }
    const wrapper2 = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper2.vm.$nextTick()
    expect(wrapper2.find('.forge-error-title').text()).toBe('Failed')
  })

  it('emits the run when quoting into chat', async () => {
    detailState.run.value = run()
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    await wrapper.find('.forge-detail-actions button').trigger('click')

    expect(wrapper.emitted('quote')?.[0][0]).toMatchObject({ id: 42, name: 'CI' })
  })

  it('links out to the run in the browser', async () => {
    detailState.run.value = run()
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    const link = wrapper.find('.forge-detail-actions a')
    expect(link.attributes('href')).toBe('https://ci/run/42')
    expect(link.attributes('target')).toBe('_blank')
  })

  it('emits back from the header', async () => {
    detailState.run.value = run()
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    await wrapper.find('.forge-back').trigger('click')
    expect(wrapper.emitted('back')).toHaveLength(1)
  })

  it('renders a linked pull request and emits open-pr on click', async () => {
    // Opening the PR is what makes the association useful; the host owns the
    // navigation, so this component's job is to emit the number.
    detailState.run.value = run({
      pullRequests: [{ number: 455, title: 'Fix the thing', url: 'https://github.com/acme/widgets/pull/455' }],
    })
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    const rows = wrapper.findAll('.forge-pipeline-pr')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('Fix the thing')

    await rows[0].trigger('click')
    expect(wrapper.emitted('open-pr')?.[0]).toEqual([455])
  })

  it('renders the number when the platform supplies no title', async () => {
    // GitLab's pipeline payload has no MR title, so the number is the honest
    // label rather than an empty row.
    detailState.run.value = run({ pullRequests: [{ number: 42 }] })
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    const rows = wrapper.findAll('.forge-pipeline-pr')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('#42')
  })

  it('shows no linked-PR section when the run has none', async () => {
    // The field is absent (not empty) for a push-to-branch run, and an empty
    // section would read as a rendering bug.
    detailState.run.value = run()
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.forge-pipeline-prs').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Linked pull request')
  })

  it('renders every linked pull request, not just the first', async () => {
    // One run can belong to several PRs (same commit, multiple open PRs).
    detailState.run.value = run({
      pullRequests: [{ number: 455, title: 'First' }, { number: 456, title: 'Second' }],
    })
    const wrapper = mount(ForgePipelineDetail, { props: { runId: 42 }, global: globalOpts })
    await wrapper.vm.$nextTick()

    const rows = wrapper.findAll('.forge-pipeline-pr')
    expect(rows).toHaveLength(2)
    expect(rows[1].text()).toContain('Second')

    await rows[1].trigger('click')
    expect(wrapper.emitted('open-pr')?.[0]).toEqual([456])
  })
})
