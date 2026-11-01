import { describe, expect, it } from 'vitest'
import { parseVttCues, vttToPlainText } from '../vtt'

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

describe('parseVttCues', () => {
  it('reads the speaker from the voice span and the position from the timing line', () => {
    expect(parseVttCues(sample)).toEqual([
      { speaker: '我', text: '第一句', startMs: 0, endMs: 1500 },
      { speaker: '张三', text: '第二句', startMs: 2000, endMs: 4000 },
    ])
  })

  it('leaves the speaker empty when the writer emitted no voice span', () => {
    expect(parseVttCues('WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n没有标注')).toEqual([
      { speaker: '', text: '没有标注', startMs: 0, endMs: 1000 },
    ])
  })

  it('strips inline markup and unescapes entities, without the speaker prefix', () => {
    expect(parseVttCues('WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n<v 张三>加<b>粗</b>的 &lt;字&gt;</v>')[0]).toMatchObject({
      speaker: '张三',
      text: '加粗的 <字>',
    })
  })

  it('skips NOTE blocks, cue ids and empty payloads', () => {
    const vtt = 'WEBVTT\n\nNOTE 备注\n\nmic\n00:00:00.000 --> 00:00:01.000\n\n\n00:00:01.000 --> 00:00:02.000\n真内容'
    expect(parseVttCues(vtt)).toEqual([{ speaker: '', text: '真内容', startMs: 1000, endMs: 2000 }])
  })

  it('tolerates cue settings after the end time and short (MM:SS) stamps', () => {
    const [cue] = parseVttCues('WEBVTT\n\n01:02.500 --> 01:04.000 align:start position:0%\n内容')
    expect(cue).toMatchObject({ startMs: 62_500, endMs: 64_000 })
  })

  it('reads a multi-hour stamp', () => {
    const [cue] = parseVttCues('WEBVTT\n\n01:02:03.004 --> 01:02:04.005\n内容')
    expect(cue).toMatchObject({ startMs: 3_723_004, endMs: 3_724_005 })
  })
})
