export interface FilePickerPlan {
  kind: string;
  maxSelectNumber: number;
  images: boolean;
  videos: boolean;
  suffixFilters: string[];
}

function extensionsForType(type: string): string[] {
  if (/^\.[a-z0-9]{1,12}$/.test(type)) {
    return [type];
  }
  switch (type) {
    case 'image/jpeg': return ['.jpg', '.jpeg'];
    case 'image/png': return ['.png'];
    case 'image/gif': return ['.gif'];
    case 'image/webp': return ['.webp'];
    case 'image/heic': return ['.heic'];
    case 'video/mp4': return ['.mp4'];
    case 'video/webm': return ['.webm'];
    case 'audio/mpeg': return ['.mp3'];
    case 'audio/wav': return ['.wav'];
    case 'application/pdf': return ['.pdf'];
    case 'text/plain': return ['.txt'];
    case 'text/csv': return ['.csv'];
    case 'application/json': return ['.json'];
    case 'application/zip': return ['.zip'];
    default: return [];
  }
}

export function filePickerPlan(accept: string[], capture: boolean, multiple: boolean): FilePickerPlan {
  const types = accept.slice(0, 32).map((type: string): string => type.trim().toLowerCase()).filter(
    (type: string): boolean => type.length > 0 && type.length <= 100);
  const images = types.some((type: string): boolean =>
    type.startsWith('image/') || /^\.(png|jpe?g|gif|webp|heic|bmp|avif)$/.test(type));
  const videos = types.some((type: string): boolean =>
    type.startsWith('video/') || /^\.(mp4|webm|mov|m4v|3gp)$/.test(type));
  const mediaOnly = types.length > 0 && types.every((type: string): boolean =>
    type.startsWith('image/') || type.startsWith('video/') ||
    /^\.(png|jpe?g|gif|webp|heic|bmp|avif|mp4|webm|mov|m4v|3gp)$/.test(type));
  const suffixes: string[] = [];
  for (const type of types) {
    const matching = extensionsForType(type);
    if (matching.length === 0) {
      suffixes.length = 0;
      break;
    }
    for (const suffix of matching) {
      if (!suffixes.includes(suffix)) {
        suffixes.push(suffix);
      }
    }
  }
  const filter = suffixes.join(',');
  return {
    kind: mediaOnly ? (capture ? 'camera' : 'photos') : 'documents',
    maxSelectNumber: multiple && !capture ? 20 : 1,
    images, videos,
    suffixFilters: filter.length > 0 && filter.length <= 100 ? [filter] : []
  };
}

export interface FileSelectionResult {
  handleFileList(files: string[]): void;
}

export class FileSelectionGate {
  private epoch: number = 0;
  private pending?: FileSelectionResult;

  cancel(): void {
    this.epoch++;
    const result = this.pending;
    this.pending = undefined;
    try { result?.handleFileList([]); } catch (_) {}
  }

  async select(result: FileSelectionResult, isCurrent: () => boolean,
    pick: () => Promise<string[]>, maxCount: number): Promise<void> {
    if (this.pending || !isCurrent()) {
      try { result.handleFileList([]); } catch (_) {}
      return;
    }
    const epoch = this.epoch;
    this.pending = result;
    try {
      const files = await pick();
      if (epoch !== this.epoch || this.pending !== result) {
        return;
      }
      this.pending = undefined;
      result.handleFileList(isCurrent() ? files.slice(0, maxCount) : []);
    } catch (_) {
      if (epoch === this.epoch && this.pending === result) {
        this.pending = undefined;
        try { result.handleFileList([]); } catch (_) {}
      }
    }
  }
}
