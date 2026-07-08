/**
 * Constructs the full HTML document for ReDoc rendering in a sandboxed iframe.
 *
 * IMPORTANT: This function is in a separate .ts file (not .vue) because
 * the generated HTML contains literal <script> and </script> tags that
 * would confuse the Vue SFC compiler's HTML parser.
 */

/** Construct the full srcdoc HTML for ReDoc */
export function buildRedocSrcdoc(specJson: string): string {
  if (!specJson) return ''

  return `<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html, body { height: 100%; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif; }
  #redoc-container { height: 100%; }
  .redoc-loading { display: flex; align-items: center; justify-content: center; height: 100vh; color: #666; }
</style>
</head><body>
<div id="redoc-container"><div class="redoc-loading">Loading API docs...</div></div>
<script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
<script>
try {
  Redoc.init(${specJson}, {
    scrollYOffset: 5,
    hideDownloadButton: false,
    nativeScrollbars: true,
    theme: {
      colors: { primary: { main: '#1890ff' } },
      typography: { fontSize: '14px', lineHeight: '1.6em' }
    }
  }, document.getElementById('redoc-container'));
} catch(e) {
  document.getElementById('redoc-container').innerHTML =
    '<div style="padding:24px;color:#dc2626;font-size:14px;">Failed to render OpenAPI spec: ' + (e.message || e) + '</div>';
}
</script>
</body></html>`
}
