import { http } from '@kit.NetworkKit';
import { webview } from '@kit.ArkWeb';
import { compatibleProtocol, parseServerAddress } from '../models/ServerAddress';

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

  async connect(input: string, password: string): Promise<string> {
    this.cancel();
    const generation = this.generation;
    const address = parseServerAddress(input);
    const health = await this.request(address.url + '/api/health', http.RequestMethod.GET);
    this.checkGeneration(generation);
    if (health.responseCode !== 200) {
      throw new Error('服务器健康检查失败');
    }
    const info = JSON.parse(String(health.result)) as HealthResponse;
    if (!compatibleProtocol(info.app, info.protocol || 0)) {
      throw new Error(info.app !== 'clawbench' ? '该地址不是 ClawBench 服务器' : '服务器客户端协议不兼容，需要协议 1');
    }
    const auth = await this.request(address.url + '/login', http.RequestMethod.POST, JSON.stringify({ password }));
    this.checkGeneration(generation);
    if (auth.responseCode === 401) {
      throw new ConnectionFailure('password', '密码不正确');
    }
    if (auth.responseCode === 429) {
      throw new ConnectionFailure('rate_limit', '登录尝试过于频繁，请稍后重试');
    }
    if (auth.responseCode !== 200) {
      throw new Error('登录失败，请检查服务器状态');
    }
    const headers = auth.header as Record<string, Object>;
    let cookieCount = 0;
    Object.keys(headers).forEach((key: string) => {
      if (key.toLowerCase() === 'set-cookie') {
        const value = headers[key];
        const cookies = Array.isArray(value) ? value as string[] : [String(value)];
        cookies.forEach((cookie: string) => {
          webview.WebCookieManager.configCookieSync(address.url + '/', cookie);
          cookieCount++;
        });
      }
    });
    if (cookieCount === 0) {
      throw new Error('服务器未返回登录凭据');
    }
    webview.WebCookieManager.saveCookieSync();
    return address.url;
  }

  private checkGeneration(generation: number): void {
    if (generation !== this.generation) {
      throw new Error('连接已取消');
    }
  }

  private async request(url: string, method: http.RequestMethod, body: string = ''): Promise<http.HttpResponse> {
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
    } finally {
      this.requests = this.requests.filter((item: http.HttpRequest) => item !== request);
      request.destroy();
    }
  }
}
