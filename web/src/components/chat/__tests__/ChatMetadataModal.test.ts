import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    props: ['open', 'zIndex'],
    emits: ['close'],
    template: '<div v-if="open" class="modal-dialog"><slot name="header" /><slot /></div>',
  },
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

// Minimal real i18n so labels resolve to readable text.
const messages = {
  en: {
    chat: {
      metadata: {
        title: 'Message Details',
        sectionOverview: 'Overview',
        sectionTokens: 'Token Usage',
        sectionRuntime: 'Runtime',
        sectionCache: 'Cache & Credit',
        sectionTrace: 'Request Trace',
        sectionIndex: 'Search Index',
        messageId: 'Message ID:',
        inputTokens: 'Input tokens:',
        outputTokens: 'Output tokens:',
        totalTokens: 'Total tokens:',
        cachedReadTokens: 'Cache read:',
        cachedWriteTokens: 'Cache write:',
        cacheHitTokens: 'Cache hit:',
        cacheCreationTokens: 'Cache creation:',
        cacheMissTokens: 'Cache miss:',
        cacheHitRate: 'Cache hit rate:',
        credit: 'Credit:',
        cost: 'Cost:',
        thoughtTokens: 'Thought tokens:',
        requestModelName: 'Request model name:',
        messageRequestId: 'Message request ID:',
        usageByCategory: 'Context breakdown:',
        requestId: 'Request ID:',
        traceId: 'Trace ID:',
        backend: 'Backend:',
        time: 'Time:',
        relatedFile: 'Related file:',
        sessionId: 'Session ID:',
        model: 'Model:',
        wallDuration: 'Wall duration:',
        ftsIndexed: 'FTS Index:',
        vecIndexed: 'Vector Index:',
        indexedYes: 'Yes',
        indexedNo: 'No',
        error: 'Error:',
        unknownError: 'Unknown error',
      },
      sessionInfo: {
        catConversation: 'Conversation',
        catTools: 'Tools',
        catSystemPrompt: 'System Prompt',
        catSkills: 'Skills',
        catMCP: 'MCP',
      },
    },
  },
}

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages,
  missingWarn: false,
  fallbackWarn: false,
})

import ChatMetadataModal from '@/components/chat/ChatMetadataModal.vue'

function mountModal(props: Record<string, unknown> = {}) {
  return mount(ChatMetadataModal, {
    global: { plugins: [i18n] },
    props: {
      show: true,
      data: {},
      ...props,
    },
    attachTo: document.body,
  })
}

describe('ChatMetadataModal', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('renders cache creation / cache miss / credit rows when present', () => {
    const wrapper = mountModal({
      data: {
        inputTokens: 31224,
        outputTokens: 3,
        totalTokens: 31227,
        cacheHitTokens: 9216,
        cacheCreationTokens: 1200,
        cacheMissTokens: 22008,
        credit: 1.57,
      },
    })
    const text = wrapper.text()
    expect(text).toContain('Cache creation:')
    expect(text).toContain('1,200')
    expect(text).toContain('Cache hit:')
    expect(text).toContain('9,216')
    expect(text).toContain('Cache miss:')
    expect(text).toContain('Credit:')
    expect(text).toContain('1.5700')
  })

  it('computes cache hit rate from hit and miss tokens', () => {
    const wrapper = mountModal({
      data: {
        cacheHitTokens: 9216,
        cacheMissTokens: 22008,
      },
    })
    // 9216 / (9216 + 22008) = 29.52%
    expect(wrapper.text()).toContain('Cache hit rate:')
    expect(wrapper.text()).toContain('29.5%')
  })

  it('hides cache hit rate when neither hit nor miss is reported', () => {
    const wrapper = mountModal({
      data: { inputTokens: 100 },
    })
    expect(wrapper.text()).not.toContain('Cache hit rate:')
  })

  it('renders cost with two decimals', () => {
    const wrapper = mountModal({ data: { costUsd: 0.05126895 } })
    const text = wrapper.text()
    expect(text).toContain('Cost:')
    expect(text).toContain('$0.05')
    expect(text).not.toContain('0.051269')
  })

  it('renders sub-cent cost as $0.00', () => {
    const wrapper = mountModal({ data: { costUsd: 0.000036 } })
    expect(wrapper.text()).toContain('$0.00')
  })

  it('renders trace/identity extension fields when present', () => {
    const wrapper = mountModal({
      data: {
        requestModelName: 'GLM-5.1',
        messageRequestId: 'msgreq-abc',
        usageByCategory: { tools: 23293, conversation: 4959 },
      },
    })
    const text = wrapper.text()
    expect(text).toContain('Request model name:')
    expect(text).toContain('GLM-5.1')
    expect(text).toContain('Message request ID:')
    expect(text).toContain('msgreq-abc')
    expect(text).toContain('Tools')
    expect(text).toContain('23,293')
  })

  // ── Section grouping ──
  //
  // The modal groups fields so the rows every backend reports sit above the
  // agent-specific extras. Ordering is the contract, so assert on the section
  // titles in DOM order rather than on individual rows.

  function sectionTitles(wrapper: ReturnType<typeof mountModal>): string[] {
    return wrapper.findAll('.md-section-title').map(el => el.text())
  }

  it('orders universal sections before agent-specific ones', () => {
    const wrapper = mountModal({
      data: {
        model: 'glm-5.1',
        inputTokens: 100,
        wallMs: 1200,
        cacheHitTokens: 40,
        requestId: 'req-1',
      },
    })
    expect(sectionTitles(wrapper)).toEqual([
      'Overview',
      'Token Usage',
      'Runtime',
      'Cache & Credit',
      'Request Trace',
      'Search Index',
    ])
  })

  it('drops agent-specific sections that carry no rows', () => {
    // A plain CLI backend reports model + tokens and nothing else. The cache /
    // trace sections must disappear rather than render empty headers.
    const wrapper = mountModal({ data: { model: 'glm-5.1', inputTokens: 100, outputTokens: 20 } })
    const titles = sectionTitles(wrapper)
    expect(titles).toEqual(['Overview', 'Token Usage', 'Search Index'])
  })

  it('renders input/output/total as metric tiles, not label rows', () => {
    const wrapper = mountModal({ data: { inputTokens: 31224, outputTokens: 3, totalTokens: 31227 } })
    const tiles = wrapper.findAll('.md-tile')
    expect(tiles).toHaveLength(3)
    expect(tiles[0].text()).toContain('Input tokens:')
    expect(tiles[0].text()).toContain('31,224')
    expect(tiles[2].text()).toContain('31,227')
  })

  it('surfaces the error above every section', () => {
    const wrapper = mountModal({ data: { isError: true, errorMessage: 'agent crashed', model: 'x' } })
    const error = wrapper.find('.md-error')
    expect(error.exists()).toBe(true)
    expect(error.text()).toContain('agent crashed')
    // The banner must precede the first section card.
    const content = wrapper.find('.md-content').element
    expect(content.firstElementChild?.classList.contains('md-error')).toBe(true)
  })

  it('renders a related file on its own line (stacked row)', () => {
    const wrapper = mountModal({ relatedFile: 'web/src/a/very/long/path.ts' })
    const stacked = wrapper.findAll('.md-row--stacked')
    expect(stacked.some(row => row.text().includes('web/src/a/very/long/path.ts'))).toBe(true)
  })

  it('marks indexed rows by tone rather than by label text alone', () => {
    const wrapper = mountModal({ ftsIndexed: true, vecIndexed: false })
    expect(wrapper.find('.md-value--ok').text()).toBe('Yes')
    expect(wrapper.find('.md-value--muted').text()).toBe('No')
  })

  it('insets the content from the card edges (rows are not flush left/right)', () => {
    // jsdom has no CSS engine, so this is a source contract. The rows carry a
    // negative inline margin of --space-2 to let their hover fill bleed past
    // the text; with a symmetric --space-4 body padding that left only 4px of
    // real inset and the values read as touching the card edge.
    const source = readFileSync(resolve(__dirname, '../ChatMetadataModal.vue'), 'utf8')
    const rule = source.match(/\.md-content\s*\{([^}]*)\}/)
    expect(rule, '.md-content rule must exist').not.toBeNull()
    const padding = rule![1].match(/padding:\s*([^;]+);/)
    expect(padding, '.md-content must declare padding').not.toBeNull()

    const tokens = padding![1].trim().split(/\s+/)
    const horizontal = tokens[1] ?? tokens[0]
    const scale: Record<string, number> = {
      'var(--space-4)': 8,
      'var(--space-5)': 10,
      'var(--space-6)': 12,
      'var(--space-7)': 16,
      'var(--space-8)': 20,
    }
    expect(scale[horizontal], `horizontal padding ${horizontal} must be a known space token`).toBeDefined()
    // 16px minus the row's 4px negative margin still leaves 12px of visible
    // inset; the old 8px left 4px.
    expect(scale[horizontal]).toBeGreaterThanOrEqual(16)
  })
})
