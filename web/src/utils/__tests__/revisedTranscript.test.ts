import { describe, expect, it } from 'vitest'
import { parseRevisedTranscript, revisedToPlainText } from '../revisedTranscript'

/**
 * `transcript-revised.md` is plain text a human may edit, so the record's 校对
 * tab parses it instead of rendering it as Markdown. These cases pin the shapes
 * the writer actually produces — both UI languages, event rows, unknown timing —
 * plus the one thing the model is not supposed to do.
 */

const FILE = `# 校对版转写 · 发版评审 · 2026-11-01 14:30

> 已结合项目上下文修正听写错误与专有名词；时间戳、说话人、行序均未改动。

[00:00] 我：先看接口文档
[00:35] 张三：ClawBench 的 ACP 还没接
[01:05] 我：好
`

describe('parseRevisedTranscript', () => {
  it('drops the heading and the note and keeps one entry per utterance', () => {
    const lines = parseRevisedTranscript(FILE)
    expect(lines).toHaveLength(3)
    expect(lines[0]).toEqual({ speaker: '我', text: '先看接口文档', startMs: 0 })
    expect(lines[1]).toEqual({ speaker: '张三', text: 'ClawBench 的 ACP 还没接', startMs: 35_000 })
    expect(lines[2].startMs).toBe(65_000)
  })

  it('reads an English transcript the same way', () => {
    // The file follows the UI language at generation time, so the separator is
    // ": " rather than "：" and the heading is English.
    const lines = parseRevisedTranscript(
      '# Proofread transcript · Weekly sync · 2026-10-30 14:30\n\n' +
        '> Dictation errors and proper nouns were corrected.\n\n' +
        '[00:10] Alice: we shipped it\n',
    )
    expect(lines).toEqual([{ speaker: 'Alice', text: 'we shipped it', startMs: 10_000 }])
  })

  it('tolerates a line whose timing is unknown', () => {
    const lines = parseRevisedTranscript('[--:--] 我：没有时间戳的一句\n')
    expect(lines).toEqual([{ speaker: '我', text: '没有时间戳的一句', startMs: 0 }])
  })

  it('marks file and screen events in either language', () => {
    const zh = parseRevisedTranscript(
      '[00:00] 文件：打开 a.md\n[00:01] 屏幕：小芳 开始共享屏幕\n',
    )
    expect(zh.map((l) => l.kind)).toEqual(['file', 'screen'])
    // A meeting recorded under English keeps English labels even when read in
    // Chinese, so both spellings have to be recognized.
    const en = parseRevisedTranscript('[00:00] File: Opened a.md\n[00:01] Screen: Bob started sharing\n')
    expect(en.map((l) => l.kind)).toEqual(['file', 'screen'])
    expect(en[0].text).toBe('Opened a.md')
  })

  it('keeps a line the model wrote without a timestamp instead of dropping it', () => {
    // Losing a paragraph is worse than showing one row without a speaker.
    const lines = parseRevisedTranscript('[00:00] 我：正文\n这一句没有前缀\n')
    expect(lines).toHaveLength(2)
    expect(lines[1]).toEqual({ speaker: '', text: '这一句没有前缀', startMs: 0 })
  })

  it('ignores blank lines and empty payloads', () => {
    expect(parseRevisedTranscript('\n\n[00:00] 我：\n\n')).toEqual([])
  })
})

describe('revisedToPlainText', () => {
  it('copies the conversation without the document chrome', () => {
    expect(revisedToPlainText(FILE)).toBe(
      '我：先看接口文档\n\n张三：ClawBench 的 ACP 还没接\n\n我：好',
    )
  })
})
