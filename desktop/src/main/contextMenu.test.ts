import { describe, it, expect } from 'vitest'
import { contextMenuLabels } from './contextMenu'

describe('contextMenuLabels', () => {
  it('uses Chinese labels for zh locales', () => {
    const labels = contextMenuLabels('zh')
    expect(labels.copyLink).toBe('复制链接')
    expect(labels.copyImage).toBe('复制图片')
  })

  it('uses Chinese labels for zh-CN / zh_TW variants', () => {
    expect(contextMenuLabels('zh-CN').copyLink).toBe('复制链接')
    expect(contextMenuLabels('zh-TW').copyImage).toBe('复制图片')
  })

  it('uses English labels for non-Chinese locales', () => {
    const labels = contextMenuLabels('en')
    expect(labels.copyLink).toBe('Copy Link')
    expect(labels.copyImage).toBe('Copy Image')
  })

  it('handles unknown/empty locale by falling back to English', () => {
    expect(contextMenuLabels('').copyLink).toBe('Copy Link')
    expect(contextMenuLabels('de').copyImage).toBe('Copy Image')
  })
})