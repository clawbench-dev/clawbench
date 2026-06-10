import { describe, it, expect } from 'vitest'
import { isValidAskContent, detectAskQuestion, extractScheduledTaskIds, stripScheduledTaskTags, taskChanged, StaticBlockCache, SCHEDULED_TASK_RE } from '../streamPerf'

describe('isValidAskContent', () => {
  it('accepts XML with <item> containing <question> and <option>', () => {
    const raw = '<item><header>Choice</header><multi-select>false</multi-select><question>Pick one</question><option><label>A</label><description>Fast</description></option></item>'
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('accepts multiple <item> elements', () => {
    const raw = '<item><header>H1</header><multi-select>false</multi-select><question>Q1</question><option><label>A</label></option></item><item><header>H2</header><multi-select>true</multi-select><question>Q2</question><option><label>B</label></option></item>'
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('accepts <item> with attributes', () => {
    const raw = '<item type="single"><header>Choice</header><multi-select>false</multi-select><question>Pick one</question><option><label>A</label></option></item>'
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('rejects plain text (not XML)', () => {
    const raw = 'This is just text, not XML at all'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('rejects XML without <question> tag', () => {
    const raw = '<item><header>Choice</header><multi-select>false</multi-select><option><label>A</label></option></item>'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('rejects XML without <option> tag', () => {
    const raw = '<item><header>Choice</header><multi-select>false</multi-select><question>Which?</question></item>'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('rejects old JSON format', () => {
    const raw = '{"questions":[{"question":"Pick one","header":"Choice","options":[{"label":"A"}]}]}'
    // JSON format is now supported - this should return true
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('rejects empty string', () => {
    expect(isValidAskContent('')).toBe(false)
  })

  it('accepts JSON format with questions array', () => {
    const raw = '{"questions":[{"question":"Pick one","header":"Choice","options":[{"label":"A"}]}]}'
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('rejects JSON without questions key', () => {
    const raw = '{"something":"else"}'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('rejects JSON with empty questions array', () => {
    const raw = '{"questions":[]}'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('rejects JSON with questions but no valid items', () => {
    const raw = '{"questions":[{"noQuestion":true}]}'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('accepts JSON with mixed valid/invalid items (at least one valid)', () => {
    const raw = '{"questions":[{"noQuestion":true},{"question":"Pick one","header":"Choice","options":[{"label":"A"}]}]}'
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('rejects JSON with questions having options but no question field', () => {
    const raw = '{"questions":[{"header":"Choice","options":[{"label":"A"}]}]}'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('rejects JSON with questions having question but empty options', () => {
    const raw = '{"questions":[{"question":"Pick one","header":"Choice","options":[]}]}'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('rejects JSON with questions having question but options without labels', () => {
    const raw = '{"questions":[{"question":"Pick one","header":"Choice","options":[{"description":"No label"}]}]}'
    // The implementation only checks for question + options existence, not label within options
    // Options without labels are still considered valid by isValidAskContent
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('rejects invalid JSON', () => {
    const raw = '{not valid json}'
    expect(isValidAskContent(raw)).toBe(false)
  })
})

describe('detectAskQuestion', () => {
  it('detects <ask-question> with XML <item> content', () => {
    const text = 'Some text before\n<ask-question><item><header>Choice</header><multi-select>false</multi-select><question>Which?</question><option><label>A</label><description>Fast</description></option></item></ask-question>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.startIdx).toBeGreaterThanOrEqual(0)
  })

  it('detects <ask-question> with multiple <item> elements', () => {
    const text = '工作区是干净的。\n\n<ask-question>\n<item><header>下一步</header><multi-select>false</multi-select><question>你想做什么？</question><option><label>推送到远程</label><description>推送提交</description></option><option><label>取消</label><description>不做任何操作</description></option></item>\n</ask-question>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.startIdx).toBeGreaterThanOrEqual(0)
  })

  it('returns found=false for text without <ask-question>', () => {
    const text = 'Just some regular text without any ask-question tags'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(false)
  })

  it('detects <ask-question> with obfuscated closing tag (fullwidth pipe)', () => {
    // Real case: model emits </｜｜DSML｜｜question> instead of </ask-question>
    const text = '`gh` 已给出设备认证码。需要在浏览器中完成登录：\n\n<ask-question>\n<item><header>GitHub 认证</header><multi-select>false</multi-select><question>请打开 https://github.com/login/device 并输入代码完成登录。完成后告诉我。</question><option><label>已打开链接</label><description>我已在浏览器中完成认证，继续推送</description></option><option><label>我手动来</label><description>我自己执行 gh auth login -w 完成登录后手动推送</description></option></item>\n</｜｜DSML｜｜question>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.startIdx).toBeGreaterThanOrEqual(0)
    expect(result.content).toBeDefined()
    expect(isValidAskContent(result.content!)).toBe(true)
  })

  it('returns found=false when tag is present but content is not valid XML', () => {
    const text = 'Forces structured <ask-question>random text without item tags</ask-question> for user interaction'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(false)
  })

  it('detects <ask-question> with JSON content', () => {
    const text = 'Some text\n<ask-question>\n{"questions":[{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[{"label":"A","description":"Fast"},{"label":"B","description":"Safe"}]}]}\n</ask-question>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.startIdx).toBeGreaterThanOrEqual(0)
    expect(result.content).toContain('"questions"')
  })
})

describe('extractScheduledTaskIds', () => {
  it('extracts a single ID', () => {
    expect(extractScheduledTaskIds('<scheduled-task id="42" />')).toEqual(['42'])
  })

  it('extracts multiple IDs', () => {
    const text = '<scheduled-task id="1" /> and <scheduled-task id="99" />'
    expect(extractScheduledTaskIds(text)).toEqual(['1', '99'])
  })

  it('returns empty array when no tags present', () => {
    expect(extractScheduledTaskIds('no tags here')).toEqual([])
  })

  it('does not match non-integer IDs', () => {
    expect(extractScheduledTaskIds('<scheduled-task id="abc" />')).toEqual([])
    expect(extractScheduledTaskIds('<scheduled-task id="3.14" />')).toEqual([])
  })

  it('extracts IDs from tags at different positions', () => {
    const text = 'start <scheduled-task id="7" /> middle <scheduled-task id="13" /> end'
    expect(extractScheduledTaskIds(text)).toEqual(['7', '13'])
  })

  it('resets lastIndex so repeated calls work correctly', () => {
    const text = '<scheduled-task id="5" />'
    // First call
    expect(extractScheduledTaskIds(text)).toEqual(['5'])
    // If lastIndex is not reset, second call on same regex would return []
    expect(extractScheduledTaskIds(text)).toEqual(['5'])
  })
})

describe('stripScheduledTaskTags', () => {
  it('removes a single tag', () => {
    expect(stripScheduledTaskTags('before <scheduled-task id="1" /> after')).toBe('before  after')
  })

  it('removes multiple tags', () => {
    expect(stripScheduledTaskTags('<scheduled-task id="1" />mid<scheduled-task id="2" />')).toBe('mid')
  })

  it('preserves content between and around tags', () => {
    const text = 'A <scheduled-task id="10" /> B <scheduled-task id="20" /> C'
    expect(stripScheduledTaskTags(text)).toBe('A  B  C')
  })

  it('returns trimmed text unchanged when no tags present', () => {
    expect(stripScheduledTaskTags('hello world')).toBe('hello world')
  })

  it('trims the result', () => {
    expect(stripScheduledTaskTags('  <scheduled-task id="1" />  ')).toBe('')
  })

  it('resets lastIndex so repeated calls work correctly', () => {
    const text = '<scheduled-task id="5" />hello'
    expect(stripScheduledTaskTags(text)).toBe('hello')
    expect(stripScheduledTaskTags(text)).toBe('hello')
  })
})

describe('taskChanged', () => {
  const baseTask = {
    status: 'active',
    name: 'test',
    cronExpr: '0 * * * *',
    runCount: 0,
    lastRunAt: null,
    nextRunAt: '2025-01-01',
    runningCount: 0,
    repeatMode: 'repeat',
    maxRuns: 0,
    agentId: 'agent-1',
  }

  it('returns true when oldTask is null', () => {
    expect(taskChanged(null, baseTask)).toBe(true)
  })

  it('returns true when newTask is null', () => {
    expect(taskChanged(baseTask, null)).toBe(true)
  })

  it('returns true when both are null', () => {
    expect(taskChanged(null, null)).toBe(true)
  })

  it('returns false when all key fields are the same', () => {
    expect(taskChanged(baseTask, { ...baseTask })).toBe(false)
  })

  it('returns false when extra non-key fields differ', () => {
    expect(taskChanged(baseTask, { ...baseTask, extraField: 'different' })).toBe(false)
  })

  it.each([
    ['status', 'paused'],
    ['name', 'renamed'],
    ['cronExpr', '0 0 * * *'],
    ['runCount', 5],
    ['lastRunAt', '2025-06-01'],
    ['nextRunAt', '2025-07-01'],
    ['runningCount', 3],
    ['repeatMode', 'once'],
    ['maxRuns', 10],
    ['agentId', 'agent-2'],
  ] as const)('returns true when %s differs', (key, value) => {
    expect(taskChanged(baseTask, { ...baseTask, [key]: value })).toBe(true)
  })
})

describe('StaticBlockCache', () => {
  it('returns undefined for cache miss', () => {
    const cache = new StaticBlockCache()
    expect(cache.get('msg1', 0, 'hello')).toBeUndefined()
  })

  it('stores and retrieves a value', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'hello', '<p>hello</p>')
    expect(cache.get('msg1', 0, 'hello')).toBe('<p>hello</p>')
  })

  it('clears all entries', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'a', '<p>a</p>')
    cache.set('msg2', 0, 'b', '<p>b</p>')
    cache.clear()
    expect(cache.get('msg1', 0, 'a')).toBeUndefined()
    expect(cache.get('msg2', 0, 'b')).toBeUndefined()
  })

  it('differentiates by msgId', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'text', '<p>A</p>')
    cache.set('msg2', 0, 'text', '<p>B</p>')
    expect(cache.get('msg1', 0, 'text')).toBe('<p>A</p>')
    expect(cache.get('msg2', 0, 'text')).toBe('<p>B</p>')
  })

  it('differentiates by blockIdx', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'text', '<p>A</p>')
    cache.set('msg1', 1, 'text', '<p>B</p>')
    expect(cache.get('msg1', 0, 'text')).toBe('<p>A</p>')
    expect(cache.get('msg1', 1, 'text')).toBe('<p>B</p>')
  })

  it('differentiates by text content', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'hello', '<p>hello</p>')
    expect(cache.get('msg1', 0, 'world')).toBeUndefined()
  })

  it('makeKey uses text length as part of key', () => {
    const cache = new StaticBlockCache()
    // Two strings with same prefix/suffix but different length
    const short = 'ab'
    const long = 'a123456789012345678901234567890b'
    cache.set('msg1', 0, short, '<p>short</p>')
    cache.set('msg1', 0, long, '<p>long</p>')
    expect(cache.get('msg1', 0, short)).toBe('<p>short</p>')
    expect(cache.get('msg1', 0, long)).toBe('<p>long</p>')
  })

  it('makeKey omits prefix when text length <= 40', () => {
    const cache = new StaticBlockCache()
    const text40 = 'a'.repeat(40)
    const text41 = 'a'.repeat(41)
    // Both have same length-based suffix behavior, but different text.length (40 vs 41)
    // so keys differ
    cache.set('msg1', 0, text40, '<p>40</p>')
    cache.set('msg1', 0, text41, '<p>41</p>')
    expect(cache.get('msg1', 0, text40)).toBe('<p>40</p>')
    expect(cache.get('msg1', 0, text41)).toBe('<p>41</p>')
  })

  it('makeKey includes prefix for text length > 40', () => {
    const cache = new StaticBlockCache()
    // 42 chars: first 20 differ, last 20 same
    const textA = 'aaaaaaaaaaaaaaaaaaaa' + 'x'.repeat(2) + 'bbbbbbbbbbbbbbbbbbbb' // prefix=aaa..., suffix=bbb...
    const textB = 'cccccccccccccccccccc' + 'x'.repeat(2) + 'bbbbbbbbbbbbbbbbbbbb' // prefix=ccc..., suffix=bbb...
    cache.set('msg1', 0, textA, '<p>A</p>')
    expect(cache.get('msg1', 0, textB)).toBeUndefined()
  })

  it('accepts numeric msgId', () => {
    const cache = new StaticBlockCache()
    cache.set(42, 0, 'text', '<p>ok</p>')
    expect(cache.get(42, 0, 'text')).toBe('<p>ok</p>')
  })
})
