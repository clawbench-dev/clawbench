/**
 * Constructs the full HTML document for Swagger UI rendering in a sandboxed iframe.
 *
 * IMPORTANT: This function is in a separate .ts file (not .vue) because
 * the generated HTML contains literal <script> and </script> tags that
 * would confuse the Vue SFC compiler's HTML parser.
 *
 * Swagger UI is loaded from unpkg CDN. It natively supports dark mode
 * via the "theme" config option, unlike ReDoc which has no built-in dark theme.
 */

/** Construct the full srcdoc HTML for Swagger UI */
export function buildSwaggerSrcdoc(specJson: string, isDark: boolean = false): string {
  if (!specJson) return ''

  const bodyBg = isDark ? '#1a1a2e' : '#fff'
  const swaggerTheme = isDark ? '"dark"' : '"classic"'

  return `<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<title>Swagger UI</title>
<link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html, body { height: 100%; background: ${bodyBg}; }
  #swagger-ui { height: 100%; }
</style>
</head><body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
try {
  SwaggerUIBundle({
    spec: ${specJson},
    dom_id: '#swagger-ui',
    layout: "BaseLayout",
    theme: { theme: ${swaggerTheme} },
    presets: [
      SwaggerUIBundle.presets.apis,
      SwaggerUIBundle.SwaggerUIStandalonePreset
    ],
    syntaxHighlight: { activate: true, theme: ${swaggerTheme} === "dark" ? "agate" : "obsidian" }
  });
} catch(e) {
  document.getElementById('swagger-ui').innerHTML =
    '<div style="padding:24px;color:#dc2626;font-size:14px;">Failed to render OpenAPI spec: ' + (e.message || e) + '</div>';
}
</script>
</body></html>`
}
