import { describe, expect, it } from 'vitest'
import {
  parseAskQuestionXML,
  parseAskQuestionJSON,
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

  // JSON format fallback tests
  it('parses JSON format when XML fails', () => {
    const json = `{"questions":[{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[{"label":"Option A","description":"Fast"},{"label":"Option B","description":"Safe"}]}]}`
    const result = parseAskQuestionXML(json)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(1)
    expect(result!.questions[0].header).toBe('Approach')
    expect(result!.questions[0].multiSelect).toBe(false)
    expect(result!.questions[0].question).toBe('Which approach?')
    expect(result!.questions[0].options).toHaveLength(2)
    expect(result!.questions[0].options[0]).toEqual({ label: 'Option A', description: 'Fast' })
    expect(result!.questions[0].options[1]).toEqual({ label: 'Option B', description: 'Safe' })
  })

  it('parses JSON format with multiple questions', () => {
    const json = `{"questions":[{"header":"Q1","multiSelect":false,"question":"First?","options":[{"label":"A"}]},{"header":"Q2","multiSelect":true,"question":"Second?","options":[{"label":"B"}]}]}`
    const result = parseAskQuestionXML(json)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(2)
    expect(result!.questions[1].multiSelect).toBe(true)
  })

  it('parses JSON format with options without description', () => {
    const json = `{"questions":[{"header":"Pick","multiSelect":false,"question":"Choose","options":[{"label":"Yes"}]}]}`
    const result = parseAskQuestionXML(json)
    expect(result).not.toBeNull()
    expect(result!.questions[0].options[0]).toEqual({ label: 'Yes' })
  })

  it('parses JSON format defaulting multiSelect to false', () => {
    const json = `{"questions":[{"header":"Pick","question":"Choose","options":[{"label":"Yes"}]}]}`
    const result = parseAskQuestionXML(json)
    expect(result).not.toBeNull()
    expect(result!.questions[0].multiSelect).toBe(false)
  })
})

// ─── parseAskQuestionJSON ────────────────────────────────────────────────

describe('parseAskQuestionJSON', () => {
  it('parses single question with options', () => {
    const json = `{"questions":[{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[{"label":"Option A","description":"Fast"},{"label":"Option B","description":"Safe"}]}]}`
    const result = parseAskQuestionJSON(json)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(1)
    expect(result!.questions[0].header).toBe('Approach')
    expect(result!.questions[0].multiSelect).toBe(false)
    expect(result!.questions[0].question).toBe('Which approach?')
    expect(result!.questions[0].options).toHaveLength(2)
  })

  it('parses multi-select question', () => {
    const json = `{"questions":[{"header":"Features","multiSelect":true,"question":"Select features","options":[{"label":"Auth"}]}]}`
    const result = parseAskQuestionJSON(json)
    expect(result).not.toBeNull()
    expect(result!.questions[0].multiSelect).toBe(true)
  })

  it('parses multiple questions', () => {
    const json = `{"questions":[{"header":"Q1","multiSelect":false,"question":"First?","options":[{"label":"A"}]},{"header":"Q2","multiSelect":false,"question":"Second?","options":[{"label":"B"}]}]}`
    const result = parseAskQuestionJSON(json)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(2)
  })

  it('returns null for invalid JSON', () => {
    expect(parseAskQuestionJSON('not json at all')).toBeNull()
  })

  it('returns null for JSON without questions', () => {
    expect(parseAskQuestionJSON('{"something":"else"}')).toBeNull()
  })

  it('returns null for JSON with empty questions array', () => {
    expect(parseAskQuestionJSON('{"questions":[]}')).toBeNull()
  })

  it('returns null for question without options', () => {
    expect(parseAskQuestionJSON('{"questions":[{"question":"Q?","options":[]}]}')).toBeNull()
  })

  it('defaults header to empty string when missing', () => {
    const json = `{"questions":[{"multiSelect":false,"question":"Choose","options":[{"label":"Yes"}]}]}`
    const result = parseAskQuestionJSON(json)
    expect(result).not.toBeNull()
    expect(result!.questions[0].header).toBe('')
  })

  it('defaults multiSelect to false when missing', () => {
    const json = `{"questions":[{"question":"Choose","options":[{"label":"Yes"}]}]}`
    const result = parseAskQuestionJSON(json)
    expect(result).not.toBeNull()
    expect(result!.questions[0].multiSelect).toBe(false)
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
    expect(result.content).toContain('<item>')
  })

  it('returns found=false when no ask-question tag', () => {
    const result = detectAskQuestion('no ask-question here')
    expect(result.found).toBe(false)
  })
})


