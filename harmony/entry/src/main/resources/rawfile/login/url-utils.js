/**
 * Parse a server address into protocol / host / port.
 *
 * Pure and dependency-free on purpose: it must be evaluable in a bare sandbox
 * (no DOM, no ClawBenchNative). Deliberately NOT built on `new URL()`:
 *   new URL('192.168.1.100:8080')  -> throws Invalid URL
 *   new URL('example.com:8080')    -> protocol "example.com:", hostname ""
 *   new URL('240e:390::1')         -> protocol "240e:", hostname ""
 * so the scheme-less forms users actually type cannot go through it. A single
 * regex treats both "with scheme" and "without scheme" uniformly.
 *
 * IPv6 literals are bracketed on the way out (`[240e:390::1]`): that is the only
 * spelling a URL can carry, and the one the manual documents for IPv6 access.
 * Input may omit the brackets, but then there is nowhere to attach a port —
 * a bare literal is the host, nothing else.
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

  var host;
  var port = null;
  var bracketed = rest.match(/^\[([^\]]+)\](?::(\d+))?$/);
  if (bracketed) {
    // Bracketed but without a colon is not a literal ('[]' or '[host]').
    if (bracketed[1].indexOf(':') < 0 || !isIPv6Literal(bracketed[1])) return null;
    host = '[' + bracketed[1] + ']';
    port = bracketed[2] || null;
    if (!isValidPort(port)) return null;
  } else {
    host = rest;
    // A literal written without brackets carries no port: there is nowhere one
    // could have been attached, and every colon belongs to the address itself.
    // ('192.168.1.100:8080' has no colon in the host part, so it lands below.)
    if (isIPv6Literal(rest)) {
      if (rest.indexOf(':') < 0) return null;
      // A trailing ':12345' of more than four digits cannot be an IPv6 group,
      // so it is a port written without brackets: reject rather than guess
      // and store an address the web form would have refused.
      if (/:[0-9]{5,}$/.test(rest)) return null;
      return { protocol: protocol, host: '[' + rest + ']', port: null };
    }
    var portMatch = rest.match(/^(.*):(\d+)$/);
    if (portMatch) {
      host = portMatch[1];
      port = portMatch[2];
      if (!isValidPort(port)) return null;
    }
    // Also rejects 'not a url', 'http://' (empty host) and 'user:pass@host'
    // (contains '@').
    if (!/^[A-Za-z0-9._-]+$/.test(host)) return null;
  }

  return { protocol: protocol, host: host, port: port };
}

/**
 * Whether `text` is shaped like an IPv6 literal: only hex digits, dots and
 * colons, and enough colons to mean it — a dotted quad such as
 * '192.168.1.100' is hex, dots and digits too, and must stay a hostname. An
 * IPv6 address has at least two colons, an IPv4-with-port has exactly one, and
 * callers match the port off first. Full validation (group counts, `::` rules)
 * is left to the platform: it rejects a malformed literal when it connects,
 * whereas a false negative here would lock out an address the user sees nothing
 * wrong with.
 */
function isIPv6Literal(text) {
  if ((text.match(/:/g) || []).length < 2) return false;
  return /^[0-9A-Fa-f:.]+$/.test(text);
}

/**
 * Whether `port` (a digit string, or null when absent) can be a TCP port.
 * Matches what `new URL()` enforces on the web side, so an address the web
 * form accepts is never rejected/clamped differently by the client.
 */
function isValidPort(port) {
  return port === null || (Number(port) >= 1 && Number(port) <= 65535);
}

/**
 * Build the canonical server URL from a single free-text address.
 *
 * Strict on purpose: the login form has no protocol radio and no port field, so
 * the address must carry BOTH explicitly. A bare host, a scheme-less
 * `host:port`, or an IPv6 address written without its brackets/port returns
 * null and the caller shows "enter a full address" rather than guessing parts
 * the user never chose (issue #696: guessing here used to save an address that
 * could not be loaded).
 *
 * Normalisation the parser already gives us: scheme lowercased, path/query/
 * fragment dropped, surrounding whitespace trimmed, IPv6 literals bracketed.
 *
 * Returns 'scheme://host:port', or null when the input is not a complete
 * server address. Callers must treat null as "reject, do not save".
 */
function buildServerUrl(text) {
  var parsed = parseServerInput(text);
  if (!parsed || !parsed.protocol || !parsed.port) return null;
  return parsed.protocol + '://' + parsed.host + ':' + parsed.port;
}

/**
 * Normalise a server's display name: trimmed. Empty means "no name", which the
 * list renders as the address instead.
 *
 * Returns '' for anything that is not a string, so callers can pass the raw
 * field value without guarding.
 */
function normalizeServerName(text) {
  if (typeof text !== 'string') return '';
  return text.trim();
}

/**
 * Find the server whose NAME equals `name`, ignoring `exceptUrl`.
 *
 * Names are unique labels, so the form refuses to save a duplicate. The
 * comparison is case-insensitive (users do not think of "Home" and "home" as
 * different servers) and an empty `name` never matches — it means "no name",
 * and unnamed servers must not collide with each other.
 *
 * `exceptUrl` is the entry currently being edited: a server must not collide
 * with itself when its name is left unchanged.
 *
 * Returns the conflicting entry (an object with at least `.url`), or null.
 */
function findServerNameConflict(servers, name, exceptUrl) {
  var wanted = normalizeServerName(name).toLowerCase();
  if (!wanted) return null;
  if (!Array.isArray(servers)) return null;
  for (var i = 0; i < servers.length; i++) {
    var srv = servers[i];
    if (!srv) continue;
    if (exceptUrl && srv.url === exceptUrl) continue;
    if (normalizeServerName(srv.name).toLowerCase() === wanted) return srv;
  }
  return null;
}
