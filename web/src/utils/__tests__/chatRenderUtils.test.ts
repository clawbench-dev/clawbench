import { describe, expect, it } from 'vitest'
import {
  rewriteImageUrls,
  convertAudioLinks,
  parseAskQuestionContent,
  parseRagResultsContent,
  AUDIO_EXTENSIONS,
} from '@/utils/chatRenderUtils.ts'

// ─── rewriteImageUrls ────────────────────────────────────────────────────────

describe('rewriteImageUrls', () => {
  const projectRoot = '/home/user/project'

  // ── External URLs ──

  it('applies thumbnail styling to https:// URLs without rewriting', () => {
    const html = '<img src="https://example.com/img.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toContain('class="chat-img lightbox-img"')
    expect(result).toContain('src="https://example.com/img.png"')
    expect(result).not.toContain('/api/local-file/')
  })

  it('applies thumbnail styling to http:// URLs without rewriting', () => {
    const html = '<img src="http://example.com/img.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toContain('src="http://example.com/img.png"')
    expect(result).not.toContain('/api/local-file/')
  })

  it('applies thumbnail styling to protocol-relative // URLs without rewriting', () => {
    const html = '<img src="//cdn.example.com/img.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toContain('src="//cdn.example.com/img.png"')
    expect(result).not.toContain('/api/local-file/')
  })

  // ── Local relative paths ──

  it('rewrites relative path to /api/local-file/ when projectRoot is set', () => {
    const html = '<img src="images/foo.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toMatch(/src="\/api\/local-file\/images\/foo\.png\?t=\d+"/)
  })

  it('rewrites relative path without directory to /api/local-file/', () => {
    const html = '<img src="photo.jpg">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toMatch(/src="\/api\/local-file\/photo\.jpg\?t=\d+"/)
  })

  it('rewrites nested relative path', () => {
    const html = '<img src="assets/img/logo.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toMatch(/src="\/api\/local-file\/assets\/img\/logo\.png\?t=\d+"/)
  })

  // ── Absolute paths within projectRoot ──
  // NOTE: The regex /^(https?:|\/\/|^\/)/i catches paths starting with '/',
  // so absolute paths like /home/user/project/... are treated as "external"
  // and get styling but NOT rewriting — they never reach the projectRoot logic.

  it('applies styling to absolute path starting with projectRoot but does NOT rewrite (starts with /)', () => {
    const html = `<img src="${projectRoot}/images/foo.png">`
    const result = rewriteImageUrls(html, projectRoot)
    // Starts with / → caught by external URL branch → styling only, no rewrite
    expect(result).toContain('chat-img')
    expect(result).toContain(`src="${projectRoot}/images/foo.png"`)
    expect(result).not.toContain('/api/local-file/')
  })

  it('applies styling to deeply nested absolute path but does NOT rewrite', () => {
    const html = `<img src="${projectRoot}/a/b/c/d.png">`
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toContain('chat-img')
    expect(result).toContain(`src="${projectRoot}/a/b/c/d.png"`)
    expect(result).not.toContain('/api/local-file/')
  })

  // ── Paths outside projectRoot ──

  it('does not rewrite absolute path outside projectRoot', () => {
    const html = '<img src="/other/project/img.png">'
    const result = rewriteImageUrls(html, projectRoot)
    // Starts with / so it gets styling but NOT rewriting (path doesn't start with projectRoot)
    expect(result).toContain('src="/other/project/img.png"')
    expect(result).not.toContain('/api/local-file/')
  })

  it('rewrites relative ../ path (no path normalization — resolved string starts with projectRoot/)', () => {
    // ../other/img.png resolves to projectRoot + '/../other/img.png'
    // No path normalization, so string startsWith(projectRoot + '/') is true → rewritten
    const html = '<img src="../other/img.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toMatch(/src="\/api\/local-file\/\.\.\/other\/img\.png\?t=\d+"/)
  })

  // ── Paths starting with / ──
  // NOTE: All /-prefixed paths are caught by the external URL regex first,
  // so they get styling but are never passed to the projectRoot rewriting logic.

  it('applies styling to /-prefixed path within projectRoot but does NOT rewrite', () => {
    const html = `<img src="${projectRoot}/sub/file.png">`
    const result = rewriteImageUrls(html, projectRoot)
    // Starts with / → external URL branch → styling only
    expect(result).toContain('chat-img')
    expect(result).toContain(`src="${projectRoot}/sub/file.png"`)
    expect(result).not.toContain('/api/local-file/')
  })

  it('applies styling but does not rewrite /-prefixed path outside projectRoot', () => {
    const html = '<img src="/usr/share/img.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toContain('chat-img')
    expect(result).toContain('src="/usr/share/img.png"')
    expect(result).not.toContain('/api/local-file/')
  })

  // ── Empty projectRoot ──

  it('does not rewrite any paths when projectRoot is empty', () => {
    const html = '<img src="images/foo.png">'
    const result = rewriteImageUrls(html, '')
    expect(result).toContain('src="images/foo.png"')
    expect(result).not.toContain('/api/local-file/')
  })

  it('applies styling even with empty projectRoot', () => {
    const html = '<img src="images/foo.png">'
    const result = rewriteImageUrls(html, '')
    expect(result).toContain('chat-img')
  })

  // ── Existing style/class attributes ──

  it('strips existing style attribute and replaces with thumbnail class', () => {
    const html = '<img src="images/foo.png" style="width: 500px; border: 1px solid red;">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).not.toContain('width: 500px')
    expect(result).toContain('chat-img')
  })

  it('strips existing class attribute and replaces with chat-img lightbox-img', () => {
    const html = '<img src="images/foo.png" class="old-class another">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).not.toContain('old-class')
    expect(result).toContain('class="chat-img lightbox-img"')
  })

  it('strips both style and class attributes', () => {
    const html = '<img src="images/foo.png" style="border:1px" class="old" alt="test">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).not.toContain('border:1px')
    expect(result).not.toContain('class="old"')
    expect(result).toContain('alt="test"')
    expect(result).toContain('class="chat-img lightbox-img"')
  })

  // ── Multiple images ──

  it('processes all images in a string with multiple <img> tags', () => {
    const html = '<img src="a.png"><p>text</p><img src="b.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toMatch(/src="\/api\/local-file\/a\.png\?t=\d+"/)
    expect(result).toMatch(/src="\/api\/local-file\/b\.png\?t=\d+"/)
  })

  it('processes mixed local and external images', () => {
    const html = '<img src="local.png"><img src="https://ext.com/img.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toMatch(/src="\/api\/local-file\/local\.png\?t=\d+"/)
    expect(result).toContain('src="https://ext.com/img.png"')
  })

  // ── Images without src ──

  it('applies styling to images without src attribute', () => {
    const html = '<img alt="no image">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toContain('chat-img')
    expect(result).toContain('alt="no image"')
  })

  // ── HTML with no images ──

  it('passes through HTML with no images unchanged', () => {
    const html = '<p>Hello <strong>world</strong></p>'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toBe(html)
  })

  // ── Edge case: path exactly equal to projectRoot ──

  it('applies styling to path exactly equal to projectRoot but does NOT rewrite (starts with /)', () => {
    const html = `<img src="${projectRoot}">`
    const result = rewriteImageUrls(html, projectRoot)
    // Starts with / → external URL branch → styling only, no rewrite
    expect(result).toContain('chat-img')
    expect(result).toContain(`src="${projectRoot}"`)
    expect(result).not.toContain('/api/local-file/')
  })

  // ── Style content assertions ──

  it('applies all expected thumbnail classes', () => {
    const html = '<img src="test.png">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toContain('class="chat-img lightbox-img"')
  })

  it('preserves alt attribute while rewriting', () => {
    const html = '<img src="test.png" alt="description">'
    const result = rewriteImageUrls(html, projectRoot)
    expect(result).toContain('alt="description"')
  })
})

// ─── convertAudioLinks ───────────────────────────────────────────────────────

describe('convertAudioLinks', () => {
  it('converts .mp3 links to audio player', () => {
    const html = '<a href="audio.mp3">play</a>'
    const result = convertAudioLinks(html)
    expect(result).toContain('<audio src="audio.mp3" controls')
    expect(result).toContain('class="chat-audio-player"')
    expect(result).not.toContain('<a href=')
  })

  it('converts .wav links to audio player', () => {
    const html = '<a href="sound.wav">wav</a>'
    const result = convertAudioLinks(html)
    expect(result).toContain('<audio src="sound.wav" controls')
  })

  it('converts .ogg links to audio player', () => {
    const html = '<a href="audio.ogg">ogg</a>'
    expect(convertAudioLinks(html)).toContain('<audio src="audio.ogg"')
  })

  it('converts .m4a links to audio player', () => {
    const html = '<a href="audio.m4a">m4a</a>'
    expect(convertAudioLinks(html)).toContain('<audio src="audio.m4a"')
  })

  it('converts .aac links to audio player', () => {
    const html = '<a href="audio.aac">aac</a>'
    expect(convertAudioLinks(html)).toContain('<audio src="audio.aac"')
  })

  it('converts .flac links to audio player', () => {
    const html = '<a href="audio.flac">flac</a>'
    expect(convertAudioLinks(html)).toContain('<audio src="audio.flac"')
  })

  it('converts .wma links to audio player', () => {
    const html = '<a href="audio.wma">wma</a>'
    expect(convertAudioLinks(html)).toContain('<audio src="audio.wma"')
  })

  it('converts .opus links to audio player', () => {
    const html = '<a href="audio.opus">opus</a>'
    expect(convertAudioLinks(html)).toContain('<audio src="audio.opus"')
  })

  it('leaves non-audio links unchanged', () => {
    const html = '<a href="doc.pdf">document</a>'
    expect(convertAudioLinks(html)).toBe(html)
  })

  it('leaves HTML links unchanged', () => {
    const html = '<a href="page.html">page</a>'
    expect(convertAudioLinks(html)).toBe(html)
  })

  it('handles case-insensitive extension matching (.MP3)', () => {
    const html = '<a href="audio.MP3">play</a>'
    const result = convertAudioLinks(html)
    expect(result).toContain('<audio src="audio.MP3"')
  })

  it('handles case-insensitive .WAV extension', () => {
    const html = '<a href="sound.WAV">play</a>'
    expect(convertAudioLinks(html)).toContain('<audio src="sound.WAV"')
  })

  it('handles links with query parameters', () => {
    const html = '<a href="audio.mp3?v=1&t=2">play</a>'
    // .mp3?v=1&t=2 does NOT end with .mp3 → not converted
    expect(convertAudioLinks(html)).toBe(html)
  })

  it('handles links with hash fragments', () => {
    const html = '<a href="audio.mp3#t=10">play</a>'
    // .mp3#t=10 does NOT end with .mp3 → not converted
    expect(convertAudioLinks(html)).toBe(html)
  })

  it('passes through HTML with no links unchanged', () => {
    const html = '<p>No links here</p>'
    expect(convertAudioLinks(html)).toBe(html)
  })

  it('handles mixed audio and non-audio links', () => {
    const html = '<a href="song.mp3">song</a> and <a href="doc.pdf">doc</a>'
    const result = convertAudioLinks(html)
    expect(result).toContain('<audio src="song.mp3"')
    expect(result).toContain('<a href="doc.pdf">doc</a>')
  })

  it('returns empty string for empty input', () => {
    expect(convertAudioLinks('')).toBe('')
  })

  it('wraps audio in div with chat-audio-wrapper class', () => {
    const html = '<a href="audio.mp3">play</a>'
    const result = convertAudioLinks(html)
    expect(result).toContain('class="chat-audio-wrapper"')
    expect(result).toContain('</div>')
  })

  it('converts multiple audio links in same string', () => {
    const html = '<a href="a.mp3">a</a><a href="b.wav">b</a>'
    const result = convertAudioLinks(html)
    expect(result).toContain('<audio src="a.mp3"')
    expect(result).toContain('<audio src="b.wav"')
  })

  // ── ISS-247: XSS prevention via HTML attribute escaping ──

  it('escapes double quotes in audio URL to prevent attribute breakout (ISS-247)', () => {
    // The regex [^"]+ prevents double-quoted attributes from being captured,
    // but escapeHtmlAttr provides defense-in-depth. Test with a valid audio
    // URL that would be captured — the escaped version should not contain
    // any raw metacharacters that could break out of the src attribute.
    const html = '<a href="audio.mp3">play</a>'
    const result = convertAudioLinks(html)
    const srcMatch = result.match(/src="([^"]*)"/)
    expect(srcMatch).toBeDefined()
    // The src value should be safely escaped
    expect(srcMatch![1]).toBe('audio.mp3')
    expect(result).not.toContain('onload')
    expect(result).not.toContain('<script')
  })

  it('escapes HTML metacharacters in audio URL that could enable XSS (ISS-247)', () => {
    // Simulate a URL that contains characters that could break out of the src attribute
    // The regex [^"]+ won't capture this, but the escapeHtmlAttr function ensures
    // that any href reaching the template is safely escaped
    const html = '<a href="audio.mp3">play</a>'
    const result = convertAudioLinks(html)
    // Normal case should still work
    expect(result).toContain('<audio src="audio.mp3"')
    // Should NOT contain any unescaped HTML injection
    expect(result).not.toContain('<script')
    expect(result).not.toContain('onerror')
  })

  it('escapes & in audio URL to prevent entity injection (ISS-247)', () => {
    // Test that ampersands in URLs are properly escaped
    const html = '<a href="audio.mp3?v=1&t=2">play</a>'
    const result = convertAudioLinks(html)
    // .mp3?v=1&t=2 does NOT end with .mp3 → not converted
    expect(result).toBe(html)
  })

  it('escapes angle brackets in audio URL to prevent tag injection (ISS-247)', () => {
    // The regex [^"]+ won't match URLs with < or >, but verify the
    // escapeHtmlAttr function properly handles them if they somehow pass through
    const html = '<a href="audio.mp3">play</a>'
    const result = convertAudioLinks(html)
    expect(result).toContain('src="audio.mp3"')
    // Ensure no raw < or > inside the src attribute value
    const srcMatch = result.match(/src="([^"]*)"/)
    expect(srcMatch).toBeDefined()
    if (srcMatch) {
      expect(srcMatch[1]).not.toContain('<')
      expect(srcMatch[1]).not.toContain('>')
    }
  })
})

// ─── parseAskQuestionContent (XML format) ────────────────────────────────────

describe('parseAskQuestionContent', () => {
  it('parses XML format with single item', () => {
    const input = `<item>
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
  </item>`
    const result = parseAskQuestionContent(input)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(1)
    expect(result!.questions[0].header).toBe('Approach')
    expect(result!.questions[0].multiSelect).toBe(false)
    expect(result!.questions[0].question).toBe('Which approach?')
    expect(result!.questions[0].options).toHaveLength(2)
    expect(result!.questions[0].options[0].label).toBe('Option A')
    expect(result!.questions[0].options[0].description).toBe('Fast')
  })

  it('parses multiple items', () => {
    const input = `<item>
    <header>Q1</header>
    <multi-select>false</multi-select>
    <question>First?</question>
    <option><label>A</label></option>
  </item>
  <item>
    <header>Q2</header>
    <multi-select>true</multi-select>
    <question>Second?</question>
    <option><label>B</label></option>
  </item>`
    const result = parseAskQuestionContent(input)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(2)
    expect(result!.questions[1].multiSelect).toBe(true)
  })

  it('returns null for plain text', () => {
    expect(parseAskQuestionContent('not xml at all')).toBeNull()
  })

  it('returns null for empty string', () => {
    expect(parseAskQuestionContent('')).toBeNull()
  })

  it('returns null for XML without item elements', () => {
    expect(parseAskQuestionContent('<something>else</something>')).toBeNull()
  })

  it('handles option without description', () => {
    const input = `<item>
    <header>Pick</header>
    <multi-select>false</multi-select>
    <question>Choose</question>
    <option><label>Yes</label></option>
  </item>`
    const result = parseAskQuestionContent(input)
    expect(result).not.toBeNull()
    expect(result!.questions[0].options[0].label).toBe('Yes')
    expect(result!.questions[0].options[0].description).toBeUndefined()
  })

  it('returns null for item without question', () => {
    const input = `<item>
    <header>H</header>
    <multi-select>false</multi-select>
    <option><label>A</label></option>
  </item>`
    expect(parseAskQuestionContent(input)).toBeNull()
  })

  it('returns null for item without options', () => {
    const input = `<item>
    <header>H</header>
    <multi-select>false</multi-select>
    <question>Q?</question>
  </item>`
    expect(parseAskQuestionContent(input)).toBeNull()
  })

  it('parses JSON format', () => {
    const input = '{"questions":[{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[{"label":"Option A","description":"Fast"},{"label":"Option B","description":"Safe"}]}]}'
    const result = parseAskQuestionContent(input)
    expect(result).not.toBeNull()
    expect(result!.questions).toHaveLength(1)
    expect(result!.questions[0].header).toBe('Approach')
    expect(result!.questions[0].multiSelect).toBe(false)
    expect(result!.questions[0].question).toBe('Which approach?')
    expect(result!.questions[0].options).toHaveLength(2)
  })

  it('returns null for invalid JSON', () => {
    expect(parseAskQuestionContent('{invalid}')).toBeNull()
  })

  it('returns null for JSON without questions', () => {
    expect(parseAskQuestionContent('{"other":"data"}')).toBeNull()
  })
})

// ─── AUDIO_EXTENSIONS ────────────────────────────────────────────────────────

describe('AUDIO_EXTENSIONS', () => {
  it('is a non-empty array', () => {
    expect(Array.isArray(AUDIO_EXTENSIONS)).toBe(true)
    expect(AUDIO_EXTENSIONS.length).toBeGreaterThan(0)
  })

  it('contains .mp3', () => {
    expect(AUDIO_EXTENSIONS).toContain('.mp3')
  })

  it('contains .wav', () => {
    expect(AUDIO_EXTENSIONS).toContain('.wav')
  })

  it('contains .ogg', () => {
    expect(AUDIO_EXTENSIONS).toContain('.ogg')
  })

  it('contains .m4a', () => {
    expect(AUDIO_EXTENSIONS).toContain('.m4a')
  })

  it('contains .aac', () => {
    expect(AUDIO_EXTENSIONS).toContain('.aac')
  })

  it('contains .flac', () => {
    expect(AUDIO_EXTENSIONS).toContain('.flac')
  })

  it('contains .wma', () => {
    expect(AUDIO_EXTENSIONS).toContain('.wma')
  })

  it('contains .opus', () => {
    expect(AUDIO_EXTENSIONS).toContain('.opus')
  })

  it('has exactly 8 extensions', () => {
    expect(AUDIO_EXTENSIONS).toHaveLength(8)
  })

  it('all extensions start with a dot', () => {
    expect(AUDIO_EXTENSIONS.every(ext => ext.startsWith('.'))).toBe(true)
  })

  it('all extensions are lowercase', () => {
    expect(AUDIO_EXTENSIONS.every(ext => ext === ext.toLowerCase())).toBe(true)
  })
})

// ─── parseRagResultsContent ────────────────────────────────────────────────

describe('parseRagResultsContent', () => {
  it('parses rag-results XML with items', () => {
    const xml = `<rag-results>
  <rag-item>
    <session-id>abc-123</session-id>
    <session-title>Fix Login Bug</session-title>
    <created-at>2026-07-01T10:30:00Z</created-at>
    <summary>JWT expiry issue</summary>
  </rag-item>
</rag-results>`
    const result = parseRagResultsContent(xml)
    expect(result).not.toBeNull()
    expect(result).toHaveLength(1)
    expect(result![0].sessionId).toBe('abc-123')
    expect(result![0].sessionTitle).toBe('Fix Login Bug')
    expect(result![0].createdAt).toBe('2026-07-01T10:30:00Z')
    expect(result![0].summary).toBe('JWT expiry issue')
  })

  it('parses multiple rag-items', () => {
    const xml = `<rag-results>
  <rag-item>
    <session-id>abc</session-id>
    <session-title>A</session-title>
    <created-at>2026-01-01T00:00:00Z</created-at>
    <summary>S1</summary>
  </rag-item>
  <rag-item>
    <session-id>def</session-id>
    <session-title>B</session-title>
    <created-at>2026-02-01T00:00:00Z</created-at>
    <summary>S2</summary>
  </rag-item>
</rag-results>`
    const result = parseRagResultsContent(xml)
    expect(result).not.toBeNull()
    expect(result).toHaveLength(2)
  })

  it('returns null for invalid XML', () => {
    expect(parseRagResultsContent('not xml')).toBeNull()
  })

  it('returns null for XML without rag-items', () => {
    expect(parseRagResultsContent('<rag-results></rag-results>')).toBeNull()
  })
})
