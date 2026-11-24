import { describe, expect, it } from 'vitest'
import { bareModelId, pickDefaultModelId, resolveModelSelection } from '@/utils/modelSelection'

const list = [
  { id: 'claude-4', name: 'Claude 4', default: true },
  { id: 'claude-3-haiku', name: 'Claude 3 Haiku', default: false },
]

describe('pickDefaultModelId', () => {
  it('prefers the preferred model while the list still offers it', () => {
    expect(pickDefaultModelId(list, 'claude-3-haiku')).toBe('claude-3-haiku')
  })

  it('ignores a preferred model the list no longer offers', () => {
    expect(pickDefaultModelId(list, 'claude-3-5-sonnet')).toBe('claude-4')
  })

  it('keeps the preferred model while the list is empty (not discovered yet)', () => {
    expect(pickDefaultModelId([], 'unreported')).toBe('unreported')
  })

  it('falls back to the first entry when none is flagged default', () => {
    expect(pickDefaultModelId([{ id: 'a' }, { id: 'b' }], '')).toBe('a')
  })

  it('returns an empty string when there is nothing to pick', () => {
    expect(pickDefaultModelId([], '')).toBe('')
  })
})

describe('resolveModelSelection', () => {
  it('keeps a candidate the list still offers, with its current display name', () => {
    expect(resolveModelSelection(list, '', 'claude-4')).toEqual({
      modelId: 'claude-4',
      modelName: 'Claude 4',
      retired: false,
    })
  })

  it('retires a candidate a populated list no longer offers', () => {
    expect(resolveModelSelection(list, '', 'claude-3-5-sonnet')).toEqual({
      modelId: 'claude-4',
      modelName: 'Claude 4',
      retired: true,
    })
  })

  it('falls back to the preferred model when the candidate is retired', () => {
    expect(resolveModelSelection(list, 'claude-3-haiku', 'claude-3-5-sonnet')).toEqual({
      modelId: 'claude-3-haiku',
      modelName: 'Claude 3 Haiku',
      retired: true,
    })
  })

  it('keeps an unknown candidate while the list is empty', () => {
    // Empty list = discovery has not reported yet; rewriting on no evidence
    // would silently change a deliberate ACP pin.
    expect(resolveModelSelection([], 'pref', 'acp-model')).toEqual({
      modelId: 'acp-model',
      modelName: 'acp-model',
      retired: false,
    })
  })

  it('uses the supplied name lookup for a candidate missing from an empty list', () => {
    expect(resolveModelSelection([], '', 'acp-model', () => 'ACP Model')).toEqual({
      modelId: 'acp-model',
      modelName: 'ACP Model',
      retired: false,
    })
  })

  it('resolves an empty candidate to the default', () => {
    expect(resolveModelSelection(list, '', '')).toEqual({
      modelId: 'claude-4',
      modelName: 'Claude 4',
      retired: false,
    })
  })

  it('trims surrounding whitespace before matching', () => {
    expect(resolveModelSelection(list, '', '  claude-4  ')).toEqual({
      modelId: 'claude-4',
      modelName: 'Claude 4',
      retired: false,
    })
  })

  it('reports the raw id as the name when the entry carries no name', () => {
    expect(resolveModelSelection([{ id: 'bare' }], '', 'bare')).toEqual({
      modelId: 'bare',
      modelName: 'bare',
      retired: false,
    })
  })
})

describe('provider-scoped ids (issue #753)', () => {
  // The built-in agent lists models from several providers, so its ids carry a
  // `p<providerId>:` scope. A session created before that holds the bare name.
  const scoped = [
    { id: 'p1:gpt-4o', name: 'gpt-4o', default: true },
    { id: 'p2:qwen3', name: 'qwen3', default: false },
  ]

  it('adopts the scoped entry for a bare id when exactly one provider serves it', () => {
    // Otherwise the first reload after the upgrade "retires" a model that is
    // still perfectly runnable and silently moves the session to the default.
    expect(resolveModelSelection(scoped, '', 'gpt-4o')).toEqual({
      modelId: 'p1:gpt-4o',
      modelName: 'gpt-4o',
      retired: false,
    })
  })

  it('does not guess when two providers serve the same name', () => {
    const ambiguous = [
      { id: 'p1:gpt-4o', name: 'gpt-4o', default: true },
      { id: 'p2:gpt-4o', name: 'gpt-4o', default: false },
    ]
    const got = resolveModelSelection(ambiguous, '', 'gpt-4o')
    // Guessing would silently move the session to another provider's endpoint.
    expect(got.retired).toBe(true)
    expect(got.modelId).toBe('p1:gpt-4o')
  })

  it('keeps a scoped candidate that is still listed', () => {
    expect(resolveModelSelection(scoped, '', 'p2:qwen3')).toEqual({
      modelId: 'p2:qwen3',
      modelName: 'qwen3',
      retired: false,
    })
  })

  it('retires a scoped candidate whose provider is gone instead of remapping it', () => {
    // p9 is not in the list at all: the provider was deleted. Falling back to
    // the default is honest; matching by bare name would send the turn to a
    // different provider than the one the user picked.
    const got = resolveModelSelection(scoped, '', 'p9:gpt-4o')
    expect(got.retired).toBe(true)
    expect(got.modelId).toBe('p1:gpt-4o')
  })

  it('leaves an opaque id with a colon alone', () => {
    // A gateway may namespace its own ids with a colon; only `p<digits>:` is
    // our scope.
    expect(bareModelId('openai:gpt-4o')).toBe('openai:gpt-4o')
    expect(bareModelId('p12:model')).toBe('model')
    expect(bareModelId('plain')).toBe('plain')
    expect(bareModelId('')).toBe('')
  })
})
