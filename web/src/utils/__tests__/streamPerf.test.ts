import { describe, it, expect, vi } from 'vitest'
import { isValidAskContent, detectAskQuestion, stripAskQuestionTag, extractScheduledTaskIds, stripScheduledTaskTags, taskChanged, StaticBlockCache } from '../streamPerf'

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

  // An item is renderable when it carries question text OR at least one option
  // — the bar the renderer applies. Detection is now "does it parse", so these
  // are no longer rejected on a literal-substring technicality.
  it('accepts XML with <option> but no <question>', () => {
    const raw = '<item><header>Choice</header><multi-select>false</multi-select><option><label>A</label></option></item>'
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('accepts XML with <question> but no <option>', () => {
    const raw = '<item><header>Choice</header><multi-select>false</multi-select><question>Which?</question></item>'
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('rejects an item with neither question nor options', () => {
    const raw = '<item><header>Choice</header><multi-select>false</multi-select></item>'
    expect(isValidAskContent(raw)).toBe(false)
  })

  it('recovers JSON content rather than rejecting it', () => {
    const raw = '{"questions":[{"question":"Pick one","header":"Choice","options":[{"label":"A"}]}]}'
    expect(isValidAskContent(raw)).toBe(true)
  })

  it('rejects empty string', () => {
    expect(isValidAskContent('')).toBe(false)
  })
})

describe('detectAskQuestion', () => {
  it('detects <ask-question> with XML <item> content', () => {
    const text = 'Some text before\n<ask-question><item><header>Choice</header><multi-select>false</multi-select><question>Which?</question><option><label>A</label><description>Fast</description></option></item></ask-question>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.matches).toHaveLength(1)
    expect(result.matches[0].raw).toContain('<ask-question>')
    expect(result.matches[0].raw).toContain('</ask-question>')
    expect(result.items[0].question).toBe('Which?')
  })

  it('detects <ask-question> with multiple <item> elements', () => {
    const text = '工作区是干净的。\n\n<ask-question>\n<item><header>下一步</header><multi-select>false</multi-select><question>你想做什么？</question><option><label>推送到远程</label><description>推送提交</description></option><option><label>取消</label><description>不做任何操作</description></option></item>\n</ask-question>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.items).toHaveLength(1)
    expect(result.items[0].options).toHaveLength(2)
  })

  it('detects every tag when several are present', () => {
    const one = '<ask-question><item><header>Q1</header><multi-select>false</multi-select><question>第一个?</question><option><label>A</label></option></item></ask-question>'
    const two = '<ask-question><item><header>Q2</header><multi-select>false</multi-select><question>第二个?</question><option><label>B</label></option></item></ask-question>'
    const result = detectAskQuestion(`${one}\n中间\n${two}`)
    expect(result.found).toBe(true)
    expect(result.items.map(i => i.header)).toEqual(['Q1', 'Q2'])
  })

  it('returns found=false for text without <ask-question>', () => {
    const text = 'Just some regular text without any ask-question tags'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(false)
  })

  it('detects <ask-question> with obfuscated closing tag (fullwidth pipe)', () => {
    // Real case: model emits a non-standard closing tag with fullwidth pipes
    // instead of </ask-question>
    const text = '`gh` 已给出设备认证码。需要在浏览器中完成登录：\n\n<ask-question>\n<item><header>GitHub 认证</header><multi-select>false</multi-select><question>请打开 https://github.com/login/device 并输入代码完成登录。完成后告诉我。</question><option><label>已打开链接</label><description>我已在浏览器中完成认证，继续推送</description></option><option><label>我手动来</label><description>我自己执行 gh auth login -w 完成登录后手动推送</description></option></item>\n</\uFF5C\uFF5CDSML\uFF5C\uFF5Cquestion>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.matches).toHaveLength(1)
    expect(result.items).toHaveLength(1)
    expect(result.items[0].header).toBe('GitHub 认证')
  })

  it('returns found=false when tag is present but content is not valid XML', () => {
    const text = 'Forces structured <ask-question>random text without item tags</ask-question> for user interaction'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(false)
  })

  it('recovers <ask-question> with JSON content', () => {
    const text = 'Some text\n<ask-question>\n{"questions":[{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[{"label":"A","description":"Fast"},{"label":"B","description":"Safe"}]}]}\n</ask-question>'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    expect(result.items).toHaveLength(1)
    expect(result.items[0].header).toBe('Approach')
  })

  it('returns found=false when <ask-question> is mentioned without structured content', () => {
    const text = 'The ask-question system uses <ask-question> tags. The function detectAskQuestionInText checks for <ask-question in block.text.'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(false)
  })

  it('returns found=false when <ask-question> tag has only description text, no item/question/option', () => {
    // Model discusses the tag format but doesn't actually emit a structured question
    const text = 'You can use `<ask-question>` to present choices. Here is how the tag works: <ask-question>Each question needs item, question and option elements</ask-question>. That is all.'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(false)
  })

  it('returns found=false when <ask-question> appears only inside a code block', () => {
    const text = 'You can use the `<ask-question>` tag to present choices:\n\n```\n<ask-question>\n<item><header>Choice</header><question>Pick one</question><option><label>A</label></option></item>\n</ask-question>\n```\n\nThis creates an interactive card.'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(false)
  })
})

describe('stripAskQuestionTag', () => {
  it('removes the ask-question tag from text', () => {
    const text = 'Before\n<ask-question><item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label></option></item></ask-question>\nAfter'
    const result = detectAskQuestion(text)
    expect(result.found).toBe(true)
    const stripped = stripAskQuestionTag(text, result)
    expect(stripped).toContain('Before')
    expect(stripped).toContain('After')
    expect(stripped).not.toContain('<ask-question')
  })

  it('returns original text when result is not found', () => {
    const text = 'Hello world'
    const result = detectAskQuestion(text)
    expect(stripAskQuestionTag(text, result)).toBe('Hello world')
  })

  it('handles ask-question tag at the start of text', () => {
    const text = '<ask-question><item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label></option></item></ask-question>\nRemaining text'
    const result = detectAskQuestion(text)
    const stripped = stripAskQuestionTag(text, result)
    expect(stripped).toBe('Remaining text')
  })

  it('handles ask-question tag at the end of text', () => {
    const text = 'Some text before\n<ask-question><item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label></option></item></ask-question>'
    const result = detectAskQuestion(text)
    const stripped = stripAskQuestionTag(text, result)
    expect(stripped).toContain('Some text before')
    expect(stripped).not.toContain('<ask-question')
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

  // ── Bounded retention (LRU) ──
  //
  // The cache is deliberately NOT cleared on session/project switch (keys are
  // globally-unique DB message ids), so it needs a bound. These tests pin the
  // eviction policy: least-recently-USED, not simply oldest-inserted.

  it('retains entries across a simulated session switch (no clear)', () => {
    const cache = new StaticBlockCache()
    cache.set('session-a-msg', 0, 'text', '<p>from session A</p>')

    // A session switch no longer calls clear(); the old entry must survive.
    cache.set('session-b-msg', 0, 'text', '<p>from session B</p>')

    expect(cache.get('session-a-msg', 0, 'text')).toBe('<p>from session A</p>')
    expect(cache.get('session-b-msg', 0, 'text')).toBe('<p>from session B</p>')
  })

  it('evicts least-recently-used entries once over the cap', () => {
    const cache = new StaticBlockCache()
    const MAX = 3000

    for (let i = 0; i < MAX; i++) {
      cache.set(`msg${i}`, 0, 'text', `<p>${i}</p>`)
    }
    expect(cache.size).toBe(MAX)

    // Refresh msg0 so it becomes the most recently used.
    expect(cache.get('msg0', 0, 'text')).toBe('<p>0</p>')

    // One more insert pushes us over the cap.
    cache.set('msg-overflow', 0, 'text', '<p>new</p>')
    expect(cache.size).toBe(MAX)

    // msg0 was touched, so it must survive; msg1 (next oldest) is evicted.
    expect(cache.get('msg0', 0, 'text')).toBe('<p>0</p>')
    expect(cache.get('msg1', 0, 'text')).toBeUndefined()
    expect(cache.get('msg-overflow', 0, 'text')).toBe('<p>new</p>')
  })

  it('re-setting an existing key refreshes its recency and does not grow the cache', () => {
    const cache = new StaticBlockCache()
    const MAX = 3000

    for (let i = 0; i < MAX; i++) {
      cache.set(`msg${i}`, 0, 'text', `<p>${i}</p>`)
    }

    // Overwrite the oldest entry — same key, new value.
    cache.set('msg0', 0, 'text', '<p>updated</p>')
    expect(cache.size).toBe(MAX)

    cache.set('msg-overflow', 0, 'text', '<p>new</p>')
    expect(cache.size).toBe(MAX)

    // msg0 was re-set (most recent), so msg1 is the eviction victim.
    expect(cache.get('msg0', 0, 'text')).toBe('<p>updated</p>')
    expect(cache.get('msg1', 0, 'text')).toBeUndefined()
  })

  it('eviction also drops the deferred flag for the evicted key', () => {
    const cache = new StaticBlockCache()
    const MAX = 3000

    cache.set('oldest', 0, 'text', '<p>old</p>', true)
    expect(cache.isDeferred('oldest', 0, 'text')).toBe(true)

    for (let i = 0; i < MAX; i++) {
      cache.set(`msg${i}`, 0, 'text', `<p>${i}</p>`)
    }

    // 'oldest' was evicted, so its deferred flag must be gone too — otherwise
    // scheduleUpgrade would keep chasing a key that is no longer cached.
    expect(cache.isDeferred('oldest', 0, 'text')).toBe(false)
    expect(cache.deferredCount).toBe(0)
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

  // ── Deferred enhancement support ──

  it('tracks deferred entries', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'text', '<p>basic</p>', true)
    expect(cache.isDeferred('msg1', 0, 'text')).toBe(true)
    expect(cache.deferredCount).toBe(1)
  })

  it('non-deferred entries are not tracked as deferred', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'text', '<p>full</p>', false)
    expect(cache.isDeferred('msg1', 0, 'text')).toBe(false)
    expect(cache.deferredCount).toBe(0)
  })

  it('markUpgraded removes entry from deferred set', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'text', '<p>basic</p>', true)
    expect(cache.isDeferred('msg1', 0, 'text')).toBe(true)

    cache.markUpgraded('msg1', 0, 'text')
    expect(cache.isDeferred('msg1', 0, 'text')).toBe(false)
    expect(cache.deferredCount).toBe(0)
  })

  it('setUpgradeFn and scheduleUpgrade invoke the fn', async () => {
    const cache = new StaticBlockCache()
    const upgradeFn = vi.fn()
    cache.setUpgradeFn(upgradeFn)

    cache.set('msg1', 0, 'text', '<p>basic</p>', true)
    cache.scheduleUpgrade()

    // Wait for the idle callback / timeout
    await new Promise(r => setTimeout(r, 50))

    expect(upgradeFn).toHaveBeenCalledTimes(1)
  })

  it('scheduleUpgrade does nothing when no deferred entries', async () => {
    const cache = new StaticBlockCache()
    const upgradeFn = vi.fn()
    cache.setUpgradeFn(upgradeFn)

    cache.scheduleUpgrade()

    await new Promise(r => setTimeout(r, 50))

    expect(upgradeFn).not.toHaveBeenCalled()
  })

  it('scheduleUpgrade does not double-schedule', async () => {
    const cache = new StaticBlockCache()
    const upgradeFn = vi.fn()
    cache.setUpgradeFn(upgradeFn)

    cache.set('msg1', 0, 'text', '<p>basic</p>', true)
    cache.scheduleUpgrade()
    cache.scheduleUpgrade() // second call should be a no-op

    await new Promise(r => setTimeout(r, 50))

    expect(upgradeFn).toHaveBeenCalledTimes(1)
  })

  it('clear resets deferred state', () => {
    const cache = new StaticBlockCache()
    cache.set('msg1', 0, 'text', '<p>basic</p>', true)
    expect(cache.deferredCount).toBe(1)

    cache.clear()
    expect(cache.deferredCount).toBe(0)
    expect(cache.isDeferred('msg1', 0, 'text')).toBe(false)
  })
})
