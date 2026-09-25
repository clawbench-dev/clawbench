package com.clawbench.app.tunnel;

import androidx.annotation.Nullable;

import java.util.concurrent.atomic.AtomicReference;

import okhttp3.MediaType;
import okhttp3.RequestBody;
import okio.BufferedSink;

/**
 * A request body that hands its sink to the caller instead of writing to it.
 *
 * <p>This is the load-bearing piece of the whole transport. OkHttp's
 * {@code CallServerInterceptor} branches on {@link #isDuplex()}:
 *
 * <pre>
 *   if (isDuplex) { exchange.flushRequest(); sink = exchange.createRequestBody(req, true);
 *                   body.writeTo(sink); /* no finishRequest() *&#47; }
 * </pre>
 *
 * <p>With {@code isDuplex() == false} the interceptor treats the body as
 * finite: it writes it and calls {@code finishRequest()} before it even looks
 * for a response, so {@code execute()} cannot return until the request body
 * ends. A tunnel stream's request body ends only when the forwarded TCP
 * connection closes, so a non-duplex body makes the tunnel strictly
 * half-duplex — the response direction would never be observed while the
 * request direction is open.
 *
 * <p>Returning {@code true} and having {@link #writeTo} publish the sink and
 * return immediately lets {@code execute()} proceed to read the response
 * headers, which is what makes the two directions independent. Measured on this
 * project: with a body that blocks in {@code writeTo}, response headers arrive
 * only after the body closes (~2000ms in the probe); with an immediate return
 * they arrive in ~2ms.
 *
 * <p>{@link #isOneShot()} is also true: the sink is a live connection, so a
 * transparent retry would send a fresh request with an already-consumed body.
 */
final class DuplexRequestBody extends RequestBody {

    private static final MediaType OCTET_STREAM = MediaType.parse("application/octet-stream");

    /**
     * The h2 sink for the request direction. Written by OkHttp on the thread
     * running {@code execute()}, read by the caller afterwards — the
     * happens-before edge comes from the caller waiting for {@code execute()}
     * to return.
     */
    private final AtomicReference<BufferedSink> sink = new AtomicReference<>();

    @Nullable
    @Override
    public MediaType contentType() {
        return OCTET_STREAM;
    }

    /**
     * Publish the sink and return. Deliberately does NOT write anything and
     * does NOT close the sink: the sink stays open for the lifetime of the
     * forwarded connection, and {@code sink.close()} later is the half-close
     * (END_STREAM) signal.
     */
    @Override
    public void writeTo(BufferedSink sink) {
        this.sink.set(sink);
    }

    @Override
    public boolean isDuplex() {
        return true;
    }

    @Override
    public boolean isOneShot() {
        return true;
    }

    /** The published sink, or {@code null} before {@code writeTo} has run. */
    @Nullable
    BufferedSink sink() {
        return sink.get();
    }
}
