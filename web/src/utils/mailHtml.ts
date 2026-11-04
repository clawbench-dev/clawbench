import DOMPurify from 'dompurify'

/**
 * Rendering untrusted email HTML.
 *
 * Three independent layers, because any one of them alone is not enough:
 *
 *   1. DOMPurify strips scripts, event handlers and anything that can load or
 *      execute on its own. `<form>` and friends are removed too — a mail body
 *      has no legitimate reason to render a control that looks like the app's.
 *   2. The result goes into a `<iframe sandbox>` WITHOUT `allow-scripts`, so
 *      even a payload DOMPurify misses cannot run. `allow-same-origin` is
 *      present only so the parent can measure/rewrite the document; scripts
 *      stay blocked regardless.
 *   3. A CSP meta inside the document blocks every request except inline
 *      styles and (optionally) images, which is what stops tracking pixels.
 *
 * Remote images are OFF by default: a newsletter's tracking pixel reports that
 * the message was opened, and the user never asked to be measured.
 */

const FORBID_TAGS = [
  'form',
  'input',
  'textarea',
  'select',
  'option',
  'button',
  'base',
  'link',
  'meta',
  'iframe',
  'frame',
  'frameset',
  'object',
  'embed',
  'applet',
]

/** Reset rules for the sandboxed document. Emails assume a white canvas. */
const BASE_CSS = `
html, body { margin: 0; padding: 0; background: #ffffff; color: #1f2328; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC",
    "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
  font-size: 14px;
  line-height: 1.6;
  padding: 12px;
  word-break: break-word;
  overflow-wrap: anywhere;
}
img { max-width: 100%; height: auto; }
table { max-width: 100%; }
pre { white-space: pre-wrap; word-break: break-word; }
blockquote {
  margin: 8px 0; padding: 4px 0 4px 12px;
  border-left: 2px solid #d0d7de; color: #57606a;
}
a { color: #0969da; }
`

export interface MailHtmlOptions {
  /** When true, remote images are allowed through the CSP. */
  allowRemoteImages?: boolean
}

/** Sanitizes one fragment of email HTML. Exported for tests and plain use. */
export function sanitizeMailHtml(html: string): string {
  if (!html) return ''
  return String(DOMPurify.sanitize(html, {
    WHOLE_DOCUMENT: true,
    FORBID_TAGS,
    FORBID_ATTR: ['srcset', 'ping'],
    ALLOW_DATA_ATTR: false,
  }))
}

/**
 * Builds the full `srcdoc` for the reading pane.
 *
 * Links are forced to open in a new tab (the desktop shell routes those to the
 * system browser); anything else would navigate the sandbox and leave the user
 * staring at a blank frame.
 */
export function buildMailDocument(html: string, opts: MailHtmlOptions = {}): string {
  const allowRemote = opts.allowRemoteImages === true
  const clean = sanitizeMailHtml(html)
  if (!clean) return ''

  const csp = [
    "default-src 'none'",
    "style-src 'unsafe-inline'",
    `img-src 'self' data:${allowRemote ? ' https: http:' : ''}`,
    "font-src data:",
    "form-action 'none'",
    "base-uri 'none'",
  ].join('; ')

  let doc: Document
  try {
    doc = new DOMParser().parseFromString(clean, 'text/html')
  } catch {
    return ''
  }

  doc.querySelectorAll('a').forEach((a) => {
    a.setAttribute('target', '_blank')
    a.setAttribute('rel', 'noopener noreferrer')
  })

  const head = doc.head ?? doc.createElement('head')
  const meta = doc.createElement('meta')
  meta.setAttribute('http-equiv', 'Content-Security-Policy')
  meta.setAttribute('content', csp)
  head.insertBefore(meta, head.firstChild)

  const style = doc.createElement('style')
  style.textContent = BASE_CSS
  head.appendChild(style)

  return '<!doctype html>' + doc.documentElement.outerHTML
}

/**
 * True when the HTML references at least one remote image. The reader uses it
 * to decide whether the "show images" affordance is worth showing.
 */
export function hasRemoteImages(html: string): boolean {
  if (!html) return false
  return /<img[^>]+src\s*=\s*["']?(https?:)?\/\//i.test(html)
}
