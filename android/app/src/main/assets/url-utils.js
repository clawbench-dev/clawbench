/**
 * Parse a server address into protocol / host / port.
 *
 * Pure and dependency-free on purpose: it must be evaluable in a bare sandbox
 * (no DOM, no ClawBenchNative). Deliberately NOT built on `new URL()`:
 *   new URL('192.168.1.100:8080')  -> throws Invalid URL
 *   new URL('example.com:8080')    -> protocol "example.com:", hostname ""
 * so the scheme-less form users actually type cannot go through it. A single
 * regex treats both "with scheme" and "without scheme" uniformly.
 *
 * Returns { protocol: 'http'|'https'|null, host: string, port: string|null },
 * or null when the input is not a server address we understand. Callers must
 * treat null as "leave the field alone".
 */
function parseServerInput(text) {
  if (typeof text !== 'string') return null;
  var s = text.trim();
  if (!s) return null;

  var protocol = null;
  var rest = s;

  // Optional scheme. Anything other than http/https is not ours to interpret.
  var schemeMatch = rest.match(/^([A-Za-z][A-Za-z0-9+.-]*):\/\//);
  if (schemeMatch) {
    var scheme = schemeMatch[1].toLowerCase();
    if (scheme !== 'http' && scheme !== 'https') return null;
    protocol = scheme;
    rest = rest.slice(schemeMatch[0].length);
  }

  // Drop path, query and fragment — only scheme/host/port are meaningful here.
  rest = rest.split(/[/?#]/)[0];

  var host = rest;
  var port = null;
  var portMatch = rest.match(/^(.*):(\d+)$/);
  if (portMatch) {
    host = portMatch[1];
    port = portMatch[2];
  }

  // Host must look like a hostname or IPv4 literal. This is also what rejects
  // 'not a url', 'http://' (empty host) and 'user:pass@host' (contains '@').
  if (!/^[A-Za-z0-9._-]+$/.test(host)) return null;

  return { protocol: protocol, host: host, port: port };
}
