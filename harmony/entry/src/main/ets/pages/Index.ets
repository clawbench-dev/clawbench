import { common } from '@kit.AbilityKit';
import { webview } from '@kit.ArkWeb';
import { picker } from '@kit.CoreFileKit';
import { util } from '@kit.ArkTS';
import { window, KeyboardAvoidMode } from '@kit.ArkUI';
import { ServerStore } from '../services/ServerStore';
import { ConnectionService, ConnectionFailure } from '../services/ConnectionService';
import { LoginSession, LoginInitialization } from '../services/LoginSession';
import { APP_VERSION, isLocalLoginPage } from '../models/AppInfo';
import { isTrustedAppUrl, parseServerAddress } from '../models/ServerAddress';
import { normalizeBarColor, barContentColor } from '../models/SystemBars';
import { SafeInsets, mergeSafeInsets } from '../models/SafeInsets';

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
  private backInFlight: boolean = false;
  private lastBack: number = 0;
  private mainWindow?: window.Window;
  private updateWindowInsets = (): void => this.updateSafeInsets();

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
      await this.showConnection();
    } catch (_) {
      this.fatalError = '本地配置读取失败，请重试';
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

  private async showConnection(message: string = '', code: string = ''): Promise<void> {
    this.connectionId++;
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
    try {
      const snapshot = await this.login.snapshot();
      if (epoch !== this.loginEpoch) {
        return;
      }
      this.loginChannel = util.generateRandomUUID(true);
      this.loginInitialization.configuration = JSON.stringify({ channel: this.loginChannel, state: snapshot });
      this.initialized = true;
    } catch (_) {
      if (epoch === this.loginEpoch) {
        this.fatalError = '无法打开本地登录页，请重试';
      }
    }
  }

  private async connect(url: string, password: string): Promise<void> {
    const id = ++this.connectionId;
    this.connecting = true;
    try {
      const address = await this.connection.connect(url, password);
      if (id !== this.connectionId) {
        return;
      }
      const existing = this.store.servers.find((server) => server.url === address);
      await this.store.save(address, password, existing?.name || '');
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
      this.loginEpoch++;
      this.closePorts(this.loginPorts);
      this.login.clear();
      this.pageReady = false;
      this.showWeb = true;
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
        this.showConnection('页面加载超时，请检查网络后重试');
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
    this.reportLoginError();
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
      switch (request.method) {
        case 'saveServer':
          await this.login.save(arg(0), arg(1));
          break;
        case 'saveServerNamed':
          await this.login.save(arg(0), arg(1), arg(2));
          break;
        case 'removeServer':
          await this.store.remove(parseServerAddress(arg(0)).url);
          break;
        case 'setAppearance':
          await this.store.setAppearance(arg(0), arg(1));
          break;
        case 'setSystemBarAppearance':
          await this.setSystemBarAppearance(arg(0), arg(1));
          break;
        case 'connectToServer':
          await this.connect(arg(0), await this.login.password(arg(1)));
          return;
        default:
          throw new Error('当前操作不受支持');
      }
      const snapshot = await this.login.snapshot();
      if (epoch === this.loginEpoch && !this.showWeb) {
        port.postMessageEvent(JSON.stringify({ id, result: JSON.stringify({ value: null, snapshot }) }));
      }
    } catch (error) {
      if (epoch === this.loginEpoch && !this.showWeb) {
        try {
          port.postMessageEvent(JSON.stringify({
            id, error: error instanceof Error ? error.message : '操作失败，请重试',
            code: error instanceof ConnectionFailure ? error.code : ''
          }));
        } catch (_) {}
      }
    }
  }

  private reportLoginError(): void {
    if (!this.loginReady || !this.pendingLoginError) {
      return;
    }
    const script = `onConnectError(${JSON.stringify(this.pendingLoginError)},${JSON.stringify(this.pendingLoginCode)});`;
    this.loginController.runJavaScript(script).then(() => {
      this.pendingLoginError = '';
      this.pendingLoginCode = '';
    }).catch(() => {});
  }

  private attachPort(url: string): void {
    if (!isTrustedAppUrl(url, this.activeUrl)) {
      return;
    }
    this.closePorts(this.ports);
    const ports = this.controller.createWebMessagePorts();
    this.ports = ports;
    const generation = this.connectionId;
    ports[0].onMessageEvent((message: webview.WebMessage) => {
      if (typeof message !== 'string' || message.length > 16384 || generation !== this.connectionId) {
        return;
      }
      this.receiveBridge(message, ports[0], generation);
    });
    this.controller.postMessage('__clawbench_harmony_port__:' + this.channelId, [ports[1]], this.activeUrl);
  }

  private async receiveBridge(message: string, port: webview.WebMessagePort, generation: number): Promise<void> {
    let id = 0;
    try {
      if (!isTrustedAppUrl(this.controller.getUrl(), this.activeUrl)) {
        return;
      }
      const request = JSON.parse(message) as BridgeRequest;
      id = request.id;
      if (!Number.isInteger(id) || !Array.isArray(request.args) || request.args.length > 5) {
        return;
      }
      const result = await this.invokeBridge(request);
      if (generation === this.connectionId) {
        port.postMessageEvent(JSON.stringify({ id, result }));
      }
    } catch (_) {
      if (generation === this.connectionId) {
        try { port.postMessageEvent(JSON.stringify({ id, error: 'HarmonyOS 操作失败或当前不支持' })); } catch (_) {}
      }
    }
  }

  private async invokeBridge(request: BridgeRequest): Promise<string> {
    const arg = (index: number): string => JSON.parse(request.args[index] || '""') as string;
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
      case 'saveServerNamed':
        await this.store.save(arg(0), arg(1), request.args.length > 2 ? arg(2) : '');
        break;
      case 'removeServer':
        await this.store.remove(parseServerAddress(arg(0)).url);
        break;
      case 'connectToServer': {
        const url = parseServerAddress(arg(0)).url;
        const password = arg(1) || await this.store.password(url);
        await this.showConnection();
        this.connect(url, password).catch((error: Error) => {
          this.pendingLoginError = error.message;
          this.pendingLoginCode = error instanceof ConnectionFailure ? error.code : '';
          this.reportLoginError();
        });
        break;
      }
      case 'showServerDialog':
        await this.showConnection();
        break;
      case 'setAppearance':
        await this.store.setAppearance(arg(0), arg(1));
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
      case 'reloadApp':
        this.controller.refresh();
        break;
      case 'rendererReady':
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
    if (!/^https?:\/\/[^\\\s]+$/i.test(url)) {
      throw new Error('Unsupported external URL');
    }
    await this.context().startAbility({ action: 'ohos.want.action.viewData', uri: url });
  }

  private async chooseFiles(event: OnShowFileSelectorEvent): Promise<void> {
    try {
      const options = new picker.DocumentSelectOptions();
      options.maxSelectNumber = event.fileSelector.getMode() === FileSelectorMode.FileOpenMultipleMode ? 20 : 1;
      const files = await new picker.DocumentViewPicker(this.context()).select(options);
      event.result.handleFileList(files);
    } catch (_) {
      event.result.handleFileList([]);
    }
  }

  private foregroundChanged(): void {
    if (this.showWeb && this.pageReady) {
      const script = this.foreground ?
        'if (typeof window.__clawbenchAppResume === "function") window.__clawbenchAppResume();' :
        'if (typeof window.__setAppForeground === "function") window.__setAppForeground(false);';
      this.controller.runJavaScript(script).catch(() => {});
    }
  }

  onBackPress(): boolean {
    if (!this.showWeb && this.connecting) {
      this.showConnection('连接已取消');
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
    const controller = this.showWeb ? this.controller : this.loginController;
    const wasWeb = this.showWeb;
    const script = wasWeb ? `(() => {
      window.__clawbenchBackHandled = false;
      window.dispatchEvent(new CustomEvent('clawbench-back-press'));
      return window.__clawbenchBackHandled === true;
    })()` : 'window.__clawbenchLoginBack()';
    controller.runJavaScript(script).then((result: string) => {
      if (generation !== this.connectionId || result === 'true') {
        return;
      }
      if (!wasWeb) {
        this.context().terminateSelf();
      } else if (Date.now() - this.lastBack < 2000) {
        this.showConnection();
      } else {
        this.lastBack = Date.now();
        this.getUIContext().getPromptAction().showToast({ message: '再按一次返回服务器列表' });
      }
    }).catch(() => this.showConnection()).finally(() => this.backInFlight = false);
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
              this.closePorts(this.ports);
              this.pageReady = false;
              this.startPageTimeout();
            })
            .onPageEnd((event) => {
              if (event && isTrustedAppUrl(event.url, this.activeUrl)) {
                this.attachPort(event.url);
                this.pageReady = true;
                clearTimeout(this.pageTimeout);
              }
            })
            .onErrorReceive((event) => {
              if (event?.request.isMainFrame()) {
                this.showConnection('页面加载失败，请检查网络和证书');
              }
            })
            .onShowFileSelector((event) => {
              if (!event) { return false; }
              this.chooseFiles(event);
              return true;
            })
            .onPermissionRequest((event) => event?.request.deny())
            .width('100%').height('100%')
          if (!this.pageReady) {
            Column({ space: 16 }) {
              LoadingProgress().width(32).height(32).color('#8DA9E8')
              Text('正在加载工作台').fontSize(14).fontColor('#BEC3CA')
              Button('取消').onClick(() => this.showConnection())
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
            Button('重试').onClick(() => this.initialize())
          } else {
            LoadingProgress().width(32).height(32).color('#8DA9E8')
          }
        }.width('100%').height('100%').justifyContent(FlexAlign.Center)
      }
    }.width('100%').height('100%').padding(this.safeInsets).backgroundColor(this.surfaceColor)
  }
}
