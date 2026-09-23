/**
 * Pure utility functions for port forwarding logic.
 * Extracted from usePortForward for testability.
 */

/** Port forwarding direction. `forward` = ssh -L (server → local), `reverse` = ssh -R (local → server). */
export type PortDirection = 'forward' | 'reverse'

export interface ForwardedPort {
  port: number
  localPort: number
  host: string
  name: string
  protocol: string
  /** Omitted by older backends; treat as 'forward'. */
  direction?: PortDirection
  active: boolean
  enabled: boolean
}

/** True when the port is an ssh -R mapping (local service exposed to the server). */
export function isReversePort(p: ForwardedPort): boolean {
  return p.direction === 'reverse'
}

/**
 * Returns the subset of ports that are enabled (user-controlled forwarding active).
 * Disabled ports are excluded from tunnel health determination.
 */
export function enabledPorts(ports: ForwardedPort[]): ForwardedPort[] {
  return ports.filter(p => p.enabled)
}

/**
 * Check if any enabled port has an active backend.
 */
export function hasActivePort(ports: ForwardedPort[]): boolean {
  return enabledPorts(ports).some(p => p.active)
}

/**
 * Determines tunnel status from port state.
 * `hasPorts` indicates whether there are any enabled registered ports.
 * When there are enabled ports but none are active, the tunnel is degraded.
 * When there are no enabled ports, or at least one is active, the tunnel is OK.
 */
export function tunnelStatusFromPorts(ports: ForwardedPort[]): 'ok' | 'degraded' {
  const hasPorts = enabledPorts(ports).length > 0
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

/**
 * Build the server-side address of a reverse mapping, for the user to reach from
 * a shell on the server host (e.g. `curl http://127.0.0.1:9000`).
 *
 * Always 127.0.0.1: reverse mappings bind the server's loopback only, so the
 * address is only meaningful on the server itself. Omits the port when it is the
 * default for the protocol.
 */
export function buildServerAddress(serverPort: number, protocol?: string): string {
  const scheme = protocol === 'https' ? 'https' : 'http'
  if ((scheme === 'http' && serverPort === 80) || (scheme === 'https' && serverPort === 443)) {
    return `${scheme}://127.0.0.1`
  }
  return `${scheme}://127.0.0.1:${serverPort}`
}

/** Result of sshInstallHint: how to obtain a local `ssh` client per OS. */
export type SshInstallHint =
  | { kind: 'windows'; url: string }              // Download page
  | { kind: 'mac'; noInstall: true }              // Preinstalled
  | { kind: 'linux'; command: string }            // Package-manager install command
  | null                                          // Unknown platform — no hint

const WINDOWS_OPENSSH_URL = 'https://learn.microsoft.com/windows-server/administration/openssh/openssh_install_firstuse'

/**
 * Resolves the platform-specific hint for getting an `ssh` client on the machine
 * that runs the manual SSH tunnel command (web mode only; tunnel guide is hidden
 * in app mode). Pass the static UA booleans so the function stays pure/testable:
 *   windows → official OpenSSH download/install page
 *   mac     → OpenSSH is bundled with macOS, nothing to install
 *   linux   → distro-agnostic apt/yum fallback install command
 */
export function sshInstallHint(opts: {
  windows: boolean
  macDesktop: boolean
  linuxDesktop: boolean
}): SshInstallHint {
  if (opts.windows) return { kind: 'windows', url: WINDOWS_OPENSSH_URL }
  if (opts.macDesktop) return { kind: 'mac', noInstall: true }
  if (opts.linuxDesktop) return { kind: 'linux', command: 'sudo apt install openssh-client || sudo yum install openssh-clients' }
  return null
}
