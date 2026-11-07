import { CaptureConsentGate } from '../models/MediaAccess';

// ArkWeb supplies a MediaStream to getDisplayMedia; no Android JPEG/PCM emulation is needed.
export class ScreenCaptureService {
  private gate: CaptureConsentGate = new CaptureConsentGate();
  private confirm: (origin: string) => Promise<boolean>;

  constructor(confirm: (origin: string) => Promise<boolean>) {
    this.confirm = confirm;
  }

  request(handler: ScreenCaptureHandler, activeOrigin: string, isCurrent: () => boolean): Promise<void> {
    return this.gate.request(handler, activeOrigin, isCurrent, this.confirm, (): void => {
      handler.grant({ captureMode: WebCaptureMode.HOME_SCREEN });
    });
  }

  cancel(): void {
    this.gate.cancel();
  }
}
