import { parseServerAddress } from './ServerAddress.ts';

export function externalHttpUrl(value: string): string {
  if (typeof value !== 'string' || value.length > 8192 || /[\u0000-\u0020\\]/.test(value)) {
    throw new Error('Invalid external URL');
  }
  const match = /^(https?:\/\/[^/?#]+)([/?#].*)?$/i.exec(value);
  if (!match) {
    throw new Error('Only HTTP(S) links are supported');
  }
  return parseServerAddress(match[1]).origin + (match[2] || '/');
}

export function portBrowserUrl(port: number, protocol: string, host: string, path: string): string {
  if (!Number.isInteger(port) || port < 1 || port > 65535 || !['http', 'https'].includes(protocol)) {
    throw new Error('Invalid browser address');
  }
  const suffix = path || '/';
  if (!suffix.startsWith('/') || suffix.startsWith('//')) {
    throw new Error('Invalid browser path');
  }
  return externalHttpUrl(`${protocol}://${host || '127.0.0.1'}:${port}${suffix}`);
}

export function selectionText(value: string): string {
  if (typeof value !== 'string' || value.length === 0 || value.length > 65536) {
    throw new Error('Invalid selection');
  }
  return value;
}

export function redactLog(value: string, secrets: string[] = []): string {
  let result = value.slice(0, 16384);
  for (const secret of secrets) {
    if (secret.length > 0) {
      result = result.split(secret).join('[redacted]');
    }
  }
  // URLs and opaque credentials are diagnostic context, never log payload.
  result = result.replace(/https?:\/\/[^\s"'<>]+/gi, '[url]');
  result = result.replace(/\b(?:authorization|cookie|set-cookie)\s*[:=][^\r\n]*/gi, '[credential]');
  result = result.replace(/((?:password|passwd|token|secret|api[_-]?key)\s*["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;}]+)/gi,
    '$1[redacted]');
  return result.replace(/[\r\n\u0000]/g, ' ').slice(0, 1024);
}
