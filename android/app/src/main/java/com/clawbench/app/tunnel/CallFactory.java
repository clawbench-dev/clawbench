package com.clawbench.app.tunnel;

import okhttp3.Call;
import okhttp3.Request;

/**
 * Creates {@link Call}s for the transport.
 *
 * <p>Identical in shape to OkHttp's own {@code Call.Factory} (and deliberately
 * source-compatible with it), but declared here so the test seam in
 * {@link H2TunnelStream} does not force tests to build a real
 * {@code OkHttpClient}. Production never supplies one; the transport creates
 * calls through its shared client.
 */
public interface CallFactory {
    Call newCall(Request request);
}
