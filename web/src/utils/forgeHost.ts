/**
 * Forge host classification for the bind dialog's pre-submit warning.
 *
 * The server accepts any host, including self-hosted instances on private
 * networks and internal names that never resolve. Binding one is a real
 * decision — it sends that host the stored credential — so the dialog warns
 * before the user commits to it.
 *
 * The warning must agree with the server's own classification, otherwise a
 * bind the server will not vouch for could look unremarkable in the UI. So
 * both helpers below mirror backend logic rather than inventing their own.
 */

/**
 * extractRemoteHost returns the host of a git remote URL, or '' when the input
 * is not a form the backend accepts.
 *
 * Mirrors splitRemote in internal/forge/remote.go: `http://`, `https://` and
 * `ssh://` URLs, plus the scp-like `git@host:path`. Anything else (a local path,
 * a bare host, a `file://` URL) yields ''.
 *
 * '' does NOT mean "safe to bind silently" — it means the backend will reject
 * the URL outright with its own error, so no host warning is owed on top.
 */
export function extractRemoteHost(raw: string): string {
    const trimmed = raw.trim()
    if (!trimmed) return ''

    // http://, https:// or ssh:// form.
    if (trimmed.includes('://')) {
        let parsed: URL
        try {
            parsed = new URL(trimmed)
        } catch {
            return ''
        }
        const scheme = parsed.protocol.replace(/:$/, '').toLowerCase()
        // http is accepted alongside https: a self-hosted instance on an
        // internal network is a supported target, so its remote must be
        // classifiable rather than silently treated as unparseable.
        if (scheme !== 'https' && scheme !== 'http' && scheme !== 'ssh') return ''
        // host (not hostname) keeps a non-default port, which matters below.
        return parsed.host
    }

    // scp-like form: [user@]host:path
    const at = trimmed.indexOf('@')
    if (at >= 0) {
        const rest = trimmed.slice(at + 1)
        const colon = rest.indexOf(':')
        // No colon, or an empty host before it.
        if (colon <= 0) return ''
        const host = rest.slice(0, colon)
        const path = rest.slice(colon + 1)
        if (!host || !path) return ''
        return host
    }

    return ''
}

/**
 * isOfficialForgeHost reports whether a host is exactly a platform-operated
 * host.
 *
 * Mirrors isOfficialForgeHost in internal/handler/forge_endpoints.go, including
 * its deliberate refusal to strip the port. The backend does not auto-bind
 * "github.com:8443" because the token would then go to
 * https://github.com:8443/api/v3; treating that as official here would hide the
 * warning for a bind the server itself declines to vouch for.
 */
export function isOfficialForgeHost(host: string): boolean {
    const normalized = host.trim().toLowerCase()
    return normalized === 'github.com' || normalized === 'gitlab.com'
}

/**
 * isNonOfficialRemote reports whether a raw remote URL points at a host that is
 * not github.com / gitlab.com. Unparseable input is NOT flagged: the backend
 * reports it as an invalid URL, and the dialog shows that error instead.
 */
export function isNonOfficialRemote(raw: string): boolean {
    const host = extractRemoteHost(raw)
    if (!host) return false
    return !isOfficialForgeHost(host)
}

/**
 * forgeRepoWebUrl builds the browser URL of a bound repository's home page.
 *
 * The binding carries everything needed to address it: host (identity, may
 * include a port), owner, repo and the resolved API scheme. Reusing the API
 * scheme keeps a self-hosted instance on plain HTTP reachable — assuming
 * https there would send the user to a URL the server does not serve.
 *
 * Returns '' when the binding is incomplete, so callers can hide the link
 * rather than render a URL that would 404.
 */
export function forgeRepoWebUrl(
    binding: { host?: string; owner?: string; repo?: string; scheme?: string } | null | undefined,
): string {
    if (!binding) return ''
    const host = (binding.host ?? '').trim()
    const owner = (binding.owner ?? '').trim()
    const repo = (binding.repo ?? '').trim()
    if (!host || !owner || !repo) return ''

    const scheme = (binding.scheme ?? '').trim().toLowerCase() === 'http' ? 'http' : 'https'
    // Per-segment escaping, not encodeURIComponent on the whole owner: GitLab
    // nests repositories in subgroups, and escaping the "/" would produce a
    // path the instance does not serve.
    const path = [owner, repo]
        .map(part => part.split('/').map(encodeURIComponent).join('/'))
        .join('/')
    return `${scheme}://${host}/${path}`
}
