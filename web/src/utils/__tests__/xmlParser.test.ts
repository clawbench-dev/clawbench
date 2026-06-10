import { describe, expect, it } from 'vitest'
import {
  parseAskQuestionXML,
  parseAskQuestionJSON,
  parseRagResultsXML,
  detectRagResults,
  stripRagResultsTags,
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

// ─── parseRagResultsXML ──────────────────────────────────────────────────

describe('parseRagResultsXML', () => {
  it('parses single rag-item', () => {
    const xml = `<rag-results>
  <rag-item>
    <session-id>abc-123</session-id>
    <session-title>Fix Login Bug</session-title>
    <created-at>2026-07-01T10:30:00Z</created-at>
    <summary>JWT expiry issue resolved</summary>
  </rag-item>
</rag-results>`

    const result = parseRagResultsXML(xml)
    expect(result).toHaveLength(1)
    expect(result[0]).toEqual({
      sessionId: 'abc-123',
      sessionTitle: 'Fix Login Bug',
      createdAt: '2026-07-01T10:30:00Z',
      summary: 'JWT expiry issue resolved',
    })
  })

  it('parses multiple rag-items', () => {
    const xml = `<rag-results>
  <rag-item>
    <session-id>abc-123</session-id>
    <session-title>Bug 1</session-title>
    <created-at>2026-07-01T10:30:00Z</created-at>
    <summary>Summary 1</summary>
  </rag-item>
  <rag-item>
    <session-id>def-456</session-id>
    <session-title>Bug 2</session-title>
    <created-at>2026-06-28T14:20:00Z</created-at>
    <summary>Summary 2</summary>
  </rag-item>
</rag-results>`

    const result = parseRagResultsXML(xml)
    expect(result).toHaveLength(2)
    expect(result[0].sessionId).toBe('abc-123')
    expect(result[1].sessionId).toBe('def-456')
  })

  it('returns empty array for invalid XML', () => {
    const result = parseRagResultsXML('not xml')
    expect(result).toEqual([])
  })

  it('returns empty array for XML without rag-items', () => {
    const result = parseRagResultsXML('<rag-results><something>else</something></rag-results>')
    expect(result).toEqual([])
  })

  it('handles missing optional fields with empty string', () => {
    const xml = `<rag-results>
  <rag-item>
    <session-id>abc-123</session-id>
    <session-title>Title</session-title>
    <created-at>2026-01-01T00:00:00Z</created-at>
    <summary>Summary</summary>
  </rag-item>
</rag-results>`

    const result = parseRagResultsXML(xml)
    expect(result).toHaveLength(1)
    expect(result[0].sessionId).toBe('abc-123')
  })
})

// ─── detectRagResults ────────────────────────────────────────────────────

describe('detectRagResults', () => {
  it('detects rag-results tag', () => {
    const text = 'Here are the results:\n<rag-results>\n<rag-item>\n<session-id>abc</session-id>\n</rag-item>\n</rag-results>\nDone.'
    const result = detectRagResults(text)
    expect(result.found).toBe(true)
    expect(result.startIdx).toBeGreaterThanOrEqual(0)
  })

  it('returns found=false when no rag-results tag', () => {
    const result = detectRagResults('no rag results here')
    expect(result.found).toBe(false)
  })
})

// ─── stripRagResultsTags ─────────────────────────────────────────────────

describe('stripRagResultsTags', () => {
  it('strips rag-results tags from text', () => {
    const text = 'Before <rag-results><rag-item><session-id>abc</session-id></rag-item></rag-results> After'
    const result = stripRagResultsTags(text)
    expect(result).toBe('Before After')
  })

  it('returns original text if no rag-results tags', () => {
    const text = 'No rag results here'
    expect(stripRagResultsTags(text)).toBe('No rag results here')
  })
})
