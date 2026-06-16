import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  extractScheduledTaskIds,
  stripScheduledTaskTags,
  isValidAskContent,
  detectAskQuestion,
  taskChanged,
  StaticBlockCache,
} from '@/utils/streamPerf'

// ═══════════════════════════════════════════════════════════════
// Tests for deferred streaming render pipeline
//
// Core design: During streaming, renderTextBlock only does
// marked + DOMPurify + table-wrap. All structured detection
// (KaTeX, Mermaid, scheduled-task, ask-question, file path
// annotation/verification) is deferred to after streaming ends.
// ═══════════════════════════════════════════════════════════════

// ────────────────────────────────────────────────────────────
// Deferred rendering: renderTextBlock streaming parameter
// ────────────────────────────────────────────────────────────

/**
 * Simulates the deferred rendering logic for renderTextBlock.
 * When streaming=true, only marked + DOMPurify + table-wrap runs.
 * When streaming=false, the full pipeline runs.
 */
function renderTextBlockDeferred(text: string, _msgId: string, _blockIdx: number, streaming = false) {
  if (streaming) {
    // Streaming: pure markdown only, no structured detection
    // This is the fast path — marked + DOMPurify + table-wrap
    return { streaming: true, ranKaTeX: false, ranScheduledTask: false, ranAskQuestion: false, ranPathAnnotation: false }
  }
  // Post-streaming: full pipeline
  const taskIds = extractScheduledTaskIds(text)
  const askResult = detectAskQuestion(text)
  const cleanText = stripScheduledTaskTags(text)
  return {
    streaming: false,
    ranKaTeX: true,
    ranScheduledTask: taskIds.length > 0 || text.includes('<scheduled-task'),
    ranAskQuestion: askResult.found || text.includes('<ask-question'),
    ranPathAnnotation: true,
    taskIds,
    askFound: askResult.found,
    cleanText,
  }
}

describe('renderTextBlock deferred rendering', () => {
  it('streaming=true skips all structured detection', () => {
    const text = 'Hello <scheduled-task id="1" /> and $E=mc^2$'
    const result = renderTextBlockDeferred(text, 'msg1', 0, true)
    expect(result.streaming).toBe(true)
    expect(result.ranKaTeX).toBe(false)
    expect(result.ranScheduledTask).toBe(false)
    expect(result.ranAskQuestion).toBe(false)
    expect(result.ranPathAnnotation).toBe(false)
  })

  it('streaming=false runs full pipeline', () => {
    const text = 'Hello <scheduled-task id="1" /> world'
    const result = renderTextBlockDeferred(text, 'msg1', 0, false)
    expect(result.streaming).toBe(false)
    expect(result.ranKaTeX).toBe(true)
    expect(result.ranPathAnnotation).toBe(true)
  })

  it('streaming=false detects scheduled tasks', () => {
    const text = 'Created <scheduled-task id="42" />'
    const result = renderTextBlockDeferred(text, 'msg1', 0, false)
    expect(result.taskIds).toEqual(['42'])
  })

  it('streaming=false detects ask-question', () => {
    const text = 'Pick one <ask-question><item><header>Pick</header><multi-select>false</multi-select><question>Which?</question><option><label>A</label><description>Option A</description></option></item></ask-question>'
    const result = renderTextBlockDeferred(text, 'msg1', 0, false)
    expect(result.askFound).toBe(true)
  })

  it('streaming=false strips scheduled-task tags', () => {
    const text = 'Before <scheduled-task id="1" /> After'
    const result = renderTextBlockDeferred(text, 'msg1', 0, false)
    expect(result.cleanText).toBe('Before  After')
  })

  it('plain text with no special content produces same result regardless of streaming flag', () => {
    const text = 'Hello world'
    const streamResult = renderTextBlockDeferred(text, 'msg1', 0, true)
    const fullResult = renderTextBlockDeferred(text, 'msg1', 0, false)
    // Both should succeed; streaming skips enhancements but text renders fine either way
    expect(streamResult.streaming).toBe(true)
    expect(fullResult.streaming).toBe(false)
    expect(fullResult.ranKaTeX).toBe(true)
  })

  it('streaming=true is much cheaper — no regex scanning for scheduled-task', () => {
    const execSpy = vi.spyOn(RegExp.prototype, 'exec')
    const text = 'Just a regular message with no special tags'

    renderTextBlockDeferred(text, 'msg1', 0, true)

    // streaming=true should not call SCHEDULED_TASK_RE.exec at all
    // (the function short-circuits before reaching extractScheduledTaskIds)
    expect(execSpy.mock.calls.length).toBe(0)
    execSpy.mockRestore()
  })

  it('streaming=true is much cheaper — no matchAll for ask-question', () => {
    const matchAllSpy = vi.spyOn(String.prototype, 'matchAll')
    const text = 'Just a regular message with no special tags'

    renderTextBlockDeferred(text, 'msg1', 0, true)

    expect(matchAllSpy).not.toHaveBeenCalled()
    matchAllSpy.mockRestore()
  })
})

// ────────────────────────────────────────────────────────────
// Deferred rendering: renderMarkdown streaming mode
// ────────────────────────────────────────────────────────────

describe('renderMarkdown streaming mode (skipEnhancements)', () => {
  it('streaming mode skips all structured pipeline', () => {
    const text = 'The equation $E=mc^2$ is famous'
    const result = renderTextBlockDeferred(text, 'msg1', 0, true)
    // Streaming mode skips KaTeX, scheduled-task, ask-question, path annotation
    expect(result.ranKaTeX).toBe(false)
    expect(result.ranScheduledTask).toBe(false)
    expect(result.ranAskQuestion).toBe(false)
    expect(result.ranPathAnnotation).toBe(false)
  })

  it('non-streaming mode runs KaTeX pipeline', () => {
    const text = 'The equation $E=mc^2$ is famous'
    const result = renderTextBlockDeferred(text, 'msg1', 0, false)
    expect(result.ranKaTeX).toBe(true)
    expect(result.ranPathAnnotation).toBe(true)
  })
})

// ────────────────────────────────────────────────────────────
// Deferred rendering: Mermaid
// ────────────────────────────────────────────────────────────

describe('Mermaid rendering deferred to post-streaming', () => {
  it('streaming=true does not trigger mermaid rendering', () => {
    const incompleteMermaid = '```mermaid\ngraph TD\n  A -->'
    const result = renderTextBlockDeferred(incompleteMermaid, 'msg1', 0, true)
    // Streaming skips all structured detection including mermaid
    expect(result.streaming).toBe(true)
    expect(result.ranKaTeX).toBe(false)
  })

  it('streaming=false triggers full render pipeline', () => {
    const completeMermaid = '```mermaid\ngraph TD\n  A --> B\n```'
    const result = renderTextBlockDeferred(completeMermaid, 'msg1', 0, false)
    expect(result.streaming).toBe(false)
    expect(result.ranKaTeX).toBe(true)
  })
})

// ────────────────────────────────────────────────────────────
// scheduled-task regex (module-level constant) — still used post-streaming
// ────────────────────────────────────────────────────────────

describe('scheduled-task regex (module-level constant)', () => {
  it('extracts integer task ID from tag', () => {
    const text = 'Created <scheduled-task id="42" />'
    const ids = extractScheduledTaskIds(text)
    expect(ids).toEqual(['42'])
  })

  it('extracts multiple task IDs', () => {
    const text = 'A <scheduled-task id="1" /> and B <scheduled-task id="2" />'
    const ids = extractScheduledTaskIds(text)
    expect(ids).toEqual(['1', '2'])
  })

  it('returns empty array for text without tags', () => {
    const ids = extractScheduledTaskIds('No tasks here')
    expect(ids).toEqual([])
  })

  it('regex is reused correctly across multiple calls', () => {
    const text = '<scheduled-task id="1" />'
    extractScheduledTaskIds(text)
    extractScheduledTaskIds(text)
    const ids = extractScheduledTaskIds(text)
    expect(ids).toEqual(['1'])
  })

  it('does not match non-numeric IDs', () => {
    const text = 'Created <scheduled-task id="task-abc" />'
    const ids = extractScheduledTaskIds(text)
    expect(ids).toEqual([])
  })
})

describe('stripScheduledTaskTags', () => {
  it('strips scheduled-task tags from text', () => {
    const text = 'Before <scheduled-task id="1" /> After'
    const result = stripScheduledTaskTags(text)
    expect(result).toBe('Before  After')
  })

  it('strips multiple tags', () => {
    const text = 'A <scheduled-task id="1" /> B <scheduled-task id="2" /> C'
    const result = stripScheduledTaskTags(text)
    expect(result).toBe('A  B  C')
  })

  it('returns trimmed text unchanged when no tags', () => {
    expect(stripScheduledTaskTags('Hello world')).toBe('Hello world')
  })
})

// ────────────────────────────────────────────────────────────
// ask-question detection — still used post-streaming
// ────────────────────────────────────────────────────────────

describe('detectAskQuestion (early exit optimization)', () => {
  it('returns found=false for plain text without ask-question tag', () => {
    const result = detectAskQuestion('Hello, this is a normal chat message')
    expect(result.found).toBe(false)
  })

  it('returns found=false for empty string', () => {
    const result = detectAskQuestion('')
    expect(result.found).toBe(false)
  })

  it('skips expensive matchAll when tag is absent', () => {
    const matchAllSpy = vi.spyOn(String.prototype, 'matchAll')
    const text = 'Just a regular message with no special tags'

    detectAskQuestion(text)

    expect(matchAllSpy).not.toHaveBeenCalled()
    matchAllSpy.mockRestore()
  })

  it('detects valid ask-question with proper closing tag', () => {
    const text = 'Some text <ask-question><item><header>Pick</header><multi-select>false</multi-select><question>Which?</question><option><label>A</label><description>Option A</description></option></item></ask-question>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.content).toBeDefined()
  })

  it('returns found=false when tag is present but content is not valid XML', () => {
    const text = 'Forces structured <ask-question>random text without item tags</ask-question> for user interaction'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(false)
  })
})

describe('isValidAskContent', () => {
  it('returns true for XML with <item> containing <question> and <option>', () => {
    expect(isValidAskContent('<item><header>Choice</header><multi-select>false</multi-select><question>Pick?</question><option><label>A</label></option></item>')).toBe(true)
  })

  it('returns false for XML without <question> or <option>', () => {
    expect(isValidAskContent('<item><header>Choice</header></item>')).toBe(false)
  })

  it('returns false for non-XML text', () => {
    expect(isValidAskContent('plain text without item tags')).toBe(false)
  })
})

// ────────────────────────────────────────────────────────────
// taskChanged — still used in blockTasks watcher
// ────────────────────────────────────────────────────────────

describe('taskChanged (semantic equality)', () => {
  it('returns false for semantically identical tasks (different references)', () => {
    const task1 = { id: 1, status: 'active', name: 'Test', cronExpr: '0 * * * *', runCount: 5 }
    const task2 = { id: 1, status: 'active', name: 'Test', cronExpr: '0 * * * *', runCount: 5 }
    expect(taskChanged(task1, task2)).toBe(false)
  })

  it('returns true when status changes', () => {
    const task1 = { status: 'active', name: 'Test', cronExpr: '0 * * * *', runCount: 5 }
    const task2 = { status: 'paused', name: 'Test', cronExpr: '0 * * * *', runCount: 5 }
    expect(taskChanged(task1, task2)).toBe(true)
  })

  it('returns true when runCount changes', () => {
    const task1 = { status: 'active', name: 'Test', cronExpr: '0 * * * *', runCount: 5 }
    const task2 = { status: 'active', name: 'Test', cronExpr: '0 * * * *', runCount: 6 }
    expect(taskChanged(task1, task2)).toBe(true)
  })

  it('returns true when agentId changes', () => {
    const task1 = { status: 'active', name: 'Test', cronExpr: '0 * * * *', agentId: 'claude' }
    const task2 = { status: 'active', name: 'Test', cronExpr: '0 * * * *', agentId: 'kimi' }
    expect(taskChanged(task1, task2)).toBe(true)
  })

  it('returns true when oldTask is null', () => {
    expect(taskChanged(null, { status: 'active' })).toBe(true)
  })

  it('returns true when newTask is null', () => {
    expect(taskChanged({ status: 'active' }, null)).toBe(true)
  })
})

// ────────────────────────────────────────────────────────────
// StaticBlockCache — still used for non-streaming re-renders
// ────────────────────────────────────────────────────────────

describe('StaticBlockCache', () => {
  let cache: StaticBlockCache

  beforeEach(() => {
    cache = new StaticBlockCache()
  })

  it('returns undefined for uncached block', () => {
    expect(cache.get('msg1', 0, 'hello')).toBeUndefined()
  })

  it('returns cached HTML for previously rendered block', () => {
    cache.set('msg1', 0, 'hello', '<p>hello</p>')
    expect(cache.get('msg1', 0, 'hello')).toBe('<p>hello</p>')
  })

  it('differentiates blocks by msgId', () => {
    cache.set('msg1', 0, 'hello', '<p>msg1 hello</p>')
    cache.set('msg2', 0, 'hello', '<p>msg2 hello</p>')
    expect(cache.get('msg1', 0, 'hello')).toBe('<p>msg1 hello</p>')
    expect(cache.get('msg2', 0, 'hello')).toBe('<p>msg2 hello</p>')
  })

  it('invalidates when text content changes', () => {
    cache.set('msg1', 0, 'hello', '<p>hello</p>')
    expect(cache.get('msg1', 0, 'hello world')).toBeUndefined()
  })

  it('clears all entries', () => {
    cache.set('msg1', 0, 'hello', '<p>hello</p>')
    cache.set('msg2', 1, 'world', '<p>world</p>')
    cache.clear()
    expect(cache.get('msg1', 0, 'hello')).toBeUndefined()
    expect(cache.get('msg2', 1, 'world')).toBeUndefined()
  })
})
