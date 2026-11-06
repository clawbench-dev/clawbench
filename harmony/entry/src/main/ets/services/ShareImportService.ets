import { common, Want } from '@kit.AbilityKit';
import { util } from '@kit.ArkTS';
import { uniformTypeDescriptor } from '@kit.ArkData';
import { webview } from '@kit.ArkWeb';
import { fileIo as fs } from '@kit.CoreFileKit';
import { http } from '@kit.NetworkKit';
import { systemShare } from '@kit.ShareKit';
import { nativeCookieHeader } from '../models/NativeCookies';
import { NativeFailure } from '../models/NativeMessages';
import { parseServerAddress } from '../models/ServerAddress';
import { safeFileName, shareMime } from '../models/FileTransferPolicy';
import {
  MAX_IMPORT_FILES, MAX_IMPORT_FILE_BYTES, MAX_IMPORT_BATCH_BYTES, SHARE_IMPORT_DIRECTORY,
  ShareImportSummary, ShareImportDestination, ShareImportResult, validateSharedUri,
  sharedProjectFromCookies, validateUploadedSharePath
} from '../models/ShareImportPolicy';

interface ImportFile {
  path: string;
  name: string;
  mime: string;
  size: number;
}

class ImportBatch {
  id: string = util.generateRandomUUID(true);
  files: ImportFile[] = [];
  busy: boolean = true;
  totalBytes: number = 0;
}

interface UploadResponse {
  ok: boolean;
  path: string;
}

export class ShareImportService {
  private static instance?: ShareImportService;
  private context: common.UIAbilityContext;
  private cachePath: string;
  private batch?: ImportBatch;
  private generation: number = 0;
  private request?: http.HttpRequest;
  private changed?: (summary: ShareImportSummary | null) => void;

  static get(context: common.UIAbilityContext): ShareImportService {
    if (!ShareImportService.instance) {
      ShareImportService.instance = new ShareImportService(context);
    }
    return ShareImportService.instance;
  }

  private constructor(context: common.UIAbilityContext) {
    this.context = context;
    this.cachePath = context.cacheDir + '/clawbench-share-in';
    if (!fs.accessSync(this.cachePath)) {
      fs.mkdirSync(this.cachePath);
    }
    // No import survives process death: only the sender's new gesture can grant its URI again.
    try {
      fs.listFileSync(this.cachePath).forEach((name: string) => {
        if (/^import_[a-f0-9-]+_/i.test(name) && !/[\\/]/.test(name)) {
          this.remove(this.cachePath + '/' + name);
        }
      });
    } catch (_) {}
  }

  onChange(listener: (summary: ShareImportSummary | null) => void): void {
    this.changed = listener;
    listener(this.summary());
  }

  offChange(): void {
    this.changed = undefined;
  }

  summary(): ShareImportSummary | null {
    if (!this.batch) {
      return null;
    }
    return {
      id: this.batch.id, names: this.batch.files.map((file: ImportFile) => file.name),
      totalBytes: this.batch.totalBytes, busy: this.batch.busy
    };
  }

  async receive(want: Want): Promise<boolean> {
    if (want.action !== 'ohos.want.action.sendData') {
      return false;
    }
    if (this.batch?.busy) {
      throw new NativeFailure('share_import_busy');
    }
    this.cancel();
    const generation = this.generation;
    const batch = new ImportBatch();
    this.batch = batch;
    this.notify();
    try {
      const data = await systemShare.getSharedData(want);
      this.checkGeneration(generation);
      const records = data.getRecords();
      if (records.length < 1 || records.length > MAX_IMPORT_FILES) {
        throw new Error('Unsupported share size');
      }
      // Parse only system ShareKit records. Never accept arbitrary paths from want.parameters.
      const uris = records.map((record: systemShare.SharedRecord) =>
        validateSharedUri(record.uri || '', this.context.abilityInfo.bundleName));
      for (let index = 0; index < records.length; index++) {
        this.checkGeneration(generation);
        const file = await this.copySharedFile(uris[index], records[index], generation,
          MAX_IMPORT_BATCH_BYTES - batch.totalBytes);
        batch.files.push(file);
        this.checkGeneration(generation);
        batch.totalBytes += file.size;
      }
      batch.busy = false;
      this.notify();
      return true;
    } catch (_) {
      this.cleanup(batch);
      if (generation === this.generation) {
        this.batch = undefined;
        this.notify();
      }
      throw new NativeFailure('share_read_failed');
    }
  }

  destination(origin: string): ShareImportDestination {
    const server = parseServerAddress(origin).url;
    const cookie = nativeCookieHeader(server, webview.WebCookieManager.fetchCookieSync(server + '/api/upload/file'));
    return { server, projectPath: sharedProjectFromCookies(cookie) };
  }

  async uploadConfirmed(id: string, origin: string, expectedProjectPath: string): Promise<ShareImportResult> {
    const batch = this.batch;
    if (!batch || batch.id !== id || batch.busy || batch.files.length === 0) {
      throw new NativeFailure('share_changed');
    }
    const server = parseServerAddress(origin).url;
    const url = server + '/api/upload/file';
    const cookie = nativeCookieHeader(server, webview.WebCookieManager.fetchCookieSync(url));
    const projectPath = sharedProjectFromCookies(cookie);
    if (!expectedProjectPath || projectPath !== expectedProjectPath) {
      throw new NativeFailure('share_target_changed');
    }
    const generation = this.generation;
    const result: ShareImportResult = { paths: [], failed: 0, cancelled: false, projectPath };
    batch.busy = true;
    this.notify();
    try {
      for (let index = 0; index < batch.files.length; index++) {
        this.checkGeneration(generation);
        const file = batch.files[index];
        const request = http.createHttp();
        this.request = request;
        try {
          const response = await request.request(url, {
            method: http.RequestMethod.POST,
            header: { 'Content-Type': 'multipart/form-data', 'Cookie': cookie },
            multiFormDataList: [
              { name: 'file', contentType: file.mime, remoteFileName: file.name, filePath: file.path },
              { name: 'dir', contentType: 'text/plain', data: SHARE_IMPORT_DIRECTORY }
            ],
            connectTimeout: 15000, readTimeout: 120000, usingCache: false, maxRedirects: 0,
            maxLimit: 65536, expectDataType: http.HttpDataType.STRING
          });
          this.checkGeneration(generation);
          if (response.responseCode < 200 || response.responseCode >= 300) {
            throw new Error('Upload rejected');
          }
          const body = JSON.parse(String(response.result)) as UploadResponse;
          if (body.ok !== true) {
            throw new Error('Upload rejected');
          }
          result.paths.push(validateUploadedSharePath(body.path));
        } finally {
          try { request.destroy(); } catch (_) {}
          if (this.request === request) {
            this.request = undefined;
          }
        }
      }
    } catch (_) {
      result.failed = batch.files.length - result.paths.length;
      result.cancelled = generation !== this.generation;
    } finally {
      this.cleanup(batch);
      if (this.batch === batch) {
        this.batch = undefined;
        this.notify();
      }
    }
    return result;
  }

  cancel(expectedId?: string): void {
    if (expectedId !== undefined && this.batch?.id !== expectedId) {
      return;
    }
    this.generation++;
    try { this.request?.destroy(); } catch (_) {}
    this.request = undefined;
    if (this.batch) {
      this.cleanup(this.batch);
    }
    this.batch = undefined;
    this.notify();
  }

  private async copySharedFile(uri: string, record: systemShare.SharedRecord,
    generation: number, remaining: number): Promise<ImportFile> {
    const input = await fs.open(uri, fs.OpenMode.READ_ONLY);
    let output: fs.File | undefined;
    let path = '';
    let complete = false;
    try {
      this.checkGeneration(generation);
      const info = await fs.stat(input.fd);
      const limit = Math.min(MAX_IMPORT_FILE_BYTES, remaining);
      if (!info.isFile() || info.size > limit) {
        throw new Error('Shared file is too large');
      }
      let name = input.name;
      try { name = decodeURIComponent(name); } catch (_) {}
      name = safeFileName(name || record.title || 'shared_file');
      path = this.cachePath + '/import_' + util.generateRandomUUID(true) + '_' + name;
      output = await fs.open(path, fs.OpenMode.CREATE | fs.OpenMode.WRITE_ONLY | fs.OpenMode.TRUNC);
      this.checkGeneration(generation);
      const buffer = new ArrayBuffer(256 * 1024);
      let size = 0;
      while (true) {
        this.checkGeneration(generation);
        const count = await fs.read(input.fd, buffer);
        if (count === 0) {
          break;
        }
        size += count;
        if (size > limit) {
          throw new Error('Shared file is too large');
        }
        let offset = 0;
        while (offset < count) {
          this.checkGeneration(generation);
          const written = await fs.write(output.fd, buffer.slice(offset, count));
          if (written <= 0) {
            throw new Error('Incomplete file write');
          }
          offset += written;
        }
      }
      await fs.fsync(output.fd);
      this.checkGeneration(generation);
      let mime = 'application/octet-stream';
      try {
        const description = uniformTypeDescriptor.getTypeDescriptor(record.utd);
        if (description.mimeTypes.length > 0) {
          mime = shareMime(description.mimeTypes[0]);
        }
      } catch (_) {}
      complete = true;
      return { path, name, size, mime };
    } finally {
      try {
        if (output) {
          await fs.close(output);
        }
      } catch (error) {
        complete = false;
        throw new Error('Unable to close the staged import file');
      } finally {
        try {
          await fs.close(input);
        } catch (error) {
          complete = false;
          throw new Error('Unable to close the shared source file');
        } finally {
          if (!complete && path) {
            this.remove(path);
          }
        }
      }
    }
  }

  private checkGeneration(generation: number): void {
    if (generation !== this.generation) {
      throw new Error('Share import cancelled');
    }
  }

  private notify(): void {
    this.changed?.(this.summary());
  }

  private cleanup(batch: ImportBatch): void {
    batch.files.forEach((file: ImportFile) => this.remove(file.path));
  }

  private remove(path: string): void {
    try { fs.unlinkSync(path); } catch (_) {}
  }
}
