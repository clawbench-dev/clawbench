import { common } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { asset } from '@kit.AssetStoreKit';
import { util } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { parseServerAddress } from '../models/ServerAddress';
import { NativeFailure } from '../models/NativeMessages';

export interface SavedServer {
  url: string;
  name: string;
}

export class ServerStore {
  private prefs?: preferences.Preferences;
  servers: SavedServer[] = [];
  lastUrl: string = '';
  language: string = 'zh';
  theme: string = 'gruvbox-dark';

  async init(context: common.UIAbilityContext): Promise<void> {
    this.prefs = await preferences.getPreferences(context, { name: 'clawbench' });
    this.servers = JSON.parse(String(await this.prefs.get('servers', '[]'))) as SavedServer[];
    this.lastUrl = String(await this.prefs.get('lastUrl', ''));
    this.language = String(await this.prefs.get('language', 'zh'));
    this.theme = String(await this.prefs.get('theme', 'gruvbox-dark'));
  }

  private bytes(value: string): Uint8Array {
    return new util.TextEncoder().encodeInto(value);
  }

  private alias(url: string): Uint8Array {
    return this.bytes('clawbench:password:' + parseServerAddress(url).url);
  }

  async password(url: string): Promise<string> {
    const query: asset.AssetMap = new Map();
    query.set(asset.Tag.ALIAS, this.alias(url));
    query.set(asset.Tag.RETURN_TYPE, asset.ReturnType.ALL);
    try {
      const results = await asset.query(query);
      if (results.length === 0) {
        return '';
      }
      return new util.TextDecoder().decodeWithStream(results[0].get(asset.Tag.SECRET) as Uint8Array);
    } catch (error) {
      if ((error as BusinessError).code === 24000002) {
        return '';
      }
      throw new NativeFailure('credential_read_failed', this.language);
    }
  }

  async save(url: string, password: string, name: string): Promise<void> {
    const normalized = parseServerAddress(url).url;
    const attributes: asset.AssetMap = new Map();
    attributes.set(asset.Tag.ALIAS, this.alias(normalized));
    attributes.set(asset.Tag.SECRET, this.bytes(password));
    attributes.set(asset.Tag.ACCESSIBILITY, asset.Accessibility.DEVICE_FIRST_UNLOCKED);
    attributes.set(asset.Tag.CONFLICT_RESOLUTION, asset.ConflictResolution.OVERWRITE);
    if (password.length > 0) {
      try {
        await asset.add(attributes);
      } catch (_) {
        throw new NativeFailure('credential_save_failed', this.language);
      }
    } else {
      await this.forgetPassword(normalized);
    }
    await this.saveMetadata(normalized, name);
  }

  async saveMetadata(url: string, name: string): Promise<void> {
    const normalized = parseServerAddress(url).url;
    this.servers = [{ url: normalized, name: name.trim().slice(0, 80) },
      ...this.servers.filter((server: SavedServer) => server.url !== normalized)];
    await this.persist();
  }

  async forgetPassword(url: string): Promise<void> {
    const query: asset.AssetMap = new Map();
    query.set(asset.Tag.ALIAS, this.alias(url));
    try {
      await asset.remove(query);
    } catch (error) {
      if ((error as BusinessError).code !== 24000002) {
        throw new NativeFailure('credential_remove_failed', this.language);
      }
    }
  }

  async remove(url: string, ignoreCredentials: boolean = false): Promise<void> {
    if (!ignoreCredentials) {
      await this.forgetPassword(url);
    }
    this.servers = this.servers.filter((server: SavedServer) => server.url !== url);
    if (this.lastUrl === url) {
      this.lastUrl = '';
    }
    await this.persist();
  }

  async setActive(url: string): Promise<void> {
    this.lastUrl = parseServerAddress(url).url;
    await this.persist();
  }

  async setAppearance(language: string, theme: string): Promise<void> {
    this.language = language === 'en' ? 'en' : 'zh';
    this.theme = theme.slice(0, 80);
    await this.persist();
  }

  private async persist(): Promise<void> {
    if (!this.prefs) {
      throw new NativeFailure('store_not_ready', this.language);
    }
    await this.prefs.put('servers', JSON.stringify(this.servers));
    await this.prefs.put('lastUrl', this.lastUrl);
    await this.prefs.put('language', this.language);
    await this.prefs.put('theme', this.theme);
    await this.prefs.flush();
  }
}
