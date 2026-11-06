import { common } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { http } from '@kit.NetworkKit';
import { webview } from '@kit.ArkWeb';
import { execute, cancel } from 'libclawbench_tunnel.so';
import { parseServerAddress } from '../models/ServerAddress';
import { nativeCookieHeader } from '../models/NativeCookies';
import {
  TunnelAddResult, TunnelCommand, TunnelDriver, TunnelFailure, TunnelMapping, TunnelPolicy, TunnelResult,
  TunnelSettings, TunnelTransport, tunnelMapping, validHostFingerprint, validTunnelPort
} from '../models/TunnelPolicy';

interface ServerTunnelSettings {
  origin: string;
  transport: TunnelTransport;
  mappings: TunnelMapping[];
}

interface TrustedHost {
  origin: string;
  port: number;
  fingerprint: string;
}

interface SshInfo {
  enabled: boolean;
  port: number;
}

class TunnelPlatform implements TunnelDriver {
  private context: common.UIAbilityContext;
  private password: (origin: string) => Promise<string>;
  private confirmHost: (origin: string, port: number, fingerprint: string, previous: string) => Promise<boolean>;
  private prefs?: preferences.Preferences;
  private servers: ServerTunnelSettings[] = [];
  private trusted: TrustedHost[] = [];
  private requests: http.HttpRequest[] = [];
  private saving: Promise<void> = Promise.resolve();

  constructor(context: common.UIAbilityContext, password: (origin: string) => Promise<string>,
    confirmHost: (origin: string, port: number, fingerprint: string, previous: string) => Promise<boolean>) {
    this.context = context;
    this.password = password;
    this.confirmHost = confirmHost;
  }

  async init(): Promise<void> {
    this.prefs = await preferences.getPreferences(this.context, { name: 'clawbench-tunnels' });
    try {
      const saved = JSON.parse(String(await this.prefs.get('servers', '[]'))) as ServerTunnelSettings[];
      if (Array.isArray(saved)) {
        for (const server of saved.slice(0, 100)) {
          try {
            const origin = parseServerAddress(server.origin).origin;
            if (!Array.isArray(server.mappings)) {
              continue;
            }
            const mappings: TunnelMapping[] = [];
            for (const mapping of server.mappings.slice(0, 32)) {
              try { mappings.push(tunnelMapping(mapping.port, mapping.targetPort, mapping.host, mapping.direction)); } catch (_) {}
            }
            this.servers.push({ origin, transport: server.transport === 'h2' ? 'h2' : 'ssh', mappings });
          } catch (_) {}
        }
      }
    } catch (_) {}
    try {
      const saved = JSON.parse(String(await this.prefs.get('trustedHosts', '[]'))) as TrustedHost[];
      if (Array.isArray(saved)) {
        for (const host of saved.slice(0, 100)) {
          try {
            const origin = parseServerAddress(host.origin).origin;
            if (validTunnelPort(host.port) && validHostFingerprint(host.fingerprint)) {
              this.trusted.push({ origin, port: host.port, fingerprint: host.fingerprint });
            }
          } catch (_) {}
        }
      }
    } catch (_) {}
  }

  settings(origin: string): TunnelSettings {
    const server = this.servers.find((item) => item.origin === origin);
    return server ? { transport: server.transport, mappings: server.mappings } : { transport: 'ssh', mappings: [] };
  }

  async execute(command: TunnelCommand): Promise<TunnelResult> {
    const response = await execute(JSON.stringify(command));
    if (response.length > 65536) {
      return { ok: false, error: 'Invalid native tunnel response', errorType: 'unknown' };
    }
    return JSON.parse(response) as TunnelResult;
  }

  cancel(): void {
    cancel();
    const requests = this.requests;
    this.requests = [];
    requests.forEach((request) => {
      try { request.destroy(); } catch (_) {}
    });
  }

  async prepare(origin: string, transport: TunnelTransport, isCurrent: () => boolean): Promise<TunnelCommand> {
    const address = parseServerAddress(origin);
    if (address.origin !== origin || !isCurrent()) {
      throw new Error('Tunnel server changed');
    }
    let cookie = '';
    try {
      cookie = nativeCookieHeader(origin, webview.WebCookieManager.fetchCookieSync(origin + '/api/tunnel/stream'));
    } catch (_) {
      throw new TunnelFailure('auth', 'Native login session unavailable; sign in again');
    }
    if (transport === 'h2') {
      return { method: 'connect', transport, origin, cookie };
    }
    const request = http.createHttp();
    this.requests.push(request);
    try {
      const response = await request.request(origin + '/api/ssh/info', {
        method: http.RequestMethod.GET,
        header: { 'Cookie': cookie },
        connectTimeout: 10000,
        readTimeout: 10000,
        usingCache: false,
        maxRedirects: 0,
        maxLimit: 65536,
        expectDataType: http.HttpDataType.STRING
      });
      if (!isCurrent() || response.responseCode !== 200) {
        throw new TunnelFailure(response.responseCode === 401 || response.responseCode === 403 ? 'auth' : 'network',
          'SSH information unavailable');
      }
      const info = JSON.parse(String(response.result)) as SshInfo;
      if (info.enabled !== true || !validTunnelPort(info.port)) {
        throw new TunnelFailure('disabled', 'SSH is not enabled on this server');
      }
      const password = await this.password(origin);
      if (!isCurrent()) {
        throw new Error('Tunnel server changed');
      }
      const trusted = this.trusted.find((host) => host.origin === origin && host.port === info.port);
      return {
        method: 'connect', transport, origin, cookie, password, username: 'clawbench',
        sshPort: info.port, hostKey: trusted?.fingerprint || ''
      };
    } finally {
      this.requests = this.requests.filter((item) => item !== request);
      try { request.destroy(); } catch (_) {}
    }
  }

  async approveHostKey(command: TunnelCommand, fingerprint: string, isCurrent: () => boolean): Promise<boolean> {
    if (!isCurrent() || !validHostFingerprint(fingerprint) || command.transport !== 'ssh' ||
      !command.origin || !validTunnelPort(command.sshPort || 0)) {
      return false;
    }
    const origin = command.origin;
    const port = command.sshPort || 0;
    const previous = command.hostKey || '';
    const approved = await this.confirmHost(origin, port, fingerprint, previous);
    if (!approved || !isCurrent()) {
      return false;
    }
    return await this.save(async (): Promise<boolean> => {
      if (!isCurrent() || !this.prefs) {
        return false;
      }
      const hosts = this.trusted.filter((host) => host.origin !== origin || host.port !== port);
      hosts.push({ origin, port, fingerprint });
      await this.prefs.put('trustedHosts', JSON.stringify(hosts.slice(-100)));
      await this.prefs.flush();
      this.trusted = hosts.slice(-100);
      return isCurrent();
    });
  }

  async persist(origin: string, settings: TunnelSettings): Promise<void> {
    await this.save(async (): Promise<void> => {
      if (!this.prefs) {
        throw new Error('Tunnel preferences unavailable');
      }
      const servers = this.servers.filter((server) => server.origin !== origin);
      servers.push({ origin, transport: settings.transport, mappings: settings.mappings });
      await this.prefs.put('servers', JSON.stringify(servers.slice(-100)));
      await this.prefs.flush();
      this.servers = servers.slice(-100);
    });
  }

  private save<T>(operation: () => Promise<T>): Promise<T> {
    const result = this.saving.then(operation);
    this.saving = result.then(() => {}, () => {});
    return result;
  }
}

export class TunnelService {
  private platform: TunnelPlatform;
  private policy: TunnelPolicy;
  private timer: number = -1;

  constructor(context: common.UIAbilityContext, password: (origin: string) => Promise<string>,
    approveHostKey: (origin: string, port: number, fingerprint: string, previous: string) => Promise<boolean>,
    onPortRebound: (requested: number, actual: number) => void = () => {}) {
    this.platform = new TunnelPlatform(context, password, approveHostKey);
    this.policy = new TunnelPolicy(this.platform, onPortRebound);
  }

  async init(): Promise<void> {
    await this.platform.init();
    if (this.timer < 0) {
      this.timer = setInterval(() => { void this.policy.tick(); }, 2000);
    }
  }

  setServer(input: string): void {
    const origin = input ? parseServerAddress(input).origin : '';
    this.policy.setServer(origin, this.platform.settings(origin));
  }

  setForeground(foreground: boolean): void {
    this.policy.setForeground(foreground);
    if (foreground) {
      void this.policy.tick();
    }
  }

  shutdown(): void {
    if (this.timer >= 0) {
      clearInterval(this.timer);
      this.timer = -1;
    }
    this.policy.shutdown();
  }

  getForwardedPorts(): string { return this.policy.getForwardedPorts(); }
  getTunnelError(): string { return this.policy.getTunnelError(); }
  getTunnelErrorType(): string { return this.policy.getTunnelErrorType(); }
  getTunnelTransport(): string { return this.policy.getTunnelTransport(); }
  getActiveTunnelTransport(): string { return this.policy.getActiveTunnelTransport(); }
  getTunnelTransportH2Enabled(): boolean { return this.policy.getTunnelTransportH2Enabled(); }
  async setTunnelTransportH2Enabled(enabled: boolean): Promise<void> { await this.policy.setTunnelTransportH2Enabled(enabled); }
  async isTunnelConnected(): Promise<boolean> { return await this.policy.isTunnelConnected(); }
  async testPortReachable(port: number): Promise<boolean> { return await this.policy.testPortReachable(port); }
  async prepareForwardedPort(port: number): Promise<number> { return await this.policy.prepareForwardedPort(port); }
  async addForwardedPort(port: number, targetPort: number, host: string): Promise<TunnelAddResult> {
    return await this.policy.addForwardedPort(port, targetPort, host);
  }
  async addReverseForwardedPort(port: number, targetPort: number, host: string): Promise<boolean> {
    return await this.policy.addReverseForwardedPort(port, targetPort, host);
  }
  async removeForwardedPort(port: number): Promise<void> { await this.policy.removeForwardedPort(port); }
  async removeReverseForwardedPort(port: number): Promise<void> { await this.policy.removeReverseForwardedPort(port); }
  async reconnectTunnel(): Promise<boolean> { return await this.policy.reconnectTunnel(); }
  async reconnectTunnelAsync(): Promise<boolean> { return await this.policy.reconnectTunnel(); }
  async forgetForwardedPorts(): Promise<void> { await this.policy.forgetForwardedPorts(); }
  async stopBackgroundService(): Promise<void> {
    this.policy.stopWhenIdle();
  }
}
