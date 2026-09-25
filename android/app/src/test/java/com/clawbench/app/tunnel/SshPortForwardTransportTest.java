package com.clawbench.app.tunnel;

import com.jcraft.jsch.JSchException;
import com.jcraft.jsch.Session;

import org.junit.Before;
import org.junit.Test;

import java.util.concurrent.atomic.AtomicBoolean;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.doNothing;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for {@link SshPortForwardTransport}.
 *
 * <p>The point of this adapter is that it changes nothing: every method must
 * issue exactly the JSch call the service used to make inline, with the same
 * "already registered means success" handling and the same dead-session
 * short-circuit. These tests pin that contract so the h2 tunnel can be added
 * without the SSH path drifting.
 */
public class SshPortForwardTransportTest {

    private Session session;
    private SshPortForwardTransport transport;
    private final AtomicBoolean reachable = new AtomicBoolean(true);

    @Before
    public void setUp() {
        session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        transport = new SshPortForwardTransport(() -> session, port -> reachable.get());
    }

    @Test
    public void addLocal_issuesTheLoopbackForward() throws Exception {
        transport.addLocal(3080, 80, "127.0.0.1");
        verify(session).setPortForwardingL("127.0.0.1", 3080, "127.0.0.1", 80);
    }

    @Test
    public void addLocal_alreadyRegisteredIsTreatedAsSuccess() throws Exception {
        // ensureConnection's replay loop may have set the forward up before the
        // explicit add ran; JSch throws, and the pre-tunnel code swallowed it.
        doThrow(new JSchException("PortForwardingL: local port 3080 is already registered"))
                .when(session).setPortForwardingL(anyString(), anyInt(), anyString(), anyInt());

        transport.addLocal(3080, 80, "127.0.0.1");
        // Reaching here without an exception IS the assertion.
    }

    @Test
    public void addLocal_otherJschErrorsPropagate() {
        try {
            doThrow(new JSchException("channel open failure"))
                    .when(session).setPortForwardingL(anyString(), anyInt(), anyString(), anyInt());
            transport.addLocal(3080, 80, "127.0.0.1");
            org.junit.Assert.fail("a genuine failure must reach the caller");
        } catch (Exception expected) {
            // The service removes the port and reports failure.
        }
    }

    @Test
    public void addLocal_withoutASessionFails() {
        transport = new SshPortForwardTransport(() -> null, port -> false);
        try {
            transport.addLocal(3080, 80, "127.0.0.1");
            org.junit.Assert.fail("no session means the add must fail");
        } catch (Exception expected) {
            // expected
        }
    }

    @Test
    public void removeLocal_delegatesWhenTheSessionIsUp() throws Exception {
        transport.removeLocal(3080);
        verify(session).delPortForwardingL(3080);
    }

    @Test
    public void removeLocal_skipsADeadSession() throws Exception {
        // JSch throws "Session is down" if asked to unregister on a closed
        // session, and the pre-tunnel code deliberately skipped that call.
        when(session.isConnected()).thenReturn(false);
        transport.removeLocal(3080);
        verify(session, never()).delPortForwardingL(anyInt());
    }

    @Test
    public void removeLocal_withoutASessionDoesNotThrow() throws Exception {
        transport = new SshPortForwardTransport(() -> null, port -> false);
        transport.removeLocal(3080);
    }

    @Test
    public void addReverse_issuesTheRemoteForward() throws Exception {
        transport.addReverse(9000, 3000, "127.0.0.1");
        verify(session).setPortForwardingR("127.0.0.1", 9000, "127.0.0.1", 3000);
    }

    @Test
    public void addReverse_alreadyRegisteredIsTreatedAsSuccess() throws Exception {
        doThrow(new JSchException("PortForwardingR: remote port 9000 is already registered"))
                .when(session).setPortForwardingR(anyString(), anyInt(), anyString(), anyInt());
        transport.addReverse(9000, 3000, "127.0.0.1");
    }

    @Test
    public void addReverse_otherJschErrorsPropagate() {
        try {
            doThrow(new JSchException("remote port forwarding failed for listen port 9000"))
                    .when(session).setPortForwardingR(anyString(), anyInt(), anyString(), anyInt());
            transport.addReverse(9000, 3000, "127.0.0.1");
            org.junit.Assert.fail("a genuine failure must reach the caller");
        } catch (Exception expected) {
            // expected
        }
    }

    @Test
    public void removeReverse_delegatesWhenTheSessionIsUp() throws Exception {
        transport.removeReverse(9000);
        verify(session).delPortForwardingR(9000);
    }

    @Test
    public void removeReverse_skipsADeadSession() throws Exception {
        when(session.isConnected()).thenReturn(false);
        transport.removeReverse(9000);
        verify(session, never()).delPortForwardingR(anyInt());
    }

    @Test
    public void isConnected_reflectsTheSession() {
        assertTrue(transport.isConnected());
        when(session.isConnected()).thenReturn(false);
        assertFalse(transport.isConnected());
    }

    @Test
    public void isLocalReachable_delegatesToTheProbe() {
        assertTrue(transport.isLocalReachable(3080));
        reachable.set(false);
        assertFalse(transport.isLocalReachable(3080));
    }

    @Test
    public void close_doesNotDisconnectTheSession() {
        // The session's lifecycle belongs to BackgroundService: disconnecting
        // here would fight the reconnect monitor and null the field's owner.
        doNothing().when(session).disconnect();
        transport.close();
        verify(session, never()).disconnect();
    }
}
