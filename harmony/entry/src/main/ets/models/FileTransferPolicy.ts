export const MAX_DOWNLOAD_BYTES: number = 2 * 1024 * 1024 * 1024;
export const MAX_SHARE_BYTES: number = 256 * 1024 * 1024;
export const MAX_SHARED_CACHE_BYTES: number = 1024 * 1024 * 1024;
export const MAX_BLOB_BYTES: number = 8 * 1024 * 1024;
export const MAX_SHARE_FILES: number = 20;
export const MAX_SHARE_TEXT_LENGTH: number = 32000;

export interface DownloadAddress {
  url: string;
  authenticated: boolean;
}

export interface TransferProgress {
  id: number;
  received: number;
  total: number;
  done: boolean;
  error: string;
  cancelled: boolean;
}

export function safeFileName(input: string, fallback: string = 'download'): string {
  const name = input.replace(/\\/g, '/').split('/').pop() || fallback;
  const cleaned = name.replace(/[\x00-\x1f\x7f<>:"|?*]/g, '_')
    .replace(/^[. ]+|[. ]+$/g, '');
  // UTF-8 may use four bytes per character; keep a file-system-safe byte budget.
  let result = '';
  let bytes = 0;
  for (let i = 0; i < cleaned.length; i++) {
    const code = cleaned.charCodeAt(i);
    const width = code < 0x80 ? 1 : (code < 0x800 ? 2 : 3);
    if (bytes + width > 180) {
      break;
    }
    result += cleaned.charAt(i);
    bytes += width;
  }
  // A truncated surrogate pair is not a valid file name on every provider.
  return result.replace(/[\uD800-\uDBFF]$/, '') || fallback;
}

export function projectDownloadUrl(origin: string, path: string): string {
  if (typeof path !== 'string' || !path || path.length > 16384 || /[\x00-\x1f\x7f]/.test(path)) {
    throw new Error('Invalid file path');
  }
  const normalized = path.replace(/\\/g, '/');
  if (/^[a-z][a-z0-9+.-]*:/i.test(normalized) && !/^[a-z]:\//i.test(normalized)) {
    throw new Error('Invalid file path');
  }
  let inspected = normalized;
  for (let i = 0; i < 4; i++) {
    if (inspected.split('/').some((part: string) => part === '..') || /[\x00-\x1f\x7f\\]/.test(inspected)) {
      throw new Error('Invalid file path');
    }
    try {
      const decoded = decodeURIComponent(inspected);
      if (decoded === inspected) {
        break;
      }
      inspected = decoded;
    } catch (_) {
      // A literal percent sign is a valid server file name.
      break;
    }
  }
  if (inspected.split('/').some((part: string) => part === '..') || /[\x00-\x1f\x7f\\]/.test(inspected)) {
    throw new Error('Invalid file path');
  }
  if (normalized.startsWith('/') || /^[a-z]:\//i.test(normalized)) {
    return origin + '/api/fs/raw/?download=1&target=' + encodeURIComponent(normalized);
  }
  return origin + '/api/fs/raw/' + normalized.split('/').map((part: string) => encodeURIComponent(part)).join('/') +
    '?download=1';
}

export function resolveDownloadAddress(origin: string, input: string): DownloadAddress {
  if (typeof input !== 'string' || !input || input.length > 32768 || /[\\\s\x00-\x1f\x7f]/.test(input)) {
    throw new Error('Invalid download URL');
  }
  const candidate = input.startsWith('/') && !input.startsWith('//') ? origin + input : input;
  const match = /^(https?):\/\/(\[[0-9a-f:]+\]|[a-z0-9.-]+)(?::([0-9]+))?([/?][^#]*)?(?:#.*)?$/i.exec(candidate);
  if (!match) {
    throw new Error('Only HTTP or HTTPS downloads are supported');
  }
  const protocol = match[1].toLowerCase();
  const port = match[3] ? Number(match[3]) : (protocol === 'https' ? 443 : 80);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error('Invalid download port');
  }
  const defaultPort = (protocol === 'https' && port === 443) || (protocol === 'http' && port === 80);
  const remote = protocol + '://' + match[2].toLowerCase() + (defaultPort ? '' : ':' + port);
  const path = match[4] || '/';
  return { url: remote + (path.startsWith('?') ? '/' : '') + path, authenticated: remote === origin };
}

export function validateBase64(input: string): number {
  if (typeof input !== 'string' || input.length > Math.ceil(MAX_BLOB_BYTES / 3) * 4 ||
      input.length % 4 !== 0) {
    throw new Error('Invalid or oversized base64 download');
  }
  const padding = input.endsWith('==') ? 2 : (input.endsWith('=') ? 1 : 0);
  if (/[^A-Za-z0-9+/]/.test(input.slice(0, input.length - padding))) {
    throw new Error('Invalid base64 download');
  }
  const bytes = input.length / 4 * 3 - padding;
  if (bytes > MAX_BLOB_BYTES) {
    throw new Error('Blob download exceeds 8 MiB');
  }
  return bytes;
}

export function shareMime(input: string): string {
  return typeof input === 'string' && /^[a-z0-9.+-]+\/[a-z0-9.+-]+$/i.test(input) ?
    input.toLowerCase() : 'application/octet-stream';
}

export class TransferProgressState {
  id: number;
  received: number = 0;
  total: number = 0;
  done: boolean = false;
  error: string = '';
  cancelled: boolean = false;
  private limit: number;

  constructor(id: number, limit: number = MAX_DOWNLOAD_BYTES) {
    if (!Number.isSafeInteger(id)) {
      throw new Error('Invalid download id');
    }
    this.id = id;
    this.limit = limit;
  }

  setTotal(value: number): void {
    if (this.done || !Number.isFinite(value) || value < 0) {
      return;
    }
    if (value > this.limit) {
      throw new Error('File exceeds the transfer size limit');
    }
    this.total = value;
  }

  receive(bytes: number): void {
    if (this.done) {
      return;
    }
    if (!Number.isSafeInteger(bytes) || bytes < 0 || this.received + bytes > this.limit) {
      throw new Error('File exceeds the transfer size limit');
    }
    this.received += bytes;
  }

  finish(error: string = '', cancelled: boolean = false): boolean {
    if (this.done) {
      return false;
    }
    this.done = true;
    this.error = error;
    this.cancelled = cancelled;
    if (!error && !cancelled && this.total === 0) {
      this.total = this.received;
    }
    return true;
  }

  snapshot(): TransferProgress {
    return {
      id: this.id, received: this.received, total: this.total,
      done: this.done, error: this.error, cancelled: this.cancelled
    };
  }
}
