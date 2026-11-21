import { describe, it, expect, beforeEach, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  addStagedQuote: vi.fn(),
  openComposer: vi.fn(),
  requestAttachmentTarget: vi.fn(),
  toastShow: vi.fn(),
}))

vi.mock('@/composables/useChatContext', () => ({
  useChatContext: () => ({ addStagedQuote: mocks.addStagedQuote }),
}))
vi.mock('@/composables/useQuoteQuestion', () => ({
  useQuoteQuestion: () => ({ openComposer: mocks.openComposer }),
}))
vi.mock('@/composables/useConversationTarget', () => ({
  requestAttachmentTarget: mocks.requestAttachmentTarget,
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mocks.toastShow }),
}))
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string, params?: Record<string, unknown>) => key + (params ? JSON.stringify(params) : ''),
}))

import { setChatRevealHandler, useChatBridge } from '../useChatBridge'
import type { QuoteData } from '../useChatContext'

const PAYLOAD: QuoteData = {
  text: 'deploy failed on step 3',
  filePath: 'acme/widgets#12',
  language: 'issue',
  startLine: 0,
  endLine: 0,
  sourceKind: 'url',
  url: 'https://example.test/acme/widgets/issues/12',
}

describe('useChatBridge', () => {
  let reveal: ReturnType<typeof vi.fn>

  beforeEach(() => {
    mocks.addStagedQuote.mockReset()
    mocks.openComposer.mockReset()
    mocks.requestAttachmentTarget.mockReset()
    mocks.toastShow.mockReset()
    reveal = vi.fn()
    setChatRevealHandler(reveal)
  })

  describe('quote', () => {
    it('stages the payload, reveals the chat and says so', () => {
      useChatBridge().quote(PAYLOAD)

      expect(mocks.addStagedQuote).toHaveBeenCalledWith(PAYLOAD, '')
      expect(reveal).toHaveBeenCalledTimes(1)
      expect(mocks.toastShow).toHaveBeenCalledWith(
        'chat.attach.quotedToChat{"label":"acme/widgets#12"}',
        expect.objectContaining({ type: 'success' }),
      )
    })

    it('carries an annotation when given one', () => {
      useChatBridge().quote(PAYLOAD, { note: 'why did this fail?' })
      expect(mocks.addStagedQuote).toHaveBeenCalledWith(PAYLOAD, 'why did this fail?')
    })

    it('can stage without jumping to the chat', () => {
      useChatBridge().quote(PAYLOAD, { reveal: false })
      expect(reveal).not.toHaveBeenCalled()
      expect(mocks.addStagedQuote).toHaveBeenCalled()
    })

    it('does nothing without a payload', () => {
      useChatBridge().quote(null)
      expect(mocks.addStagedQuote).not.toHaveBeenCalled()
      expect(mocks.toastShow).not.toHaveBeenCalled()
    })

    it('survives a missing reveal handler (unit tests / early boot)', () => {
      setChatRevealHandler(null)
      expect(() => useChatBridge().quote(PAYLOAD)).not.toThrow()
      expect(mocks.addStagedQuote).toHaveBeenCalled()
    })
  })

  describe('comment', () => {
    it('opens the composer with the payload and its label', () => {
      useChatBridge().comment(PAYLOAD)
      expect(mocks.openComposer).toHaveBeenCalledWith(
        { quote: PAYLOAD, label: 'acme/widgets#12' },
        { anchorEl: null },
      )
    })

    it('floats next to the control that opened it', () => {
      const anchor = document.createElement('button')
      useChatBridge().comment(PAYLOAD, { anchorEl: anchor })
      expect(mocks.openComposer).toHaveBeenCalledWith(
        expect.anything(),
        { anchorEl: anchor },
      )
    })

    it('lets the caller override the label', () => {
      useChatBridge().comment(PAYLOAD, { label: 'Deploy log' })
      expect(mocks.openComposer).toHaveBeenCalledWith(
        { quote: PAYLOAD, label: 'Deploy log' },
        { anchorEl: null },
      )
    })

    it('does nothing without a payload', () => {
      useChatBridge().comment(undefined)
      expect(mocks.openComposer).not.toHaveBeenCalled()
    })
  })

  describe('attach', () => {
    it('accepts a single path and asks which conversation (file-manager default)', () => {
      useChatBridge().attach('src/a.ts')
      expect(mocks.requestAttachmentTarget).toHaveBeenCalledWith(
        [{ path: 'src/a.ts', isDir: false }],
        {},
      )
    })

    it('accepts entries and drops unusable ones', () => {
      useChatBridge().attach([
        { path: 'src/a.ts', isDir: false },
        { path: '', isDir: false },
        'docs/b.md',
      ])
      expect(mocks.requestAttachmentTarget).toHaveBeenCalledWith(
        [{ path: 'src/a.ts', isDir: false }, { path: 'docs/b.md', isDir: false }],
        {},
      )
    })

    it('delivers to the active conversation when asked', () => {
      useChatBridge().attach('src/a.ts', { to: 'active' })
      expect(mocks.requestAttachmentTarget).toHaveBeenCalledWith(
        [{ path: 'src/a.ts', isDir: false }],
        { deliverTo: 'active' },
      )
    })

    it('does nothing for an empty input', () => {
      useChatBridge().attach('')
      useChatBridge().attach([])
      useChatBridge().attach(null)
      expect(mocks.requestAttachmentTarget).not.toHaveBeenCalled()
    })
  })
})
