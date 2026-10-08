import { describe, expect, it } from 'vitest'
import {
  parseGroupRouting,
  stripGroupBccSpans,
  stripGroupProtocolTags,
  renderMentionChips,
  buildMemberCandidates,
  buildMentionTag,
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
