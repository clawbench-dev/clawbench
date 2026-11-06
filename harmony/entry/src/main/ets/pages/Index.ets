import { common } from '@kit.AbilityKit';
import { webview } from '@kit.ArkWeb';
import { util } from '@kit.ArkTS';
import { window, KeyboardAvoidMode } from '@kit.ArkUI';
import { KeyCode } from '@kit.InputKit';
import { ServerStore } from '../services/ServerStore';
import { ConnectionService, ConnectionFailure } from '../services/ConnectionService';
import { LoginSession, LoginInitialization, LoginCredential } from '../services/LoginSession';
import { APP_VERSION, isLocalLoginPage } from '../models/AppInfo';
import { isTrustedAppUrl, parseServerAddress } from '../models/ServerAddress';
import { normalizeBarColor, barContentColor } from '../models/SystemBars';
import { SafeInsets, mergeSafeInsets } from '../models/SafeInsets';
import { externalHttpUrl, portBrowserUrl, selectionText } from '../models/NativeActions';
import { clearServerCookies } from '../models/NativeCookies';
import { NativeFailure, nativeMessage, formatNativeError } from '../models/NativeMessages';
import { navigationEventName, NativeNavigation } from '../models/NativeEvents';
import { FileTransferService } from '../services/FileTransferService';
import { NativeEventService } from '../services/NativeEventService';
import { NativeLogService } from '../services/NativeLogService';
import { ClipboardService } from '../services/ClipboardService';
import { MediaPermissionService } from '../services/MediaPermissionService';
import { ScreenCaptureService } from '../services/ScreenCaptureService';
import { FilePickerService } from '../services/FilePickerService';
import { ShareImportService } from '../services/ShareImportService';
import { TunnelService } from '../services/TunnelService';

interface BridgeRequest {
  id: number;
  method: string;
  args: string[];
}

@Entry
@Component
struct Index {
  @State private initialized: boolean = false;
  @State private connecting: boolean = false;
  @State private showWeb: boolean = false;
  @State private pageReady: boolean = false;
  @State private fatalError: string = '';
  @State private activeUrl: string = '';
  @State private bootstrapScript: string = '';
  @State private safeInsets: SafeInsets = { top: 0, bottom: 0, left: 0, right: 0 };
  @State private surfaceColor: string = '#17191D';
  @StorageLink('clawbenchForeground') @Watch('foregroundChanged') private foreground: boolean = true;
  @StorageLink('clawbenchNotificationToken') @Watch('notificationChanged') private notificationToken: string = '';
  @StorageLink('clawbenchShareError') @Watch('shareErrorChanged') private shareError: boolean = false;
  private store: ServerStore = new ServerStore();
  private login: LoginSession = new LoginSession(this.store);
  private loginInitialization: LoginInitialization = new LoginInitialization();
  private connection: ConnectionService = new ConnectionService();
  private controller: webview.WebviewController = new webview.WebviewController();
  private loginController: webview.WebviewController = new webview.WebviewController();
  private ports: webview.WebMessagePort[] = [];
  private loginPorts: webview.WebMessagePort[] = [];
  private loginQueue: Promise<void> = Promise.resolve();
  private pageTimeout: number = -1;
  private connectionId: number = 0;
  private loginEpoch: number = 0;
  private channelId: string = '';
  private loginChannel: string = '';
  private loginReady: boolean = false;
  private pendingLoginError: string = '';
  private pendingLoginCode: string = '';
  private loginTargetUrl: string = '';
  private ignoreSavedCredentials: boolean = false;
  private credentialQueue: Promise<void> = Promise.resolve();
  private backInFlight: boolean = false;
  private lastBack: number = 0;
  private mainWindow?: window.Window;
  private updateWindowInsets = (): void => this.updateSafeInsets();
  private files?: FileTransferService;
  private notifications: NativeEventService = new NativeEventService(
    (name: string, detail: string) => this.emitNativeEvent(name, detail));
  private logger?: NativeLogService;
  private clipboard?: ClipboardService;
  private media?: MediaPermissionService;
  private capture?: ScreenCaptureService;
  private filePicker?: FilePickerService;
  private nativeReady: boolean = false;
  private rendererReady: boolean = false;
  private documentId: number = 0;
  private activePassword: string = '';
  private volumeKeyMode: boolean = false;
  private notificationEpoch: number = 0;
  private shareImport?: ShareImportService;
  private shareDialog: boolean = false;
  private tunnels?: TunnelService;

  aboutToAppear(): void {
    this.getUIContext().setKeyboardAvoidMode(KeyboardAvoidMode.RESIZE);
    this.initialize();
  }

  aboutToDisappear(): void {
    this.connectionId++;
    this.loginEpoch++;
    this.connection.cancel();
    this.closePorts(this.ports);
    this.closePorts(this.loginPorts);
    this.login.clear();
    clearTimeout(this.pageTimeout);
    this.keepScreenOn(false);
    this.disposePageServices();
    this.notifications.destroy();
    this.tunnels?.shutdown();
    this.activePassword = '';
    this.notificationEpoch++;
    this.shareImport?.offChange();
    this.shareImport?.cancel();
    try {
      this.mainWindow?.off('avoidAreaChange', this.updateWindowInsets);
      this.mainWindow?.off('windowSizeChange', this.updateWindowInsets);
    } catch (_) {}
  }

  private context(): common.UIAbilityContext {
    return this.getUIContext().getHostContext() as common.UIAbilityContext;
  }

  private async initialize(): Promise<void> {
    try {
      this.mainWindow = await window.getLastWindow(this.context());
      this.mainWindow.on('avoidAreaChange', this.updateWindowInsets);
      this.mainWindow.on('windowSizeChange', this.updateWindowInsets);
      this.updateSafeInsets();
      await this.store.init(this.context());
      this.clipboard = new ClipboardService(this.context());
      this.media = new MediaPermissionService(this.context());
      this.filePicker = new FilePickerService(this.context());
      this.shareImport = ShareImportService.get(this.context());
      this.shareImport.onChange(() => this.handleShareImport());
      this.capture = new ScreenCaptureService(async (origin: string): Promise<boolean> => {
        const en = this.store.language === 'en';
        const answer = await this.getUIContext().getPromptAction().showDialog({
          title: en ? 'Share screen' : '共享屏幕',
          message: origin + (en ? '\nThis site will see your screen while sharing is active.' :
            '\n共享期间，此服务器可以看到屏幕上的内容。'),
          buttons: [{ text: en ? 'Cancel' : '取消', color: '#666666' },
            { text: en ? 'Share' : '共享', color: '#2676D9' }]
        });
        return answer.index === 1;
      });
      await this.notifications.init(this.context());
      this.notifications.setForeground(this.foreground);
      this.tunnels = new TunnelService(this.context(), async (origin: string): Promise<string> => {
        if (origin !== this.activeUrl || !this.showWeb) {
          throw new Error('Tunnel server changed');
        }
        return this.activePassword;
      }, async (origin: string, port: number, fingerprint: string, previous: string): Promise<boolean> => {
        const connection = this.connectionId;
        if (!this.foreground || !this.showWeb || origin !== this.activeUrl) {
          return false;
        }
        const en = this.store.language === 'en';
        const result = await this.getUIContext().getPromptAction().showDialog({
          title: previous ? (en ? 'SSH host key changed' : 'SSH 主机密钥已变化') :
            (en ? 'Confirm SSH host key' : '确认 SSH 主机密钥'),
          message: origin + '\nSSH: ' + port + '\n\nSHA256 (hex): ' + fingerprint +
            (previous ? '\n\n' + (en ? 'Previously trusted: ' : '上次信任：') + previous : '') +
            '\n\n' + (en ? 'Compare this fingerprint with the server before trusting it.' :
              '请与服务器核对指纹后再信任。'),
          buttons: [{ text: en ? 'Cancel' : '取消', color: '#666666' },
            { text: en ? 'Trust' : '信任', color: '#2676D9' }]
        });
        return result.index === 1 && connection === this.connectionId && this.foreground &&
          this.showWeb && origin === this.activeUrl;
      }, (requested: number, actual: number): void => {
        this.emitNativeEvent('clawbench-port-rebound', JSON.stringify({ requested, actual }));
      });
      await this.tunnels.init();
      this.tunnels.setForeground(this.foreground);
      this.nativeReady = true;
      await this.showConnection();
      await this.notificationChanged();
      this.shareErrorChanged();
    } catch (_) {
      this.fatalError = nativeMessage('config_load_failed', this.store.language);
    }
  }

  private disposePageServices(): void {
    this.documentId++;
    this.rendererReady = false;
    this.volumeKeyMode = false;
    this.files?.dispose();
    this.files = undefined;
    this.logger?.dispose();
    this.logger = undefined;
    this.media?.cancel();
    this.capture?.cancel();
    this.filePicker?.cancel();
  }

  private emitNativeEvent(name: string, detail: string): void {
    try {
      if (!this.showWeb || !isTrustedAppUrl(this.controller.getUrl(), this.activeUrl)) {
        return;
      }
      this.ports[0]?.postMessageEvent(JSON.stringify({ event: name, detail: JSON.parse(detail) }));
    } catch (_) {}
  }

  private async notificationChanged(): Promise<void> {
    if (!this.nativeReady || !this.notificationToken) {
      return;
    }
    const epoch = ++this.notificationEpoch;
    let connection = this.connectionId;
    let loginEpoch = this.loginEpoch;
    const current = (): boolean => epoch === this.notificationEpoch &&
      connection === this.connectionId && loginEpoch === this.loginEpoch;
    const launch = await this.notifications.takeLaunch(this.store.servers.map((server) => server.url));
    if (!launch || !current()) {
      return;
    }
    if (this.showWeb && launch.origin === this.activeUrl) {
      this.notifications.queueNavigation(launch);
      if (this.rendererReady) {
        this.deliverPendingNavigation();
      }
      return;
    }
    try {
      const password = this.ignoreSavedCredentials ? '' : await this.store.password(launch.origin);
      if (!current()) {
        return;
      }
      const switching = this.showConnection('', '', launch.origin);
      connection = this.connectionId;
      loginEpoch = this.loginEpoch;
      await switching;
      if (!current() || this.showWeb) {
        return;
      }
      this.notifications.queueNavigation(launch);
      if (!password) {
        this.pendingLoginError = nativeMessage('login_required_for_notification', this.store.language);
        this.pendingLoginCode = 'password';
        this.reportLoginError();
        return;
      }
      const connecting = this.connect(launch.origin, password);
      connection = this.connectionId;
      await connecting;
    } catch (_) {
      if (current()) {
        await this.showConnection(nativeMessage('login_required_for_notification', this.store.language),
          'password', launch.origin);
      }
    }
  }

  private shareErrorChanged(): void {
    if (this.nativeReady && this.shareError) {
      this.shareError = false;
      this.getUIContext().getPromptAction().showToast({
        message: this.store.language === 'en' ? 'Unable to read the shared files. Please share again.' :
          '无法读取分享文件，请重新分享'
      });
    }
  }

  private async handleShareImport(): Promise<void> {
    const summary = this.shareImport?.summary();
    if (!summary || summary.busy || this.shareDialog || !this.rendererReady || !this.foreground) {
      return;
    }
    const origin = this.activeUrl;
    const document = this.documentId;
    const en = this.store.language === 'en';
    this.shareDialog = true;
    try {
      const destination = this.shareImport!.destination(origin);
      const answer = await this.getUIContext().getPromptAction().showDialog({
        title: en ? 'Import shared files' : '导入分享文件',
        message: destination.server + '\n' + destination.projectPath + '\n\n' + summary.names.join('\n'),
        buttons: [{ text: en ? 'Cancel' : '取消', color: '#666666' },
          { text: en ? 'Import' : '导入', color: '#2676D9' }]
      });
      if (answer.index !== 1 || document !== this.documentId || origin !== this.activeUrl) {
        this.shareImport?.cancel(summary.id);
        return;
      }
      const result = await this.shareImport!.uploadConfirmed(summary.id, origin, destination.projectPath);
      if (document === this.documentId && origin === this.activeUrl) {
        this.emitNativeEvent('clawbench-share-imported', JSON.stringify(result));
      }
    } catch (_) {
      if (document === this.documentId && this.shareImport?.summary()?.id === summary.id) {
        this.getUIContext().getPromptAction().showToast({
          message: en ? 'Import failed. Select a project and share again.' : '导入失败，请选择项目后重新分享'
        });
      }
      this.shareImport?.cancel(summary.id);
    } finally {
      this.shareDialog = false;
      const next = this.shareImport?.summary();
      if (next && next.id !== summary.id) {
        this.handleShareImport();
      }
    }
  }

  private deliverPendingNavigation(): void {
    const pending = this.notifications.takePendingNavigation();
    if (pending) {
      const navigation = JSON.parse(pending) as NativeNavigation;
      this.emitNativeEvent(navigationEventName(navigation), pending);
    }
  }

  private updateSafeInsets(): void {
    if (!this.mainWindow) {
      return;
    }
    try {
      const types = [window.AvoidAreaType.TYPE_SYSTEM, window.AvoidAreaType.TYPE_CUTOUT,
        window.AvoidAreaType.TYPE_NAVIGATION_INDICATOR];
      const areas = types.map((type: window.AvoidAreaType): SafeInsets => {
        const area = this.mainWindow!.getWindowAvoidArea(type);
        const ui = this.getUIContext();
        return {
          top: ui.px2vp(area.topRect.height), bottom: ui.px2vp(area.bottomRect.height),
          left: ui.px2vp(area.leftRect.width), right: ui.px2vp(area.rightRect.width)
        };
      });
      const keyboard = this.mainWindow.getWindowAvoidArea(window.AvoidAreaType.TYPE_KEYBOARD);
      this.safeInsets = mergeSafeInsets(areas, keyboard.bottomRect.height > 0);
    } catch (_) {}
  }

  private closePorts(ports: webview.WebMessagePort[]): void {
    ports.forEach((port: webview.WebMessagePort) => {
      try { port.close(); } catch (_) {}
    });
    ports.length = 0;
  }

  private async showConnection(message: string = '', code: string = '', targetUrl: string = ''): Promise<void> {
    if (this.showWeb) {
      this.shareImport?.cancel();
    }
    this.connectionId++;
    this.loginTargetUrl = targetUrl ? parseServerAddress(targetUrl).url :
      this.loginTargetUrl || this.activeUrl || this.store.lastUrl;
    const epoch = ++this.loginEpoch;
    this.connection.cancel();
    this.closePorts(this.ports);
    this.closePorts(this.loginPorts);
    this.login.clear();
    clearTimeout(this.pageTimeout);
    this.connecting = false;
    this.pageReady = false;
    this.loginReady = false;
    this.initialized = false;
    this.showWeb = false;
    this.fatalError = '';
    this.pendingLoginError = message;
    this.pendingLoginCode = code;
    this.keepScreenOn(false);
    this.disposePageServices();
    this.notifications.stop();
    this.tunnels?.setServer('');
    this.activePassword = '';
    try {
      // Asset writes cannot be cancelled. Read the retry form after any active write settles.
      await this.credentialQueue.catch(() => {});
      if (epoch !== this.loginEpoch) {
        return;
      }
      const snapshot = await this.login.snapshot(this.loginTargetUrl, this.ignoreSavedCredentials);
      if (epoch !== this.loginEpoch) {
        return;
      }
      this.loginChannel = util.generateRandomUUID(true);
      this.loginTargetUrl = snapshot.activeUrl || this.loginTargetUrl;
      this.loginInitialization.configuration = JSON.stringify({ channel: this.loginChannel, state: snapshot });
      this.initialized = true;
    } catch (_) {
      if (epoch === this.loginEpoch) {
        this.fatalError = nativeMessage('login_page_failed', this.store.language);
      }
    }
  }

  private async connect(url: string, password: string): Promise<void> {
    const id = ++this.connectionId;
    this.loginTargetUrl = parseServerAddress(url).url;
    this.connecting = true;
    if (this.loginReady) {
      this.loginController.runJavaScript(`onConnectStart(${JSON.stringify(this.loginTargetUrl)});`).catch(() => {});
    }
    try {
      const address = await this.connection.connect(url, password, this.store.language);
      if (id !== this.connectionId) {
        return;
      }
      const save = this.credentialQueue.catch(() => {}).then(async (): Promise<void> => {
        if (id !== this.connectionId) {
          return;
        }
        const existing = this.store.servers.find((server) => server.url === address);
        if (this.ignoreSavedCredentials) {
          await this.store.saveMetadata(address, existing?.name || '');
        } else {
          await this.store.save(address, password, existing?.name || '');
        }
      });
      this.credentialQueue = save;
      await save;
      if (id !== this.connectionId) {
        return;
      }
      await this.store.setActive(address);
      const raw = await this.context().resourceManager.getRawFileContent('harmony-bootstrap.js');
      const systemBars = await this.context().resourceManager.getRawFileContent('system-bars.js');
      if (id !== this.connectionId) {
        return;
      }
      this.channelId = util.generateRandomUUID(true);
      this.bootstrapScript = new util.TextDecoder().decodeWithStream(raw).replace('__HARMONY_CONFIG__',
        JSON.stringify({
          origin: address, channel: this.channelId, language: this.store.language,
          theme: this.store.theme, version: APP_VERSION
        })) + '\n' + new util.TextDecoder().decodeWithStream(systemBars);
      this.activeUrl = address;
      this.loginTargetUrl = address;
      this.activePassword = password;
      await this.notifications.bind(address, this.store.language, this.foreground);
      if (id !== this.connectionId) {
        return;
      }
      this.loginEpoch++;
      this.closePorts(this.loginPorts);
      this.login.clear();
      this.pageReady = false;
      this.showWeb = true;
      this.tunnels?.setServer(address);
      this.startPageTimeout();
    } finally {
      if (id === this.connectionId) {
        this.connecting = false;
      }
    }
  }

  private startPageTimeout(): void {
    clearTimeout(this.pageTimeout);
    this.pageTimeout = setTimeout(() => {
      if (!this.pageReady) {
        this.showConnection(nativeMessage('page_timeout', this.store.language));
      }
    }, 30000);
  }

  private attachLoginPort(): void {
    if (!isLocalLoginPage(this.loginController.getUrl()) || this.showWeb) {
      return;
    }
    this.closePorts(this.loginPorts);
    const ports = this.loginController.createWebMessagePorts();
    this.loginPorts = ports;
    const epoch = this.loginEpoch;
    ports[0].onMessageEvent((message: webview.WebMessage) => {
      if (typeof message !== 'string' || message.length > 16384) {
        return;
      }
      this.loginQueue = this.loginQueue.then(() => this.receiveLoginBridge(message, ports[0], epoch)).catch(() => {});
    });
    // The local resource has an opaque origin. This controller only accepts the packaged login document.
    this.loginController.postMessage('__clawbench_login_port__:' + this.loginChannel, [ports[1]], '*');
    this.loginReady = true;
    if (this.connecting) {
      this.loginController.runJavaScript(`onConnectStart(${JSON.stringify(this.loginTargetUrl)});`).catch(() => {});
    }
    this.reportLoginError();
    void this.notifications.requestStartupPermission();
  }

  private async receiveLoginBridge(message: string, port: webview.WebMessagePort, epoch: number): Promise<void> {
    if (epoch !== this.loginEpoch || this.showWeb || !isLocalLoginPage(this.loginController.getUrl())) {
      return;
    }
    let id = 0;
    try {
      const request = JSON.parse(message) as BridgeRequest;
      id = request.id;
      if (!Number.isInteger(id) || !Array.isArray(request.args) || request.args.length > 3) {
        return;
      }
      const arg = (index: number): string => JSON.parse(request.args[index] || '""') as string;
      const credential = (): LoginCredential => JSON.parse(request.args[1] || 'null') as LoginCredential;
      switch (request.method) {
        case 'saveServer':
          await this.login.save(arg(0), credential(), undefined, this.ignoreSavedCredentials);
          break;
        case 'saveServerNamed':
          await this.login.save(arg(0), credential(), arg(2), this.ignoreSavedCredentials);
          break;
        case 'removeServer': {
          const removedUrl = parseServerAddress(arg(0)).url;
          await this.store.remove(removedUrl, this.ignoreSavedCredentials);
          clearServerCookies(removedUrl);
          if (this.loginTargetUrl === removedUrl) {
            this.loginTargetUrl = '';
          }
          break;
        }
        case 'selectServer':
          if (!this.connecting) {
            this.loginTargetUrl = parseServerAddress(arg(0)).url;
          }
          break;
        case 'ignoreSavedCredentials':
          this.ignoreSavedCredentials = true;
          this.login.clear();
          break;
        case 'setAppearance':
          await this.store.setAppearance(arg(0), arg(1));
          this.notifications.setLanguage(this.store.language);
          break;
        case 'setSystemBarAppearance':
          await this.setSystemBarAppearance(arg(0), arg(1));
          break;
        case 'connectToServer': {
          const target = parseServerAddress(arg(0)).url;
          this.loginTargetUrl = target;
          const attempt = ++this.connectionId;
          this.connecting = true;
          try {
            const password = await this.login.password(credential(), target);
            if (attempt !== this.connectionId || epoch !== this.loginEpoch || this.showWeb ||
                !isLocalLoginPage(this.loginController.getUrl())) {
              return;
            }
            await this.connect(target, password);
          } finally {
            if (attempt === this.connectionId && epoch === this.loginEpoch) {
              this.connecting = false;
            }
          }
          return;
        }
        default:
          throw new NativeFailure('unsupported_operation', this.store.language);
      }
      const snapshot = await this.login.snapshot(this.loginTargetUrl, this.ignoreSavedCredentials);
      if (epoch === this.loginEpoch && !this.showWeb) {
        this.loginTargetUrl = snapshot.activeUrl || this.loginTargetUrl;
        port.postMessageEvent(JSON.stringify({ id, result: JSON.stringify({ value: null, snapshot }) }));
      }
    } catch (error) {
      if (epoch === this.loginEpoch && !this.showWeb) {
        try {
          port.postMessageEvent(JSON.stringify({
            id, error: error instanceof Error ? formatNativeError(error, this.store.language) :
              nativeMessage('operation_failed', this.store.language),
            code: error instanceof ConnectionFailure || error instanceof NativeFailure ? error.code : ''
          }));
        } catch (_) {}
      }
    }
  }

  private reportLoginError(): void {
    if (!this.loginReady || !this.pendingLoginError) {
      return;
    }
    const epoch = this.loginEpoch;
    const message = this.pendingLoginError;
    const code = this.pendingLoginCode;
    const target = this.loginTargetUrl;
    const script = `onConnectError(${JSON.stringify(message)},${JSON.stringify(code)},${JSON.stringify(target)});`;
    this.loginController.runJavaScript(script).then(() => {
      if (epoch === this.loginEpoch && message === this.pendingLoginError && code === this.pendingLoginCode) {
        this.pendingLoginError = '';
        this.pendingLoginCode = '';
      }
    }).catch(() => {});
  }

  private attachPort(url: string): void {
    if (!isTrustedAppUrl(url, this.activeUrl) || this.ports.length > 0) {
      return;
    }
    this.closePorts(this.ports);
    const ports = this.controller.createWebMessagePorts();
    this.ports = ports;
    const generation = this.connectionId;
    const document = this.documentId;
    this.files = new FileTransferService(this.context(), this.activeUrl, (progress) => {
      if (document === this.documentId && generation === this.connectionId) {
        this.emitNativeEvent('clawbench-download-progress', JSON.stringify(progress));
      }
    }, () => {
      if (document === this.documentId && generation === this.connectionId) {
        this.reportDownloadFailure();
      }
    }, this.store.language);
    this.logger = new NativeLogService(this.activeUrl, [this.activePassword]);
    ports[0].onMessageEvent((message: webview.WebMessage) => {
      if (typeof message !== 'string' || message.length > 12 * 1024 * 1024 ||
          generation !== this.connectionId || document !== this.documentId) {
        return;
      }
      this.receiveBridge(message, ports[0], generation, document);
    });
    this.controller.postMessage('__clawbench_harmony_port__:' + this.channelId, [ports[1]], this.activeUrl);
  }

  private async receiveBridge(message: string, port: webview.WebMessagePort, generation: number,
    document: number): Promise<void> {
    let id = 0;
    let method = '';
    try {
      if (!isTrustedAppUrl(this.controller.getUrl(), this.activeUrl)) {
        return;
      }
      const request = JSON.parse(message) as BridgeRequest;
      id = request.id;
      method = request.method;
      if (!Number.isSafeInteger(id) || id <= 0 || !Array.isArray(request.args) || request.args.length > 5 ||
          (message.length > 131072 && request.method !== 'downloadBlob') ||
          !request.args.every((arg: string) => typeof arg === 'string')) {
        return;
      }
      const result = await this.invokeBridge(request);
      if (generation === this.connectionId && document === this.documentId) {
        port.postMessageEvent(JSON.stringify({ id, result }));
      }
    } catch (error) {
      if (generation === this.connectionId && document === this.documentId) {
        if (method === 'downloadFile' || method === 'downloadUrl') {
          this.reportDownloadFailure();
        }
        if (method === 'setSSHPassword' && !this.rendererReady) {
          // The shared login page has not installed App's native-error listener yet.
          this.getUIContext().getPromptAction().showToast({
            message: error instanceof Error ? formatNativeError(error, this.store.language) :
              nativeMessage('credential_save_failed', this.store.language)
          });
        }
        try {
          port.postMessageEvent(JSON.stringify({ id, error: error instanceof Error ?
            formatNativeError(error, this.store.language) : nativeMessage('bridge_failed', this.store.language) }));
        } catch (_) {}
      }
    }
  }

  private reportDownloadFailure(): void {
    this.getUIContext().getPromptAction().showToast({
      message: this.store.language === 'en' ? 'Unable to save file' : '文件保存失败'
    });
  }

  private async invokeBridge(request: BridgeRequest): Promise<string> {
    const arg = (index: number): string => JSON.parse(request.args[index] || '""') as string;
    const numericArg = (index: number): number => JSON.parse(request.args[index] || '0') as number;
    const document = this.documentId;
    const current = (): boolean => document === this.documentId && this.showWeb &&
      isTrustedAppUrl(this.controller.getUrl(), this.activeUrl);
    switch (request.method) {
      case 'getServerList':
        return JSON.stringify(JSON.stringify(this.store.servers));
      case 'getServerUrl':
        return JSON.stringify(this.activeUrl);
      case 'getSavedServerConfig': {
        const address = parseServerAddress(this.activeUrl);
        return JSON.stringify(JSON.stringify({
          protocol: address.protocol, host: address.host, port: address.port, password: ''
        }));
      }
      case 'saveServer':
      case 'saveServerNamed': {
        const url = parseServerAddress(arg(0)).url;
        const password = arg(1);
        const save = this.credentialQueue.catch(() => {}).then(async (): Promise<void> => {
          if (!current()) {
            return;
          }
          const existing = this.store.servers.find((server) => server.url === url);
          const name = request.args.length > 2 ? arg(2) : existing?.name || '';
          if (this.ignoreSavedCredentials) {
            await this.store.saveMetadata(url, name);
          } else {
            await this.store.save(url, password, name);
          }
        });
        this.credentialQueue = save;
        await save;
        break;
      }
      case 'setSSHPassword': {
        if (!current()) {
          break;
        }
        const origin = this.activeUrl;
        const password = arg(0);
        if (typeof password !== 'string' || !password) {
          throw new NativeFailure('password', this.store.language);
        }
        // LoginView sends this without awaiting it. Register redaction before any async work.
        this.logger?.addSecret(password);
        this.activePassword = password;
        const save = this.credentialQueue.catch(() => {}).then(async (): Promise<void> => {
          // Accepted credentials keep their captured origin even after the page closes.
          // Later saves, removals and logins use this queue and cannot be overtaken.
          const existing = this.store.servers.find((server) => server.url === origin);
          if (!this.ignoreSavedCredentials) {
            await this.store.save(origin, password, existing?.name || '');
          }
        });
        this.credentialQueue = save;
        await save;
        break;
      }
      case 'removeServer': {
        const url = parseServerAddress(arg(0)).url;
        const remove = this.credentialQueue.catch(() => {}).then(async (): Promise<void> => {
          if (current()) {
            await this.store.remove(url, this.ignoreSavedCredentials);
            clearServerCookies(url);
          }
        });
        this.credentialQueue = remove;
        await remove;
        break;
      }
      case 'connectToServer': {
        const url = parseServerAddress(arg(0)).url;
        let password = arg(1);
        let readFailed = false;
        if (!password && !this.ignoreSavedCredentials) {
          try {
            password = await this.store.password(url);
          } catch (_) {
            readFailed = true;
          }
        }
        if (!current()) {
          break;
        }
        const switching = this.showConnection(readFailed ?
          nativeMessage('credential_read_failed', this.store.language) : '',
        readFailed ? 'credential_read_failed' : '', url);
        const switchId = this.connectionId;
        const loginEpoch = this.loginEpoch;
        await switching;
        if (switchId !== this.connectionId || loginEpoch !== this.loginEpoch || this.showWeb) {
          break;
        }
        if (!password) {
          break;
        }
        const connecting = this.connect(url, password);
        const attempt = this.connectionId;
        connecting.catch((error: Error) => {
          if (attempt === this.connectionId && loginEpoch === this.loginEpoch && !this.showWeb) {
            this.pendingLoginError = formatNativeError(error, this.store.language);
            this.pendingLoginCode = error instanceof ConnectionFailure || error instanceof NativeFailure ? error.code : '';
            this.reportLoginError();
          }
        });
        break;
      }
      case 'showServerDialog':
        await this.showConnection();
        break;
      case 'setAppearance':
        await this.store.setAppearance(arg(0), arg(1));
        this.files?.setLanguage(this.store.language);
        this.notifications.setLanguage(this.store.language);
        break;
      case 'setSystemBarAppearance':
        await this.setSystemBarAppearance(arg(0), arg(1));
        break;
      case 'setKeepScreenOn':
        await this.keepScreenOn(request.args[0] === 'true');
        break;
      case 'openExternalUrl':
        await this.openExternal(arg(0));
        break;
      case 'openInBrowser':
      case 'openInSandbox': {
        let port = numericArg(0);
        let host = arg(2).toLowerCase();
        const forwarded = ['', 'localhost', '127.0.0.1', '[::1]', '::1', '0.0.0.0'].includes(host);
        if (forwarded) {
          host = '127.0.0.1';
        }
        portBrowserUrl(port, arg(1), host, arg(3));
        if (forwarded) {
          if (!this.tunnels) {
            throw new Error('Tunnel unavailable');
          }
          port = await this.tunnels.prepareForwardedPort(port);
        }
        if (!current() || !this.foreground) {
          throw new Error('Page changed before opening the browser');
        }
        // The OS browser has independent cookies/storage and never receives this application's bridge.
        await this.openExternal(portBrowserUrl(port, arg(1), host, arg(3)));
        break;
      }
      case 'downloadFileWithProgress':
        this.files!.startDownload(arg(0), arg(1), numericArg(2));
        break;
      case 'downloadFile':
        this.files!.downloadFile(arg(0));
        break;
      case 'downloadUrl':
        this.files!.downloadUrl(arg(0), arg(1));
        break;
      case 'cancelDownload':
        this.files!.cancelDownload(numericArg(0));
        break;
      case 'downloadBlob':
        await this.files!.downloadBlob(arg(0), arg(1));
        break;
      case 'shareText':
        await this.files!.shareText(arg(0));
        break;
      case 'shareFile':
        await this.files!.shareFile(arg(0), arg(1));
        break;
      case 'shareFiles':
        await this.files!.shareFiles(arg(0), arg(1));
        break;
      case 'readClipboardText':
        return JSON.stringify(await this.clipboard!.read(current));
      case 'writeClipboardText':
        await this.clipboard!.write(arg(0));
        break;
      case 'showSelectionMenu':
        await this.showSelectionMenu(selectionText(arg(0)), current);
        break;
      case 'setNativePushEnabled':
        await this.notifications.setEnabled(request.args[0] === 'true');
        break;
      case 'updateLastSeenEventId':
        await this.notifications.updateLastSeenEventId(arg(0));
        break;
      case 'dismissEventNotification':
        await this.notifications.dismiss(arg(0), arg(1));
        break;
      case 'getNativePushStatus':
        return JSON.stringify(await this.notifications.refreshStatus());
      case 'getForwardedPorts':
        return JSON.stringify(this.tunnels!.getForwardedPorts());
      case 'isTunnelConnected':
        return JSON.stringify(await this.tunnels!.isTunnelConnected());
      case 'testPortReachable':
        return JSON.stringify(await this.tunnels!.testPortReachable(numericArg(0)));
      case 'getTunnelError':
        return JSON.stringify(this.tunnels!.getTunnelError());
      case 'getTunnelErrorType':
        return JSON.stringify(this.tunnels!.getTunnelErrorType());
      case 'getTunnelTransport':
        return JSON.stringify(this.tunnels!.getTunnelTransport());
      case 'getActiveTunnelTransport':
        return JSON.stringify(this.tunnels!.getActiveTunnelTransport());
      case 'getTunnelTransportH2Enabled':
        return JSON.stringify(this.tunnels!.getTunnelTransportH2Enabled());
      case 'setTunnelTransportH2Enabled':
        await this.tunnels!.setTunnelTransportH2Enabled(request.args[0] === 'true');
        break;
      case 'reconnectTunnel':
      case 'reconnectTunnelAsync':
        return JSON.stringify(await this.tunnels!.reconnectTunnel());
      case 'addForwardedPort':
        return JSON.stringify(await this.tunnels!.addForwardedPort(numericArg(0), numericArg(1), arg(2)));
      case 'addReverseForwardedPort':
        return JSON.stringify(await this.tunnels!.addReverseForwardedPort(numericArg(0), numericArg(1), arg(2)));
      case 'removeForwardedPort':
        await this.tunnels!.removeForwardedPort(numericArg(0));
        break;
      case 'removeReverseForwardedPort':
        await this.tunnels!.removeReverseForwardedPort(numericArg(0));
        break;
      case 'forgetForwardedPorts':
        await this.tunnels!.forgetForwardedPorts();
        break;
      case 'getPendingNavigation':
        return JSON.stringify(this.notifications.takePendingNavigation());
      case 'openSession':
        if (!/^[a-zA-Z0-9_-]{1,160}$/.test(arg(0))) {
          throw new Error('Invalid session');
        }
        this.emitNativeEvent('clawbench-open-session', JSON.stringify({ sessionId: arg(0) }));
        break;
      case 'startLogCapture':
        this.logger!.start();
        break;
      case 'stopLogCapture':
        this.logger!.stop();
        break;
      case 'log':
        this.logger!.log(arg(0), arg(1), arg(2));
        break;
      case 'setVolumeKeyMode':
        this.volumeKeyMode = request.args[0] === 'true';
        break;
      case 'stopBackgroundService':
        // Notification delivery has its own explicit switch; a tunnel teardown must not disable it.
        await this.tunnels!.stopBackgroundService();
        this.keepScreenOn(false);
        break;
      case 'reloadApp':
        this.controller.refresh();
        break;
      case 'rendererReady':
        this.rendererReady = true;
        this.deliverPendingNavigation();
        this.handleShareImport();
        break;
      case 'dismissSplash':
        this.pageReady = true;
        clearTimeout(this.pageTimeout);
        break;
      default:
        throw new Error('Unsupported bridge method');
    }
    return 'null';
  }

  private async keepScreenOn(enabled: boolean): Promise<void> {
    try {
      const mainWindow = await window.getLastWindow(this.context());
      await mainWindow.setWindowKeepScreenOn(enabled);
    } catch (_) {}
  }

  private async setSystemBarAppearance(top: string, bottom: string): Promise<void> {
    const statusBarColor = normalizeBarColor(top);
    const navigationBarColor = normalizeBarColor(bottom);
    this.surfaceColor = statusBarColor;
    const mainWindow = await window.getLastWindow(this.context());
    await mainWindow.setWindowSystemBarProperties({
      statusBarColor, navigationBarColor,
      statusBarContentColor: barContentColor(statusBarColor),
      navigationBarContentColor: barContentColor(navigationBarColor)
    });
  }

  private async openExternal(url: string): Promise<void> {
    const target = externalHttpUrl(url);
    try {
      await this.context().openLink(target);
    } catch (_) {
      await this.context().startAbility({ action: 'ohos.want.action.viewData', uri: target });
    }
  }

  private async showSelectionMenu(text: string, isCurrent: () => boolean): Promise<void> {
    const en = this.store.language === 'en';
    try {
      const result = await this.getUIContext().getPromptAction().showActionMenu({
        buttons: [
          { text: en ? 'Copy' : '复制', color: this.surfaceColor === '#FFFFFF' ? '#222222' : '#2676D9' },
          { text: en ? 'Quote' : '引用', color: '#2676D9' },
          { text: en ? 'Discuss' : '讨论', color: '#2676D9' }
        ]
      });
      if (!isCurrent()) {
        return;
      }
      if (result.index === 0) {
        await this.clipboard!.write(text);
      } else if (result.index === 1 || result.index === 2) {
        this.emitNativeEvent('clawbench-selection-action',
          JSON.stringify({ action: result.index === 1 ? 'quote' : 'comment' }));
      }
    } catch (_) {}
  }

  private foregroundChanged(): void {
    this.notifications.setForeground(this.foreground);
    this.tunnels?.setForeground(this.foreground);
    if (this.foreground) {
      if (this.loginReady || this.pageReady) {
        void this.notifications.requestStartupPermission();
      }
      this.handleShareImport();
      this.shareErrorChanged();
    }
    if (this.showWeb && this.pageReady) {
      const script = this.foreground ?
        'if (typeof window.__clawbenchAppResume === "function") window.__clawbenchAppResume();' :
        'if (typeof window.__setAppForeground === "function") window.__setAppForeground(false);';
      this.controller.runJavaScript(script).catch(() => {});
    }
  }

  onBackPress(): boolean {
    if (!this.showWeb && this.connecting) {
      this.showConnection(nativeMessage('cancelled', this.store.language));
      return true;
    }
    if (!this.showWeb && !this.loginReady) {
      return false;
    }
    if (this.backInFlight) {
      return true;
    }
    this.backInFlight = true;
    const generation = this.connectionId;
    const document = this.documentId;
    const loginEpoch = this.loginEpoch;
    const controller = this.showWeb ? this.controller : this.loginController;
    const wasWeb = this.showWeb;
    const current = (): boolean => generation === this.connectionId && document === this.documentId &&
      loginEpoch === this.loginEpoch && wasWeb === this.showWeb;
    const script = wasWeb ? `(() => {
      window.__clawbenchBackHandled = false;
      window.dispatchEvent(new CustomEvent('clawbench-back-press'));
      return window.__clawbenchBackHandled === true;
    })()` : 'window.__clawbenchLoginBack()';
    controller.runJavaScript(script).then((result: string) => {
      if (!current() || result === 'true') {
        return;
      }
      if (!wasWeb) {
        this.context().terminateSelf();
      } else if (Date.now() - this.lastBack < 2000) {
        this.showConnection();
      } else {
        this.lastBack = Date.now();
        this.getUIContext().getPromptAction().showToast({ message: nativeMessage('back_again', this.store.language) });
      }
    }).catch(() => {
      if (current()) {
        this.showConnection();
      }
    }).finally(() => this.backInFlight = false);
    return true;
  }

  build() {
    Column() {
      if (this.showWeb) {
        Stack() {
          Web({ src: this.activeUrl + '/', controller: this.controller })
            .javaScriptAccess(true).domStorageAccess(true).fileAccess(false).mixedMode(MixedMode.None)
            .javaScriptOnDocumentStart([{ script: this.bootstrapScript, scriptRules: [this.activeUrl] }])
            .onLoadIntercept((event) => {
              if (!event || !event.data.isMainFrame()) {
                return false;
              }
              const url = event.data.getRequestUrl();
              if (isTrustedAppUrl(url, this.activeUrl)) {
                if (url.slice(this.activeUrl.length).split(/[?#]/)[0] === '/login') {
                  this.showConnection();
                  return true;
                }
                return false;
              }
              if (event.data.isRequestGesture()) {
                this.openExternal(url).catch(() => {});
              }
              return true;
            })
            .onPageBegin(() => {
              this.disposePageServices();
              this.closePorts(this.ports);
              this.pageReady = false;
              this.startPageTimeout();
            })
            .onPageEnd((event) => {
              if (event && isTrustedAppUrl(event.url, this.activeUrl)) {
                this.attachPort(event.url);
                this.pageReady = true;
                clearTimeout(this.pageTimeout);
                void this.notifications.requestStartupPermission();
              }
            })
            .onErrorReceive((event) => {
              if (event?.request.isMainFrame()) {
                this.showConnection(nativeMessage('page_failed', this.store.language));
              }
            })
            .onShowFileSelector((event) => {
              if (!event) { return false; }
              const document = this.documentId;
              this.filePicker?.choose(event, () => document === this.documentId && this.showWeb &&
                isTrustedAppUrl(this.controller.getUrl(), this.activeUrl));
              return true;
            })
            .onPermissionRequest((event) => {
              if (!event) { return; }
              const document = this.documentId;
              this.media?.request(event.request, this.activeUrl, () => document === this.documentId &&
                this.showWeb && isTrustedAppUrl(this.controller.getUrl(), this.activeUrl));
            })
            .onScreenCaptureRequest((event) => {
              if (!event) { return; }
              const document = this.documentId;
              this.capture?.request(event.handler, this.activeUrl, () => document === this.documentId &&
                this.showWeb && isTrustedAppUrl(this.controller.getUrl(), this.activeUrl));
            })
            .onKeyEvent((event: KeyEvent) => {
              if (this.volumeKeyMode && event.type === KeyType.Down &&
                  (event.keyCode === KeyCode.KEYCODE_VOLUME_UP || event.keyCode === KeyCode.KEYCODE_VOLUME_DOWN)) {
                event.stopPropagation();
                this.emitNativeEvent('clawbench-volume-key', JSON.stringify({
                  direction: event.keyCode === KeyCode.KEYCODE_VOLUME_UP ? 'up' : 'down'
                }));
              }
            })
            .editMenuOptions({
              onCreateMenu: (items: TextMenuItem[]): TextMenuItem[] => {
                const en = this.store.language === 'en';
                return items.concat([
                  { id: TextMenuItemId.of('clawbench-quote'), content: en ? 'Quote' : '引用' },
                  { id: TextMenuItemId.of('clawbench-comment'), content: en ? 'Discuss' : '讨论' },
                  { id: TextMenuItemId.of('clawbench-search-file'), content: en ? 'Find file' : '搜索文件' }
                ]);
              },
              onMenuItemClick: (item: TextMenuItem): boolean => {
                for (const action of ['quote', 'comment', 'search-file']) {
                  if (item.id.equals(TextMenuItemId.of('clawbench-' + action))) {
                    this.emitNativeEvent('clawbench-selection-action', JSON.stringify({ action }));
                    return true;
                  }
                }
                return false;
              }
            })
            .width('100%').height('100%')
          if (!this.pageReady) {
            Column({ space: 16 }) {
              LoadingProgress().width(32).height(32).color('#8DA9E8')
              Text(nativeMessage('loading', this.store.language)).fontSize(14).fontColor('#BEC3CA')
              Button(nativeMessage('cancel', this.store.language)).onClick(() => this.showConnection())
            }.width('100%').height('100%').justifyContent(FlexAlign.Center).backgroundColor('#17191D')
          }
        }.width('100%').height('100%')
      } else if (this.initialized) {
        Web({ src: $rawfile('login/index.html'), controller: this.loginController })
          .javaScriptAccess(true).domStorageAccess(false).fileAccess(false).mixedMode(MixedMode.None)
          .javaScriptProxy({
            object: this.loginInitialization, name: 'ClawBenchLoginInit', methodList: ['getConfig'],
            controller: this.loginController
          })
          .onLoadIntercept((event) => {
            if (!event) { return true; }
            return event.data.isMainFrame() && !isLocalLoginPage(event.data.getRequestUrl());
          })
          .onPageBegin(() => {
            this.loginReady = false;
            this.closePorts(this.loginPorts);
          })
          .onPageEnd(() => this.attachLoginPort())
          .onPermissionRequest((event) => event?.request.deny())
          .width('100%').height('100%')
      } else {
        Column({ space: 16 }) {
          if (this.fatalError) {
            Text(this.fatalError).fontColor('#F1A0A0').fontSize(14)
            Button(nativeMessage('retry', this.store.language)).onClick(() => {
              if (this.nativeReady) {
                this.showConnection();
              } else {
                this.initialize();
              }
            })
            if (this.nativeReady) {
              Button(nativeMessage('ignore_credentials', this.store.language)).onClick(() => {
                this.ignoreSavedCredentials = true;
                this.showConnection();
              })
            }
          } else {
            LoadingProgress().width(32).height(32).color('#8DA9E8')
          }
        }.width('100%').height('100%').justifyContent(FlexAlign.Center)
      }
    }.width('100%').height('100%').padding(this.safeInsets).backgroundColor(this.surfaceColor)
  }
}
