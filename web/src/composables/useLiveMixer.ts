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
  /** The track currently attached to the viewer's sender. */
  outTrack: MediaStreamTrack
}

export class LiveMixer {
  private readonly ctx: AudioContext
  private readonly systemSource: MediaStreamAudioSourceNode | null
  private readonly micSource: MediaStreamAudioSourceNode | null
  private readonly slots = new Map<string, ViewerSlot>()

  constructor(ctx: AudioContext, systemStream: MediaStream | null, micStream: MediaStream | null) {
    this.ctx = ctx
    // A missing track is normal (no system audio on macOS, or the user turned
    // the microphone off). The graph simply has fewer inputs.
    this.systemSource =
      systemStream && systemStream.getAudioTracks().length > 0
        ? ctx.createMediaStreamSource(systemStream)
        : null
    this.micSource =
      micStream && micStream.getAudioTracks().length > 0
        ? ctx.createMediaStreamSource(micStream)
        : null
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
    this.slots.set(viewerId, { dest, remoteSource: null, outTrack })
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
    const slot = this.slots.get(viewerId)
    if (!slot?.remoteSource) return
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
