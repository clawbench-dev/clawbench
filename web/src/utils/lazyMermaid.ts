/**
 * Lazy-loaded mermaid singleton.
 *
 * mermaid.core (608KB) is split into a separate chunk via dynamic import().
 * It is only loaded when getMermaid() is called at runtime — i.e., when a
 * chat message contains a mermaid diagram.
 */

let _mermaid: typeof import('mermaid').default | null = null
let _mermaidPending: Promise<typeof import('mermaid').default> | null = null

const DEFAULT_ATTEMPTS = 3

/**
 * Invoke an async loader, retrying on failure.
 *
 * The mermaid chunk is fetched via dynamic import() over the network (or an SSH
 * tunnel). Transient fetch failures (timeouts, flaky tunnels, chunk still
 * propagating) can make that import reject once even though it would succeed a
 * moment later. Retrying lets the load self-heal before surfacing an error to
 * the UI.
 */
export async function retryableImport<T>(loader: () => Promise<T>, attempts: number = DEFAULT_ATTEMPTS): Promise<T> {
    let lastErr: unknown
    for (let i = 0; i < attempts; i++) {
        try {
            return await loader()
        } catch (err) {
            lastErr = err
        }
    }
    throw lastErr
}

export async function getMermaid() {
    if (_mermaid) return _mermaid
    if (_mermaidPending) return _mermaidPending
    _mermaidPending = retryableImport(() => import('mermaid')).then(mod => {
        _mermaid = mod.default
        _mermaidPending = null
        return _mermaid
    }).catch(err => {
        _mermaidPending = null
        throw err
    })
    return _mermaidPending
}
