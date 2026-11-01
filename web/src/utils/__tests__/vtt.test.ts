import { describe, expect, it } from 'vitest'
import { vttToPlainText } from '../vtt'

const sample = `WEBVTT

mic
00:00:00.000 --> 00:00:01.500
<v 我>第一句

viewer:v1
00:00:02.000 --> 00:00:04.000
<v 张三>第二句
`

describe('vttToPlainText', () => {
  it('drops the header, cue ids and timing, and turns the voice span into a speaker prefix', () => {
    expect(vttToPlainText(sample)).toBe('我：第一句\n\n张三：第二句')
  })

  it('handles CRLF files', () => {
    expect(vttToPlainText(sample.replace(/\n/g, '\r\n'))).toBe('我：第一句\n\n张三：第二句')
  })

  it('keeps a cue that has no cue id', () => {
    expect(vttToPlainText('WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n没有标注')).toBe('没有标注')
  })

  it('unwraps a closed voice span and drops other inline tags', () => {
    expect(vttToPlainText('WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n<v 张三>加<b>粗</b>的字</v>')).toBe(
      '张三：加粗的字',
    )
  })

  it('unescapes entities written by the server', () => {
    expect(vttToPlainText('WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n<v 我>a &lt; b &amp; c &gt; d')).toBe(
      '我：a < b & c > d',
    )
  })

  it('keeps a literal entity that was escaped twice', () => {
    // The meeting said "&lt;", which the writer escaped to "&amp;lt;".
    expect(vttToPlainText('WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n&amp;lt;不是标签&amp;gt;')).toBe(
      '&lt;不是标签&gt;',
    )
  })

  it('ignores NOTE blocks and empty payloads', () => {
    const vtt = 'WEBVTT\n\nNOTE 这段是备注\n\n00:00:00.000 --> 00:00:01.000\n\n\n00:00:01.000 --> 00:00:02.000\n真内容'
    expect(vttToPlainText(vtt)).toBe('真内容')
  })

  it('returns an empty string for empty input', () => {
    expect(vttToPlainText('')).toBe('')
    expect(vttToPlainText('WEBVTT\n\n')).toBe('')
  })
})
