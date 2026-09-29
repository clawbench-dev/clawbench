import { describe, expect, it } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'

/**
 * ISS-021 originally asserted that the HTML preview iframe sandbox does NOT
 * combine `allow-scripts` with `allow-same-origin`, because that combination
 * defeats the sandbox.
 *
 * That guard has since been REVERSED DELIBERATELY. The combination is now
 * required, and this file records the decision plus its cost so a future reader
 * does not "fix" it back.
 *
 * Why it is needed: the preview loads the document from /api/fs/raw/ so the
 * browser resolves the document's own relative references (stylesheets,
 * scripts, images, fonts) against its real URL. Without `allow-same-origin` the
 * frame is an opaque origin, its subresource requests are cross-site, and the
 * SameSite=Lax session cookie is withheld — the document renders but none of
 * its assets load.
 *
 * What it costs: the previewed HTML now runs with the user's session. It can
 * call any authenticated API and read/write the parent document. Previewed HTML
 * is untrusted (AI-written or downloaded), so this is a real trade. The
 * narrower alternatives considered were (a) inlining every subresource as a
 * data: URI in the parent, which cannot resolve paths the document builds at
 * runtime, and (b) a token-scoped unauthenticated endpoint, which needs a new
 * endpoint and leaks its token to the previewed content.
 *
 * The tests below therefore assert the CURRENT contract: same-origin IS
 * present, and the attributes that keep the frame from escaping on its own are
 * still absent.
 */
describe('FileViewer iframe sandbox (ISS-021, decision reversed)', () => {
  const componentPath = resolve(__dirname, '../file/FileViewer.vue')
  const source = readFileSync(componentPath, 'utf-8')

  /** The HTML preview iframe element's source text. */
  function previewIframeTag(): string {
    const iframeMatch = source.match(/<iframe[^>]*:src="htmlPreviewSrc[\s\S]*?\/>/)
    expect(iframeMatch, 'HTML preview iframe not found').not.toBeNull()
    return iframeMatch![0]
  }

  /** Tokens of the HTML preview iframe's sandbox attribute. */
  function sandboxTokens(): string[] {
    const sandboxMatch = previewIframeTag().match(/sandbox="([^"]*)"/)
    expect(sandboxMatch, 'sandbox attribute not found on HTML preview iframe').not.toBeNull()
    return sandboxMatch![1].split(/\s+/).filter(Boolean)
  }

  it('keeps allow-scripts so the HTML preview can execute its own scripts', () => {
    expect(sandboxTokens()).toContain('allow-scripts')
  })

  it('includes allow-same-origin so relative subresources load (deliberate)', () => {
    // See the file header: this is the reversal. If this assertion fails, the
    // HTML preview will silently stop loading local CSS/JS/images/fonts.
    expect(sandboxTokens()).toContain('allow-same-origin')
  })

  it('does not grant sandbox-escape capabilities beyond scripts + same-origin', () => {
    // The frame must not be able to pop windows, submit forms, run plugins, or
    // navigate the top-level page. Those are separate from same-origin and are
    // not needed by a static HTML preview.
    const tokens = sandboxTokens()
    for (const forbidden of [
      'allow-top-navigation',
      'allow-top-navigation-by-user-activation',
      'allow-popups',
      'allow-modals',
      'allow-forms',
      'allow-pointer-lock',
      'allow-presentation',
      'allow-downloads',
    ]) {
      expect(tokens, `${forbidden} must not be granted`).not.toContain(forbidden)
    }
  })

  it('loads a project-relative document by URL', () => {
    // The whole point of the same-origin grant: a srcdoc document has no URL of
    // its own, so relative references resolve against the app root and 404.
    // A project-relative file is served at /api/fs/raw/<dir>/index.html, whose
    // base URL IS the document's own directory.
    expect(previewIframeTag()).toContain(':src="htmlPreviewSrc')
  })

  it('falls back to srcdoc for external (absolute-path) documents', () => {
    // An external file is served as /api/fs/raw/?target=/abs/index.html. The
    // browser strips the query when deriving the base URL, so the document's
    // base is /api/fs/raw/ and `src="pic.png"` resolves to <project>/pic.png —
    // silently the WRONG file when the project root has one of that name.
    // htmlPreviewSrc therefore returns '' for absolute paths and the iframe
    // binds srcdoc instead. Verified against the real handler: the URL form
    // loaded the project-root decoy rather than the file beside the document.
    expect(previewIframeTag()).toContain(':srcdoc="htmlPreviewSrc ? undefined : file.content"')
  })

  it('htmlPreviewSrc returns empty for absolute paths', () => {
    // Source-level guard for the branch above (jsdom cannot resolve this: the
    // component would need a real server to observe which file is fetched).
    const computedBody = source.match(/const htmlPreviewSrc = computed\(\(\) => \{[\s\S]*?\n\}\)/)
    expect(computedBody, 'htmlPreviewSrc computed not found').not.toBeNull()
    expect(computedBody![0]).toContain('isAbsolutePath(props.file.path)')
    expect(computedBody![0]).toMatch(/isAbsolutePath\(props\.file\.path\)\)\s*return ''/)
  })
})
