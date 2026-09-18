import { describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'

/**
 * Integration test for the ask-question handling inside renderTextBlock.
 *
 * This file deliberately does NOT mock @/utils/streamPerf or
 * @/utils/askQuestion — the point is to exercise the real detection, parsing
 * and stripping pipeline. The existing useChatRender.test.ts mocks those
 * wholesale, which is why it could not catch the silent content-loss defect.
 *
 * The defect: detectAskQuestion (loose regex) and parseAskQuestionContent
 * (strict DOMParser) disagreed; on `found && !parsed` the tag was stripped
 * anyway, so the user saw neither a card nor the text.
 */

vi.mock('@/composables/useMarkdownRenderer', () => ({
  // Echo the text so assertions can see exactly what reached markdown.
  renderMarkdown: vi.fn((text: string) => ({ html: `<p>${text}</p>`, detectedPaths: [], detectedSHAs: [] })),
  renderMarkdownHtml: vi.fn((text: string) => `<p>${text}</p>`),
  renderMermaidInElement: vi.fn(() => Promise.resolve()),
}))
vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({ verifyFilePaths: vi.fn() }),
}))
vi.mock('@/composables/useCommitHashAnnotation', () => ({
  useCommitHashAnnotation: () => ({ verifyCommitHashes: vi.fn() }),
}))
vi.mock('@/composables/useThinkingContent', () => ({ clearThinkingCache: vi.fn() }))
vi.mock('@/stores/app', () => ({ store: { state: { tasks: [], projectRoot: '', homeDir: '' } } }))
vi.mock('@/utils/api', () => ({ apiGet: vi.fn(() => Promise.resolve({})) }))
vi.mock('@/utils/renderToolDetail', () => ({ formatToolInput: vi.fn(() => '') }))
vi.mock('@/utils/taskBlockStore', () => ({
  createTaskBlockStore: () => ({
    set: vi.fn(), get: vi.fn(), delete: vi.fn(), clear: vi.fn(),
    keys: () => [], size: () => 0,
  }),
}))
vi.mock('@/utils/chatBlocks', () => ({
  parseAssistantContent: vi.fn(() => []),
  toolCallSummary: vi.fn(() => ''),
  hasImagesInContent: vi.fn(() => false),
  formatMessageTime: vi.fn(() => ''),
  formatDetailTime: vi.fn(() => ''),
  truncate: vi.fn((s: string) => s),
}))

import { useChatRender } from '@/composables/useChatRender.ts'

function setup() {
  const messages = reactive({ value: [] as Array<Record<string, unknown>> })
  return useChatRender({
    messages,
    theme: { value: 'dark' },
    currentSessionId: { value: 's1' },
  })
}

const WELL_FORMED =
  '<ask-question><item><header>Approach</header><multi-select>false</multi-select>' +
  '<question>Which?</question><option><label>A</label><description>Fast</description></option></item></ask-question>'

describe('renderTextBlock — ask-question', () => {
  it('renders a card and strips the tag for a well formed payload', () => {
    const r = setup()
    const text = `前言\n${WELL_FORMED}\n后记`
    const html = r.renderTextBlock(text, 'm1', 0)

    expect(r.blockAskQuestions['m1-0']).toBeTruthy()
    expect((r.blockAskQuestions['m1-0'] as { questions: unknown[] }).questions).toHaveLength(1)
    expect(html).not.toContain('<ask-question')
    expect(html).toContain('前言')
    expect(html).toContain('后记')
  })

  it('retains an unparseable payload instead of deleting it', () => {
    // The core defect: a JSON payload is deliberately unsupported, so parsing
    // fails. The old code stripped the tag anyway and the question vanished.
    const r = setup()
    const text = '前言\n<ask-question>\n{"questions":[{"question":"你最喜欢哪种水果？"}]}\n</ask-question>\n后记'
    const html = r.renderTextBlock(text, 'm1', 0)

    expect(r.blockAskQuestions['m1-0']).toBeUndefined()
    // The raw payload must still be visible.
    expect(html).toContain('你最喜欢哪种水果？')
    expect(html).toContain('前言')
    expect(html).toContain('后记')
  })

  it('retains a payload whose XML is malformed beyond repair', () => {
    const r = setup()
    // Unclosed <item> with no option at all: nothing to parse.
    const text = '前言\n<ask-question>\n<item><header>H</header><question>Q?</question>\n后记'
    const html = r.renderTextBlock(text, 'm1', 0)
    expect(r.blockAskQuestions['m1-0']).toBeUndefined()
    expect(html).toContain('前言')
  })

  it('collects every tag into one card', () => {
    const r = setup()
    const two =
      '<ask-question><item><header>Q1</header><multi-select>false</multi-select>' +
      '<question>第一个?</question><option><label>A</label></option></item></ask-question>\n中间\n' +
      '<ask-question><item><header>Q2</header><multi-select>false</multi-select>' +
      '<question>第二个?</question><option><label>B</label></option></item></ask-question>'
    const html = r.renderTextBlock(two, 'm1', 0)

    const stored = r.blockAskQuestions['m1-0'] as { questions: Array<{ header: string }> }
    expect(stored.questions.map(q => q.header)).toEqual(['Q1', 'Q2'])
    expect(html).not.toContain('<ask-question')
    expect(html).toContain('中间')
  })

  it('drops a stale card when a re-render no longer parses', () => {
    const r = setup()
    r.renderTextBlock(`前言\n${WELL_FORMED}`, 'm1', 0)
    expect(r.blockAskQuestions['m1-0']).toBeTruthy()

    // The block content changed to an unparseable payload.
    r.renderTextBlock('前言\n<ask-question>{"questions":[]}</ask-question>', 'm1', 0)
    expect(r.blockAskQuestions['m1-0']).toBeUndefined()
  })

  it('ignores a tag inside a code fence', () => {
    const r = setup()
    const text = `示例：\n\n\`\`\`\n${WELL_FORMED}\n\`\`\`\n\n结束。`
    const html = r.renderTextBlock(text, 'm1', 0)
    expect(r.blockAskQuestions['m1-0']).toBeUndefined()
    expect(html).toContain('```')
  })

  it('leaves plain text untouched', () => {
    const r = setup()
    const html = r.renderTextBlock('普通文本，没有标签', 'm1', 0)
    expect(r.blockAskQuestions['m1-0']).toBeUndefined()
    expect(html).toContain('普通文本，没有标签')
  })
})
