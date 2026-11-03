/**
 * Whether this machine can actually apply remote-control events.
 *
 * The bridge answers it (the desktop main process knows whether the injector
 * helper exists and whether the platform has an implementation at all). The
 * publisher UI must ask before offering "allow": on Linux and macOS the helper
 * is a stub, so granting a request would leave the viewer clicking into a void
 * with nothing on screen explaining why.
 *
 * Defaults to `false` on every uncertainty — a missing bridge, a throw, a
 * non-boolean answer. Offering a control that cannot work is worse than not
 * offering it.
 */
export interface RemoteControlBridge {
  remoteControlSupported?: () => Promise<boolean> | boolean
}

export async function resolveRemoteControlSupport(
  bridge: RemoteControlBridge | null | undefined,
): Promise<boolean> {
  if (!bridge?.remoteControlSupported) return false
  try {
    return (await bridge.remoteControlSupported()) === true
  } catch {
    return false
  }
}
