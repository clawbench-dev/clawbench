import { describe, expect, it } from 'vitest'
import { parseGroupRouting, stripGroupBccTags, stripGroupBccSpans } from '@/utils/groupRouting.ts'

describe('parseGroupRouting before (pre-tag background)', () => {
  it('is empty when the tag leads the message', () => {
    const r = parseGroupRouting('<clawbench-speaker>A</clawbench-speaker> 请谈谈')
    expect(r.found).toBe(true)
    expect(r.before).toBe('')
  })

  it('returns the text preceding the tag', () => {
    const r = parseGroupRouting('A 的观点不错 <clawbench-speaker>B</clawbench-speaker> 请回应')
    expect(r.found).toBe(true)
    expect(r.before).toBe('A 的观点不错')
  })

  it('trims surrounding whitespace around the background', () => {
    const r = parseGroupRouting('\n  背景上下文 \n <clawbench-speaker>A</clawbench-speaker> 请表态')
    expect(r.before).toBe('背景上下文')
  })

  it('is empty when no tag is present', () => {
    const r = parseGroupRouting('普通发言，没有标签')
    expect(r.found).toBe(false)
    expect(r.before).toBe('')
  })

  it('still slices the background ahead of a malformed tag (never strips)', () => {
    const r = parseGroupRouting('背景在此 <clawbench-speaker></clawbench-speaker>')
    expect(r.found).toBe(false)
    expect(r.before).toBe('背景在此')
  })
})

describe('parseGroupRouting bcc (private notes)', () => {
  it('extracts a note and removes it from the public instruction', () => {
    const r = parseGroupRouting(
      '<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc targets="A">你重点看性能</clawbench-bcc>',
    )
    expect(r.bcc).toEqual([{ targets: ['A'], content: '你重点看性能' }])
    expect(r.instruction).toBe('请表态')
    expect(r.instruction).not.toContain('性能')
  })

  it('splits and trims multiple targets', () => {
    const r = parseGroupRouting(
      '<clawbench-speaker>A,B</clawbench-speaker> 表态 <clawbench-bcc targets="A, B ,C">都注意</clawbench-bcc>',
    )
    expect(r.bcc[0].targets).toEqual(['A', 'B', 'C'])
  })

  it('does not leak a note placed before the tag into before', () => {
    const r = parseGroupRouting(
      '<clawbench-bcc targets="A">私下话</clawbench-bcc>背景在此 <clawbench-speaker>A</clawbench-speaker> 请回应',
    )
    expect(r.before).not.toContain('私下话')
    expect(r.before).toContain('背景在此')
    expect(r.bcc).toEqual([{ targets: ['A'], content: '私下话' }])
  })

  it('keeps multiple notes in order', () => {
    const r = parseGroupRouting(
      '<clawbench-speaker>A,B</clawbench-speaker> 表态' +
        '<clawbench-bcc targets="A">给A一</clawbench-bcc>' +
        '<clawbench-bcc targets="B">给B二</clawbench-bcc>',
    )
    expect(r.bcc.map((e) => e.content)).toEqual(['给A一', '给B二'])
  })

  it('leaves a malformed note unparsed and untouched', () => {
    expect(parseGroupRouting('<clawbench-bcc>无 targets</clawbench-bcc>').bcc).toEqual([])
    expect(parseGroupRouting('<clawbench-bcc targets="">空名单</clawbench-bcc>').bcc).toEqual([])
    expect(parseGroupRouting("<clawbench-bcc targets='A'>单引号</clawbench-bcc>").bcc).toEqual([])
  })

  it('does not treat an end tag inside a note as a discussion end', () => {
    const r = parseGroupRouting(
      '<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A">如果没意见就 <clawbench-group-end/> 收尾</clawbench-bcc>',
    )
    expect(r.end).toBe(false)
    expect(r.bcc[0].content).toContain('收尾')
  })
})

describe('stripGroupBccTags', () => {
  it('removes a well-formed note and keeps the surroundings', () => {
    expect(stripGroupBccTags('前 <clawbench-bcc targets="A">私</clawbench-bcc> 后')).toBe('前  后')
  })

  it('returns the text unchanged when there is no note', () => {
    expect(stripGroupBccTags('no tag here')).toBe('no tag here')
  })

  it('leaves a malformed note verbatim (never lose content)', () => {
    const malformed = '<clawbench-bcc>畸形</clawbench-bcc>'
    expect(stripGroupBccTags(malformed)).toBe(malformed)
  })
})

describe('parseGroupRouting fail-closed (injection boundary)', () => {
  const cases: Array<[string, string]> = [
    ['single quotes', `<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets='A'>秘密一</clawbench-bcc>`],
    ['blank targets', `<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="   ">秘密二</clawbench-bcc>`],
    ['uppercase attr', `<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc Targets="A">秘密三</clawbench-bcc>`],
    ['unclosed', `<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A">未闭合秘密`],
    ['nbsp separator', `<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc\u00A0targets="A">秘密五</clawbench-bcc>`],
  ]
  for (const [name, text] of cases) {
    it(`strips ${name} from the instruction`, () => {
      const r = parseGroupRouting(text)
      expect(r.instruction).not.toContain('秘密')
      expect(r.instruction).not.toContain('clawbench-bcc')
    })
  }
})

describe('stripGroupBccSpans', () => {
  it('removes well-formed and malformed spans', () => {
    expect(stripGroupBccSpans('前 <clawbench-bcc targets="A">良</clawbench-bcc> 后')).toBe('前  后')
    expect(stripGroupBccSpans("前 <clawbench-bcc targets='A'>畸</clawbench-bcc> 后")).toBe('前  后')
  })

  it('drops an unclosed tag and its tail', () => {
    expect(stripGroupBccSpans('前 <clawbench-bcc targets="A">未闭合')).toBe('前')
  })

  it('returns text unchanged when there is no bcc', () => {
    expect(stripGroupBccSpans('no tag here')).toBe('no tag here')
  })
})
