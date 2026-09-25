package com.clawbench.app.tunnel;

import androidx.annotation.Nullable;

import okhttp3.HttpUrl;

/**
 * The server endpoint a tunnel session connects to, parsed from the same URL
 * the WebView is pointed at.
 *
 * <p>The tunnel only needs host and port from it. The scheme is deliberately
 * <em>not</em> used to pick a transport: the design doc's priority chain probes
 * h2-over-TLS first and h2c second regardless of what the page loaded over
 * (§2.3), so an {@code http://} instance still pays one fast TLS rejection on
 * the first connect and then remembers h2c.
 */
final class ServerTarget {

    final String host;
    final int port;
    /** The original URL, used verbatim to read the WebView's cookie jar. */
    final String originalUrl;

    private ServerTarget(String host, int port, String originalUrl) {
        this.host = host;
        this.port = port;
        this.originalUrl = originalUrl;
    }

    /**
     * Parse a server URL. Returns {@code null} when it has no usable host/port
     * — the caller reports that as a configuration error rather than guessing.
     */
    @Nullable
    static ServerTarget parse(String serverUrl) {
        if (serverUrl == null || serverUrl.trim().isEmpty()) return null;
        HttpUrl url = HttpUrl.parse(serverUrl.trim());
        if (url == null || url.host().isEmpty()) return null;
        return new ServerTarget(url.host(), url.port(), serverUrl.trim());
    }

    /** A base URL for this target on the given transport's scheme. */
    HttpUrl.Builder base(TransportKind kind) {
        return new HttpUrl.Builder()
                .scheme(kind.scheme())
                .host(host)
                .port(port);
    }

    @Override
    public String toString() {
        return host + ":" + port;
    }
}
