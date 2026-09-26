#!/usr/bin/env node
/**
 * Dependency-free mock ClawBench server for the Android E2E harness.
 *
 * The Android app refuses to leave the login page unless the server it talks to
 * identifies itself correctly, so this mock implements exactly the three
 * endpoints `MainActivity` touches:
 *
 *   GET  /api/health  -> {"app":"clawbench","version":"<APK versionName>"}
 *   POST /login       -> 200 + Set-Cookie session cookie
 *   GET  /            -> home page carrying E2E_HOME_MARKER
 *
 * Anything else returns the same marker page (so `webView.loadUrl(serverUrl)`
 * always lands somewhere assertable).
 *
 * The `version` field MUST equal the APK's versionName: `VersionCompare`
 * (`MainActivity.gateVersionMismatchAndProceed`) shows a *blocking* native
 * dialog when appVersion < serverVersion, which would hang the smoke test.
 * Returning the exact APK version is always safe (it never trips `<`).
 *
 * The version is never hardcoded here: it comes from CLAWBENCH_VERSION (set by
 * scripts/run.sh from the built APK's output-metadata.json), falling back to a
 * metadata file path if one is provided.
 *
 * Request log: every request is appended to an in-memory list and mirrored to
 * stdout, so the test can assert the call sequence via GET /__requests and
 * `docker compose logs mock`.
 */
import http from 'node:http';
import fs from 'node:fs';

export const E2E_HOME_MARKER = 'e2e-home-marker';
const PORT = Number(process.env.MOCK_PORT || 20000);
const HOST = process.env.MOCK_HOST || '0.0.0.0';

function resolveVersion() {
  if (process.env.CLAWBENCH_VERSION) return process.env.CLAWBENCH_VERSION.trim();
  const metaPath = process.env.APK_METADATA;
  if (metaPath && fs.existsSync(metaPath)) {
    try {
      const meta = JSON.parse(fs.readFileSync(metaPath, 'utf8'));
      const v = meta?.elements?.[0]?.versionName;
      if (v) return v;
    } catch (e) {
      console.error(`[mock] failed to parse ${metaPath}: ${e.message}`);
    }
  }
  // Deliberately loud: a missing version silently becomes a blocking
  // version-mismatch dialog inside the app, which looks like a test hang.
  throw new Error(
    'no server version available: set CLAWBENCH_VERSION or APK_METADATA',
  );
}

const SERVER_VERSION = resolveVersion();
const requests = [];

function homePage(version) {
  return `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>ClawBench (mock)</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body>
  <h1>ClawBench mock server</h1>
  <div id="${E2E_HOME_MARKER}">e2e-home-ok</div>
  <p id="e2e-version">${version}</p>
  <script>window.__E2E_HOME__ = true;</script>
</body>
</html>`;
}

function send(res, status, body, headers = {}) {
  const buf = Buffer.isBuffer(body) ? body : Buffer.from(body);
  res.writeHead(status, {
    'Content-Type': 'text/html; charset=utf-8',
    'Content-Length': buf.length,
    ...headers,
  });
  res.end(buf);
}

function logRequest(req, body) {
  const entry = {
    method: req.method,
    path: req.url,
    at: new Date().toISOString(),
    ...(body ? { body } : {}),
  };
  requests.push(entry);
  console.log(`[mock] ${entry.method} ${entry.path}${body ? ` body=${body}` : ''}`);
}

const server = http.createServer((req, res) => {
  let raw = '';
  req.on('data', (c) => {
    raw += c;
    if (raw.length > 1e6) req.destroy();
  });
  req.on('end', () => {
    const path = (req.url || '/').split('?')[0];
    logRequest(req, raw || undefined);

    // Test-control endpoints (not part of the real server contract).
    if (path === '/__requests') {
      const buf = Buffer.from(JSON.stringify(requests, null, 2));
      res.writeHead(200, { 'Content-Type': 'application/json', 'Content-Length': buf.length });
      return res.end(buf);
    }
    if (path === '/__reset' && req.method === 'POST') {
      requests.length = 0;
      return send(res, 200, '{"ok":true}', { 'Content-Type': 'application/json' });
    }

    if (path === '/api/health') {
      const buf = Buffer.from(
        JSON.stringify({ app: 'clawbench', version: SERVER_VERSION, status: 'ok' }),
      );
      res.writeHead(200, { 'Content-Type': 'application/json', 'Content-Length': buf.length });
      return res.end(buf);
    }

    if (path === '/login' && req.method === 'POST') {
      // The app only inspects the status code and Set-Cookie headers; the body
      // is irrelevant. Cookie name matches the unscoped port-20000 convention.
      return send(res, 200, '{"ok":true}', {
        'Content-Type': 'application/json',
        'Set-Cookie': `clawbench_session=e2e-${Date.now()}; Path=/; HttpOnly; SameSite=Lax`,
      });
    }

    return send(res, 200, homePage(SERVER_VERSION));
  });
});

server.listen(PORT, HOST, () => {
  console.log(`[mock] listening on http://${HOST}:${PORT}`);
  console.log(`[mock] server version = ${SERVER_VERSION}`);
  console.log(`[mock] home marker = #${E2E_HOME_MARKER}`);
});
