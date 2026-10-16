/**
 * Native-surface label tables for the Electron main process.
 *
 * The desktop shell renders several strings OUTSIDE the web page — the
 * version-gate dialogs and the first-run login page — where the web app's
 * vue-i18n bundle is not reachable. They follow the language the user picked in
 * the web UI (mirrored into `store.language` by the `native:set-language`
 * bridge), falling back to the OS locale on first run, exactly like
 * `contextMenuLabels` already does for the right-click menu.
 *
 * Every resolver here is a PURE function of a raw locale string, so the wording
 * is unit-testable without booting Electron. `resolveNativeLanguage()` is the
 * one impure helper; it only reads the store.
 *
 * 主进程原生界面(版本门控对话框、首启登录页)的文案表。这些界面渲染在网页
 * 之外,取不到 web 端的 vue-i18n bundle,因此跟随用户在网页里选择的语言
 * (经 `native:set-language` 桥镜像进 `store.language`),首启回退系统语言——
 * 与右键菜单的 `contextMenuLabels` 同一套规则。解析函数都是 locale 字符串的
 * 纯函数,便于单测;仅 `resolveNativeLanguage()` 读 store。
 */
import { app } from 'electron'
import { getStore } from './store'

/** A resolved native language. Only the two locales the shell ships. */
export type NativeLanguage = 'zh' | 'en'

/**
 * Normalize a raw locale (e.g. `zh-CN`, `en_US`, `de`) to one of the two
 * shipped languages. Any `zh*` maps to Chinese; everything else — including an
 * empty string — falls back to English.
 */
export function normalizeNativeLanguage(locale: string): NativeLanguage {
  return locale.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

/**
 * The language the native surfaces should render in: the web UI's choice when
 * it has reported one, otherwise the OS locale.
 */
export function resolveNativeLanguage(): NativeLanguage {
  const stored = getStore().get('language')
  if (stored) return normalizeNativeLanguage(stored)
  return normalizeNativeLanguage(app.getLocale())
}

/** Labels for the version-gate dialogs in `versionGate.ts`. */
export interface VersionGateLabels {
  ok: string
  downloadFailedTitle: string
  downloadFailedMessage: string
  restartNow: string
  later: string
  versionReadyTitle: string
  /** `{version}` is the version that was installed. */
  versionReadyMessage: string
  versionReadyDetail: string
  restartFailedTitle: string
  restartFailedMessage: string
  /** Prefix for the payload (npm) failure inside the install-failure detail. */
  payloadInstallFailed: string
  /** Prefix for the full-package failure inside the install-failure detail. */
  fullDownloadFailed: string
}

/**
 * Resolve the version-gate dialog strings from a raw locale string.
 * Any locale starting with `zh` maps to Chinese; everything else to English.
 */
export function versionGateLabels(locale: string): VersionGateLabels {
  if (normalizeNativeLanguage(locale) === 'zh') {
    return {
      ok: '确定',
      downloadFailedTitle: '下载失败',
      downloadFailedMessage: '下载或安装该版本失败，当前版本不受影响。',
      restartNow: '立即重启',
      later: '稍后',
      versionReadyTitle: '版本已就绪',
      versionReadyMessage: 'ClawBench 桌面版 {version} 已安装。',
      versionReadyDetail: '重启后生效。选择“稍后”也可在下次启动时自动使用该版本。',
      restartFailedTitle: '重启失败',
      restartFailedMessage: '无法启动该版本，请手动重启应用。',
      payloadInstallFailed: '增量包（npm）安装失败：',
      fullDownloadFailed: '全量包下载失败：',
    }
  }
  return {
    ok: 'OK',
    downloadFailedTitle: 'Download Failed',
    downloadFailedMessage: 'Failed to download or install this version. Your current version is unaffected.',
    restartNow: 'Restart Now',
    later: 'Later',
    versionReadyTitle: 'Version Ready',
    versionReadyMessage: 'ClawBench Desktop {version} is installed.',
    versionReadyDetail: 'It takes effect after a restart. Choosing “Later” also picks it up automatically on the next launch.',
    restartFailedTitle: 'Restart Failed',
    restartFailedMessage: 'Could not launch this version. Please restart the app manually.',
    payloadInstallFailed: 'Payload (npm) install failed:',
    fullDownloadFailed: 'Full package download failed:',
  }
}

/** Labels for the TLS certificate prompt in `certificate.ts`. */
export interface CertificateLabels {
  title: string
  message: string
  /** The affirmative button: trust the certificate and continue. */
  trust: string
  /** The negative button: abandon the connection. */
  cancel: string
}

/**
 * Resolve the certificate-prompt strings from a raw locale string.
 *
 * Wording mirrors Android's `conn_ssl_title` / `conn_ssl_message` /
 * `conn_ssl_positive` / `conn_ssl_negative` so both platforms read identically.
 */
export function certificateLabels(locale: string): CertificateLabels {
  if (normalizeNativeLanguage(locale) === 'zh') {
    return {
      title: 'SSL 证书验证失败',
      message: '服务器使用了自签名证书，连接可能不安全。\n\n仅当您信任该服务器时才继续。',
      trust: '信任并继续',
      cancel: '取消连接',
    }
  }
  return {
    title: 'SSL certificate verification failed',
    message: 'The server uses a self-signed certificate, the connection may not be secure.\n\nOnly continue if you trust this server.',
    trust: 'Trust and continue',
    cancel: 'Cancel connection',
  }
}
