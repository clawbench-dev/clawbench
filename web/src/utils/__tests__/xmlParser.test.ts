import { describe, expect, it } from 'vitest'
import {
  parseAskQuestionXML,
} from '@/utils/xmlParser.ts'
import { isValidAskContent, detectAskQuestion } from '@/utils/streamPerf.ts'

// ─── parseAskQuestionXML ─────────────────────────────────────────────────

describe('parseAskQuestionXML', () => {
  it('parses single item with options', () => {
    const xml = `<ask-question>
  <item>
    <header>Approach</header>
    <multi-select>false</multi-select>
    <question>Which approach?</question>
    <option>
      <label>Option A</label>
      <description>Fast</description>
    </option>
    <option>
      <label>Option B</label>
      <description>Safe</description>
    </option>
  </item>
</ask-question>`

    const result = parseAskQuestionXML(xml)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(1)
    expect(result!.questions[0].header).toBe('Approach')
    expect(result!.questions[0].multiSelect).toBe(false)
    expect(result!.questions[0].question).toBe('Which approach?')
    expect(result!.questions[0].options).toHaveLength(2)
    expect(result!.questions[0].options[0]).toEqual({ label: 'Option A', description: 'Fast' })
    expect(result!.questions[0].options[1]).toEqual({ label: 'Option B', description: 'Safe' })
  })

  it('parses multi-select item', () => {
    const xml = `<ask-question>
  <item>
    <header>Features</header>
    <multi-select>true</multi-select>
    <question>Select features</question>
    <option>
      <label>Auth</label>
    </option>
  </item>
</ask-question>`

    const result = parseAskQuestionXML(xml)
    expect(result).not.toBeNull()
    expect(result!.questions[0].multiSelect).toBe(true)
    expect(result!.questions[0].options[0]).toEqual({ label: 'Auth' })
  })

  it('parses multiple items', () => {
    const xml = `<ask-question>
  <item>
    <header>Q1</header>
    <multi-select>false</multi-select>
    <question>First?</question>
    <option><label>A</label></option>
  </item>
  <item>
    <header>Q2</header>
    <multi-select>false</multi-select>
    <question>Second?</question>
    <option><label>B</label></option>
  </item>
</ask-question>`

    const result = parseAskQuestionXML(xml)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(2)
  })

  it('returns null for invalid XML', () => {
    const result = parseAskQuestionXML('not xml at all')
    expect(result).toBeNull()
  })

  it('returns null for XML without item elements', () => {
    const result = parseAskQuestionXML('<ask-question><something>else</something></ask-question>')
    expect(result).toBeNull()
  })

  it('handles option without description', () => {
    const xml = `<ask-question>
  <item>
    <header>Pick</header>
    <multi-select>false</multi-select>
    <question>Choose</question>
    <option><label>Yes</label></option>
  </item>
</ask-question>`

    const result = parseAskQuestionXML(xml)
    expect(result).not.toBeNull()
    expect(result!.questions[0].options[0]).toEqual({ label: 'Yes' })
  })

  it('defaults multi-select to false when missing', () => {
    const xml = `<ask-question>
  <item>
    <header>Pick</header>
    <question>Choose</question>
    <option><label>Yes</label></option>
  </item>
</ask-question>`

    const result = parseAskQuestionXML(xml)
    expect(result).not.toBeNull()
    expect(result!.questions[0].multiSelect).toBe(false)
  })

  it('returns null for JSON content (only XML is supported)', () => {
    const json = `{"questions":[{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[{"label":"Option A","description":"Fast"}]}]}`
    expect(parseAskQuestionXML(json)).toBeNull()
  })
})

// ─── isValidAskContent (XML mode) ────────────────────────────────────────

describe('isValidAskContent', () => {
  it('returns true for XML with <item> child elements', () => {
    const content = `
  <item>
    <header>Approach</header>
    <multi-select>false</multi-select>
    <question>Which?</question>
    <option><label>A</label></option>
  </item>
`
    expect(isValidAskContent(content)).toBe(true)
  })

  it('returns false for plain text without XML structure', () => {
    expect(isValidAskContent('just some text')).toBe(false)
  })

  it('returns false for empty content', () => {
    expect(isValidAskContent('')).toBe(false)
  })
})

// ─── detectAskQuestion (XML mode) ────────────────────────────────────────

describe('detectAskQuestion', () => {
  it('detects XML-format ask-question', () => {
    const text = 'Some text before <ask-question><item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label></option></item></ask-question> more text'
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


