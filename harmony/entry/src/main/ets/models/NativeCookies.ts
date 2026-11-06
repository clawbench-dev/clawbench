interface ServerCookieScope {
  sessionName: string;
  sessionValue: string;
  prefix: string;
}

const scopes = new Map<string, ServerCookieScope>();

// The login response is authoritative: a reverse proxy's public port need not
// match the backend port embedded in ClawBench's cookie names.
export function registerServerCookies(origin: string, setCookies: string[]): void {
  const candidates: ServerCookieScope[] = [];
  for (const cookie of setCookies) {
    const pair = cookie.split(';')[0];
    const match = /^((cb[0-9]{1,5}_)?clawbench_session)=([^;\r\n]+)$/.exec(pair);
    if (match && !/[\u0000-\u0020\u007f",;\\]/.test(match[3])) {
      candidates.push({ sessionName: match[1], sessionValue: match[3], prefix: match[2] || '' });
    }
  }
  if (candidates.length !== 1) {
    scopes.delete(origin);
    throw new Error('The login response must identify exactly one session cookie');
  }
  scopes.set(origin, candidates[0]);
}

export function nativeCookieHeader(origin: string, cookieHeader: string): string {
  const scope = scopes.get(origin);
  if (!scope) {
    throw new Error('Native authentication is unavailable for this server');
  }
  const cookies = [scope.sessionName + '=' + scope.sessionValue];
  const names = [scope.prefix + 'clawbench_project', scope.prefix + 'chat_session_id'];
  let sameSession = false;
  for (const pair of cookieHeader.split(';')) {
    if (pair.trim() === scope.sessionName + '=' + scope.sessionValue) {
      sameSession = true;
    }
  }
  // If another origin overwrote a shared cookie name, its project selection is
  // not evidence of this origin's project. Never pair those values together.
  if (!sameSession) {
    return cookies.join('; ');
  }
  for (const pair of cookieHeader.split(';')) {
    const trimmed = pair.trim();
    const equal = trimmed.indexOf('=');
    if (equal > 0 && names.includes(trimmed.slice(0, equal)) && !/[\u0000-\u001f\u007f]/.test(trimmed)) {
      cookies.push(trimmed);
      names.splice(names.indexOf(trimmed.slice(0, equal)), 1);
    }
  }
  return cookies.join('; ');
}

export function clearServerCookies(origin: string): void {
  scopes.delete(origin);
}
