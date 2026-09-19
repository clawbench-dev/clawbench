import { describe, expect, it } from 'vitest'
import {
  askItemsToInputMap,
  askItemsToPlainText,
  extractAskMatches,
  hasParsedMatches,
  normalizeAskInput,
  parseItems,
  stripAskMatches,
  unparsedReasons,
  type AskItem,
} from '@/utils/askQuestion.ts'

const TAG = 'clawbench-ask-question'

/** Wrap a Markdown payload in the tag. */
const tag = (payload: string) => `<${TAG}>\n${payload}\n</${TAG}>`

// ─── normalizeAskInput (Path A) ─────────────────────────────────────────

describe('normalizeAskInput', () => {
  it('accepts a canonical questions array', () => {
    const got = normalizeAskInput({
      questions: [{ header: 'H', multiSelect: false, question: 'Q?', options: [{ label: 'A' }] }],
    })
    expect(got).toHaveLength(1)
    expect(got[0].question).toBe('Q?')
    expect(got[0].options).toEqual([{ label: 'A' }])
  })

  it('unwraps params.items', () => {
    const got = normalizeAskInput({ params: { items: [{ question: 'Q?', options: [{ label: 'A' }] }] } })
    expect(got).toHaveLength(1)
    expect(got[0].question).toBe('Q?')
  })

  it('maps choices to options', () => {
    const got = normalizeAskInput({ question: 'Q?', choices: [{ label: 'A' }] })
    expect(got[0].options).toEqual([{ label: 'A' }])
  })

  it('coerces a string multiSelect', () => {
    const got = normalizeAskInput({ questions: [{ question: 'Q?', multiSelect: 'true' }] })
    expect(got[0].multiSelect).toBe(true)
  })

  it('coerces string options to labelled objects', () => {
    const got = normalizeAskInput({ question: 'Q?', options: ['A', 'B'] })
    expect(got[0].options).toEqual([{ label: 'A' }, { label: 'B' }])
  })

  it('discards hallucinated shapes', () => {
    for (const input of [
      { type: 'ask-question' },
      { askUserQuestion: true },
      { taskId: '' },
      { schema: [{ name: 'header', type: 'string' }] },
    ]) {
      expect(normalizeAskInput(input), JSON.stringify(input)).toEqual([])
    }
  })

  it('discards a bare enum map', () => {
    expect(normalizeAskInput({ a: 1, b: 2 })).toEqual([])
  })

  it('keeps a flat question with no options', () => {
    const got = normalizeAskInput({ question: '存量统计展示在哪里？' })
    expect(got).toHaveLength(1)
    expect(got[0].question).toBe('存量统计展示在哪里？')
  })

  it('returns [] for non-objects', () => {
    for (const input of [null, undefined, 'x', 42, []]) {
      expect(normalizeAskInput(input)).toEqual([])
    }
  })
})

// ─── parseItems (Path B, Markdown only) ─────────────────────────────────

describe('parseItems', () => {
  it('parses a single-select question', () => {
    const got = parseItems('**方案选择**\n你更倾向哪种实现方式？\n- 方案 A — 快但不够安全\n- 方案 B — 安全但慢')
    expect(got).toHaveLength(1)
    expect(got[0]).toEqual({
      header: '方案选择',
      multiSelect: false,
      question: '你更倾向哪种实现方式？',
      options: [
        { label: '方案 A', description: '快但不够安全' },
        { label: '方案 B', description: '安全但慢' },
      ],
    })
  })

  it('treats a checkbox list as multi-select', () => {
    const got = parseItems('**需要启用哪些**\n- [ ] 语法高亮\n- [ ] 自动换行')
    expect(got[0].multiSelect).toBe(true)
    expect(got[0].options.map(o => o.label)).toEqual(['语法高亮', '自动换行'])
  })

  it('accepts marker variants beyond CommonMark', () => {
    const cases: Array<[string, string[]]> = [
      ['no space after dash', ['-甲', '-乙']],
      ['fullwidth hyphen', ['－ 甲', '－ 乙']],
      ['plus bullet', ['+ 甲', '+ 乙']],
      ['cjk ordinal dot', ['1、甲', '2、乙']],
      ['cjk numeral', ['一、甲', '二、乙']],
      ['paren ordered', ['1) 甲', '2) 乙']],
    ]
    for (const [name, lines] of cases) {
      const got = parseItems(`Q?\n${lines.join('\n')}`)
      expect(got, name).toHaveLength(1)
      expect(got[0].options.map(o => o.label), name).toEqual(['甲', '乙'])
    }
  })

  it('does not mistake prose for a list', () => {
    for (const input of ['温度是 -5 度', 'Q?\n---', 'Q?\n1.5 倍速', 'Q?\n**重点**', 'Q?\n*斜体*']) {
      expect(parseItems(input), input).toEqual([])
    }
  })

  it('accepts checkbox variants beyond ASCII brackets', () => {
    for (const input of [
      'Q?\n- [ ] 甲\n- [ ] 乙',
      'Q?\n- [x] 甲\n- [X] 乙',
      'Q?\n- ［ ］ 甲\n- ［ ］ 乙',
      'Q?\n- 【 】 甲\n- 【 】 乙',
      'Q?\n- []甲\n- []乙',
    ]) {
      const got = parseItems(input)
      expect(got, input).toHaveLength(1)
      expect(got[0].multiSelect, input).toBe(true)
    }
  })

  it('accepts heading variants beyond a bold line', () => {
    for (const input of [
      '# 方案选择\nQ?\n- 甲\n- 乙',
      '### 方案选择\nQ?\n- 甲\n- 乙',
      '__方案选择__\nQ?\n- 甲\n- 乙',
      '**方案选择**\nQ?\n- 甲\n- 乙',
    ]) {
      const got = parseItems(input)
      expect(got, input).toHaveLength(1)
      expect(got[0].header, input).toBe('方案选择')
    }
  })

  it('splits a description on an en dash too', () => {
    const got = parseItems('Q?\n- 甲 \u2013 说明')
    expect(got[0].options[0]).toEqual({ label: '甲', description: '说明' })
  })

  it('drops a description identical to the label', () => {
    const got = parseItems('Q?\n- A — A')
    expect(got[0].options[0].description).toBeUndefined()
  })

  it('does not treat a fenced block as options', () => {
    const got = parseItems('Q?\n- 甲\n```\n- not an option\n```')
    expect(got).toHaveLength(1)
    expect(got[0].options).toEqual([{ label: '甲' }])
  })

  // No fallback reader exists: anything that is not Markdown-with-a-list yields
  // nothing, so the caller degrades it to plain text.
  it('rejects prose and non-Markdown payloads', () => {
    for (const input of [
      'Each question needs item, question and option elements',
      '**标题**\n只有说明文字，没有列表',
      '{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}',
      '<item><question>Q?</question><option><label>A</label></option></item>',
      '',
      '   \n  \n',
    ]) {
      expect(parseItems(input), input).toEqual([])
    }
  })
})

// ─── extractAskMatches + stripAskMatches ────────────────────────────────

describe('extractAskMatches', () => {
  it('returns [] when no tag is present', () => {
    expect(extractAskMatches('普通文本，没有任何标签')).toEqual([])
  })

  it('locates a well formed tag and strips it', () => {
    const text = `前言\n${tag('**H**\nQ?\n- A')}\n后记`
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).not.toBeNull()
    expect(stripAskMatches(text, ms)).toBe('前言\n\n后记')
  })

  it('converts every tag when several are present', () => {
    const text = `${tag('**Q1**\n第一个?\n- A')}\n中间\n${tag('**Q2**\n第二个?\n- B')}`
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(2)
    expect(ms.every(m => m.parsed !== null)).toBe(true)
    expect(stripAskMatches(text, ms)).toBe('\n中间\n')
  })

  it('does not swallow a following <details> block', () => {
    const text = `分析如下\n<${TAG}>\n**H**\nQ?\n- A\n\n<details>\n<summary>更多</summary>\n正文内容必须保留\n</details>`
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).toBeNull()
    const stripped = stripAskMatches(text, ms)
    expect(stripped).toContain('正文内容必须保留')
    expect(stripped).toContain('<details>')
  })

  it('unparseable payload keeps its text but loses the wrapper', () => {
    const text = `前言\n${tag('这里没有列表，只是一段说明。')}\n后记`
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).toBeNull()
    const stripped = stripAskMatches(text, ms)
    expect(stripped).not.toContain(`<${TAG}`)
    expect(stripped).toContain('这里没有列表')
    expect(stripped).toContain('前言')
    expect(stripped).toContain('后记')
  })

  it('ignores a tag inside a fenced code block', () => {
    const text = `你可以这样用：\n\n\`\`\`\n${tag('**Choice**\nPick one\n- A')}\n\`\`\`\n\n这就是全部。`
    expect(extractAskMatches(text)).toEqual([])
  })

  it('ignores a tag inside inline code', () => {
    expect(extractAskMatches(`用 \`<${TAG}>\` 标签来提问。`)).toEqual([])
  })

  it('is not fooled by an orphaned backtick before a live question', () => {
    const text = '分析如下，前面有个孤立的反引号 ` 没有配对。\n\n' +
      tag('**助手消息宽度**\n当前助手消息已是 `align-self: stretch`。具体指什么？\n- 去掉左右 padding')
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).not.toBeNull()
    expect(ms[0].parsed?.[0].header).toBe('助手消息宽度')
  })

  it('does not swallow prose that merely mentions the tag', () => {
    const text = `要发起提问，就用 <${TAG}> 标签包起来。\n\n现在问你：\n${tag('**Q2**\n第二个?\n- B')}`
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(2)
    expect(ms[0].parsed).toBeNull()
    expect(ms[1].parsed).not.toBeNull()
    const stripped = stripAskMatches(text, ms)
    expect(stripped).toContain('要发起提问')
    expect(stripped).toContain('现在问你')
    expect(stripped).not.toContain('第二个?')
  })

  it("does not consume an outer element's closing tag", () => {
    const text = `分析\n<details>\n<summary>更多</summary>\n<${TAG}>\n**H**\nQ?\n- A\n</details>\n正文`
    const ms = extractAskMatches(text)
    const stripped = stripAskMatches(text, ms)
    expect(stripped).toContain('</details>')
    expect(stripped).toContain('正文')
  })

  it('converts a payload that mentions the tag in its own text', () => {
    const ms = extractAskMatches(tag(`**H**\n怎么渲染 <${TAG}> 这个标签？\n- 保留`))
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).not.toBeNull()
  })

  // A mention of the tag inside the payload's own text must not be mistaken for
  // a sibling payload. Regression: a line-start mention inside a fenced block or
  // an indented example made the enclosing tag unparseable, losing the card and
  // leaking the raw wrapper.
  it('does not treat a mention inside the payload as a sibling', () => {
    for (const payload of [
      `**Which syntax?**\nUse it:\n\`\`\`\n<${TAG}>\n\`\`\`\n- Option A\n- Option B`,
      `**H**\nQ?\n- A\n  <${TAG}> note\n- B`,
      `**H**\n怎么渲染 <${TAG}> 这个标签？\n- 保留`,
      `**H**\nQ?\n- A\n> <${TAG}> 引用\n- B`,
    ]) {
      const ms = extractAskMatches(tag(payload))
      expect(ms, payload).toHaveLength(1)
      expect(ms[0].parsed, payload).not.toBeNull()
    }
  })

  // A bullet whose label is empty must not be dropped: because the span parses,
  // the whole span is removed from the text, so the entry would vanish from both
  // the card and the visible text. Failing the parse keeps it visible.
  it('fails the parse rather than dropping an empty-label option', () => {
    const text = tag('Pick one?\n- A\n- — orphan description text')
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).toBeNull()
    expect(stripAskMatches(text, ms)).toContain('orphan description text')
  })

  // A close that belongs to a later tag must not be consumed by the mention
  // before it.
  it('does not consume a later tag\'s close', () => {
    const text = `说明：<${TAG}> 只是个标签名。\n${tag('**H**\nQ?\n- A')}`
    const ms = extractAskMatches(text)
    const stripped = stripAskMatches(text, ms)
    expect(stripped).toContain('说明：')
    expect(stripped).not.toContain('Q?')
  })
})

describe('stripAskMatches', () => {
  it('returns the input unchanged for no matches', () => {
    expect(stripAskMatches('unchanged', [])).toBe('unchanged')
  })

  it('leaves surrounding text intact', () => {
    const text = `A${tag('**H**\nQ?\n- A')}B`
    expect(stripAskMatches(text, extractAskMatches(text))).toBe('AB')
  })
})

describe('helpers', () => {
  it('hasParsedMatches / unparsedReasons', () => {
    const text = `前言\n${tag('没有列表的说明')}\n后记`
    const ms = extractAskMatches(text)
    expect(hasParsedMatches(ms)).toBe(false)
    expect(unparsedReasons(ms)).toHaveLength(1)
    expect(unparsedReasons(ms)[0]).not.toBe('')
  })

  it('reports no_standard_close when there is no close tag', () => {
    const ms = extractAskMatches(`前言\n<${TAG}>\n**H**\nQ?\n- A`)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).toBeNull()
    expect(ms[0].reason).toBe('no_standard_close')
  })

  it('renders plain text', () => {
    const items: AskItem[] = [{
      header: 'Approach',
      multiSelect: false,
      question: 'Which approach?',
      options: [
        { label: 'Option A', description: 'Fast' },
        { label: 'Option B', description: 'Safe' },
      ],
    }]
    expect(askItemsToPlainText(items)).toBe(
      'Which approach? (Approach): Option A — Fast, Option B — Safe',
    )
  })

  it('renders the input map with the expected field names', () => {
    const got = askItemsToInputMap([{
      header: 'H',
      multiSelect: true,
      question: 'Q?',
      options: [{ label: 'A', description: 'd' }, { label: 'B' }],
    }])
    const q = got.questions[0]
    expect(q.header).toBe('H')
    expect(q.multiSelect).toBe(true)
    expect(q.question).toBe('Q?')
    expect(q.options[0]).toEqual({ label: 'A', description: 'd' })
    expect(q.options[1]).toEqual({ label: 'B' })
  })
})
