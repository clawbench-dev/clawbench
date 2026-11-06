import { util } from '@kit.ArkTS';
import { ServerStore } from './ServerStore';
import { APP_VERSION } from '../models/AppInfo';
import { parseServerAddress } from '../models/ServerAddress';

interface LoginServer {
  url: string;
  name: string;
  password: string;
}

export interface LoginSnapshot {
  servers: LoginServer[];
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

  constructor(store: ServerStore) {
    this.store = store;
  }

  clear(): void {
    this.refs.clear();
    this.byServer.clear();
  }

  async snapshot(): Promise<LoginSnapshot> {
    const servers: LoginServer[] = [];
    for (const server of this.store.servers) {
      let reference = '';
      if ((await this.store.password(server.url)).length > 0) {
        reference = this.byServer.get(server.url) || PASSWORD_REF_PREFIX + util.generateRandomUUID(true);
        this.byServer.set(server.url, reference);
        this.refs.set(reference, server.url);
      }
      servers.push({ url: server.url, name: server.name, password: reference });
    }
    return {
      servers, language: this.store.language, theme: this.store.theme, version: APP_VERSION
    };
  }

  async password(value: string): Promise<string> {
    if (!value.startsWith(PASSWORD_REF_PREFIX)) {
      return value;
    }
    const url = this.refs.get(value);
    if (!url) {
      throw new Error('保存的密码引用已失效，请重新选择服务器');
    }
    return await this.store.password(url);
  }

  async save(url: string, password: string, name?: string): Promise<void> {
    const address = parseServerAddress(url);
    const existing = this.store.servers.find((server) => server.url === address.url);
    await this.store.save(address.url, await this.password(password), name === undefined ? existing?.name || '' : name);
  }
}
