import { describe, expect, it } from 'vitest'
import { parseEntryChunkName } from '../lib/entryChunk'

/**
 * Regression guard for how the build-size spec locates the live entry chunk.
 *
 * `.clawbench-web/` is never emptied (`emptyOutDir: false`, deliberate — it
 * protects the vendored Excalidraw assets), so every build leaves its
 * `main-<hash>.js` behind. Measured on a working checkout: 7 of them, all
 * ~1.2MB and several byte-identical. The old code did
 *
 *     readdirSync(dir).find(f => f.startsWith('main-') && f.endsWith('.js'))
 *
 * which returns whichever entry the filesystem happens to list first. It only
 * produced the right answer by luck (the first match happened to be the live
 * one). Once a stale chunk diverges in size, the gate would silently measure a
 * file that no browser loads — passing on an old bundle while the real one grew.
 *
 * index.html is the authority: it names the chunk the browser will fetch.
 */

const REAL_INDEX = `<!DOCTYPE html>
<html>
  <head>
    <script>/* theme bootstrap */</script>
    <script type="module" crossorigin src="/main-3xydJyte.js"></script>
    <link rel="modulepreload" crossorigin href="/vendor-vue-B0GJtANy.js">
  </head>
</html>`

describe('parseEntryChunkName', () => {
  it('reads the entry chunk out of the module script tag', () => {
    expect(parseEntryChunkName(REAL_INDEX)).toBe('main-3xydJyte.js')
  })

  it('ignores the inline bootstrap scripts that precede it', () => {
    // index.html runs several plain `<script>` blocks (theme, fonts) before the
    // module tag. A naive /src="(.*\.js)"/ would be fine here, but a naive
    // "first <script>" scan would not be.
    const html = '<script>var x=1</script><script type="module" src="/main-abc.js"></script>'
    expect(parseEntryChunkName(html)).toBe('main-abc.js')
  })

  it('works for the share entry too', () => {
    const html = '<script type="module" crossorigin src="/share-BmVOA07n.js"></script>'
    expect(parseEntryChunkName(html)).toBe('share-BmVOA07n.js')
  })

  it('tolerates a src without the leading slash', () => {
    const html = '<script type="module" src="main-noslash.js"></script>'
    expect(parseEntryChunkName(html)).toBe('main-noslash.js')
  })

  it('returns null when there is no module script', () => {
    // The caller asserts non-null, so a missing tag must surface as a failure
    // rather than as an arbitrary directory match.
    expect(parseEntryChunkName('<html><body>no scripts</body></html>')).toBeNull()
  })

  it('does not pick a stale chunk that merely looks like the entry', () => {
    // The whole point: a leftover `main-<oldhash>.js` on disk must not be able
    // to win. Only the tag decides, so the stale name is irrelevant here — but
    // assert the returned name is exactly the referenced one.
    const html = '<script type="module" src="/main-live.js"></script>'
    const stale = 'main-stale.js'
    const picked = parseEntryChunkName(html)
    expect(picked).toBe('main-live.js')
    expect(picked).not.toBe(stale)
  })
})
