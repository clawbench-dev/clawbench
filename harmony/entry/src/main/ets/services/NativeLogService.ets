import { http } from '@kit.NetworkKit';
import { webview } from '@kit.ArkWeb';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { redactLog } from '../models/NativeActions';
import { nativeCookieHeader } from '../models/NativeCookies';

interface NativeLogEntry {
  level: string;
  tag: string;
  msg: string;
  ts: number;
  source: string;
}

export class NativeLogService {
  private origin: string;
  private secrets: string[];
  private enabled: boolean = false;
  private queue: NativeLogEntry[] = [];
  private timer: number = -1;
  private request?: http.HttpRequest;
  private generation: number = 0;

  constructor(origin: string, secrets: string[]) {
    this.origin = origin;
    this.secrets = secrets;
  }

  addSecret(secret: string): void {
    if (secret && !this.secrets.includes(secret)) {
      this.secrets.push(secret);
      this.queue.forEach((entry: NativeLogEntry) => {
        entry.tag = redactLog(entry.tag, this.secrets);
        entry.msg = redactLog(entry.msg, this.secrets);
      });
    }
  }

  start(): void {
    if (this.enabled) {
      return;
    }
    this.enabled = true;
    this.timer = setInterval(() => this.flush(), 3000);
  }

  stop(): void {
    this.enabled = false;
    this.generation++;
    clearInterval(this.timer);
    this.timer = -1;
    this.queue = [];
    try { this.request?.destroy(); } catch (_) {}
    this.request = undefined;
  }

  dispose(): void {
    this.stop();
    this.secrets = [];
  }

  log(level: string, tag: string, message: string): void {
    if (!this.enabled) {
      return;
    }
    const entry: NativeLogEntry = {
      level: ['D', 'I', 'W', 'E'].includes(level) ? level : 'I',
      tag: redactLog(tag, this.secrets).slice(0, 64),
      msg: redactLog(message, this.secrets),
      ts: Date.now(),
      source: 'harmonyos'
    };
    hilog.info(0x0001, 'ClawBench', '%{public}s %{public}s %{public}s', entry.level, entry.tag, entry.msg);
    this.queue.push(entry);
    if (this.queue.length > 100) {
      this.queue.shift();
    }
  }

  private async flush(): Promise<void> {
    if (!this.enabled || this.request || this.queue.length === 0) {
      return;
    }
    const endpoint = this.origin + '/api/client-log';
    const generation = this.generation;
    const entries = this.queue.splice(0, 100);
    let request: http.HttpRequest | undefined;
    try {
      const cookie = nativeCookieHeader(this.origin, webview.WebCookieManager.fetchCookieSync(endpoint));
      if (!cookie) {
        return;
      }
      request = http.createHttp();
      this.request = request;
      await request.request(endpoint, {
        method: http.RequestMethod.POST,
        header: { 'Content-Type': 'application/json', 'Cookie': cookie },
        extraData: JSON.stringify({ entries }),
        connectTimeout: 5000, readTimeout: 5000, usingCache: false,
        maxRedirects: 0, maxLimit: 4096
      });
    } catch (_) {
      // A diagnostic upload must not retry indefinitely or recursively log itself.
    } finally {
      try { request?.destroy(); } catch (_) {}
      if (generation === this.generation) {
        this.request = undefined;
      }
    }
  }
}
