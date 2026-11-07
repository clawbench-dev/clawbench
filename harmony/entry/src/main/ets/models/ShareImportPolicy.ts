export const MAX_IMPORT_FILES: number = 20;
export const MAX_IMPORT_FILE_BYTES: number = 128 * 1024 * 1024;
export const MAX_IMPORT_BATCH_BYTES: number = 256 * 1024 * 1024;
export const SHARE_IMPORT_DIRECTORY: string = '.clawbench/share-in';

export interface ShareImportSummary {
  id: string;
  names: string[];
  totalBytes: number;
  busy: boolean;
}

export interface ShareImportDestination {
  server: string;
  projectPath: string;
}

export interface ShareImportResult {
  paths: string[];
  failed: number;
  cancelled: boolean;
  projectPath: string;
}

export function validateSharedUri(uri: string, ownBundle: string): string {
  if (typeof uri !== 'string' || uri.length > 32768 || /[\\\x00-\x20\x7f?#]/.test(uri)) {
    throw new Error('Invalid shared file URI');
  }
  const match = /^file:\/\/([a-z0-9_.-]+)(\/.*)$/i.exec(uri);
  if (!match || ['localhost', '.', '..', ownBundle.toLowerCase()].includes(match[1].toLowerCase())) {
    // External wants must never use this app's existing sandbox authority as a file source.
    throw new Error('The shared file must belong to an external provider');
  }
  let path = match[2];
  for (let index = 0; index < 5; index++) {
    if (path.split('/').some((part: string) => part === '..') || /[\\\x00-\x1f\x7f]/.test(path)) {
      throw new Error('Invalid shared file path');
    }
    let decoded = '';
    try {
      decoded = decodeURIComponent(path);
    } catch (_) {
      throw new Error('Invalid shared file encoding');
    }
    if (decoded === path) {
      return uri;
    }
    path = decoded;
  }
  throw new Error('Excessive shared file encoding');
}

export function sharedProjectFromCookies(header: string): string {
  let project = '';
  for (const item of header.split(';')) {
    const match = /^(?:cb[0-9]{1,5}_)?clawbench_project=(.+)$/.exec(item.trim());
    if (!match) {
      continue;
    }
    if (project) {
      throw new Error('Ambiguous destination project');
    }
    try {
      project = decodeURIComponent(match[1].replace(/\+/g, ' '));
    } catch (_) {
      throw new Error('Invalid destination project');
    }
  }
  if (!project || project.length > 16384 || /[\x00-\x1f\x7f]/.test(project)) {
    throw new Error('Choose a project before importing shared files');
  }
  return project;
}

export function validateUploadedSharePath(path: string): string {
  const prefix = SHARE_IMPORT_DIRECTORY + '/';
  if (typeof path !== 'string' || !path.startsWith(prefix) || path.length > 2048 ||
      /[\\\x00-\x1f\x7f]/.test(path)) {
    throw new Error('Invalid upload result path');
  }
  const name = path.slice(prefix.length);
  if (!name || name === '.' || name === '..' || name.includes('/')) {
    throw new Error('Invalid upload result path');
  }
  return path;
}
