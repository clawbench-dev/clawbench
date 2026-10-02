/**
 * Transport annotation for tunnel status messages.
 *
 * The tunnel can be carried by the legacy SSH wire or the h2 stream tunnel.
 * Status copy is written transport-neutrally and, when the wire is actually
 * known, gets a parenthetical annotation (e.g. `隧道未连接（SSH）`).
 *
 * Kept in its own module rather than `portForwardUtils.ts` on purpose: the
 * port-forward tests hand-roll a partial mock of `portForwardUtils`, so adding
 * an export there would force every one of those mocks to be updated.
 */

/** i18n key of the human label for a single, concrete wire. */
const TRANSPORT_LABEL_KEYS: Record<string, string> = {
  ssh: 'proxy.transportSsh',
  h2: 'proxy.transportH2',
}

/**
 * Resolve the i18n label key for a transport value, or `''` when no single wire
 * is known.
 *
 * `''` (unknown — web mode, or a host that predates the bridge methods) yields
 * `''`: the caller then renders the neutral wording with no annotation rather
 * than guessing.
 */
export function transportLabelKey(transport: string): string {
  return TRANSPORT_LABEL_KEYS[transport] ?? ''
}
