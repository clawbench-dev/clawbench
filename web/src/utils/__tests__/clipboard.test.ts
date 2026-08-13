import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Mock DOM APIs for clipboard utility
const mockWriteText = vi.fn()
const mockExecCommand = vi.fn()
let mockTextarea: any

beforeEach(() => {
  mockWriteText.mockReset()
  mockExecCommand.mockReset()
  mockWriteText.mockResolvedValue(undefined)
  mockExecCommand.mockReturnValue(true)

  mockTextarea = {
    value: '',
    style: { cssText: '' },
    focus: vi.fn(),
    select: vi.fn(),
  }
})

// Mock navigator.clipboard
Object.defineProperty(globalThis, 'navigator', {
  value: {
    clipboard: {
      writeText: mockWriteText,
    },
  },
  writable: true,
})

// Mock document methods
vi.stubGlobal('document', {
  createElement: vi.fn(() => mockTextarea),
  body: {
    appendChild: vi.fn(),
    removeChild: vi.fn(),
  },
  execCommand: mockExecCommand,
})

import { copyText, readClipboardText } from '@/utils/clipboard.ts'

describe('copyText', () => {
  it('calls navigator.clipboard.writeText with the text', async () => {
    const onSuccess = vi.fn()
    copyText('hello world', onSuccess)

    expect(mockWriteText).toHaveBeenCalledWith('hello world')
    await vi.waitFor(() => {
      expect(onSuccess).toHaveBeenCalled()
    })
  })

  it('calls onSuccess callback on successful copy', async () => {
    const onSuccess = vi.fn()
    copyText('test', onSuccess)

    await vi.waitFor(() => {
      expect(onSuccess).toHaveBeenCalledTimes(1)
    })
  })

  it('uses fallback when clipboard API fails', async () => {
    mockWriteText.mockRejectedValue(new Error('Not allowed'))
    const onSuccess = vi.fn()

    copyText('test', onSuccess)

    // Should try fallback with execCommand
    await vi.waitFor(() => {
      expect(mockExecCommand).toHaveBeenCalledWith('copy')
    })
    // Since fallback succeeds (mockExecCommand returns true), onSuccess should be called
    await vi.waitFor(() => {
      expect(onSuccess).toHaveBeenCalled()
    })
  })

  it('calls onError when clipboard API fails and fallback returns false', async () => {
    mockWriteText.mockRejectedValue(new Error('Not allowed'))
    mockExecCommand.mockReturnValue(false)
    const onError = vi.fn()

    copyText('test', undefined, onError)

    await vi.waitFor(() => {
      expect(onError).toHaveBeenCalled()
    })
  })

  it('calls onError when clipboard API fails and fallback throws', async () => {
    mockWriteText.mockRejectedValue(new Error('Not allowed'))
    mockExecCommand.mockImplementation(() => { throw new Error('exec failed') })
    const onError = vi.fn()

    copyText('test', undefined, onError)

    await vi.waitFor(() => {
      expect(onError).toHaveBeenCalled()
    })
  })

  it('uses fallback when navigator.clipboard is not available', async () => {
    // Save and remove clipboard
    const originalClipboard = globalThis.navigator.clipboard
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: undefined,
      writable: true,
    })

    const onSuccess = vi.fn()
    copyText('test', onSuccess)

    expect(mockExecCommand).toHaveBeenCalledWith('copy')
    // Restore
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: originalClipboard,
      writable: true,
    })
  })

  it('works without callbacks', async () => {
    copyText('test')
    expect(mockWriteText).toHaveBeenCalledWith('test')
  })

  it('handles empty string', async () => {
    const onSuccess = vi.fn()
    copyText('', onSuccess)

    expect(mockWriteText).toHaveBeenCalledWith('')
    await vi.waitFor(() => {
      expect(onSuccess).toHaveBeenCalled()
    })
  })

  it('handles special characters in text', async () => {
    copyText('hello <world> & "quotes"')
    expect(mockWriteText).toHaveBeenCalledWith('hello <world> & "quotes"')
  })

  it('handles long text', async () => {
    const longText = 'x'.repeat(10000)
    copyText(longText)
    expect(mockWriteText).toHaveBeenCalledWith(longText)
  })
})

describe('readClipboardText', () => {
  const mockReadText = vi.fn()
  let originalNative: any

  beforeEach(() => {
    mockReadText.mockReset()
    originalNative = (globalThis as any).ClawBenchNative
  })

  afterEach(() => {
    ;(globalThis as any).ClawBenchNative = originalNative
  })

  it('resolves with the text from navigator.clipboard.readText', async () => {
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { readText: mockReadText },
      writable: true,
    })
    mockReadText.mockResolvedValue('hello from clipboard')

    await expect(readClipboardText()).resolves.toBe('hello from clipboard')
    expect(mockReadText).toHaveBeenCalled()
  })

  it('coerces null/undefined readText result to empty string', async () => {
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { readText: mockReadText },
      writable: true,
    })
    mockReadText.mockResolvedValue(undefined)

    await expect(readClipboardText()).resolves.toBe('')
  })

  it('falls back to execCommand paste when clipboard API is unavailable', async () => {
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: undefined,
      writable: true,
    })
    mockTextarea.value = 'fallback text'
    mockExecCommand.mockReturnValue(true)

    await expect(readClipboardText()).resolves.toBe('fallback text')
    expect(mockExecCommand).toHaveBeenCalledWith('paste')
  })

  it('rejects when execCommand paste returns false', async () => {
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: undefined,
      writable: true,
    })
    mockExecCommand.mockReturnValue(false)

    await expect(readClipboardText()).rejects.toThrow('clipboard read unsupported')
  })

  it('prefers the native ClawBenchNative.readClipboardText bridge', async () => {
    const nativeRead = vi.fn().mockReturnValue('native clipboard text')
    ;(globalThis as any).ClawBenchNative = { readClipboardText: nativeRead }

    await expect(readClipboardText()).resolves.toBe('native clipboard text')
    expect(nativeRead).toHaveBeenCalled()
  })

  it('falls through when native bridge readClipboardText is missing', async () => {
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { readText: mockReadText },
      writable: true,
    })
    ;(globalThis as any).ClawBenchNative = {}
    mockReadText.mockResolvedValue('fallback')

    await expect(readClipboardText()).resolves.toBe('fallback')
    expect(mockReadText).toHaveBeenCalled()
  })
})
