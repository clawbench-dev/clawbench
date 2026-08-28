/**
 * Constructs the full HTML document for Swagger UI rendering in a sandboxed iframe.
 *
 * IMPORTANT: This function is in a separate .ts file (not .vue) because
 * the generated HTML contains literal <script> and </script> tags that
 * would confuse the Vue SFC compiler's HTML parser.
 *
 * The swagger-ui-bundle (~1.5MB) and swagger-ui CSS are loaded via static
 * ?raw imports so Vite inlines them as strings. Combined with
 * defineAsyncComponent for OpenApiPreview and manualChunks split, the
 * swagger-ui code is only fetched when the user actually opens an
 * OpenAPI preview.
 */

import swaggerUiBundle from 'swagger-ui-dist/swagger-ui-bundle.js?raw'
import swaggerUiCss from 'swagger-ui-dist/swagger-ui.css?raw'

/** Construct the full srcdoc HTML for Swagger UI */
export function buildSwaggerSrcdoc(specJson: string, isDark: boolean = false, scrollbarThumb: string = '#c1c1c1', scrollbarTrack: string = 'transparent'): string {
  if (!specJson) return ''

  // Swagger UI's dark-mode CSS uses the "dark-mode" class on <html>.
  const htmlClass = isDark ? ' class="dark-mode"' : ''
  // Use "agate" syntax highlight for dark, default for light
  const syntaxTheme = isDark ? '"agate"' : '""'

  return `<!DOCTYPE html>
<html${htmlClass}><head>
<meta charset="utf-8">
<style>
  ${swaggerUiCss}
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html, body { height: 100%; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif; }
  #swagger-ui { height: 100%; overflow: auto; }
  .swagger-loading { display: flex; align-items: center; justify-content: center; height: 100vh; color: #666; }
  ::-webkit-scrollbar { width: 4px; height: 4px; }
  ::-webkit-scrollbar-track { background: ${scrollbarTrack}; }
  ::-webkit-scrollbar-thumb { background: ${scrollbarThumb}; border-radius: 4px; }
  ::-webkit-scrollbar-thumb:hover { background: #999; }
  ::-webkit-scrollbar-button { display: none; }
  ::-webkit-scrollbar-corner { background: transparent; }
  * { scrollbar-color: ${scrollbarThumb} ${scrollbarTrack}; }
  /* Hide the top bar (Swagger UI logo + URL input) — we provide the spec inline */
  .swagger-ui .topbar { display: none; }
  /* Tighten page margins so content uses more of the preview area */
  .swagger-ui .wrapper { padding: 0 8px; max-width: none; }
  .swagger-ui .info { margin: 8px 0; }
  .swagger-ui .scheme-container { padding: 8px 0; }
  .swagger-ui .opblock-tag { padding: 8px 0 8px 12px; }
  .swagger-ui .opblock { margin: 0 0 10px; }
  .swagger-ui .opblock .opblock-summary { padding: 7px 8px; }
  .swagger-ui .opblock .opblock-section { padding: 10px 12px; }
</style>
</head><body>
<div id="swagger-ui"><div class="swagger-loading">Loading API docs...</div></div>
<script>${swaggerUiBundle}</script>
<script>
try {
  SwaggerUIBundle({
    spec: ${specJson},
    dom_id: '#swagger-ui',
    presets: [
      SwaggerUIBundle.presets.apis,
      SwaggerUIBundle.SwaggerUIStandalonePreset
    ],
    layout: "BaseLayout",
    docExpansion: "list",
    defaultModelsExpandDepth: 1,
    defaultModelExpandDepth: 1,
    deepLinking: true,
    showExtensions: true,
    showCommonExtensions: true,
    syntaxHighlight: { activate: true, theme: ${syntaxTheme} },
    requestInterceptor: (req) => {
      // Route http/https requests through the CORS proxy so "Try it out"
      // works even when the target API doesn't return CORS headers.
      if (req.url && (req.url.startsWith('http://') || req.url.startsWith('https://'))) {
        // Skip proxying if the URL resolves to the same origin (e.g. from a
        // relative server URL like "/v1" which Swagger UI resolves against the
        // iframe's about:srcdoc origin → falls back to the parent origin).
        // These are not real API endpoints and would loop back to ClawBench.
        const proxyPrefix = '/api/openapi-proxy?url=';
        if (!req.url.startsWith(window.location.origin) && !req.url.includes(proxyPrefix)) {
          req.url = proxyPrefix + encodeURIComponent(req.url);
        }
      }
      // Remove Origin/Referer to avoid confusing the target server
      delete req.headers['Origin'];
      delete req.headers['Referer'];
      return req;
    }
  });
} catch(e) {
  document.getElementById('swagger-ui').innerHTML =
    '<div style="padding:24px;color:#dc2626;font-size:14px;">Failed to render OpenAPI spec: ' + (e.message || e) + '</div>';
}
</script>
</body></html>`
}
