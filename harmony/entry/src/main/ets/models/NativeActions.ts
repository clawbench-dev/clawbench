import { parseServerAddress } from './ServerAddress.ts';
import { NativeFailure } from './NativeMessages.ts';

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

// The share center falls back to the system composer when no built-in mailbox
// is available. Keep this separate from the HTTP-only port-browser policy.
export function externalAppUrl(value: string, language: string = 'zh'): string {
  if (typeof value !== 'string' || !/^mailto:/i.test(value)) {
    return externalHttpUrl(value);
  }
  if (value.length > 8192 || /[\u0000-\u0020\\#]/.test(value)) {
    throw new NativeFailure('invalid_mail_url', language);
  }
  const parts = value.substring(7).split('?');
  if (parts.length > 2 || /[\u0000-\u0020:/\\?#]/.test(decodeMailPart(parts[0], language))) {
    throw new NativeFailure('invalid_mail_url', language);
  }
  const seen: string[] = [];
  for (const field of parts[1] ? parts[1].split('&') : []) {
    if (!field) continue;
    const separator = field.indexOf('=');
    const key = decodeMailPart(field.substring(0, separator), language).toLowerCase();
    const content = decodeMailPart(field.substring(separator + 1), language);
    if (separator < 1 || !['subject', 'body', 'cc', 'bcc'].includes(key) || seen.includes(key) ||
        (key === 'body' ? /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/ : /[\u0000-\u001f\u007f]/).test(content)) {
      throw new NativeFailure('invalid_mail_url', language);
    }
    seen.push(key);
  }
  return 'mailto:' + value.substring(7);
}

function decodeMailPart(value: string, language: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    throw new NativeFailure('invalid_mail_url', language);
  }
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
