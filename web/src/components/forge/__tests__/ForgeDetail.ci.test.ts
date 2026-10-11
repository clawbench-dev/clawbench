import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, nextTick } from 'vue'

/**
 * The CI section of a change request's detail view.
 *
 * Its defining property is that it is LAZY: the run list costs an upstream
 * request, so it must not be fetched until the user expands it. These tests pin
 * that, plus the per-platform field gaps (GitLab's MR-pipeline endpoint reports
 * no timestamp).
 */

const mockItemPipelines = {
  pipelines: ref<unknown[]>([]),
  loading: ref(false),
  error: ref<unknown>(null),
  loaded: ref(false),
  load: vi.fn(),
  reset: vi.fn(),
}

const mockDetail = {
  item: ref<unknown>(null),
  comments: ref<unknown[]>([]),
  loading: ref(false),
  loadingComments: ref(false),
  error: ref<unknown>(null),
  hasMoreComments: ref(false),
  open: vi.fn(),
  loadOlderComments: vi.fn(),
  close: vi.fn(),
}

vi.mock('@/composables/useForge', () => ({
  useForgeDetail: () => mockDetail,
  useForgeItemPipelines: () => mockItemPipelines,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) =>
    params && 'count' in params ? `${key}:${params.count}` : key }),
}))

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({ name, template: `<svg data-icon="${name}" />` })
  return {
    ChevronLeft: stub('ChevronLeft'),
    ChevronRight: stub('ChevronRight'),
    ExternalLink: stub('ExternalLink'),
    MessageSquare: stub('MessageSquare'),
    AlertCircle: stub('AlertCircle'),
    Activity: stub('Activity'),
  }
})

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: () => ({ html: '', detectedPaths: [], detectedSHAs: [] }),
  renderMarkdown: () => ({ html: '', detectedPaths: [], detectedSHAs: [] }),
}))
vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({
    verifyFilePaths: vi.fn(),
    openFilePath: vi.fn(),
    readLineTargetFromEl: () => null,
  }),
}))
vi.mock('@/composables/useCommitHashAnnotation', () => ({ verifyCommitHashes: vi.fn() }))
vi.mock('@/composables/useCodeLinkPreview', () => ({
  useCodeLinkPreview: () => ({ enabled: ref(false) }),
  handleVerifiedFilePathClick: vi.fn(),
}))
vi.mock('@/composables/useLocalhostAnnotation', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: vi.fn() }),
}))
vi.mock('@/composables/useCodeBlockHeader', () => ({
  handleCodeBlockClick: vi.fn(),
  handleTableBlockClick: vi.fn(),
}))
vi.mock('@/composables/useDoubleClickCopy', () => ({ useDoubleClickCopy: () => ({ onDblClick: vi.fn() }) }))
vi.mock('@/composables/useQuoteQuestion', () => ({ useQuoteQuestion: () => ({}) }))
vi.mock('@/utils/quoteQuestionUtils', () => ({ getQuoteSource: () => '' }))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div class="loading-stub" />' },
}))
vi.mock('@/components/file/CodeLinkPreview.vue', () => ({
  default: { name: 'CodeLinkPreview', template: '<div class="code-preview-stub" />' },
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import ForgeDetail from '@/components/forge/ForgeDetail.vue'

function item(overrides: Record<string, unknown> = {}) {
  return {
    platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets',
    type: 'pr', number: 455, title: 'Fix the thing', body: '',
    state: 'open', author: 'octocat', commentCount: 0,
    url: 'https://github.com/acme/widgets/pull/455',
    createdAt: '2026-09-14T10:00:00Z', updatedAt: '2026-09-14T11:00:00Z',
    slug: 'acme/widgets', sourceBranch: 'feat/x',
    ...overrides,
  }
}

function run(overrides: Record<string, unknown> = {}) {
  return {
    platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets',
    id: 7, name: 'CI', number: 3, status: 'failure', ref: 'feat/x',
    sha: 'a91957a', event: 'pull_request', actor: 'octocat',
    url: 'https://github.com/acme/widgets/actions/runs/7',
    createdAt: '2026-09-14T10:00:00Z', updatedAt: '2026-09-14T10:05:00Z',
    durationSeconds: 245, slug: 'acme/widgets',
    ...overrides,
  }
}

function mountDetail(props: Record<string, unknown> = {}) {
  return mount(ForgeDetail, {
    props: { type: 'pr', number: 455, ...props },
  })
}

describe('ForgeDetail CI section', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockItemPipelines.pipelines.value = []
    mockItemPipelines.loading.value = false
    mockItemPipelines.error.value = null
    mockItemPipelines.loaded.value = false
  })

  it('renders the section collapsed and does NOT fetch on mount', async () => {
    // The whole point: opening a PR must not pay for a list most users never
    // expand.
    mockDetail.item.value = item()
    const w = mountDetail()
    await nextTick()

    expect(w.find('.forge-item-ci-header').exists()).toBe(true)
    expect(w.find('.forge-item-ci-body').exists()).toBe(false, 'collapsed by default')
    expect(mockItemPipelines.load).not.toHaveBeenCalled()
  })

  it('fetches on first expand and renders the runs', async () => {
    mockDetail.item.value = item()
    mockItemPipelines.pipelines.value = [run()]
    mockItemPipelines.loaded.value = true

    const w = mountDetail()
    await nextTick()
    await w.find('.forge-item-ci-header').trigger('click')
    await nextTick()

    expect(mockItemPipelines.load).toHaveBeenCalledWith('pr', 455)
    const rows = w.findAll('.forge-item-ci-row')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('CI')
  })

  it('emits open-pipeline with the run id when a row is clicked', async () => {
    // The host owns navigation, so the component's job is to emit the id.
    mockDetail.item.value = item()
    mockItemPipelines.pipelines.value = [run({ id: 77 })]
    mockItemPipelines.loaded.value = true

    const w = mountDetail()
    await nextTick()
    await w.find('.forge-item-ci-header').trigger('click')
    await nextTick()

    await w.find('.forge-item-ci-row').trigger('click')
    expect(w.emitted('open-pipeline')?.[0]).toEqual([77])
  })

  it('shows an empty message rather than an error when there are no runs', async () => {
    mockDetail.item.value = item()
    mockItemPipelines.loaded.value = true
    mockItemPipelines.pipelines.value = []

    const w = mountDetail()
    await nextTick()
    await w.find('.forge-item-ci-header').trigger('click')
    await nextTick()

    expect(w.find('.forge-item-ci-empty').exists()).toBe(true)
    expect(w.find('.forge-item-ci-error').exists()).toBe(false)
  })

  it('shows an error, not an empty state, when the load failed', async () => {
    // "No pipelines" and "could not load" are different facts; conflating them
    // would tell the user their change has no CI.
    mockDetail.item.value = item()
    mockItemPipelines.error.value = { message: 'boom', code: 'ForgeNetworkError' }

    const w = mountDetail()
    await nextTick()
    await w.find('.forge-item-ci-header').trigger('click')
    await nextTick()

    expect(w.find('.forge-item-ci-error').exists()).toBe(true)
    expect(w.find('.forge-item-ci-empty').exists()).toBe(false)
  })

  it('omits the timestamp cell when the platform does not report one', async () => {
    // GitLab's MR-pipeline endpoint returns a reduced payload: no timestamp and
    // no duration. Rendering an empty/zero value there would be a lie.
    mockDetail.item.value = item({ platform: 'gitlab' })
    mockItemPipelines.pipelines.value = [
      run({ platform: 'gitlab', updatedAt: '', durationSeconds: undefined, ref: 'refs/merge-requests/42/head' }),
    ]
    mockItemPipelines.loaded.value = true

    const w = mountDetail()
    await nextTick()
    await w.find('.forge-item-ci-header').trigger('click')
    await nextTick()

    const meta = w.find('.forge-item-ci-meta')
    expect(meta.text()).toContain('refs/merge-requests/42/head', 'the ref is still shown')
    expect(meta.text().trim()).toBe('refs/merge-requests/42/head', 'nothing is invented for the missing time')
  })

  it('hides the section entirely for an issue', async () => {
    // An issue has no branch and therefore no CI; an empty section would be noise.
    mockDetail.item.value = item({ type: 'issue', sourceBranch: undefined })
    const w = mountDetail({ type: 'issue', number: 1 })
    await nextTick()

    expect(w.find('.forge-item-ci').exists()).toBe(false)
  })

  it('resets the CI state when the item changes', async () => {
    // Otherwise the previous PR's runs would show under the new PR.
    mockDetail.item.value = item()
    const w = mountDetail()
    await nextTick()

    await w.setProps({ number: 456 })
    await nextTick()

    expect(mockItemPipelines.reset).toHaveBeenCalled()
    expect(w.find('.forge-item-ci-body').exists()).toBe(false, 'the section collapses again')
  })
})
