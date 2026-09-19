import { describe, expect, it } from 'vitest'
import {
  ReasonNoChildClose,
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

// ─── normalizeAskInput (Path A) ─────────────────────────────────────────

describe('normalizeAskInput', () => {
  it('accepts a canonical questions array', () => {
    const got = normalizeAskInput({
      questions: [{
        header: 'Approach',
        multiSelect: false,
        question: 'Which?',
        options: [{ label: 'A', description: 'Fast' }],
      }],
    })
    expect(got).toEqual([{
      header: 'Approach',
      multiSelect: false,
      question: 'Which?',
      options: [{ label: 'A', description: 'Fast' }],
    }])
  })

  it('unwraps params.items', () => {
    const got = normalizeAskInput({
      params: { items: [{ question: 'Q?', options: [{ label: 'A' }] }] },
    })
    expect(got).toHaveLength(1)
    expect(got[0].question).toBe('Q?')
  })

  it('maps choices to options', () => {
    const got = normalizeAskInput({
      parameters: [{ question: 'Q?', choices: [{ label: 'A' }, { label: 'B' }] }],
    })
    expect(got[0].options).toEqual([{ label: 'A' }, { label: 'B' }])
  })

  it('coerces a string multiSelect', () => {
    const got = normalizeAskInput({
      questions: [{ question: 'Q?', multiSelect: 'true', options: [{ label: 'A' }] }],
    })
    expect(got[0].multiSelect).toBe(true)
  })

  it('coerces string options to labelled objects', () => {
    const got = normalizeAskInput({ questions: [{ question: 'Q?', options: ['A', 'B'] }] })
    expect(got[0].options).toEqual([{ label: 'A' }, { label: 'B' }])
  })

  it('discards hallucinated shapes', () => {
    for (const raw of [
      { type: 'ask-question' },
      { askUserQuestion: true },
      { taskId: '' },
      { schema: [{ name: 'header', type: 'string' }] },
      {},
    ]) {
      expect(normalizeAskInput(raw), JSON.stringify(raw)).toEqual([])
    }
  })

  it('discards a bare enum map', () => {
    expect(normalizeAskInput({
      '"1': 'Browser (Chrome/Firefox on desktop)',
      '1': 'Browser (Chrome/Firefox on desktop)',
      '2': 'Android App (WebView)',
    })).toEqual([])
  })

  it('keeps a flat question with no options', () => {
    expect(normalizeAskInput({ question: '存量统计展示在哪里？' })).toEqual([
      { header: '', multiSelect: false, question: '存量统计展示在哪里？', options: [] },
    ])
  })

  it('parses a raw <item> XML payload in a string field', () => {
    const got = normalizeAskInput({
      ask: '<item><header>H</header><question>Q?</question><option><label>A</label></option></item>',
    })
    expect(got).toEqual([{
      header: 'H', multiSelect: false, question: 'Q?', options: [{ label: 'A' }],
    }])
  })

  it('returns [] for non-objects', () => {
    for (const v of [null, undefined, 'text', 42, []]) {
      expect(normalizeAskInput(v)).toEqual([])
    }
  })
})

// ─── parseItems (Path B) ────────────────────────────────────────────────

describe('parseItems', () => {
  it('parses a well formed payload', () => {
    const got = parseItems(
      '<item><header>H</header><multi-select>false</multi-select>' +
      '<question>Q?</question><option><label>A</label><description>d</description></option></item>',
    )
    expect(got).toEqual([{
      header: 'H', multiSelect: false, question: 'Q?',
      options: [{ label: 'A', description: 'd' }],
    }])
  })

  it('parses an unclosed option bounded by the item end', () => {
    const got = parseItems(
      '<item><header>H</header><question>Q?</question>' +
      '<option><label>A</label><description>d</description></item>',
    )
    expect(got).toEqual([{
      header: 'H', multiSelect: false, question: 'Q?',
      options: [{ label: 'A', description: 'd' }],
    }])
  })

  it('parses an unclosed option bounded by the next option', () => {
    const got = parseItems(
      '<item><header>H</header><question>Q?</question>' +
      '<option><label>A</label><option><label>B</label></option></item>',
    )
    expect(got[0].options).toEqual([{ label: 'A' }, { label: 'B' }])
  })

  it('rescues a label from an option attribute', () => {
    const got = parseItems(
      '<item><header>H</header><question>Q?</question><option value="restore_only"></option></item>',
    )
    expect(got[0].options).toEqual([{ label: 'restore_only' }])
  })

  it('unwraps the plural <options> container', () => {
    const got = parseItems(
      '<item><header>H</header><question>Q?</question>' +
      '<options><option><label>A</label></option></options></item>',
    )
    expect(got[0].options).toEqual([{ label: 'A' }])
  })

  it('uses bare option text as the label', () => {
    const got = parseItems(
      '<item><header>H</header><question>Q?</question><option>是，就是它</option></item>',
    )
    expect(got[0].options).toEqual([{ label: '是，就是它' }])
  })

  it('accepts the multi_select spelling', () => {
    const got = parseItems(
      '<item><header>H</header><multi_select>true</multi_select><question>Q?</question>' +
      '<option><label>A</label></option></item>',
    )
    expect(got[0].multiSelect).toBe(true)
  })

  it('repairs raw & and angle brackets', () => {
    const got = parseItems(
      '<item><header>A & B</header><question>选哪个 < 5 还是 > 5?</question>' +
      '<option><label>R&D</label></option></item>',
    )
    expect(got[0].header).toBe('A & B')
    expect(got[0].question).toBe('选哪个 < 5 还是 > 5?')
    expect(got[0].options[0].label).toBe('R&D')
  })

  it('decodes the shared entity set and numeric references', () => {
    const got = parseItems(
      '<item><header>H</header><question>Q?</question>' +
      '<option><label>Step&nbsp;1 &hellip; &#8212; end &copy;</label></option></item>',
    )
    expect(got[0].options[0].label).toBe('Step 1 \u2026 \u2014 end \u00a9')
  })

  it('leaves entities outside the shared table verbatim', () => {
    // Go and TS must agree: an entity neither table knows stays as written.
    const got = parseItems(
      '<item><header>H</header><question>Q?</question>' +
      '<option><label>&epsilon; &forall;</label></option></item>',
    )
    expect(got[0].options[0].label).toBe('&epsilon; &forall;')
  })

  it('recovers a JSON payload instead of discarding it', () => {
    // JSON is not the documented format, but recovering it beats discarding a
    // readable question. If recovery fails the payload renders as Markdown.
    const got = parseItems('{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}')
    expect(got).toHaveLength(1)
    expect(got[0].question).toBe('Q?')
    expect(got[0].options).toEqual([{ label: 'A' }])
  })

  it('recovers JSON containing unescaped quotes in a value', () => {
    const got = parseItems('{"questions":[{"question":"可以被"临时分裂"多久？","options":[{"label":"数秒"}]}]}')
    expect(got).toHaveLength(1)
    expect(got[0].question).toBe('可以被"临时分裂"多久？')
  })

  // --- Format robustness: the markers models actually emit ---

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

  it('recovers JSON with unescaped quotes and rejects JSON without a question', () => {
    const recovered = parseItems('{"questions":[{"question":"可以被"临时分裂"多久？","options":[{"label":"数秒"}]}]}')
    expect(recovered).toHaveLength(1)
    expect(recovered[0].question).toBe('可以被"临时分裂"多久？')

    for (const input of ['{"taskId":""}', '{"questions":[]}', '{not valid json at all']) {
      expect(parseItems(input), input).toEqual([])
    }
  })

  it('rejects prose', () => {
    expect(parseItems('Each question needs item, question and option elements')).toEqual([])
  })

  it('drops a description identical to the label', () => {
    const got = parseItems(
      '<item><question>Q?</question><option><label>A</label><description>A</description></option></item>',
    )
    expect(got[0].options[0].description).toBeUndefined()
  })

  it('strips nested tags from text', () => {
    const got = parseItems(
      '<item><question>用 <code>x</code> 还是 <code>y</code>?</question>' +
      '<option><label>用 <b>x</b></label></option></item>',
    )
    expect(got[0].question).toBe('用 x 还是 y?')
    expect(got[0].options[0].label).toBe('用 x')
  })
})

// ─── extractAskMatches + stripAskMatches ────────────────────────────────

describe('extractAskMatches', () => {
  const good = (header: string) =>
    `<ask-question><item><header>${header}</header><multi-select>false</multi-select>` +
    `<question>${header}?</question><option><label>A</label></option></item></ask-question>`

  it('returns [] when no tag is present', () => {
    expect(extractAskMatches('普通文本')).toEqual([])
  })

  it('locates a well formed tag and strips it', () => {
    const text = `前言\n${good('H')}\n后记`
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).not.toBeNull()
    expect(stripAskMatches(text, ms)).toBe('前言\n\n后记')
  })

  it('converts every tag when several are present', () => {
    const text = `${good('Q1')}\n中间\n${good('Q2')}`
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(2)
    expect(ms.map(m => m.parsed![0].header)).toEqual(['Q1', 'Q2'])
    expect(stripAskMatches(text, ms)).toBe('\n中间\n')
  })

  it('does not swallow a following <details> block', () => {
    // The over-strip regression: an unclosed tag used to consume everything up
    // to the next closing token (</details>), deleting real prose.
    const text =
      '分析如下\n<ask-question>\n<item><header>H</header><multi-select>false</multi-select>' +
      '<question>Q?</question><option><label>A</label></option></item>\n\n' +
      '<details>\n<summary>更多</summary>\n正文内容必须保留\n</details>'
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).not.toBeNull()
    const stripped = stripAskMatches(text, ms)
    expect(stripped).toContain('正文内容必须保留')
    expect(stripped).toContain('<details>')
    expect(stripped).not.toContain('<ask-question')
  })

  it('unparseable payload keeps its text but loses the wrapper', () => {
    // New contract: the wrapper is stripped and the inner text is shown, so the
    // payload degrades to readable prose instead of exposing raw markup. The
    // text itself must still be present — that is the no-loss rule.
    const text = '前言\n<ask-question>\n这里没有列表也没有 JSON，只是一段说明。\n</ask-question>\n后记'
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).toBeNull()
    const stripped = stripAskMatches(text, ms)
    expect(stripped).not.toContain('<ask-question')
    expect(stripped).toContain('这里没有列表也没有 JSON')
    expect(stripped).toContain('前言')
    expect(stripped).toContain('后记')
  })

  it('ignores a tag inside a fenced code block', () => {
    const text = `示例：\n\n\`\`\`\n${good('H')}\n\`\`\`\n\n结束。`
    expect(extractAskMatches(text)).toEqual([])
  })

  it('ignores a tag inside inline code', () => {
    expect(extractAskMatches('用 `<ask-question>` 标签提问。')).toEqual([])
  })

  it('is not fooled by an orphaned backtick before a live question', () => {
    // Regression from the real corpus: a stray backtick earlier in a long
    // message paired with one inside the payload and hid the whole question.
    const text =
      '分析如下，前面有个孤立的反引号 ` 没有配对。\n\n' +
      '<ask-question>\n  <item>\n    <header>宽度</header>\n    <multi-select>false</multi-select>\n' +
      '    <question>当前已是 `align-self: stretch`，具体指什么？</question>\n' +
      '    <option><label>去掉 padding</label></option>\n  </item>\n</ask-question>'
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed![0].header).toBe('宽度')
  })

  it('does not swallow prose that merely mentions the tag', () => {
    // D1 regression: an unparseable mention followed by a real question used to
    // have the whole prose span (and the real tag's open) deleted.
    const text =
      '要发起提问，就用 <ask-question> 标签包起来，里面放 <item> 元素。\n\n现在问你：\n' +
      '<ask-question><item><header>Q2</header><multi-select>false</multi-select>' +
      '<question>第二个?</question><option><label>B</label></option></item></ask-question>'
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(2)
    expect(ms[0].parsed).toBeNull()
    expect(ms[1].parsed).not.toBeNull()
    const stripped = stripAskMatches(text, ms)
    expect(stripped).toContain('要发起提问')
    expect(stripped).toContain('现在问你')
    expect(stripped).not.toContain('第二个?')
  })

  it('does not consume an outer element\'s closing tag', () => {
    // D2 regression: the gap was punctuation-only, so </details> was eaten.
    const text =
      '分析\n<details>\n<summary>更多</summary>\n<ask-question>\n' +
      '<item><header>H</header><question>Q?</question><option><label>A</label></option></item>\n' +
      '</details>\n正文'
    const stripped = stripAskMatches(text, extractAskMatches(text))
    expect(stripped).toContain('</details>')
    expect(stripped).toContain('正文')
  })

  it('converts a payload that mentions the tag in its own text', () => {
    // The self-containment guard must not mistake a literal mention for a
    // sibling payload.
    const text =
      '<ask-question><item><header>H</header><question>怎么处理 <ask-question> 标签露出？</question>' +
      '<option><label>A</label></option></item></ask-question>'
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).not.toBeNull()
    expect(stripAskMatches(text, ms)).toBe('')
  })

  it('does not split a payload when an early item mentions the tag', () => {
    const text =
      '<ask-question><item><question>Q about <ask-question> tags</question><option><label>A</label></option></item>' +
      '<item><question>Q2?</question><option><label>B</label></option></item></ask-question>'
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).toHaveLength(2)
    // No dangling fragment may be left behind.
    expect(stripAskMatches(text, ms)).toBe('')
  })

  it('tolerates an obfuscated closing tag', () => {
    const text =
      '前\n<ask-question><item><header>H</header><question>Q?</question>' +
      '<option><label>A</label></option></item>\n</\uFF5C\uFF5CDSML\uFF5C\uFF5Cquestion>'
    const ms = extractAskMatches(text)
    expect(ms).toHaveLength(1)
    expect(ms[0].parsed).not.toBeNull()
    expect(stripAskMatches(text, ms)).not.toContain('<ask-question')
  })
})

describe('stripAskMatches', () => {
  it('returns the input unchanged for no matches', () => {
    expect(stripAskMatches('unchanged', [])).toBe('unchanged')
  })

  it('leaves surrounding text intact', () => {
    const text = 'A<ask-question><item><question>Q?</question><option><label>A</label></option></item></ask-question>B'
    expect(stripAskMatches(text, extractAskMatches(text))).toBe('AB')
  })
})

describe('helpers', () => {
  it('hasParsedMatches / unparsedReasons', () => {
    const ok = extractAskMatches(
      '<ask-question><item><question>Q?</question><option><label>A</label></option></item></ask-question>',
    )
    expect(hasParsedMatches(ok)).toBe(true)
    expect(unparsedReasons(ok)).toEqual([])

    const bad = extractAskMatches('前言<ask-question>{"a":1}</ask-question>后')
    expect(hasParsedMatches(bad)).toBe(false)
    expect(unparsedReasons(bad)).toEqual([bad[0].reason])
  })

  it('reports no_child_close when there is no item at all', () => {
    const ms = extractAskMatches('前言<ask-question>随便写点什么')
    expect(ms[0].parsed).toBeNull()
    expect(ms[0].reason).toBe(ReasonNoChildClose)
  })

  it('renders plain text', () => {
    const items: AskItem[] = [{
      header: 'Approach', multiSelect: false, question: 'Which approach?',
      options: [{ label: 'A', description: 'Fast' }, { label: 'B' }],
    }]
    expect(askItemsToPlainText(items)).toBe('Which approach? (Approach): A — Fast, B')
  })

  it('renders the input map with the expected field names', () => {
    const map = askItemsToInputMap([{
      header: 'H', multiSelect: true, question: 'Q?',
      options: [{ label: 'A', description: 'd' }, { label: 'B' }],
    }])
    expect(map).toEqual({
      questions: [{
        header: 'H', multiSelect: true, question: 'Q?',
        options: [{ label: 'A', description: 'd' }, { label: 'B' }],
      }],
    })
  })
})
