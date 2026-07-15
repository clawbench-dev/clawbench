/**
 * Pure utility functions for port forwarding logic.
 * Extracted from usePortForward for testability.
 */

export interface ForwardedPort {
  port: number
  localPort: number
  host: string
  name: string
  protocol: string
  autoDetect: boolean
  active: boolean
}

/**
 * Check if any port in the list has an active backend.
 */
export function hasActivePort(ports: ForwardedPort[]): boolean {
  return ports.some(p => p.active)
}

/**
 * Determines tunnel status from port state.
 * `hasPorts` indicates whether there are any registered ports.
 * When there are ports but none are active, the tunnel is degraded.
 * When there are no ports, or at least one is active, the tunnel is OK.
 */
export function tunnelStatusFromPorts(ports: ForwardedPort[]): 'ok' | 'degraded' {
  const hasPorts = ports.length > 0
  const anyActive = hasActivePort(ports)
  if (hasPorts && !anyActive) return 'degraded'
  return 'ok'
}

/**
 * Build the URL for opening a forwarded port.
 * Uses localhost since it's the local listening address.
 * Omits the port number when it's the default for the protocol (80 for http, 443 for https).
 */
export function buildPortUrl(localPort: number, protocol?: string, path?: string): string {
  const scheme = protocol === 'https' ? 'https' : 'http'
  // Omit port if it's the default for the protocol
  if ((scheme === 'http' && localPort === 80) || (scheme === 'https' && localPort === 443)) {
    return `${scheme}://localhost${path || '/'}`
  }
  return `${scheme}://localhost:${localPort}${path || '/'}`
}
