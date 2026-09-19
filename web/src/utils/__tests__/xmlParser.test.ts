import { describe, expect, it } from 'vitest'
import {
  parseAskQuestionXML,
} from '@/utils/xmlParser.ts'
import { isValidAskContent, detectAskQuestion } from '@/utils/streamPerf.ts'

// ─── parseAskQuestionXML ─────────────────────────────────────────────────
//
// The payload is native Markdown: an optional bold title line, the question
// text, then a bullet list of options ("- Label — description"). A checkbox
// list ("- [ ] item") marks multi-select. A payload with no list is not a
// question and parses to null.

describe('parseAskQuestionXML', () => {
  it('parses a payload with a header, question and options', () => {
    const md = `**Approach**
Which approach?
- Option A — Fast
- Option B — Safe`

    const result = parseAskQuestionXML(md)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(1)
    expect(result!.questions[0].header).toBe('Approach')
    expect(result!.questions[0].multiSelect).toBe(false)
    expect(result!.questions[0].question).toBe('Which approach?')
    expect(result!.questions[0].options).toHaveLength(2)
    expect(result!.questions[0].options[0]).toEqual({ label: 'Option A', description: 'Fast' })
    expect(result!.questions[0].options[1]).toEqual({ label: 'Option B', description: 'Safe' })
  })

  it('parses a multi-select payload (checkbox list)', () => {
    const md = `**Features**
Select features
- [ ] Auth`

    const result = parseAskQuestionXML(md)
    expect(result).not.toBeNull()
    expect(result!.questions[0].multiSelect).toBe(true)
    expect(result!.questions[0].options[0]).toEqual({ label: 'Auth' })
  })

  it('returns null for a legacy <item>/<option> XML payload', () => {
    // The bespoke XML child-element format was removed with no fallback.
    const legacy = '<clawbench-ask-question><item><header>H</header><question>Q?</question><option><label>A</label></option></item></clawbench-ask-question>'
    expect(parseAskQuestionXML(legacy)).toBeNull()
  })

  it('returns null for a payload with no list', () => {
    expect(parseAskQuestionXML('Just some prose with no list')).toBeNull()
  })

  it('returns null when a header and question are present but no list', () => {
    const md = `**Approach**
Which approach?`
    expect(parseAskQuestionXML(md)).toBeNull()
  })

  it('handles an option without description', () => {
    const md = `**Pick**
Choose
- Yes`

    const result = parseAskQuestionXML(md)
    expect(result).not.toBeNull()
    expect(result!.questions[0].options[0]).toEqual({ label: 'Yes' })
  })

  it('defaults multi-select to false when no checkbox is present', () => {
    const md = `**Pick**
Choose
- Yes`

    const result = parseAskQuestionXML(md)
    expect(result).not.toBeNull()
    expect(result!.questions[0].multiSelect).toBe(false)
  })

  it('returns null for a JSON payload (no JSON recovery)', () => {
    const json = `{"questions":[{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[{"label":"Option A","description":"Fast"}]}]}`
    expect(parseAskQuestionXML(json)).toBeNull()
  })
})

// ─── isValidAskContent ───────────────────────────────────────────────────

describe('isValidAskContent', () => {
  it('returns true for a Markdown payload with a list', () => {
    const content = `**Approach**
Which?
- A`
    expect(isValidAskContent(content)).toBe(true)
  })

  it('returns false for plain text with no list', () => {
    expect(isValidAskContent('just some text')).toBe(false)
  })

  it('returns false for empty content', () => {
    expect(isValidAskContent('')).toBe(false)
  })
})

// ─── detectAskQuestion ───────────────────────────────────────────────────

describe('detectAskQuestion', () => {
  it('detects a Markdown-format clawbench-ask-question tag', () => {
    const text = 'Some text before <clawbench-ask-question>**H**\nQ?\n- A</clawbench-ask-question> more text'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.items).toHaveLength(1)
    expect(result.items[0].question).toBe('Q?')
  })

  it('returns found=false when no ask-question tag', () => {
    const result = detectAskQuestion('no ask-question here')
    expect(result.found).toBe(false)
  })
})
