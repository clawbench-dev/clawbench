import { ref } from 'vue'

/**
 * Whether the connected server was built with FRP support.
 *
 * This is a property of the SERVER BINARY, not of user configuration: a build
 * compiled with the `nofrp` tag excludes the frp client library entirely (it is
 * flagged by endpoint security products as Greyware/riskware). On such a build
 * the FRP settings panel must be hidden rather than shown as a switch that can
 * never take effect.
 *
 * Defaults to `true` so that a UI rendered before the first `/api/config`
 * response — or while the server is unreachable — keeps its existing shape
 * instead of briefly hiding the panel on every load. The value only ever
 * narrows, and only once the server has actually told us.
 *
 * A leaf module (no imports beyond `vue`) on purpose: it is read by the
 * settings index, the settings category renderer, and written by
 * useSettingsConfig, and routing that through a composable would risk an import
 * cycle between those.
 */
export const frpAvailable = ref(true)

/** Update the cached availability. Called by useSettingsConfig after /api/config loads. */
export function setFrpAvailable(available: boolean): void {
  frpAvailable.value = available
}
