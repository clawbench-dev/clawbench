import { describe, it, expect } from 'vitest'
import { makeSpeakerResolver } from '../speakerIdentity'

describe('makeSpeakerResolver', () => {
  it('returns the session agent for an empty agentId', () => {
    const resolve = makeSpeakerResolver({ name: 'Solo', backend: 'claude' }, null)
    expect(resolve('')).toEqual({ name: 'Solo', backend: 'claude' })
  })

  it('returns the mapped member for a non-empty agentId', () => {
    const resolve = makeSpeakerResolver(
      { name: 'Host', backend: 'codebuddy' },
      { m2: { name: 'Alice', backend: 'claude' } },
    )
    expect(resolve('m2')).toEqual({ name: 'Alice', backend: 'claude' })
  })

  it('returns null for an unknown member id (never the session agent)', () => {
    const resolve = makeSpeakerResolver({ name: 'Host', backend: 'codebuddy' }, {})
    expect(resolve('ghost')).toBeNull()
  })

  it('returns null when the session agent has no backend', () => {
    const resolve = makeSpeakerResolver({ name: 'X', backend: '' }, null)
    expect(resolve('')).toBeNull()
  })

  it('returns null when a mapped member has no backend', () => {
    const resolve = makeSpeakerResolver(null, { m1: { name: 'X', backend: '' } })
    expect(resolve('m1')).toBeNull()
  })

  it('returns null when both inputs are absent', () => {
    const resolve = makeSpeakerResolver(null, null)
    expect(resolve('')).toBeNull()
    expect(resolve('m1')).toBeNull()
  })
})
