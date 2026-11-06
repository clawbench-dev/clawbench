interface NativeMessage {
  zh: string;
  en: string;
}

const MESSAGES: Record<string, NativeMessage> = {
  password: { zh: '密码不正确', en: 'Wrong password' },
  rate_limit: { zh: '登录尝试过于频繁，请稍后重试', en: 'Too many login attempts. Please try again later.' },
  health_failed: { zh: '服务器健康检查失败，请检查服务器状态', en: 'The server health check failed. Check the server status.' },
  invalid_server: { zh: '该地址未返回有效的 ClawBench 服务器信息', en: 'This address did not return a valid ClawBench server response.' },
  protocol: { zh: '服务器客户端协议不兼容，需要协议 1', en: 'The server uses an incompatible client protocol. Protocol 1 is required.' },
  invalid_address: { zh: '请输入有效的 HTTP 或 HTTPS 服务器地址', en: 'Enter a valid HTTP or HTTPS server address.' },
  auth_failed: { zh: '登录失败，请检查服务器状态', en: 'Login failed. Check the server status.' },
  auth_cookie_missing: { zh: '服务器未返回登录凭据', en: 'The server did not return a login session.' },
  cancelled: { zh: '连接已取消', en: 'Connection cancelled.' },
  certificate: {
    zh: '服务器证书验证失败，可能为自签名证书、证书已过期或域名不匹配。请使用受信任且匹配服务器域名的证书。',
    en: 'The server certificate could not be verified. It may be self-signed, expired, or issued for another host. Use a trusted certificate that matches the server hostname.'
  },
  tls_local_certificate: { zh: '本地 TLS 证书配置无效，请检查客户端证书', en: 'The local TLS certificate is invalid. Check the client certificate configuration.' },
  tls_ca_unavailable: { zh: '无法读取 TLS 证书颁发机构配置，请检查证书配置', en: 'The TLS certificate authority configuration is unavailable. Check the certificate configuration.' },
  tls_protocol: { zh: '无法协商 TLS 加密连接，请检查服务器 TLS 配置', en: 'A TLS connection could not be negotiated. Check the server TLS configuration.' },
  dns: { zh: '无法解析服务器或代理地址，请检查地址和网络', en: 'The server or proxy address could not be resolved. Check the address and network.' },
  timeout: { zh: '连接超时，请检查网络后重试', en: 'The connection timed out. Check the network and try again.' },
  network: { zh: '无法连接到服务器，请检查地址和网络', en: 'Unable to connect to the server. Check the address and network.' },
  network_permission: { zh: '网络访问被拒绝，请检查应用网络权限', en: 'Network access was denied. Check the app network permissions.' },
  operation_failed: { zh: '操作失败，请重试', en: 'The operation failed. Please try again.' },
  credential_ref_invalid: { zh: '保存的密码引用已失效，请重新选择服务器', en: 'The saved password reference has expired. Select the server again.' },
  credential_read_failed: { zh: '无法读取保存的密码，请重新输入', en: 'The saved password could not be read. Please enter it again.' },
  credential_remove_failed: { zh: '无法删除保存的密码，请重试', en: 'The saved password could not be removed. Please try again.' },
  credential_save_failed: { zh: '无法保存密码，请重试', en: 'The password could not be saved. Please try again.' },
  store_not_ready: { zh: '本地存储尚未就绪，请重试', en: 'Local storage is not ready. Please try again.' },
  config_load_failed: { zh: '本地配置读取失败，请重试', en: 'The local configuration could not be loaded. Please try again.' },
  login_page_failed: { zh: '无法打开本地登录页，请重试', en: 'The local login page could not be opened. Please try again.' },
  login_required_for_notification: { zh: '请登录通知所属的服务器后重试', en: 'Log in to the server for this notification and try again.' },
  notification_not_connected: { zh: '通知服务尚未连接', en: 'The notification service is not connected.' },
  notification_settings_failed: { zh: '无法读取通知设置，将在重新连接或返回应用时重试', en: 'Native notification settings unavailable; retrying on reconnect or resume.' },
  notification_permission_denied: { zh: '通知权限尚未授予，请在系统设置中允许通知', en: 'Notification permission was not granted. Allow notifications in system settings.' },
  notification_status_failed: { zh: '无法读取或更改通知状态，请重试', en: 'Notification status or settings are unavailable. Please try again.' },
  notification_connection_failed: { zh: '通知连接暂不可用，正在重试并补拉消息', en: 'Native event connection unavailable; retrying and recovering pending events.' },
  page_timeout: { zh: '页面加载超时，请检查网络后重试', en: 'The page took too long to load. Check the network and try again.' },
  page_failed: { zh: '页面加载失败，请检查网络和证书', en: 'The page could not be loaded. Check the network and certificate.' },
  unsupported_operation: { zh: '当前操作不受支持', en: 'This operation is not supported.' },
  bridge_failed: { zh: 'HarmonyOS 操作失败或当前不支持', en: 'The HarmonyOS operation failed or is not supported.' },
  retry: { zh: '重试', en: 'Retry' },
  ignore_credentials: { zh: '忽略已保存凭据', en: 'Ignore saved credentials' },
  cancel: { zh: '取消', en: 'Cancel' },
  connecting: { zh: '正在连接', en: 'Connecting' },
  loading: { zh: '正在加载工作台', en: 'Loading workbench' },
  back_again: { zh: '再按一次返回服务器列表', en: 'Press Back again to return to the server list.' },
  local_timeout: { zh: '本地操作超时，请重试', en: 'The local operation timed out. Please try again.' },
  file_save_failed: { zh: '无法保存文件，请重试', en: 'The file could not be saved. Please try again.' },
  share_text_invalid: { zh: '分享文本为空或超过长度限制', en: 'The shared text is empty or exceeds the length limit.' },
  file_busy: { zh: '已有文件操作正在进行，请稍后重试', en: 'A file operation is already in progress. Please try again later.' },
  file_share_failed: { zh: '无法分享文件，请检查网络后重试', en: 'The file could not be shared. Check the network and try again.' },
  file_too_large: { zh: '文件超过传输大小限制', en: 'The file exceeds the transfer size limit.' },
  storage_slow: { zh: '存储写入过慢，请重试', en: 'Storage is too slow to complete this transfer. Please try again.' },
  file_write_failed: { zh: '文件写入失败，请检查剩余存储空间', en: 'The file could not be written. Check the available storage space.' },
  download_rejected: { zh: '服务器拒绝下载或要求重新登录', en: 'The server rejected the download or requires you to log in again.' },
  file_operation_failed: { zh: '文件操作失败，请重试', en: 'The file operation failed. Please try again.' },
  share_cache_full: { zh: '分享缓存已满，请稍后重试', en: 'The share cache is full. Please try again later.' },
  share_import_busy: { zh: '分享导入正在进行，请稍后重试', en: 'A shared file import is in progress. Please try again later.' },
  share_read_failed: { zh: '无法读取分享文件，请重新分享并确认文件访问权限和大小', en: 'The shared files could not be read. Share them again and check their permissions and size.' },
  share_changed: { zh: '分享内容已改变，请重新确认', en: 'The shared content has changed. Please confirm it again.' },
  share_target_changed: { zh: '目标项目已改变，请重新确认', en: 'The destination project has changed. Please confirm it again.' }
};

export function nativeMessage(key: string, language: string = 'zh'): string {
  const message = Object.keys(MESSAGES).includes(key) ? MESSAGES[key] : MESSAGES.operation_failed;
  return language === 'en' ? message.en : message.zh;
}

export class NativeFailure extends Error {
  code: string;

  constructor(code: string, language: string = 'zh') {
    super(nativeMessage(code, language));
    this.code = code;
  }
}

export function formatNativeError(error: Error, language: string = 'zh'): string {
  const code = (error as NativeFailure).code;
  if (typeof code === 'string' && Object.keys(MESSAGES).includes(code)) {
    return nativeMessage(code, language);
  }
  return error.message || nativeMessage('operation_failed', language);
}
