import { http } from '@kit.NetworkKit';
import { webview } from '@kit.ArkWeb';
import { BusinessError } from '@kit.BasicServicesKit';
import { compatibleProtocol, parseServerAddress } from '../models/ServerAddress';
import { registerServerCookies } from '../models/NativeCookies';
import { nativeMessage } from '../models/NativeMessages';

interface HealthResponse {
  app: string;
  protocol?: number;
  version?: string;
}

export class ConnectionFailure extends Error {
  code: string;

  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

export class ConnectionService {
  private generation: number = 0;
  private requests: http.HttpRequest[] = [];

  cancel(): void {
    this.generation++;
    this.requests.forEach((request: http.HttpRequest) => request.destroy());
    this.requests = [];
  }

  async connect(input: string, password: string, language: string = 'zh'): Promise<string> {
    this.cancel();
    const generation = this.generation;
    let address: string;
    try {
      address = parseServerAddress(input).url;
    } catch (_) {
      throw new ConnectionFailure('invalid_address', nativeMessage('invalid_address', language));
    }
    const health = await this.request(address + '/api/health', http.RequestMethod.GET, generation, language);
    this.checkGeneration(generation, language);
    if (health.responseCode !== 200) {
      throw new ConnectionFailure('health_failed', nativeMessage('health_failed', language));
    }
    let info: HealthResponse;
    try {
      info = JSON.parse(String(health.result)) as HealthResponse;
    } catch (_) {
      throw new ConnectionFailure('invalid_server', nativeMessage('invalid_server', language));
    }
    if (!info || typeof info !== 'object' || Array.isArray(info) || info.app !== 'clawbench' ||
      (info.protocol !== undefined && (typeof info.protocol !== 'number' || !Number.isInteger(info.protocol))) ||
      (info.version !== undefined && typeof info.version !== 'string')) {
      throw new ConnectionFailure('invalid_server', nativeMessage('invalid_server', language));
    }
    if (!compatibleProtocol(info.app, info.protocol || 0)) {
      throw new ConnectionFailure('protocol', nativeMessage('protocol', language));
    }
    const auth = await this.request(address + '/login', http.RequestMethod.POST, generation, language,
      JSON.stringify({ password }));
    this.checkGeneration(generation, language);
    if (auth.responseCode === 401) {
      throw new ConnectionFailure('password', nativeMessage('password', language));
    }
    if (auth.responseCode === 429) {
      throw new ConnectionFailure('rate_limit', nativeMessage('rate_limit', language));
    }
    if (auth.responseCode !== 200) {
      throw new ConnectionFailure('auth_failed', nativeMessage('auth_failed', language));
    }
    const headers = auth.header as Record<string, Object>;
    const receivedCookies: string[] = [];
    Object.keys(headers).forEach((key: string) => {
      if (key.toLowerCase() === 'set-cookie') {
        const value = headers[key];
        const cookies = Array.isArray(value) ? value as string[] : [String(value)];
        cookies.forEach((cookie: string) => {
          webview.WebCookieManager.configCookieSync(address + '/', cookie);
          receivedCookies.push(cookie);
        });
      }
    });
    if (receivedCookies.length === 0) {
      throw new ConnectionFailure('auth_cookie_missing', nativeMessage('auth_cookie_missing', language));
    }
    registerServerCookies(address, receivedCookies);
    webview.WebCookieManager.saveCookieSync();
    return address;
  }

  private checkGeneration(generation: number, language: string): void {
    if (generation !== this.generation) {
      throw new ConnectionFailure('cancelled', nativeMessage('cancelled', language));
    }
  }

  private async request(url: string, method: http.RequestMethod, generation: number,
    language: string, body: string = ''): Promise<http.HttpResponse> {
    const request = http.createHttp();
    this.requests.push(request);
    try {
      return await request.request(url, {
        method,
        header: { 'Content-Type': 'application/json' },
        extraData: body,
        connectTimeout: 10000,
        readTimeout: 15000,
        usingCache: false,
        maxRedirects: 0,
        maxLimit: 65536,
        expectDataType: http.HttpDataType.STRING
      });
    } catch (error) {
      this.checkGeneration(generation, language);
      const nativeCode = error ? (error as BusinessError).code : 0;
      let code = 'network';
      // Keep certificate failures distinct from DNS, timeouts and HTTP status errors.
      switch (nativeCode) {
        case 201:
          code = 'network_permission';
          break;
        case 2300005:
        case 2300006:
          code = 'dns';
          break;
        case 2300028:
          code = 'timeout';
          break;
        case 2300058:
          code = 'tls_local_certificate';
          break;
        case 2300059:
          code = 'tls_protocol';
          break;
        case 2300060:
          code = 'certificate';
          break;
        case 2300077:
          code = 'tls_ca_unavailable';
          break;
      }
      throw new ConnectionFailure(code, nativeMessage(code, language));
    } finally {
      this.requests = this.requests.filter((item: http.HttpRequest) => item !== request);
      request.destroy();
    }
  }
}
