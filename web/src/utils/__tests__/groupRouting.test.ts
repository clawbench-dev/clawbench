import { describe, expect, it } from 'vitest'
import {
  parseGroupRouting,
  stripGroupBccSpans,
  stripGroupProtocolTags,
  renderMentionChips,
  buildMemberCandidates,
  buildMentionTag,
  serializeMentionCards,
  resolveMentionDisplayName,
  MENTION_CHIP_CLASS,
} from '@/utils/groupRouting.ts'

describe('parseGroupRouting before/after (prose around mentions)', () => {
  it('is empty when the mention leads the message', () => {
    const r = parseGroupRouting('<clawbench-mention targets="A">请谈谈</clawbench-mention>')
    expect(r.found).toBe(true)
    expect(r.before).toBe('')
    expect(r.after).toBe('')
  })

  it('returns the text preceding the mention', () => {
    const r = parseGroupRouting('A 的观点不错 <clawbench-mention targets="B">请回应</clawbench-mention>')
    expect(r.found).toBe(true)
    expect(r.before).toBe('A 的观点不错')
  })

  it('returns the text following the mention', () => {
    const r = parseGroupRouting('<clawbench-mention targets="B">请回应</clawbench-mention> 大家补充')
    expect(r.after).toBe('大家补充')
  })

  it('trims surrounding whitespace around the background', () => {
    const r = parseGroupRouting('\n  背景上下文 \n <clawbench-mention targets="A">请表态</clawbench-mention>')
    expect(r.before).toBe('背景上下文')
  })

  it('is empty when no tag is present', () => {
    const r = parseGroupRouting('普通发言，没有标签')
    expect(r.found).toBe(false)
    expect(r.before).toBe('')
  })

  it('still slices the background ahead of a malformed tag (never strips)', () => {
    const r = parseGroupRouting('背景在此 <clawbench-mention targets=""></clawbench-mention>')
    expect(r.found).toBe(false)
    expect(r.before).toBe('背景在此')
  })
})

describe('parseGroupRouting mentions', () => {
  it('parses a public mention: targets + body', () => {
    const r = parseGroupRouting('<clawbench-mention targets="A,B">请分别表态</clawbench-mention>')
    expect(r.found).toBe(true)
    expect(r.speakers).toEqual(['A', 'B'])
    expect(r.instruction).toBe('请分别表态')
  })

  it('de-duplicates targets across several public mentions', () => {
    const r = parseGroupRouting(
      '<clawbench-mention targets="A">先说</clawbench-mention><clawbench-mention targets="A,B">再说</clawbench-mention>',
    )
    expect(r.speakers).toEqual(['A', 'B'])
    expect(r.instruction).toBe('先说\n\n再说')
  })
})

describe('parseGroupRouting private notes', () => {
  it('extracts a note and removes it from the public instruction', () => {
    const r = parseGroupRouting(
      '<clawbench-mention targets="A">请表态</clawbench-mention><clawbench-mention targets="A" private>你重点看性能</clawbench-mention>',
    )
    expect(r.bcc).toEqual([{ targets: ['A'], content: '你重点看性能' }])
    expect(r.instruction).toBe('请表态')
    expect(r.instruction).not.toContain('性能')
  })

  it('splits and trims multiple targets', () => {
    const r = parseGroupRouting('<clawbench-mention targets="A, B ,C" private>都注意</clawbench-mention>')
    expect(r.bcc[0].targets).toEqual(['A', 'B', 'C'])
  })

  it('does not leak a note placed before the public mention into before', () => {
    const r = parseGroupRouting(
      '<clawbench-mention targets="A" private>私下话</clawbench-mention>背景在此 <clawbench-mention targets="A">请回应</clawbench-mention>',
    )
    expect(r.before).not.toContain('私下话')
    expect(r.before).toContain('背景在此')
    expect(r.bcc).toEqual([{ targets: ['A'], content: '私下话' }])
  })

  it('keeps multiple notes in order', () => {
    const r = parseGroupRouting(
      '<clawbench-mention targets="A,B">表态</clawbench-mention>' +
        '<clawbench-mention targets="A" private>给A一</clawbench-mention>' +
        '<clawbench-mention targets="B" private>给B二</clawbench-mention>',
    )
    expect(r.bcc.map((e) => e.content)).toEqual(['给A一', '给B二'])
  })

  it('leaves a malformed note unparsed and untouched', () => {
    expect(parseGroupRouting('<clawbench-mention private>无 targets</clawbench-mention>').bcc).toEqual([])
    expect(parseGroupRouting('<clawbench-mention targets="" private>空名单</clawbench-mention>').bcc).toEqual([])
    expect(parseGroupRouting("<clawbench-mention targets='A' private>单引号</clawbench-mention>").bcc).toEqual([])
  })

  it('does not treat an end tag inside a note as a discussion end', () => {
    const r = parseGroupRouting(
      '<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention targets="A" private>如果没意见就 <clawbench-group-end/> 收尾</clawbench-mention>',
    )
    expect(r.end).toBe(false)
    expect(r.bcc[0].content).toContain('收尾')
  })
})

describe('stripGroupProtocolTags (public unwrap + private drop)', () => {
  it('removes a private mention and keeps the public body', () => {
    expect(stripGroupProtocolTags('<clawbench-mention targets="A">公开</clawbench-mention><clawbench-mention targets="A" private>私</clawbench-mention>')).toBe('公开')
  })

  it('returns the text unchanged when there is no tag', () => {
    expect(stripGroupProtocolTags('no tag here')).toBe('no tag here')
  })
})

describe('parseGroupRouting fail-closed (injection boundary)', () => {
  const cases: Array<[string, string]> = [
    ['single quotes', `<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention targets='A' private>秘密一</clawbench-mention>`],
    ['blank targets', `<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention targets="   " private>秘密二</clawbench-mention>`],
    ['uppercase attr', `<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention Targets="A" private>秘密三</clawbench-mention>`],
    ['unclosed', `<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention targets="A" private>未闭合秘密`],
    ['nbsp separator', `<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention\u00A0targets="A" private>秘密五</clawbench-mention>`],
  ]
  for (const [name, text] of cases) {
    it(`strips ${name} from the instruction`, () => {
      const r = parseGroupRouting(text)
      expect(r.instruction).not.toContain('秘密')
      expect(r.instruction).not.toContain('clawbench-mention')
    })
  }
})

describe('stripGroupBccSpans', () => {
  it('removes well-formed and malformed spans', () => {
    expect(stripGroupBccSpans('前 <clawbench-mention targets="A">良</clawbench-mention> 后')).toBe('前  后')
    expect(stripGroupBccSpans("前 <clawbench-mention targets='A'>畸</clawbench-mention> 后")).toBe('前  后')
  })

  it('drops an unclosed tag and its tail', () => {
    expect(stripGroupBccSpans('前 <clawbench-mention targets="A">未闭合')).toBe('前')
  })

  it('returns text unchanged when there is no tag', () => {
    expect(stripGroupBccSpans('no tag here')).toBe('no tag here')
  })
})

describe('stripGroupProtocolTags', () => {
  it('unwraps public mentions and drops private ones', () => {
    expect(
      stripGroupProtocolTags('<clawbench-mention targets="A">公开</clawbench-mention><clawbench-mention targets="A" private>私</clawbench-mention>'),
    ).toBe('公开')
  })

  it('drops the end signal', () => {
    expect(stripGroupProtocolTags('收尾 <clawbench-group-end/>')).toBe('收尾')
  })

  // Fail-closed on an UNCLOSED mention: a truncated private note (turn
  // cancelled/timed out mid-stream) must not survive as prose. This diverged
  // from the Go side once and leaked the note into the quoted text, which is
  // then inlined into every member's prompt.
  it('drops an unclosed mention and its tail (fail-closed)', () => {
    expect(stripGroupProtocolTags('前 <clawbench-mention targets="A" private>SECRET')).toBe('前')
    expect(stripGroupProtocolTags('a PUB b <clawbench-mention targets="A" private>SECRET')).toBe('a PUB b')
  })
})

describe('renderMentionChips', () => {
  it('turns a public mention into an inline chip followed by its body', () => {
    const html = renderMentionChips('<clawbench-mention targets="B">请回应</clawbench-mention>')
    expect(html).toContain(`<span class="${MENTION_CHIP_CLASS}">@B</span>`)
    expect(html).toContain('请回应')
    expect(html).not.toContain('clawbench-mention')
  })

  it('renders one chip per target', () => {
    const html = renderMentionChips('<clawbench-mention targets="A,B">表态</clawbench-mention>')
    expect(html).toContain('@A')
    expect(html).toContain('@B')
  })

  it('drops a private mention entirely', () => {
    const html = renderMentionChips('<clawbench-mention targets="A">公开</clawbench-mention><clawbench-mention targets="A" private>秘密</clawbench-mention>')
    expect(html).toContain('公开')
    expect(html).not.toContain('秘密')
  })

  it('drops the end signal', () => {
    expect(renderMentionChips('收尾 <clawbench-group-end/>')).toBe('收尾')
  })

  it('escapes a target name so it cannot inject markup', () => {
    const html = renderMentionChips('<clawbench-mention targets="a<b&c">x</clawbench-mention>')
    expect(html).not.toContain('<b')
    expect(html).toContain('&lt;b')
    expect(html).toContain('&amp;')
  })

  it('leaves a malformed (unparseable) tag verbatim (never lose content)', () => {
    const malformed = '<clawbench-mention targets="">空</clawbench-mention>'
    expect(renderMentionChips(malformed)).toBe(malformed)
  })

  it('resolves a target id to a display name through the resolver', () => {
    const html = renderMentionChips(
      '<clawbench-mention targets="m-b"></clawbench-mention>',
      (t) => (t === 'm-b' ? 'Bob' : t),
    )
    expect(html).toContain('@Bob')
    expect(html).not.toContain('m-b')
  })
})

describe('resolveMentionDisplayName', () => {
  const byId = (t: string) => (t === 'm-b' ? { name: 'Bob' } : null)
  const byName = (t: string) => (t === 'Alice' ? { name: 'Alice' } : null)

  it('renders the reserved human name as the reader label, never the raw token', () => {
    // "User" must resolve to the label even when the resolvers would (wrongly)
    // claim it, and even when there is no roster (both resolvers null).
    expect(resolveMentionDisplayName('User', byId, byName, '你')).toBe('你')
    expect(resolveMentionDisplayName('User', null, null, 'you')).toBe('you')
  })

  it('renders a configured nickname for the reserved human target (5th arg)', () => {
    // With a nickname configured, both the display label AND the matched token
    // are the nickname — the raw "User" is no longer special (only-new-nickname
    // semantics: a historical "User" chip falls through to the raw token).
    expect(resolveMentionDisplayName('老板', byId, byName, '老板', '老板')).toBe('老板')
    expect(resolveMentionDisplayName('User', byId, byName, '老板', '老板')).toBe('User')
    // The default 5th arg keeps the pre-setting behavior intact.
    expect(resolveMentionDisplayName('User', byId, byName, '老板')).toBe('老板')
  })

  it('resolves by member row id, then by display name', () => {
    expect(resolveMentionDisplayName('m-b', byId, byName, '你')).toBe('Bob')
    expect(resolveMentionDisplayName('Alice', byId, byName, '你')).toBe('Alice')
  })

  it('falls back to the raw target when nothing resolves (intent stays visible)', () => {
    expect(resolveMentionDisplayName('ghost', byId, byName, '你')).toBe('ghost')
    expect(resolveMentionDisplayName('ghost', null, null, '你')).toBe('ghost')
  })
})

describe('buildMemberCandidates', () => {
  const members = [
    { id: 'm-a', name: 'Alice' },
    { id: 'm-b', name: 'Bob' },
    { id: 'm-gone', name: 'Gone', left: true },
  ]

  it('lists every active member for an empty query', () => {
    expect(buildMemberCandidates(members, '').map((c) => c.label)).toEqual(['Alice', 'Bob'])
  })

  it('filters by case-insensitive substring', () => {
    expect(buildMemberCandidates(members, 'bo').map((c) => c.label)).toEqual(['Bob'])
  })

  it('excludes left members', () => {
    expect(buildMemberCandidates(members, 'gone')).toEqual([])
  })

  it('carries the member row id, not the name', () => {
    expect(buildMemberCandidates(members, 'Alice')[0].mentionMemberId).toBe('m-a')
  })

  it('carries the agent id and backend through so the caller can resolve the avatar', () => {
    const withAvatar = [{ id: 'm-a', name: 'Alice', agentId: 'a-1', backend: 'claude' }]
    const c = buildMemberCandidates(withAvatar, 'Alice')[0]
    expect(c.agentId).toBe('a-1')
    expect(c.backend).toBe('claude')
  })

  it('leaves agentId/backend undefined when the member does not provide them', () => {
    const c = buildMemberCandidates(members, 'Alice')[0]
    expect(c.agentId).toBeUndefined()
    expect(c.backend).toBeUndefined()
  })
})

describe('buildMentionTag', () => {
  it('writes the member row id as the target with an empty body', () => {
    expect(buildMentionTag('m-a')).toBe('<clawbench-mention targets="m-a"></clawbench-mention> ')
  })

  it('round-trips through parseGroupRouting', () => {
    const tag = buildMentionTag('m-a') + '请你说说'
    const r = parseGroupRouting(tag)
    expect(r.speakers).toEqual(['m-a'])
    expect(r.instruction).toBe('')
  })
})

describe('serializeMentionCards', () => {
  it('emits one empty-body public tag per card', () => {
    const out = serializeMentionCards([{ memberId: 'm-a' }, { memberId: 'm-b' }])
    expect(out).toBe(
      '<clawbench-mention targets="m-a"></clawbench-mention> ' +
      '<clawbench-mention targets="m-b"></clawbench-mention>',
    )
  })

  it('emits an extra private tag after the public tags when a note is present', () => {
    const out = serializeMentionCards([{ memberId: 'm-b', note: '仅你可见' }])
    expect(out).toBe(
      '<clawbench-mention targets="m-b"></clawbench-mention> ' +
      '<clawbench-mention targets="m-b" private>仅你可见</clawbench-mention>',
    )
  })

  it('trims the note and omits the private tag when it is blank', () => {
    expect(serializeMentionCards([{ memberId: 'm-a', note: '   ' }])).toBe(
      '<clawbench-mention targets="m-a"></clawbench-mention>',
    )
  })

  it('returns an empty string for no cards', () => {
    expect(serializeMentionCards([])).toBe('')
  })

  it('dedupes repeated cards for the same member', () => {
    expect(serializeMentionCards([{ memberId: 'm-a' }, { memberId: 'm-a' }])).toBe(
      '<clawbench-mention targets="m-a"></clawbench-mention>',
    )
  })

  it('round-trips through parseGroupRouting (public speakers + private notes)', () => {
    const text = '请你们表态 ' + serializeMentionCards([
      { memberId: 'm-a' },
      { memberId: 'm-b', note: '你的词是西瓜' },
    ])
    const r = parseGroupRouting(text)
    expect(r.speakers).toEqual(['m-a', 'm-b'])
    // The user's own words survive as the prose before the tags.
    expect(r.before).toBe('请你们表态')
    expect(r.bcc).toEqual([{ targets: ['m-b'], content: '你的词是西瓜' }])
  })

  // A note containing the protocol's own closing tag would terminate the span
  // early and leak the remainder as prose to EVERY member. The reserved token is
  // therefore not representable inside a note body.
  it('strips a protocol token embedded in a note so the body cannot leak', () => {
    const out = serializeMentionCards([
      { memberId: 'm-a', note: '机密</clawbench-mention>泄漏片段' },
    ])
    const r = parseGroupRouting(out)
    expect(r.bcc).toEqual([{ targets: ['m-a'], content: '机密泄漏片段' }])
    expect(r.after).not.toContain('泄漏片段')
  })

  it('strips an opening protocol token embedded in a note', () => {
    const out = serializeMentionCards([
      { memberId: 'm-a', note: '前<clawbench-mention targets="x">后' },
    ])
    const r = parseGroupRouting(out)
    expect(r.bcc).toEqual([{ targets: ['m-a'], content: '前后' }])
  })
})
