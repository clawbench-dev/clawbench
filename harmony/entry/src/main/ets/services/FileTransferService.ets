import { common } from '@kit.AbilityKit';
import { util } from '@kit.ArkTS';
import { webview } from '@kit.ArkWeb';
import { fileIo as fs, fileUri, picker } from '@kit.CoreFileKit';
import { uniformTypeDescriptor } from '@kit.ArkData';
import { http } from '@kit.NetworkKit';
import { systemShare } from '@kit.ShareKit';
import { parseServerAddress } from '../models/ServerAddress';
import { nativeCookieHeader } from '../models/NativeCookies';
import { NativeFailure, nativeMessage } from '../models/NativeMessages';
import {
  MAX_DOWNLOAD_BYTES, MAX_SHARE_BYTES, MAX_SHARED_CACHE_BYTES, MAX_SHARE_FILES, MAX_SHARE_TEXT_LENGTH,
  TransferProgress, TransferProgressState, projectDownloadUrl, resolveDownloadAddress,
  safeFileName, shareMime, validateBase64
} from '../models/FileTransferPolicy';

const MAX_QUEUED_BYTES: number = 4 * 1024 * 1024;
const CACHE_LIFETIME_MS: number = 24 * 60 * 60 * 1000;

class TransferJob {
  state: TransferProgressState;
  epoch: number;
  request?: http.HttpRequest;
  interrupt?: () => void;
  failure: string = '';
  lastProgress: number = 0;

  constructor(id: number, epoch: number, limit: number) {
    this.state = new TransferProgressState(id, limit);
    this.epoch = epoch;
  }
}

export class FileTransferService {
  private context: common.UIAbilityContext;
  private origin: string;
  private emit: (progress: TransferProgress) => void;
  private onLegacyFailure: () => void;
  private jobs: Map<number, TransferJob> = new Map();
  private epoch: number = 0;
  private internalId: number = -1;
  private disposed: boolean = false;
  private pickerBusy: boolean = false;
  private reservedShareBytes: number = 0;
  private cachePath: string;
  private language: string;

  constructor(context: common.UIAbilityContext, origin: string, emit: (progress: TransferProgress) => void,
    onLegacyFailure: () => void = () => {}, language: string = 'zh') {
    this.context = context;
    this.origin = parseServerAddress(origin).url;
    this.emit = emit;
    this.onLegacyFailure = onLegacyFailure;
    this.language = language;
    this.cachePath = context.cacheDir + '/clawbench-transfers';
    if (!fs.accessSync(this.cachePath)) {
      fs.mkdirSync(this.cachePath);
    }
    this.pruneCache();
  }

  setLanguage(language: string): void {
    this.language = language === 'en' ? 'en' : 'zh';
  }

  startDownload(path: string, fileName: string, id: number): void {
    if (id < 0) {
      throw new Error('Invalid download id');
    }
    this.enqueueDownload(projectDownloadUrl(this.origin, path), safeFileName(fileName || path), id);
  }

  downloadFile(path: string): void {
    this.enqueueDownload(projectDownloadUrl(this.origin, path), safeFileName(path), this.internalId--, true);
  }

  downloadUrl(url: string, fileName: string): void {
    const address = resolveDownloadAddress(this.origin, url);
    const path = address.url.split(/[?#]/)[0];
    this.enqueueDownload(address.url, safeFileName(fileName || path), this.internalId--, true);
  }

  private enqueueDownload(url: string, name: string, id: number, legacy: boolean = false): void {
    const address = resolveDownloadAddress(this.origin, url);
    // Snapshot the project-scoped cookies before a picker or a project switch can change them.
    const cookie = address.authenticated ?
      nativeCookieHeader(this.origin, webview.WebCookieManager.fetchCookieSync(address.url)) : '';
    const job = this.createJob(id, MAX_DOWNLOAD_BYTES);
    this.progress(job, true);
    this.saveDownload(job, address.url, cookie, name, legacy).catch(() => {});
  }

  cancelDownload(id: number): void {
    const job = this.jobs.get(id);
    if (job) {
      this.cancel(job, true);
    }
  }

  dispose(): void {
    this.jobs.forEach((job: TransferJob) => this.cancel(job, false));
    this.disposed = true;
    this.epoch++;
    this.jobs.clear();
  }

  async downloadBlob(base64: string, fileName: string): Promise<void> {
    validateBase64(base64);
    const name = safeFileName(fileName);
    const job = this.createJob(this.internalId--, MAX_DOWNLOAD_BYTES);
    const temp = this.temporaryPath(name);
    try {
      const destination = await this.selectDestination(job, name);
      const bytes = new util.Base64Helper().decodeSync(base64);
      const file = await fs.open(temp, fs.OpenMode.CREATE | fs.OpenMode.WRITE_ONLY | fs.OpenMode.TRUNC);
      try {
        await this.writeAll(job, file.fd, bytes.buffer as ArrayBuffer);
      } finally {
        await fs.close(file);
      }
      job.state.receive(bytes.byteLength);
      await this.copyToDestination(job, temp, destination);
      this.complete(job);
    } catch (_) {
      this.fail(job);
      if (!job.state.cancelled) {
        throw new NativeFailure('file_save_failed', this.language);
      }
    } finally {
      this.removeTemporary(temp);
      this.release(job);
    }
  }

  async shareText(text: string): Promise<void> {
    if (this.disposed || typeof text !== 'string' || !text || text.length > MAX_SHARE_TEXT_LENGTH) {
      throw new NativeFailure('share_text_invalid', this.language);
    }
    const data = new systemShare.SharedData({
      utd: uniformTypeDescriptor.UniformDataType.PLAIN_TEXT, content: text
    });
    await new systemShare.ShareController(data).show(this.context, {
      previewMode: systemShare.SharePreviewMode.DEFAULT,
      selectionMode: systemShare.SelectionMode.SINGLE
    });
  }

  async shareFile(path: string, mime: string): Promise<void> {
    await this.shareFiles(JSON.stringify([path]), JSON.stringify([mime]));
  }

  async shareFiles(pathsJson: string, mimesJson: string): Promise<void> {
    const paths = JSON.parse(pathsJson) as string[];
    const mimes = JSON.parse(mimesJson) as string[];
    if (!Array.isArray(paths) || !Array.isArray(mimes) || paths.length < 1 ||
        paths.length > MAX_SHARE_FILES || paths.length !== mimes.length ||
        !paths.every((path: string) => typeof path === 'string') ||
        !mimes.every((mime: string) => typeof mime === 'string')) {
      throw new Error('Invalid share file list');
    }
    const urls = paths.map((path: string) => projectDownloadUrl(this.origin, path));
    const cookies = urls.map((url: string) =>
      nativeCookieHeader(this.origin, webview.WebCookieManager.fetchCookieSync(url)));
    const budget = this.reserveShareBudget();
    let job: TransferJob;
    try {
      job = this.createJob(this.internalId--, budget);
    } catch (_) {
      this.reservedShareBytes -= budget;
      throw new NativeFailure('file_busy', this.language);
    }
    const temporary: string[] = [];
    let handedOver = false;
    try {
      const records: systemShare.SharedRecord[] = [];
      for (let index = 0; index < paths.length; index++) {
        this.assertActive(job);
        const name = safeFileName(paths[index]);
        const temp = this.temporaryPath(name);
        temporary.push(temp);
        await this.stream(job, urls[index], cookies[index], temp);
        this.assertActive(job);
        let utd: string = uniformTypeDescriptor.UniformDataType.FILE;
        try {
          utd = uniformTypeDescriptor.getUniformDataTypeByMIMEType(
            shareMime(mimes[index]), uniformTypeDescriptor.UniformDataType.FILE
          ) || uniformTypeDescriptor.UniformDataType.FILE;
        } catch (_) {}
        records.push({ utd, uri: fileUri.getUriFromPath(temp), title: name });
      }
      const data = new systemShare.SharedData(records[0]);
      for (let index = 1; index < records.length; index++) {
        data.addRecord(records[index]);
      }
      this.assertActive(job);
      await new systemShare.ShareController(data).show(this.context, {
        previewMode: systemShare.SharePreviewMode.DEFAULT,
        selectionMode: systemShare.SelectionMode.BATCH
      });
      // A receiving app may read its granted URI after the share panel disappears.
      handedOver = true;
      this.complete(job);
    } catch (_) {
      this.fail(job);
      if (!job.state.cancelled) {
        throw new NativeFailure('file_share_failed', this.language);
      }
    } finally {
      this.reservedShareBytes -= budget;
      if (!handedOver) {
        temporary.forEach((path: string) => this.removeTemporary(path));
      }
      this.release(job);
    }
  }

  private createJob(id: number, limit: number): TransferJob {
    if (this.disposed || this.jobs.has(id) || this.jobs.size >= 3) {
      throw new NativeFailure('file_busy', this.language);
    }
    const job = new TransferJob(id, this.epoch, limit);
    this.jobs.set(id, job);
    return job;
  }

  private async saveDownload(job: TransferJob, url: string, cookie: string, name: string,
    legacy: boolean): Promise<void> {
    const temp = this.temporaryPath(name);
    try {
      const destination = await this.selectDestination(job, name);
      await this.stream(job, url, cookie, temp);
      await this.copyToDestination(job, temp, destination);
      this.complete(job);
    } catch (_) {
      this.fail(job);
      if (legacy && job.state.error && !job.state.cancelled &&
          !this.disposed && job.epoch === this.epoch) {
        this.onLegacyFailure();
      }
    } finally {
      this.removeTemporary(temp);
      this.release(job);
    }
  }

  private async selectDestination(job: TransferJob, name: string): Promise<string> {
    this.assertActive(job);
    if (this.pickerBusy) {
      throw new Error('A save dialog is already open');
    }
    this.pickerBusy = true;
    try {
      const options = new picker.DocumentSaveOptions();
      options.newFileNames = [name];
      const choices = await new picker.DocumentViewPicker(this.context).save(options);
      this.assertActive(job);
      if (choices.length !== 1) {
        this.cancel(job, true);
        throw new Error('Cancelled');
      }
      return choices[0];
    } finally {
      this.pickerBusy = false;
    }
  }

  private async stream(job: TransferJob, url: string, cookie: string, path: string): Promise<void> {
    this.assertActive(job);
    const file = await fs.open(path, fs.OpenMode.CREATE | fs.OpenMode.WRITE_ONLY | fs.OpenMode.TRUNC);
    let writes: Promise<void> = Promise.resolve();
    let queuedBytes = 0;
    let request: http.HttpRequest | undefined;
    try {
      this.assertActive(job);
      const activeRequest = http.createHttp();
      request = activeRequest;
      job.request = activeRequest;
      const code = await new Promise<number>((resolve, reject) => {
        let status = -1;
        let ended = false;
        job.interrupt = () => reject(new Error('Transfer interrupted'));
        const fail = (message: string): void => {
          job.failure = message;
          reject(new Error(message));
          try { activeRequest.destroy(); } catch (_) {}
        };
        activeRequest.on('headersReceive', (headers: Object) => {
          try {
            const values = headers as Record<string, Object>;
            const key = Object.keys(values).find((name: string) => name.toLowerCase() === 'content-length');
            if (key) {
              const length = Number(String(values[key]));
              job.state.setTotal(job.state.received + length);
            }
          } catch (_) {
            fail(nativeMessage('file_too_large', this.language));
          }
        });
        activeRequest.on('dataReceive', (buffer: ArrayBuffer) => {
          if (!this.active(job) || job.failure) {
            return;
          }
          try {
            job.state.receive(buffer.byteLength);
            queuedBytes += buffer.byteLength;
            if (queuedBytes > MAX_QUEUED_BYTES) {
              fail(nativeMessage('storage_slow', this.language));
              return;
            }
          } catch (_) {
            fail(nativeMessage('file_too_large', this.language));
            return;
          }
          // Bound queued buffers while native async writes run off the UI thread.
          writes = writes.then(async () => {
            this.assertActive(job);
            if (job.failure) {
              return;
            }
            await this.writeAll(job, file.fd, buffer);
          }).catch(() => {
            if (this.active(job)) {
              fail(nativeMessage('file_write_failed', this.language));
            }
          }).finally(() => {
            queuedBytes -= buffer.byteLength;
          });
          this.progress(job);
        });
        activeRequest.on('dataEnd', () => {
          ended = true;
          if (status >= 0) {
            resolve(status);
          }
        });
        const headers: Record<string, string> = { 'Accept-Encoding': 'identity' };
        if (cookie) {
          headers['Cookie'] = cookie;
        }
        activeRequest.requestInStream(url, {
          method: http.RequestMethod.GET, header: headers, usingCache: false,
          connectTimeout: 15000, readTimeout: 60000, maxRedirects: 0
        }).then((response: number) => {
          status = response;
          if (status < 200 || status >= 300) {
            fail(nativeMessage('download_rejected', this.language));
          } else if (ended) {
            resolve(status);
          }
        }).catch(() => reject(new Error('Download request failed')));
      });
      await writes;
      this.assertActive(job);
      if (job.failure || code < 200 || code >= 300) {
        throw new Error('Download failed');
      }
      await fs.fsync(file.fd);
    } finally {
      job.interrupt = undefined;
      job.request = undefined;
      try { request?.destroy(); } catch (_) {}
      await writes;
      await fs.close(file);
    }
  }

  private async writeAll(job: TransferJob, fd: number, buffer: ArrayBuffer): Promise<void> {
    let offset = 0;
    while (offset < buffer.byteLength) {
      this.assertActive(job);
      const written = await fs.write(fd, buffer.slice(offset));
      if (written <= 0) {
        throw new Error('Incomplete file write');
      }
      offset += written;
    }
  }

  private async copyToDestination(job: TransferJob, source: string, destination: string): Promise<void> {
    this.assertActive(job);
    const input = await fs.open(source, fs.OpenMode.READ_ONLY);
    let output: fs.File | undefined;
    try {
      this.assertActive(job);
      output = await fs.open(destination, fs.OpenMode.WRITE_ONLY | fs.OpenMode.TRUNC);
      const buffer = new ArrayBuffer(256 * 1024);
      while (true) {
        this.assertActive(job);
        const count = await fs.read(input.fd, buffer);
        if (count === 0) {
          break;
        }
        await this.writeAll(job, output.fd, buffer.slice(0, count));
      }
      await fs.fsync(output.fd);
      this.assertActive(job);
    } finally {
      try {
        if (output) {
          await fs.close(output);
        }
      } finally {
        await fs.close(input);
      }
    }
  }

  private active(job: TransferJob): boolean {
    return !this.disposed && job.epoch === this.epoch && !job.state.done;
  }

  private assertActive(job: TransferJob): void {
    if (!this.active(job)) {
      throw new Error('Transfer cancelled');
    }
  }

  private cancel(job: TransferJob, notify: boolean): void {
    if (job.state.finish('', true) && notify) {
      this.progress(job, true);
    }
    job.interrupt?.();
    try { job.request?.destroy(); } catch (_) {}
  }

  private complete(job: TransferJob): void {
    this.assertActive(job);
    if (job.state.finish()) {
      this.progress(job, true);
    }
  }

  private fail(job: TransferJob): void {
    if (job.state.finish(job.failure || nativeMessage('file_operation_failed', this.language))) {
      this.progress(job, true);
    }
  }

  private progress(job: TransferJob, force: boolean = false): void {
    if (this.disposed || job.epoch !== this.epoch) {
      return;
    }
    const now = Date.now();
    if (force || now - job.lastProgress >= 200) {
      job.lastProgress = now;
      this.emit(job.state.snapshot());
    }
  }

  private release(job: TransferJob): void {
    if (this.jobs.get(job.state.id) === job) {
      this.jobs.delete(job.state.id);
    }
  }

  private temporaryPath(name: string): string {
    return this.cachePath + '/' + util.generateRandomUUID(true) + '_' + name;
  }

  private reserveShareBudget(): number {
    this.pruneCache();
    let used = 0;
    fs.listFileSync(this.cachePath).forEach((name: string) => {
      if (/[\\/]/.test(name)) {
        return;
      }
      const info = fs.statSync(this.cachePath + '/' + name);
      if (info.isFile()) {
        used += Math.max(0, info.size);
      }
    });
    const available = MAX_SHARED_CACHE_BYTES - used - this.reservedShareBytes;
    if (available <= 0) {
      throw new NativeFailure('share_cache_full', this.language);
    }
    const budget = Math.min(MAX_SHARE_BYTES, available);
    this.reservedShareBytes += budget;
    return budget;
  }

  private removeTemporary(path: string): void {
    try {
      fs.unlinkSync(path);
    } catch (_) {}
  }

  private pruneCache(): void {
    try {
      const now = Date.now();
      const names = fs.listFileSync(this.cachePath);
      names.forEach((name: string) => {
        if (!/^[a-f0-9-]+_/i.test(name) || /[\\/]/.test(name)) {
          return;
        }
        const path = this.cachePath + '/' + name;
        try {
          const info = fs.statSync(path);
          if (info.isFile() && now - info.mtime * 1000 > CACHE_LIFETIME_MS) {
            fs.unlinkSync(path);
          }
        } catch (_) {}
      });
    } catch (_) {}
  }
}
