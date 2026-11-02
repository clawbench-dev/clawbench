import { ref, type Ref } from 'vue'

/**
 * Transfer-rate meter driven by cumulative byte reports.
 *
 * Shared by downloads (`useDownloadProgress`) and directory uploads
 * (`useFileUpload`) so both progress bars show a rate computed the same way.
 *
 * The rate is averaged over a sliding window rather than measured between two
 * consecutive events: both native download hosts throttle progress reports to
 * ~1% of the file, so on a slow link two events can be many seconds apart and
 * an instant delta would swing wildly. Keeping one sample older than the window
 * as a baseline means sparse reports still produce a rate instead of 0.
 *
 * No timer: the meter only advances when the caller reports bytes, so it costs
 * nothing while idle.
 */
export interface TransferSpeedMeter {
  /** Bytes per second over the recent window; 0 until two samples exist. */
  speed: Ref<number>
  /** Start a new transfer at `received` bytes (default 0). */
  reset(received?: number): void
  /** Record a cumulative byte count for the running transfer. */
  sample(received: number): void
}

interface SpeedSample {
  t: number
  received: number
}

export function createTransferSpeedMeter(windowMs = 3000): TransferSpeedMeter {
  const speed = ref(0)
  let samples: SpeedSample[] = []

  function reset(received = 0): void {
    samples = [{ t: Date.now(), received }]
    speed.value = 0
  }

  function sample(received: number): void {
    const now = Date.now()
    // A restarted or re-pointed transfer must not produce a negative rate.
    if (samples.length > 0 && received < samples[samples.length - 1].received) {
      samples = []
    }
    samples.push({ t: now, received })
    while (samples.length > 2 && samples[1].t < now - windowMs) {
      samples.shift()
    }
    if (samples.length < 2) {
      speed.value = 0
      return
    }
    const first = samples[0]
    const last = samples[samples.length - 1]
    const elapsedSec = (last.t - first.t) / 1000
    speed.value = elapsedSec > 0
      ? Math.max(0, (last.received - first.received) / elapsedSec)
      : 0
  }

  reset()
  return { speed, reset, sample }
}
