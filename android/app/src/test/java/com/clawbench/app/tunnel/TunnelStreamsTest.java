package com.clawbench.app.tunnel;

import org.junit.After;
import org.junit.Test;

import javax.net.ssl.SSLContext;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertSame;

/**
 * Tests for the process-wide transport holder.
 *
 * <p>The single instance is a correctness requirement, not a cache: the control
 * stream and every data stream must share one {@code OkHttpClient} so they land
 * on one h2 connection, because a {@code -R} claim token is scoped to the
 * connection that minted it.
 */
public class TunnelStreamsTest {

    @After
    public void tearDown() {
        TunnelStreams.reset();
        TunnelStreams.setPlatformForTesting(null);
    }

    @Test
    public void get_returnsTheSameInstanceEveryTime() {
        TunnelStreams.setPlatformForTesting(new NoopPlatform());
        TunnelStream first = TunnelStreams.get();
        TunnelStream second = TunnelStreams.get();
        assertNotNull(first);
        assertSame("all callers must share one transport", first, second);
    }

    @Test
    public void reset_buildsAFreshTransport() {
        TunnelStreams.setPlatformForTesting(new NoopPlatform());
        TunnelStream first = TunnelStreams.get();
        TunnelStreams.reset();
        TunnelStream second = TunnelStreams.get();
        assertNotSame("reset must drop the old transport", first, second);
    }

    @Test
    public void reset_isSafeWhenNothingWasCreated() {
        TunnelStreams.reset();
        TunnelStreams.reset();
    }

    @Test
    public void newTransport_startsDisconnected() {
        TunnelStreams.setPlatformForTesting(new NoopPlatform());
        TunnelStream transport = TunnelStreams.get();
        assertFalse(transport.isConnected());
        assertFalse(transport.hasActiveStreams());
    }

    private static final class NoopPlatform implements TunnelPlatform {
        @Override
        public String sessionCookie(String serverUrl) {
            return null;
        }

        @Override
        public SSLContext trustAllSslContext() {
            return null;
        }

        @Override
        public void log(String level, String message) {
        }

        @Override
        public void log(String level, String message, Throwable error) {
        }
    }
}
