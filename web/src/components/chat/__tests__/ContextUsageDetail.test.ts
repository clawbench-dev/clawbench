import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ContextUsageDetail from '../ContextUsageDetail.vue'

/**
 * ContextUsageDetail is the shared body of BOTH the single-chat context popup
 * and the group member panel. Its job is to render exactly the rows the
 * producer reported and to hide the rest — so each case below asserts one
 * section's presence/absence rather than a whole snapshot.
 */
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        sessionInfo: {
          used: 'Used',
          size: 'Size',
          remaining: 'Remaining',
          tokenDetail: 'Token detail',
          inputTokens: 'Input',
          outputTokens: 'Output',
          tokenRate: 'Output speed',
          tokenRateHint: 'Latest reply generated for {seconds}s',
          tokenRateHintNoDuration: 'Latest reply speed',
          totalTokens: 'Total',
          cachedReadTokens: 'Cache read',
          cacheHitTokens: 'Cache hit',
          cacheHitRate: 'Hit rate',
          cachedWriteTokens: 'Cache write',
          cacheCreationTokens: 'Cache create',
          cacheMissTokens: 'Cache miss',
          thoughtTokens: 'Thinking',
          credit: 'Credit',
          contextCost: 'Cost',
          categoryBreakdown: 'Breakdown',
          catConversation: 'Conversation',
          catTools: 'Tools',
          compact: 'Compact context',
          compacting: 'Compacting…',
        },
      },
    },
  },
})

function mountDetail(props: Record<string, unknown> = {}) {
  return mount(ContextUsageDetail, {
    props: { usage: { used: 50000, size: 200000 }, ...props },
    global: { plugins: [i18n] },
  })
}

describe('ContextUsageDetail', () => {
  it('renders the bar, percentage and the used/size/remaining rows', () => {
    const w = mountDetail()
    expect(w.find('.usage-popup-pct').text()).toBe('25%')
    // 50000 / 200000
    expect(w.text()).toContain('50,000')
    expect(w.text()).toContain('200,000')
    expect(w.text()).toContain('150,000')
  })

  it('clamps the bar width at 100% but keeps the reported percentage', () => {
    const w = mountDetail({ usage: { used: 300000, size: 200000 } })
    const style = w.find('.usage-popup-bar-fill').attributes('style') || ''
    expect(style).toContain('width: 100%')
    expect(w.find('.usage-popup-pct').text()).toBe('150%')
  })

  it('hides the Token detail section when only used/size are reported', () => {
    const w = mountDetail()
    expect(w.text()).not.toContain('Token detail')
  })

  it('groups token rows under the Token detail header', () => {
    const w = mountDetail({
      usage: { used: 1000, size: 200000, inputTokens: 29495, outputTokens: 3, thoughtTokens: 12 },
    })
    expect(w.text()).toContain('Token detail')
    expect(w.text()).toContain('29,495')
    expect(w.text()).toContain('Thinking')
  })

  it('shows the cache hit rate only when cache stats exist', () => {
    expect(mountDetail({ usage: { used: 1, size: 100 } }).text()).not.toContain('Hit rate')
    const w = mountDetail({ usage: { used: 1, size: 100, cacheHitTokens: 99, cacheMissTokens: 1 } })
    expect(w.text()).toContain('Hit rate')
    expect(w.text()).toContain('99.0%')
  })

  it('places cache hit rate immediately after output speed and avoids duplicates', () => {
    const w = mountDetail({
      usage: {
        used: 238591,
        size: 1000000,
        inputTokens: 238591,
        outputTokens: 2152,
        totalTokens: 240743,
        cachedReadTokens: 235640,
      },
      tokenRate: { tokensPerSecond: 42.5, genMs: 10000 },
    })
    const labels = w.findAll('.usage-popup-label').map(el => el.text())
    const rateIdx = labels.indexOf('Output speed')
    const hitRateIdx = labels.indexOf('Hit rate')
    expect(rateIdx).toBeGreaterThan(-1)
    expect(hitRateIdx).toBe(rateIdx + 1)
    expect(w.text()).toContain('98.8%')

    // Even when an agent already reports cacheHitTokens/cacheMissTokens alongside
    // cachedReadTokens, Hit rate renders exactly once right after Output speed.
    const both = mountDetail({
      usage: {
        used: 326400,
        size: 1000000,
        inputTokens: 329384,
        outputTokens: 500,
        totalTokens: 329884,
        cachedReadTokens: 326400,
        cacheHitTokens: 326400,
        cacheMissTokens: 2984,
      },
      tokenRate: { tokensPerSecond: 30, genMs: 5000 },
    })
    const bothLabels = both.findAll('.usage-popup-label').map(el => el.text())
    expect(bothLabels.filter(l => l === 'Hit rate')).toHaveLength(1)
    expect(bothLabels.indexOf('Hit rate')).toBe(bothLabels.indexOf('Output speed') + 1)
  })

  it('renders the cost with the reported currency only', () => {
    const usd = mountDetail({ usage: { used: 1, size: 100, cost: 0.0512, currency: 'USD' } })
    expect(usd.text()).toContain('$0.05')

    // No currency: bare number, never a fabricated dollar sign.
    const bare = mountDetail({ usage: { used: 1, size: 100, cost: 1.43 } })
    expect(bare.text()).toContain('1.43')
    expect(bare.text()).not.toContain('$')
  })

  it('renders the context breakdown by value', () => {
    const w = mountDetail({ usage: { used: 1, size: 100, usageByCategory: { tools: 22701, conversation: 3894 } } })
    expect(w.text()).toContain('Breakdown')
    expect(w.text()).toContain('Tools')
    expect(w.text()).toContain('22,701')
  })

  it('shows the token rate row only when the backend measured it', () => {
    const measured = mountDetail({
      usage: { used: 1000, size: 200000, outputTokens: 500 },
      tokenRate: { tokensPerSecond: 42.46, genMs: 11800 },
    })
    expect(measured.text()).toContain('Output speed')
    expect(measured.text()).toContain('42.5 tok/s')
    // The tooltip explains the denominator, so the number cannot be mistaken
    // for a wall-clock throughput that includes tool execution.
    const value = measured.findAll('.usage-popup-value').find(v => v.text().includes('tok/s'))
    expect(value?.attributes('title')).toBe('Latest reply generated for 11.8s')

    // Not measured (or no output at all): the row must not render "0 tok/s".
    expect(mountDetail({
      usage: { used: 1000, size: 200000, outputTokens: 500 },
      tokenRate: null,
    }).text()).not.toContain('Output speed')
  })

  it('explains the measurement without a window when only a rate is known', () => {
    const w = mountDetail({
      usage: { used: 1, size: 100, outputTokens: 100 },
      tokenRate: { tokensPerSecond: 20 },
    })
    const value = w.findAll('.usage-popup-value').find(v => v.text().includes('tok/s'))
    expect(value?.attributes('title')).toBe('Latest reply speed')
  })

  it('shows the compact action only when asked (single chat), never for a member', () => {
    expect(mountDetail().find('.usage-popup-compact-btn').exists()).toBe(false)
    const w = mountDetail({ showCompact: true })
    expect(w.find('.usage-popup-compact-btn').exists()).toBe(true)
    w.find('.usage-popup-compact-btn').trigger('click')
    expect(w.emitted('compact')).toHaveLength(1)
  })

  it('shows a spinner and blocks a second click while compacting', async () => {
    // The agent round-trip takes tens of seconds. Without a busy state the
    // button looks inert after the click (the reported "no reaction" defect),
    // and a second click would queue a duplicate compaction.
    const w = mountDetail({ showCompact: true })
    expect(w.find('.usage-popup-compact-btn .li-spinner').exists()).toBe(false)

    await w.setProps({ compacting: true })
    const btn = w.find('.usage-popup-compact-btn')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.find('.li-spinner').exists()).toBe(true)
    expect(btn.text()).toContain('Compacting')

    await btn.trigger('click')
    expect(w.emitted('compact')).toBeFalsy()
  })

  it('renders zeros rather than throwing when usage is null', () => {
    const w = mountDetail({ usage: null })
    expect(w.find('.usage-popup-pct').text()).toBe('0%')
    expect(w.text()).toContain('0')
  })
})
