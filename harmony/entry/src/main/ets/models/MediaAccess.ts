import { parseServerAddress } from './ServerAddress';

export interface MediaRequest {
  getOrigin(): string;
  getAccessibleResource(): string[];
  grant(resources: string[]): void;
  deny(): void;
}

export interface CaptureRequest {
  getOrigin(): string;
  deny(): void;
}

export function isCurrentMediaOrigin(requestOrigin: string, activeOrigin: string): boolean {
  if (!activeOrigin || !requestOrigin) {
    return false;
  }
  try {
    return parseServerAddress(requestOrigin).origin === parseServerAddress(activeOrigin).origin;
  } catch (_) {
    return false;
  }
}

export function mediaPermissions(resources: string[]): string[] {
  if (resources.length === 0 || resources.length > 2) {
    return [];
  }
  const permissions: string[] = [];
  for (const resource of resources) {
    let permission = '';
    if (resource === 'TYPE_VIDEO_CAPTURE') {
      permission = 'ohos.permission.CAMERA';
    } else if (resource === 'TYPE_AUDIO_CAPTURE') {
      permission = 'ohos.permission.MICROPHONE';
    } else {
      return [];
    }
    if (!permissions.includes(permission)) {
      permissions.push(permission);
    }
  }
  return permissions;
}

export class MediaAccessGate {
  private pending?: MediaRequest;
  private epoch: number = 0;

  cancel(): void {
    this.epoch++;
    const request = this.pending;
    this.pending = undefined;
    try { request?.deny(); } catch (_) {}
  }

  async request(request: MediaRequest, activeOrigin: string, isCurrent: () => boolean,
    authorize: (permissions: string[]) => Promise<boolean>): Promise<void> {
    let resources: string[] = [];
    try {
      resources = request.getAccessibleResource();
      if (this.pending || !isCurrent() || !isCurrentMediaOrigin(request.getOrigin(), activeOrigin)) {
        request.deny();
        return;
      }
    } catch (_) {
      try { request.deny(); } catch (_) {}
      return;
    }
    const permissions = mediaPermissions(resources);
    if (permissions.length === 0) {
      try { request.deny(); } catch (_) {}
      return;
    }
    const epoch = this.epoch;
    this.pending = request;
    try {
      const authorized = await authorize(permissions);
      if (epoch !== this.epoch || this.pending !== request) {
        return;
      }
      if (authorized && isCurrent() && isCurrentMediaOrigin(request.getOrigin(), activeOrigin)) {
        request.grant(resources);
      } else {
        request.deny();
      }
      this.pending = undefined;
    } catch (_) {
      if (epoch === this.epoch && this.pending === request) {
        this.pending = undefined;
        try { request.deny(); } catch (_) {}
      }
    }
  }
}

export class CaptureConsentGate {
  private pending?: CaptureRequest;
  private epoch: number = 0;

  cancel(): void {
    this.epoch++;
    const request = this.pending;
    this.pending = undefined;
    try { request?.deny(); } catch (_) {}
  }

  async request(request: CaptureRequest, activeOrigin: string, isCurrent: () => boolean,
    confirm: (origin: string) => Promise<boolean>, grant: () => void): Promise<void> {
    try {
      if (this.pending || !isCurrent() || !isCurrentMediaOrigin(request.getOrigin(), activeOrigin)) {
        request.deny();
        return;
      }
    } catch (_) {
      try { request.deny(); } catch (_) {}
      return;
    }
    const epoch = this.epoch;
    this.pending = request;
    try {
      const accepted = await confirm(activeOrigin);
      if (epoch !== this.epoch || this.pending !== request) {
        return;
      }
      this.pending = undefined;
      if (accepted && isCurrent() && isCurrentMediaOrigin(request.getOrigin(), activeOrigin)) {
        grant();
      } else {
        request.deny();
      }
    } catch (_) {
      if (epoch === this.epoch) {
        this.pending = undefined;
        try { request.deny(); } catch (_) {}
      }
    }
  }
}
