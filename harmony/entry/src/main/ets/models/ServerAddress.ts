export interface ServerAddress {
  url: string;
  origin: string;
  protocol: string;
  host: string;
  port: string;
}

export function parseServerAddress(input: string): ServerAddress {
  let value = input.trim();
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
    value = `https://${value}`;
  }
  const match = /^(https?):\/\/(\[[0-9a-f:]+\]|[a-z0-9.-]+)(?::([0-9]+))?\/?$/i.exec(value);
  if (!match || match[2].length === 0) {
    throw new Error('请输入 HTTP 或 HTTPS 服务器地址，不含路径、账号或查询参数');
  }
  const protocol = match[1].toLowerCase();
  const portNumber = match[3] ? Number(match[3]) : (protocol === 'https' ? 443 : 80);
  if (!Number.isInteger(portNumber) || portNumber < 1 || portNumber > 65535) {
    throw new Error('端口必须在 1 到 65535 之间');
  }
  const isDefault = (protocol === 'https' && portNumber === 443) || (protocol === 'http' && portNumber === 80);
  const host = match[2].toLowerCase();
  const port = isDefault ? '' : String(portNumber);
  const origin = `${protocol}://${host}${port ? ':' + port : ''}`;
  return { url: origin, origin, protocol, host, port };
}

export function isTrustedAppUrl(candidate: string, origin: string): boolean {
  if (!origin || !candidate.startsWith(origin + '/')) {
    return false;
  }
  const path = candidate.slice(origin.length).split(/[?#]/)[0];
  return path === '/' || path === '/login';
}

export function compatibleProtocol(app: string, protocol: number): boolean {
  return app === 'clawbench' && protocol === 1;
}
