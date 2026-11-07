import { util } from '@kit.ArkTS';
import { ServerStore } from './ServerStore';
import { APP_VERSION } from '../models/AppInfo';
import { parseServerAddress } from '../models/ServerAddress';
import { NativeFailure } from '../models/NativeMessages';

interface LoginServer {
  url: string;
  name: string;
  password: string;
  credentialState: string;
}

export interface LoginCredential {
  kind: string;
  value?: string;
  ref?: string;
  sourceUrl?: string;
}

export interface LoginSnapshot {
  servers: LoginServer[];
  activeUrl: string;
  credentialsIgnored: boolean;
  language: string;
  theme: string;
  version: string;
}

const PASSWORD_REF_PREFIX = 'clawbench-saved:';

export class LoginInitialization {
  configuration: string = '';

  getConfig(): string {
    return this.configuration;
  }
}

export class LoginSession {
  private store: ServerStore;
  private refs: Map<string, string> = new Map();
  private byServer: Map<string, string> = new Map();
  private epoch: number = 0;
  private snapshotId: number = 0;

  constructor(store: ServerStore) {
    this.store = store;
  }

  clear(): void {
    this.epoch++;
    this.refs.clear();
    this.byServer.clear();
  }

  async snapshot(selectedUrl: string = '', ignoreCredentials: boolean = false): Promise<LoginSnapshot> {
    const epoch = this.epoch;
    const snapshotId = ++this.snapshotId;
    const servers: LoginServer[] = [];
    const refs: Map<string, string> = new Map();
    const byServer: Map<string, string> = new Map();
    for (const server of this.store.servers) {
      let reference = '';
      let credentialState = ignoreCredentials ? 'ignored' : 'missing';
      if (!ignoreCredentials) {
        try {
          if ((await this.store.password(server.url)).length > 0) {
            reference = this.byServer.get(server.url) || PASSWORD_REF_PREFIX + util.generateRandomUUID(true);
            byServer.set(server.url, reference);
            refs.set(reference, server.url);
            credentialState = 'saved';
          }
        } catch (_) {
          credentialState = 'unavailable';
        }
      }
      servers.push({ url: server.url, name: server.name, password: reference, credentialState });
    }
    // A late snapshot must not restore references cleared by a newer login document.
    if (epoch === this.epoch && snapshotId === this.snapshotId) {
      this.refs = refs;
      this.byServer = byServer;
    }
    let requested = '';
    try {
      requested = parseServerAddress(selectedUrl).url;
    } catch (_) {}
    if (requested && !servers.some((server) => server.url === requested)) {
      servers.push({
        url: requested, name: '', password: '', credentialState: ignoreCredentials ? 'ignored' : 'missing'
      });
    }
    const selected = servers.find((server) => server.url === requested) ||
      servers.find((server) => server.url === this.store.lastUrl) || servers[0];
    return {
      servers, activeUrl: selected?.url || '', credentialsIgnored: ignoreCredentials,
      language: this.store.language, theme: this.store.theme, version: APP_VERSION
    };
  }

  private invalidCredential(): NativeFailure {
    return new NativeFailure('credential_ref_invalid', this.store.language);
  }

  async password(credential: LoginCredential, targetUrl: string,
    allowSourceServer: boolean = false): Promise<string> {
    if (!credential || typeof credential !== 'object') {
      throw this.invalidCredential();
    }
    if (credential.kind === 'plain') {
      if (typeof credential.value !== 'string') {
        throw this.invalidCredential();
      }
      return credential.value;
    }
    if (credential.kind !== 'saved' || typeof credential.ref !== 'string' ||
      typeof credential.sourceUrl !== 'string') {
      throw this.invalidCredential();
    }
    const reference = credential.ref;
    const source = this.refs.get(reference);
    let origin = '';
    let target = '';
    try {
      origin = parseServerAddress(credential.sourceUrl).url;
      target = parseServerAddress(targetUrl).url;
    } catch (_) {
      throw this.invalidCredential();
    }
    if (!source || source !== origin || (!allowSourceServer && source !== target)) {
      throw this.invalidCredential();
    }
    const epoch = this.epoch;
    let password: string;
    try {
      password = await this.store.password(source);
    } catch (_) {
      if (epoch === this.epoch && this.refs.get(reference) === source) {
        this.refs.delete(reference);
        this.byServer.delete(source);
      }
      throw new NativeFailure('credential_read_failed', this.store.language);
    }
    if (epoch !== this.epoch || this.refs.get(reference) !== source) {
      throw this.invalidCredential();
    }
    if (!password) {
      this.refs.delete(reference);
      this.byServer.delete(source);
      throw this.invalidCredential();
    }
    return password;
  }

  async save(url: string, credential: LoginCredential, name?: string,
    ignoreCredentials: boolean = false): Promise<void> {
    const address = parseServerAddress(url);
    const existing = this.store.servers.find((server) => server.url === address.url);
    const label = name === undefined ? existing?.name || '' : name;
    if (ignoreCredentials) {
      await this.store.saveMetadata(address.url, label);
      return;
    }
    await this.store.save(address.url, await this.password(credential, address.url, true), label);
  }
}
