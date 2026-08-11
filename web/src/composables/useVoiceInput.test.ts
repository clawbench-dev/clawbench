import { describe, it, expect, vi, beforeEach } from 'vitest'

describe('useVoiceInput', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('starts idle', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    expect(v.state.value).toBe('idle')
  })

  it('setState transitions the state machine', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setState('recording')
    expect(v.state.value).toBe('recording')
    v.setState('transcribing')
    expect(v.state.value).toBe('transcribing')
    v.setState('done')
    expect(v.state.value).toBe('done')
  })

  it('appendText appends to inputText with newline separator', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setInputText('你好')
    v.appendText('世界')
    expect(v.inputText.value).toBe('你好\n世界')
  })

  it('appendText ignores empty text', async () => {
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    v.setInputText('你好')
    v.appendText('')
    expect(v.inputText.value).toBe('你好')
  })

  it('toggle with denied mic returns to idle', async () => {
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockRejectedValue(new Error('denied')) },
    })
    const { useVoiceInput } = await import('./useVoiceInput')
    const v = useVoiceInput()
    await v.toggle()
    expect(v.state.value).toBe('idle')
    expect(v.error.value).toBeTruthy()
  })
})
