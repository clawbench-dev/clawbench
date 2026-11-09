/**
 * Per-viewer audio mixing graph for the live-share publisher.
 *
 * The publisher already holds one RTCPeerConnection per viewer and already
 * receives every viewer's microphone track. Mixing is therefore free: for each
 * viewer we build one destination carrying
 *
 *     system audio + publisher mic + every OTHER viewer's track
 *
 * and feed it to that viewer's sender. Excluding the viewer's own track is the
 * whole point — including it would send their voice back to them, and (via the
 * publisher's speakers) re-record it into the mix, producing unbounded echo.
 *
 * Kept free of Vue and of DOM events so it can be exercised against an
 * OfflineAudioContext in tests. The caller supplies the AudioContext.
 */

/** What a mixer needs from one viewer. */
export interface MixerViewerInput {
  /** The viewer's own microphone, as received from its PeerConnection. */
  stream: MediaStream | null
}

interface ViewerSlot {
  /** Destination fed to this viewer's sender. Created once, never replaced. */
  dest: MediaStreamAudioDestinationNode
  /** Source node for this viewer's inbound audio (null until ontrack fires). */
  remoteSource: MediaStreamAudioSourceNode | null
  /** Keeps that inbound audio actually rendering in Chromium (see below). */
  sink: RemoteAudioSink | null
  /** The track currently attached to the viewer's sender. */
  outTrack: MediaStreamTrack
}

/** Something to release once a remote track is no longer routed anywhere. */
export interface RemoteAudioSink {
  dispose(): void
}

/**
 * A silent element that keeps one remote track's audio flowing.
 *
 * Chromium does not render an inbound audio track into a Web Audio graph by
 * itself: a `MediaStreamAudioSourceNode` fed from a remote track stays silent
 * until that track is ALSO attached to a media element. The element does not
 * have to be in the document, playing, or unmuted — attaching the track is the
 * whole trigger — so this is a muted, detached sink, not a second playback
 * path.
 *
 * Without it the publisher hears nobody while every viewer hears the publisher:
 * the mix sent DOWN is built from local sources, which do render. Measured on
 * Chromium: remote track → `createMediaStreamSource` alone reads 0.000 RMS
 * while inbound RTP bytes climb; adding this sink makes the same node read the
 * real level. Firefox and WebKit render without it, so it is a no-op there.
 */
export function openRemoteAudioSink(stream: MediaStream): RemoteAudioSink {
  if (typeof document === 'undefined') return { dispose: () => undefined }
  const el = document.createElement('audio')
  el.muted = true
  try {
    el.srcObject = stream
  } catch {
    return { dispose: () => undefined }
  }
  return {
    dispose: () => {
      try {
        el.srcObject = null
      } catch {
        /* already detached */
      }
    },
  }
}

/**
 * RMS of one waveform window, 0..1.
 *
 * The bytes are unsigned 8-bit samples centred on 128 (Web Audio's
 * `getByteTimeDomainData`), so the value is scaled back to [-1, 1] first.
 * Exported so the arithmetic is testable without an AudioContext.
 */
export function rmsLevel(bytes: Uint8Array): number {
  if (bytes.length === 0) return 0
  let sum = 0
  for (let i = 0; i < bytes.length; i++) {
    const v = (bytes[i] - 128) / 128
    sum += v * v
  }
  return Math.sqrt(sum / bytes.length)
}

export class LiveMixer {
  private readonly ctx: AudioContext
  private systemSource: MediaStreamAudioSourceNode | null
  /** The stream `systemSource` was built from, so a repeat call is a no-op. */
  private systemStream: MediaStream | null
  private micSource: MediaStreamAudioSourceNode | null
  /** The stream `micSource` was built from; same no-op guard as the system one. */
  private micStream: MediaStream | null
  private readonly slots = new Map<string, ViewerSlot>()
  /**
   * Level meters, keyed by `v:<viewerId>` or `self`.
   *
   * One analyser per source, created lazily: an analyser only reads what flows
   * through it, so it can be tapped off the same node that already feeds the
   * mix without changing the audio.
   */
  private readonly analysers = new Map<string, AnalyserNode>()
  private levelBuf = new Uint8Array(256)

  constructor(ctx: AudioContext, systemStream: MediaStream | null, micStream: MediaStream | null) {
    this.ctx = ctx
    // A missing track is normal (no system audio on macOS, or the user turned
    // the microphone off). The graph simply has fewer inputs.
    this.systemStream =
      systemStream && systemStream.getAudioTracks().length > 0 ? systemStream : null
    this.systemSource = this.systemStream ? ctx.createMediaStreamSource(this.systemStream) : null
    this.micStream = micStream && micStream.getAudioTracks().length > 0 ? micStream : null
    this.micSource = this.micStream ? ctx.createMediaStreamSource(this.micStream) : null
    if (this.micSource) this.tap('self', this.micSource)
  }

  /**
   * Swap the system-audio input mid-meeting.
   *
   * Meetings can start or stop a screen share at any time, and the loopback
   * track arrives with the screen capture — so the input the graph was built
   * with is not necessarily the one it needs a minute later. Rewiring the
   * source node is enough: the per-viewer destinations are stable.
   *
   * Passing the same stream twice is a no-op, so callers can be liberal.
   */
  setSystemStream(stream: MediaStream | null): void {
    const next = stream && stream.getAudioTracks().length > 0 ? stream : null
    if (next === this.systemStream) return
    const previous = this.systemSource
    this.systemStream = next
    this.systemSource = next ? this.ctx.createMediaStreamSource(next) : null
    if (this.systemSource) {
      for (const slot of this.slots.values()) this.systemSource.connect(slot.dest)
    }
    if (previous) {
      // Drops every edge, including the per-viewer ones just replaced.
      try {
        previous.disconnect()
      } catch {
        /* already disconnected */
      }
    }
  }

  /**
   * Swap the microphone input mid-meeting.
   *
   * A machine with a headset and a built-in mic: the host picks one before the
   * meeting and may change their mind once the headset is on. Same shape as
   * setSystemStream — the per-viewer destinations are stable, only the source
   * node is replaced — plus the level meter, which is an analyser tapped off
   * that source and therefore cannot be moved: it is rebuilt against the new
   * one, otherwise the meter freezes at the last level of the old input.
   *
   * Passing the same stream twice is a no-op, so callers can be liberal.
   */
  setMicStream(stream: MediaStream | null): void {
    const next = stream && stream.getAudioTracks().length > 0 ? stream : null
    if (next === this.micStream) return
    const previous = this.micSource
    this.micStream = next
    this.micSource = next ? this.ctx.createMediaStreamSource(next) : null
    if (this.micSource) {
      for (const slot of this.slots.values()) this.micSource.connect(slot.dest)
    }
    this.analysers.delete('self')
    if (previous) {
      // Drops every edge, including the one into the discarded analyser.
      try {
        previous.disconnect()
      } catch {
        /* already disconnected */
      }
    }
    if (this.micSource) this.tap('self', this.micSource)
  }

  /** Tap a node for level metering; a context without analysers is fine. */
  private tap(key: string, node: AudioNode): void {
    if (this.analysers.has(key)) return
    if (typeof this.ctx.createAnalyser !== 'function') return
    const analyser = this.ctx.createAnalyser()
    analyser.fftSize = 256
    node.connect(analyser)
    this.analysers.set(key, analyser)
  }

  /** Current microphone level (0..1) of one viewer, 0 when not speaking. */
  level(viewerId: string): number {
    return this.readLevel(`v:${viewerId}`)
  }

  /** Current microphone level (0..1) of the publisher. */
  selfLevel(): number {
    return this.readLevel('self')
  }

  private readLevel(key: string): number {
    const analyser = this.analysers.get(key)
    if (!analyser) return 0
    if (this.levelBuf.length !== analyser.fftSize) {
      this.levelBuf = new Uint8Array(analyser.fftSize)
    }
    analyser.getByteTimeDomainData(this.levelBuf)
    return rmsLevel(this.levelBuf)
  }

  /** Number of viewers currently in the graph (for tests and diagnostics). */
  get size(): number {
    return this.slots.size
  }

  /** The destination track for a viewer, or null when unknown. */
  outTrackFor(viewerId: string): MediaStreamTrack | null {
    return this.slots.get(viewerId)?.dest.stream.getAudioTracks()[0] ?? null
  }

  /**
   * Add a viewer and return the track to attach to its sender.
   *
   * Idempotent: adding the same id twice returns the existing track rather than
   * building a second destination (a duplicate destination would double the
   * viewer's audio and leak a node).
   */
  addViewer(viewerId: string): MediaStreamTrack {
    const existing = this.slots.get(viewerId)
    if (existing) return existing.outTrack

    const dest = this.ctx.createMediaStreamDestination()
    if (this.systemSource) this.systemSource.connect(dest)
    if (this.micSource) this.micSource.connect(dest)

    // Feed every OTHER viewer's audio into this destination. Viewers added
    // later are wired up in attachRemote() below.
    for (const [otherId, slot] of this.slots) {
      if (otherId === viewerId || !slot.remoteSource) continue
      slot.remoteSource.connect(dest)
    }

    const outTrack = dest.stream.getAudioTracks()[0]
    this.slots.set(viewerId, { dest, remoteSource: null, sink: null, outTrack })
    return outTrack
  }

  /**
   * Attach a viewer's inbound audio so the OTHER viewers can hear it.
   *
   * Called from `ontrack`. The viewer's own destination is deliberately not
   * connected — that exclusion is the anti-echo invariant.
   */
  attachRemote(viewerId: string, stream: MediaStream): void {
    const slot = this.slots.get(viewerId)
    if (!slot) return
    if (stream.getAudioTracks().length === 0) return

    if (slot.remoteSource) {
      // Re-attaching (a renegotiation delivered a new stream): drop the old
      // source so the graph does not accumulate dead nodes.
      this.detachRemote(viewerId)
    }
    const src = this.ctx.createMediaStreamSource(stream)
    slot.remoteSource = src
    // Chromium needs the track attached to a media element before it renders
    // into the graph at all; see openRemoteAudioSink.
    slot.sink = openRemoteAudioSink(stream)
    this.tap(`v:${viewerId}`, src)
    for (const [otherId, other] of this.slots) {
      if (otherId === viewerId) continue
      src.connect(other.dest)
    }
    // The publisher must hear the audience too, otherwise "allow viewers to
    // speak" is a one-way announcement rather than a conversation. This is the
    // ONLY edge into ctx.destination: the publisher's own system audio and mic
    // must never be monitored, or the publisher hears itself with a delay.
    src.connect(this.ctx.destination)
  }

  /** Stop routing a viewer's inbound audio to everyone else. */
  detachRemote(viewerId: string): void {
    this.analysers.delete(`v:${viewerId}`)
    const slot = this.slots.get(viewerId)
    if (!slot) return
    slot.sink?.dispose()
    slot.sink = null
    if (!slot.remoteSource) return
    // disconnect() with no argument drops every edge, including the monitor
    // edge to ctx.destination added in attachRemote.
    try {
      slot.remoteSource.disconnect()
    } catch {
      /* already disconnected */
    }
    slot.remoteSource = null
  }

  /**
   * Remove a viewer entirely: unroute its audio, disconnect its destination
   * from the shared inputs and forget its slot.
   *
   * Without the destination disconnect the node stays wired to the system/mic
   * sources and keeps producing silence forever.
   */
  removeViewer(viewerId: string): void {
    const slot = this.slots.get(viewerId)
    if (!slot) return

    // Drop this viewer's own inbound audio from everyone else's mix.
    this.detachRemote(viewerId)

    // ...and drop everyone ELSE's audio from this viewer's destination. That
    // edge was created in attachRemote (and in addViewer for viewers already
    // present), so leaving it would keep the removed destination alive — still
    // pulled by N-1 source nodes, still producing a stream nobody reads.
    for (const [otherId, other] of this.slots) {
      if (otherId === viewerId || !other.remoteSource) continue
      try {
        other.remoteSource.disconnect(slot.dest)
      } catch {
        /* not connected */
      }
    }

    try {
      if (this.systemSource) this.systemSource.disconnect(slot.dest)
    } catch {
      /* not connected */
    }
    try {
      if (this.micSource) this.micSource.disconnect(slot.dest)
    } catch {
      /* not connected */
    }
    try {
      slot.dest.disconnect()
    } catch {
      /* not connected */
    }
    this.slots.delete(viewerId)
  }

  /** Tear the whole graph down. */
  close(): void {
    for (const id of [...this.slots.keys()]) this.removeViewer(id)
  }
}
