import { powerSaveBlocker } from 'electron'

let id: number | null = null
export function setKeepScreenOnImpl(on: boolean): void {
  if (on && id === null) id = powerSaveBlocker.start('prevent-display-sleep')
  else if (!on && id !== null) { powerSaveBlocker.stop(id); id = null }
}
