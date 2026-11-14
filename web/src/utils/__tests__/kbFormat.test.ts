import { describe, expect, it } from 'vitest'
import { formatLocation, formatSize, statusKind, statusLabel, type TFn } from '../kbFormat'
import type { KBDocument } from '@/composables/useKnowledgeBase'

const t: TFn = (key, named) => `${key}${named ? JSON.stringify(named) : ''}`

function doc(over: Partial<KBDocument>): KBDocument {
  return {
    id: 1,
    collection_id: 1,
    title: 'a.txt',
    size_bytes: 0,
    status: 'ready',
    chunk_count: 0,
    fts_ready: true,
    vector_ready: true,
    created_at: '',
    updated_at: '',
    ...over,
  }
}


describe('formatLocation', () => {
  it('translates every neutral label the server emits', () => {
    expect(formatLocation('p.12', t)).toBe('kb.locationPage{"n":"12"}')
    expect(formatLocation('slide 3', t)).toBe('kb.locationSlide{"n":"3"}')
    expect(formatLocation('sheet 报价 · row 7', t)).toBe('kb.locationSheetRow{"name":"报价","n":"7"}')
    expect(formatLocation('sheet 报价 · rows 1-50', t)).toBe('kb.locationSheetRows{"name":"报价","start":"1","end":"50"}')
    expect(formatLocation('L1-L80', t)).toBe('kb.locationLines{"start":"1","end":"80"}')
    expect(formatLocation('L5', t)).toBe('kb.locationLine{"n":"5"}')
    expect(formatLocation('¶1-¶6', t)).toBe('kb.locationParagraphs{"start":"1","end":"6"}')
    expect(formatLocation('¶2', t)).toBe('kb.locationParagraph{"n":"2"}')
    expect(formatLocation('title', t)).toBe('kb.locationTitle')
  })

  it('passes through unknown labels and empty values', () => {
    expect(formatLocation('', t)).toBe('')
    expect(formatLocation(undefined, t)).toBe('')
    expect(formatLocation('custom-label', t)).toBe('custom-label')
  })
})


describe('status helpers', () => {
  it('maps each state to a badge kind', () => {
    expect(statusKind(doc({ status: 'failed' }))).toBe('failed')
    expect(statusKind(doc({ status: 'pending' }))).toBe('pending')
    expect(statusKind(doc({ status: 'extracting' }))).toBe('working')
    expect(statusKind(doc({ status: 'ready', vector_ready: false }))).toBe('fts')
    expect(statusKind(doc({ status: 'ready', vector_ready: true }))).toBe('ready')
  })

  it('labels each state', () => {
    expect(statusLabel(doc({ status: 'failed' }), t)).toBe('kb.statusFailed')
    expect(statusLabel(doc({ status: 'pending' }), t)).toBe('kb.statusPending')
    expect(statusLabel(doc({ status: 'extracting' }), t)).toBe('kb.statusExtracting')
    expect(statusLabel(doc({ status: 'ready', vector_ready: false }), t)).toBe('kb.statusReadyFts')
    expect(statusLabel(doc({ status: 'ready', vector_ready: true }), t)).toBe('kb.statusReady')
  })
})

describe('formatSize', () => {
  it('formats bytes, KB and MB', () => {
    expect(formatSize(0)).toBe('0 B')
    expect(formatSize(512)).toBe('512 B')
    expect(formatSize(1536)).toBe('1.5 KB')
    expect(formatSize(2 * 1024 * 1024)).toBe('2.0 MB')
  })
})
