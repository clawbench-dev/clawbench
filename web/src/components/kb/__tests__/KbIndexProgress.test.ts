import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import KbIndexProgress from '@/components/kb/KbIndexProgress.vue'
import type { KBStats } from '@/composables/useKnowledgeBase'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      kb: {
        indexOffHint: 'Keyword search only',
        indexDocsProgress: 'Parsing documents {done} / {total}',
        indexVectorsProgress: 'Text embedding {done} / {total} chunks',
        indexFailedCount: '{count} failed',
        indexReady: 'Index ready',
      },
    },
  },
})

function stats(over: Partial<KBStats> = {}): KBStats {
  return {
    collections: 1,
    documents: 5,
    ready_docs: 2,
    failed_docs: 0,
    pending_docs: 3,
    chunks: 10,
    embedded_chunks: 4,
    stored_bytes: 1024,
    ...over,
  }
}

function mountProgress(props: {
  stats: KBStats | null
  vectorEnabled: boolean
  compact?: boolean
}) {
  return mount(KbIndexProgress, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('KbIndexProgress', () => {
  it('shows both phases with their own denominators', () => {
    const w = mountProgress({ stats: stats(), vectorEnabled: true })

    const docs = w.find('[data-kb-progress="docs"]')
    const vectors = w.find('[data-kb-progress="vectors"]')
    expect(docs.exists()).toBe(true)
    expect(vectors.exists()).toBe(true)

    // "Done" for extraction is documents that left the queue, not ready ones:
    // failed documents are done too, and counting only ready_docs would leave
    // the bar stuck whenever something fails.
    expect(docs.text()).toContain('Parsing documents 2 / 5')
    expect(vectors.text()).toContain('Text embedding 4 / 10 chunks')

    const widths = w.findAll('.kb-progress__bar').map((b) => (b.element as HTMLElement).style.width)
    expect(widths).toEqual(['40%', '40%'])
    expect(w.findAll('.kb-progress__bar.is-active')).toHaveLength(2)
  })

  it('does not animate a bar whose phase has nothing left to do', () => {
    const w = mountProgress({
      stats: stats({ documents: 5, pending_docs: 0, ready_docs: 5, chunks: 10, embedded_chunks: 4 }),
      vectorEnabled: true,
    })
    const [docsBar, vectorsBar] = w.findAll('.kb-progress__bar')
    expect(docsBar.classes()).not.toContain('is-active')
    expect(vectorsBar.classes()).toContain('is-active')
  })

  it('reports failures next to the extraction phase', () => {
    const w = mountProgress({ stats: stats({ failed_docs: 2 }), vectorEnabled: true })
    expect(w.find('[data-kb-progress="docs"]').text()).toContain('2 failed')
  })

  it('says keyword-only instead of drawing a vector bar with no embedder', () => {
    const w = mountProgress({ stats: stats(), vectorEnabled: false })
    expect(w.text()).toContain('Keyword search only')
    expect(w.find('[data-kb-progress="vectors"]').exists()).toBe(false)
  })

  it('keeps the full picture on the settings card when idle', () => {
    const w = mountProgress({
      stats: stats({ documents: 3, pending_docs: 0, ready_docs: 3, chunks: 6, embedded_chunks: 6 }),
      vectorEnabled: true,
    })
    expect(w.find('[data-kb-progress="docs"]').exists()).toBe(true)
    expect(w.find('[data-kb-progress="vectors"]').exists()).toBe(true)
    expect(w.text()).toContain('Index ready')
  })

  it('hides completed rows and the whole block when compact and idle', () => {
    const idle = mountProgress({
      stats: stats({ documents: 3, pending_docs: 0, ready_docs: 3, chunks: 6, embedded_chunks: 6 }),
      vectorEnabled: true,
      compact: true,
    })
    expect(idle.find('.kb-progress').exists()).toBe(false)

    // Still compact, but the embedding phase is behind: show only that row.
    const vectorsOnly = mountProgress({
      stats: stats({ documents: 3, pending_docs: 0, ready_docs: 3, chunks: 6, embedded_chunks: 2 }),
      vectorEnabled: true,
      compact: true,
    })
    expect(vectorsOnly.find('[data-kb-progress="docs"]').exists()).toBe(false)
    expect(vectorsOnly.find('[data-kb-progress="vectors"]').exists()).toBe(true)
  })

  it('renders nothing before the first status response', () => {
    const w = mountProgress({ stats: null, vectorEnabled: true })
    expect(w.find('.kb-progress').exists()).toBe(false)
  })
})
