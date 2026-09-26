package com.clawbench.app.tunnel;

import com.clawbench.app.tunnel.FakeSockets.Factory;
import com.clawbench.app.tunnel.FakeSockets.FakeServerSocket;
import com.clawbench.app.tunnel.FakeSockets.FakeSocket;
import com.clawbench.app.tunnel.FakeTunnelStream.FakeConnection;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.io.IOException;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for {@link H2PortForwardTransport} — the local {@code ServerSocket}
 * listener and the per-connection h2 stream pump.
 *
 * <p>No real sockets anywhere: a bare socket in a Robolectric test leaves the
 * Gradle worker un-exitable (measured while building T9). The assertions are
 * about behaviour — which listener exists, which stream was opened for which
 * target, whether the write half was closed — rather than about bytes on a wire.
 */
public class H2PortForwardTransportTest {

    private FakeTunnelStream tunnel;
    private Factory sockets;
    private H2PortForwardTransport transport;

    @Before
    public void setUp() {
        tunnel = new FakeTunnelStream();
        sockets = new Factory();
        transport = new H2PortForwardTransport(() -> tunnel, sockets, sockets.dialer);
    }

    @After
    public void tearDown() {
        transport.close();
    }

    // ==================================================================
    // Listener lifecycle
    // ==================================================================

    @Test
    public void addLocal_bindsLoopbackOnly() throws Exception {
        transport.addLocal(3080, 80, "127.0.0.1");

        assertEquals("exactly one listener", 1, sockets.created.size());
        FakeServerSocket server = sockets.created.get(0);
        assertTrue("listener must be bound", server.bound);
        assertEquals("must bind loopback, never 0.0.0.0",
                new java.net.InetSocketAddress("127.0.0.1", 3080), server.bindAddress);
        assertFalse("listener must stay open", server.closed.get());
        assertTrue(transport.isLocalReachable(3080));
    }

    @Test
    public void addLocal_doesNotOpenAStreamUntilAConnectionArrives() throws Exception {
        transport.addLocal(3080, 80, "127.0.0.1");

        // Binding a port is local work. Opening a stream eagerly would dial the
        // target on every add and make addPortForward block on a server
        // round-trip it does not need.
        assertEquals("no stream before a connection", 0, tunnel.openedHosts.size());
    }

    @Test
    public void addLocal_isIdempotentWhileTheListenerSurvives() throws Exception {
        transport.addLocal(3080, 80, "127.0.0.1");
        transport.addLocal(3080, 80, "127.0.0.1");

        // A reconnect replay re-adds every port. Binding a second time would
        // fail with EADDRINUSE and report a working forward as broken.
        assertEquals("must reuse the existing listener", 1, sockets.created.size());
    }

    @Test
    public void addLocal_rebindsAfterTheListenerWasClosed() throws Exception {
        transport.addLocal(3080, 80, "127.0.0.1");
        transport.removeLocal(3080);
        transport.addLocal(3080, 80, "127.0.0.1");

        assertEquals("a released port must be re-bindable", 2, sockets.created.size());
        assertTrue(transport.isLocalReachable(3080));
    }

    @Test
    public void addLocal_whenTunnelIsDown_failsWithoutBinding() {
        tunnel.connected = false;
        try {
            transport.addLocal(3080, 80, "127.0.0.1");
            org.junit.Assert.fail("expected the add to fail while the tunnel is down");
        } catch (Exception expected) {
            // The service relies on this to drop the port from its bookkeeping
            // instead of advertising a listener that cannot forward anything.
        }
        assertEquals("must not bind when the tunnel is down", 0, sockets.created.size());
        assertFalse(transport.isLocalReachable(3080));
    }

    @Test
    public void addLocal_bindFailureLeavesNoListenerRegistered() throws Exception {
        sockets.failNextBind = true;
        try {
            transport.addLocal(3080, 80, "127.0.0.1");
            org.junit.Assert.fail("expected the bind failure to propagate");
        } catch (IOException expected) {
            // expected
        }
        assertFalse("a failed bind must not be reported as reachable",
                transport.isLocalReachable(3080));
        assertTrue("the failed socket must be closed", sockets.created.get(0).closed.get());
        assertEquals("no listener may stay tracked", 0, transport.listenerCount());
    }

    @Test
    public void removeLocal_closesTheListenerAndIsIdempotent() throws Exception {
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        transport.removeLocal(3080);
        assertTrue("the listener must be closed", server.closed.get());
        assertFalse(transport.isLocalReachable(3080));
        assertEquals(0, transport.listenerCount());

        // A second remove (double tap, or disconnect after remove) must not
        // throw: the service calls it unconditionally.
        transport.removeLocal(3080);
    }

    @Test
    public void isLocalReachable_isFalseForUnknownPort() {
        assertFalse(transport.isLocalReachable(3080));
    }

    @Test
    public void isConnected_reflectsTheUnderlyingTunnel() {
        assertTrue(transport.isConnected());
        tunnel.connected = false;
        assertFalse(transport.isConnected());
    }

    // ==================================================================
    // Port validation (see the report on the validation asymmetry)
    // ==================================================================

    @Test
    public void addLocal_clearlyInvalidPort_propagatesAnUncheckedFailure() {
        // H2PortForwardTransport does not validate localPort; the range check
        // happens in InetSocketAddress, so an out-of-range port surfaces as an
        // unchecked IllegalArgumentException. The service's catch (Exception)
        // still drops the mapping, but the transport itself never reports a
        // TunnelException for it.
        for (int bad : new int[]{-1, 65536}) {
            try {
                transport.addLocal(bad, 80, "127.0.0.1");
                org.junit.Assert.fail("expected port " + bad + " to be rejected");
            } catch (IllegalArgumentException expected) {
                // From new InetSocketAddress(LOOPBACK, bad).
            } catch (Exception other) {
                org.junit.Assert.fail("expected an unchecked failure, got " + other);
            }
        }
        assertEquals("a rejected port must not register a listener",
                0, transport.listenerCount());
    }

    @Test
    public void addLocal_portZero_isAcceptedAsAnEphemeralRequest() throws Exception {
        // Pins current behaviour: the transport treats 0 like InetSocketAddress
        // does (an OS-assigned port). It is NOT rejected, which is a latent
        // problem for -L: the frontend reaches the forward at
        // localhost:{localPort}, and the server's registry never assigns 0
        // (allocateLocalPort remaps <1024 to 1024+), so a 0 here yields a
        // listener nothing can address. See the report.
        transport.addLocal(0, 80, "127.0.0.1");
        assertEquals(1, transport.listenerCount());
        assertTrue(transport.isLocalReachable(0));
    }

    // ==================================================================
    // accept -> openStream
    // ==================================================================

    @Test
    public void acceptLoopRunsOnTheTransportsOwnDaemonThread() throws Exception {
        // The single-threaded networkExecutor in BackgroundService serialises
        // ensureConnection/disconnect/WS-reconnect; parking it on accept() would
        // starve all of them. The transport must therefore run its own threads
        // (named H2Tunnel-* and daemon so an idle tunnel never pins the process).
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        server.accept(new FakeSocket("x"));
        tunnel.awaitOpenedStreams(1);

        Thread thread = server.acceptThread;
        assertTrue("the accept loop must run on a transport thread",
                thread != null && thread.getName().startsWith("H2Tunnel-"));
        assertTrue("the tunnel threads must be daemons", thread.isDaemon());
    }

    @Test
    public void acceptedConnection_opensStreamForTheResolvedTarget() throws Exception {
        transport.addLocal(3080, 80, "10.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        server.accept(new FakeSocket("hello"));
        tunnel.awaitOpenedStreams(1);

        assertEquals("one stream per accepted connection", 1, tunnel.openedHosts.size());
        assertEquals("the resolved target host must be used verbatim",
                "10.0.0.1", tunnel.openedHosts.get(0));
        assertEquals(Integer.valueOf(80), tunnel.openedPorts.get(0));
    }

    @Test
    public void acceptedConnection_pumpsLocalBytesIntoTheRequestDirection() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        server.accept(new FakeSocket("ping"));

        assertTrue(connection.awaitRequestBytes());
        assertEquals("ping", connection.requestBytes());
    }

    @Test
    public void acceptedConnection_pumpsResponseBytesBackToTheLocalSocket() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        FakeSocket socket = new FakeSocket("");
        server.accept(socket);
        // "" is an immediate EOF, so this also proves the stream was opened.
        assertTrue(connection.awaitCloseWrite());

        connection.pushResponse("pong");
        assertTrue(socket.awaitLocalBytes());
        assertEquals("pong", socket.localBytes());
    }

    @Test
    public void localEof_halfClosesInsteadOfClosingTheStream() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        // "hello" then EOF: the local peer is done sending but may still read.
        server.accept(new FakeSocket("hello"));

        assertTrue("EOF on the local socket must send END_STREAM",
                connection.awaitCloseWrite());
        assertFalse("a half-close must not cancel the whole stream", connection.isClosed());
    }

    @Test
    public void responseDirectionStaysUsableAfterHalfClose() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        FakeSocket socket = new FakeSocket("hello");
        server.accept(socket);
        assertTrue(connection.awaitCloseWrite());

        // The remote is allowed to keep replying after our END_STREAM.
        connection.pushResponse("late reply");
        assertTrue(socket.awaitLocalBytes());
        assertEquals("late reply", socket.localBytes());
    }

    @Test
    public void responseEof_tearsDownTheLocalSocket() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        FakeSocket socket = new FakeSocket("hello");
        server.accept(socket);
        assertTrue(connection.awaitRequestBytes());

        connection.endResponse();

        assertTrue("the local socket must be closed when the stream ends",
                socket.awaitClosed());
        assertTrue("the stream must be released", connection.isClosed());
    }

    @Test
    public void openStreamFailure_closesTheAcceptedSocket() throws Exception {
        transport.addLocal(3080, 80, "127.0.0.1");
        tunnel.failNextOpen = true;
        FakeServerSocket server = sockets.created.get(0);

        FakeSocket socket = new FakeSocket("hello");
        server.accept(socket);

        // The client is waiting on a connection that will never carry bytes;
        // leaving it open would hang the browser tab that dialed the port.
        assertTrue("the local socket must be dropped when the dial fails", socket.awaitClosed());
        assertEquals("no relay may stay tracked", 0, transport.activeRelayCount());
    }

    @Test
    public void removeLocal_closesInFlightConnectionsOnThatPort() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        FakeSocket socket = new FakeSocket("hello");
        server.accept(socket);
        assertTrue(connection.awaitRequestBytes());

        transport.removeLocal(3080);

        assertTrue("the in-flight local socket must be closed", socket.awaitClosed());
        assertTrue("the in-flight h2 stream must be cancelled", connection.isClosed());
    }

    @Test
    public void removeLocal_leavesOtherPortsConnectionsAlone() throws Exception {
        FakeConnection first = tunnel.queueConnection();
        FakeConnection second = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        transport.addLocal(3081, 81, "127.0.0.1");
        FakeServerSocket firstServer = sockets.created.get(0);
        FakeServerSocket secondServer = sockets.created.get(1);

        FakeSocket socketA = new FakeSocket("a");
        firstServer.accept(socketA);
        assertTrue(first.awaitRequestBytes());
        FakeSocket socketB = new FakeSocket("b");
        secondServer.accept(socketB);

        transport.removeLocal(3080);

        assertTrue(socketA.awaitClosed());
        assertFalse("a different port's connection must survive", socketB.closed.get());
        assertFalse("a different port's stream must survive", second.isClosed());
    }

    @Test
    public void close_releasesEveryListenerAndConnection() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        transport.addLocal(3081, 81, "127.0.0.1");
        FakeServerSocket first = sockets.created.get(0);
        FakeSocket socket = new FakeSocket("hello");
        first.accept(socket);
        assertTrue(connection.awaitRequestBytes());

        transport.close();

        assertTrue("every listener must be closed", sockets.created.get(0).closed.get());
        assertTrue(sockets.created.get(1).closed.get());
        assertTrue("in-flight sockets must be closed", socket.awaitClosed());
        assertTrue("in-flight streams must be cancelled", connection.isClosed());
        assertFalse(transport.isLocalReachable(3080));
        assertEquals(0, transport.listenerCount());
        assertTrue("close() must tear down the h2 session too", tunnel.closed);
    }

    @Test
    public void close_isIdempotentAndAllowsRebindAfterReconnect() throws Exception {
        transport.addLocal(3080, 80, "127.0.0.1");
        transport.close();
        transport.close();

        // close() tears the h2 session down too, so a bare re-add is correctly
        // refused: the service must reconnect first (ensureConnection). Once the
        // tunnel is back, the transport must be usable again — the executor was
        // shut down by close() and has to be rebuilt.
        try {
            transport.addLocal(3080, 80, "127.0.0.1");
            org.junit.Assert.fail("must not bind while the tunnel is down");
        } catch (TunnelException expected) {
            // expected
        }

        tunnel.closed = false;
        transport.addLocal(3080, 80, "127.0.0.1");
        assertTrue(transport.isLocalReachable(3080));
    }

    // ==================================================================
    // -R control stream + claim
    // ==================================================================

    @Test
    public void addReverse_opensControlStreamBeforeBinding() throws Exception {
        transport.addReverse(9000, 3000, "127.0.0.1");

        // The control stream must exist (and its incoming handler be wired)
        // before the bind is requested: a connection arriving right after
        // `bound` is announced with a single-use token, and a token nobody is
        // listening for is lost.
        assertEquals("control stream must be opened for a reverse bind", 1, tunnel.controlOpens.get());
        assertEquals("the requested port must be bound", 1, tunnel.boundPorts.size());
        assertEquals(Integer.valueOf(9000), tunnel.boundPorts.get(0));
    }

    @Test
    public void addReverse_usesTheBoundPortForRoutingWhenTheServerReassigns() throws Exception {
        // bind(0) asks the OS to choose; the reply carries the real port and
        // that is the port `incoming` will name.
        tunnel.nextBoundPort = 45123;
        transport.addReverse(0, 3000, "127.0.0.1");

        tunnel.control.incoming(45123, "tok-1");
        assertTrue(tunnel.awaitClaims(1));
        assertEquals("tok-1", tunnel.claimedTokens.get(0));
    }

    @Test
    public void addReverse_whenBindIsRefused_throwsAndRegistersNothing() {
        tunnel.failNextBind = true;
        try {
            transport.addReverse(9000, 3000, "127.0.0.1");
            org.junit.Assert.fail("a refused bind must reach the caller");
        } catch (Exception expected) {
            // The service drops the mapping and reports failure.
        }
        assertEquals(0, transport.reverseForwardCount());
    }

    @Test
    public void addReverse_whenTunnelIsDown_throws() {
        tunnel.connected = false;
        try {
            transport.addReverse(9000, 3000, "127.0.0.1");
            org.junit.Assert.fail("must not bind without a tunnel");
        } catch (Exception expected) {
            // expected
        }
        assertEquals(0, tunnel.boundPorts.size());
    }

    @Test
    public void removeReverse_unbindsAndForgetsTheMapping() throws Exception {
        transport.addReverse(9000, 3000, "127.0.0.1");
        transport.removeReverse(9000);

        assertEquals(1, tunnel.unboundPorts.size());
        assertEquals(Integer.valueOf(9000), tunnel.unboundPorts.get(0));
        assertEquals("the mapping must be forgotten", 0, transport.reverseForwardCount());
    }

    @Test
    public void removeReverse_closesInFlightClaimRelaysForThatPort() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addReverse(9000, 3000, "127.0.0.1");
        FakeSocket target = new FakeSocket("hello");
        sockets.dialer.nextSocket = target;

        tunnel.control.incoming(9000, "tok-1");
        assertTrue(connection.awaitRequestBytes());

        transport.removeReverse(9000);

        assertTrue("the claimed connection's target socket must be closed", target.awaitClosed());
        assertTrue("the claimed stream must be cancelled", connection.isClosed());
    }

    @Test
    public void incoming_dialsTheTargetThenClaimsWithTheToken() throws Exception {
        transport.addReverse(9000, 3000, "10.0.0.9");
        tunnel.control.incoming(9000, "tok-42");

        assertTrue(tunnel.awaitClaims(1));
        assertEquals("the local target must be dialed", 1, sockets.dialer.dialCount());
        assertEquals("10.0.0.9", sockets.dialer.dialedHosts.get(0));
        assertEquals(Integer.valueOf(3000), sockets.dialer.dialedPorts.get(0));
        assertEquals("tok-42", tunnel.claimedTokens.get(0));
    }

    @Test
    public void incoming_targetUnreachable_doesNotSpendTheToken() throws Exception {
        transport.addReverse(9000, 3000, "127.0.0.1");
        sockets.dialer.failNextDial = true;

        tunnel.control.incoming(9000, "tok-1");

        // Wait for the claim task to actually reach the dialer before asserting
        // what it did next. A fixed sleep could expire before the task ran, so
        // the "no claim" assertion would pass without the task ever executing.
        assertTrue("the claim task must attempt the local dial",
                sockets.dialer.awaitDials(1));
        assertEquals("an unreachable target must not spend the single-use token",
                0, tunnel.claimedTokens.size());
        assertEquals("no relay may be tracked for a failed dial", 0, transport.activeRelayCount());
    }

    @Test
    public void incoming_unknownPort_isIgnoredWithoutClaiming() throws Exception {
        transport.addReverse(9000, 3000, "127.0.0.1");

        // Positive control first: prove the claim pipeline actually runs for a
        // mapped port (the handler is wired, the executor works). Without this
        // the "unknown port did nothing" assertion could pass because nothing
        // async ever ran, not because the unknown port was correctly ignored.
        tunnel.control.incoming(9000, "tok-known");
        assertTrue("the known port must reach the dialer", sockets.dialer.awaitDials(1));
        assertTrue(tunnel.awaitClaims(1));

        // Now the unknown port: it is rejected synchronously inside the control
        // handler, so no additional dial or claim may appear.
        tunnel.control.incoming(9999, "tok-unknown");

        assertEquals("no dial for an unmapped port", 1, sockets.dialer.dialCount());
        assertEquals("no claim for an unmapped port", 1, tunnel.claimedTokens.size());
    }

    @Test
    public void incoming_withoutAToken_isIgnored() throws Exception {
        transport.addReverse(9000, 3000, "127.0.0.1");

        // Positive control: a well-formed incoming does dial and claim.
        tunnel.control.incoming(9000, "tok-known");
        assertTrue("the known incoming must reach the dialer", sockets.dialer.awaitDials(1));
        assertTrue(tunnel.awaitClaims(1));

        // A token-less incoming is rejected synchronously (the token guard runs
        // before the port lookup), so it must add no dial and no claim.
        tunnel.control.deliver(ControlMessage.parse("{\"type\":\"incoming\",\"port\":9000}"));

        assertEquals("a token-less incoming must not dial", 1, sockets.dialer.dialCount());
        assertEquals(1, tunnel.claimedTokens.size());
    }

    @Test
    public void addReverse_isIdempotentForTheSameServerPort() throws Exception {
        // H2PortForwardTransport:221-224 returns "already bound, success"
        // without re-binding. A reconnect replay re-adds every reverse port, so
        // this double-add is exactly the case it guards: re-binding would be
        // answered with bind_err (the server's listener already holds the port)
        // and the working forward would be reported as broken.
        transport.addReverse(9000, 3000, "127.0.0.1");
        transport.addReverse(9000, 3000, "127.0.0.1");

        assertEquals("the second add must be a no-op", 1, tunnel.boundPorts.size());
        assertEquals("exactly one mapping may be tracked", 1, transport.reverseForwardCount());
    }

    @Test
    public void removeReverse_afterBindZero_dropsBothKeys() throws Exception {
        // A bind(0) is answered with the OS-assigned port; H2PortForwardTransport
        // :242-243 indexes the mapping under BOTH the request key (0) and the
        // bound key. removeReverse(0) is what the service recorded, so it must
        // drop the twin via values().remove(forward) (:253) — otherwise the
        // bound key would linger and a later incoming would still claim.
        tunnel.nextBoundPort = 45123;
        transport.addReverse(0, 3000, "127.0.0.1");
        assertEquals("both keys share one instance", 2, transport.reverseForwardCount());

        transport.removeReverse(0);

        assertEquals("removing the request key must drop the bound twin too",
                0, transport.reverseForwardCount());

        // And a later announcement for the bound port must be ignored.
        tunnel.control.incoming(45123, "tok-late");
        assertEquals("a dropped mapping must not claim", 0, tunnel.claimedTokens.size());
        assertEquals("a dropped mapping must not dial", 0, sockets.dialer.dialCount());
    }

    @Test
    public void controlStreamDeath_clearsReverseMappings() throws Exception {
        // H2PortForwardTransport:283-288: the reverse mappings are server-side
        // state that dies with the control stream. Keeping them would advertise
        // reverse ports nothing can serve after the server dropped the bind —
        // the reconnect-safety invariant.
        transport.addReverse(9000, 3000, "127.0.0.1");
        assertEquals(1, transport.reverseForwardCount());

        tunnel.control.close();

        assertEquals("a dead control stream must forget every reverse mapping",
                0, transport.reverseForwardCount());
        tunnel.control.incoming(9000, "tok-after-death");
        assertEquals("a stale mapping must not claim after the stream died",
                0, tunnel.claimedTokens.size());
    }

    @Test
    public void incoming_tunnelDownAfterDial_doesNotClaimAndClosesTarget() throws Exception {
        // H2PortForwardTransport:333-337: the target is dialed first, then the
        // tunnel is re-checked. If it went down in between, the dialed target
        // must be closed rather than leaked, and the token left unspent. The
        // dialer's afterDial hook flips the flag exactly in that window, so the
        // test is deterministic rather than racy.
        transport.addReverse(9000, 3000, "127.0.0.1");
        FakeSocket target = new FakeSocket("");
        sockets.dialer.nextSocket = target;
        sockets.dialer.afterDial = () -> tunnel.connected = false;

        tunnel.control.incoming(9000, "tok-1");

        assertTrue("the dialed target must be closed once the tunnel is down",
                target.awaitClosed());
        assertEquals("a dead tunnel must not spend the token", 0, tunnel.claimedTokens.size());
        assertEquals("no relay may be tracked", 0, transport.activeRelayCount());
    }

    @Test
    public void failedServe_doesNotStopTheAcceptLoop() throws Exception {
        // H2PortForwardTransport:444-448 breaks the accept loop only when
        // accept() itself fails; per-connection failures are handled inside
        // serve(). A failed serve (openStream throws) must therefore leave the
        // loop alive for the next connection, or one bad dial would kill the
        // whole port.
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);
        // Sticky: every stream on this port fails, so the second accept proves
        // the loop survived rather than the failure being a one-off.
        tunnel.streamFailure = new TunnelException(
                TunnelErrorKind.TARGET_UNREACHABLE, "dial failed");

        FakeSocket first = new FakeSocket("a");
        server.accept(first);
        assertTrue("the first connection's socket must be dropped", first.awaitClosed());

        // The loop must still be accepting: a second connection is also served
        // (and also fails), which only happens if the loop is still running.
        FakeSocket second = new FakeSocket("b");
        server.accept(second);
        assertTrue("the accept loop must survive a failed serve", second.awaitClosed());
        assertEquals("both accepted connections must have reached openStream",
                2, tunnel.openedHosts.size());
    }

    @Test
    public void concurrentConnectionsToTheSamePort_openOneStreamEach() throws Exception {
        // H2PortForwardTransport:450-453: one thread per accepted connection.
        // Every existing test accepts exactly one, so the per-connection
        // fan-out is unverified.
        FakeConnection first = tunnel.queueConnection();
        FakeConnection second = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        server.accept(new FakeSocket("one"));
        server.accept(new FakeSocket("two"));

        tunnel.awaitOpenedStreams(2);
        assertEquals("each accepted connection must open its own stream",
                2, tunnel.openedHosts.size());
        assertEquals("both relays must be tracked", 2, transport.activeRelayCount());
        assertTrue(first.awaitRequestBytes());
        assertTrue(second.awaitRequestBytes());
    }

    @Test
    public void pumpTunnelReadError_closesTheLocalSocket() throws Exception {
        // H2PortForwardTransport:551-557: a read error on the response
        // direction is not a clean EOF but still tears the pair down in the
        // finally block. Only clean EOF is currently tested.
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);

        FakeSocket socket = new FakeSocket("hello");
        server.accept(socket);
        assertTrue(connection.awaitRequestBytes());

        connection.failResponse(new IOException("stream reset"));

        assertTrue("a response-direction read error must close the local socket",
                socket.awaitClosed());
        assertTrue("the stream must be released", connection.isClosed());
    }

    @Test
    public void pumpLocalWriteError_closesBothEnds() throws Exception {
        // H2PortForwardTransport:526-532: a local->tunnel write error (the
        // stream died mid-write) closes both ends. Only the clean-EOF
        // half-close path is tested today. The write failure is armed before
        // the connection is accepted, so the pump's very first write throws and
        // the assertion is deterministic.
        FakeConnection connection = tunnel.queueConnection();
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);
        connection.failWrites();

        FakeSocket socket = new FakeSocket("hello");
        server.accept(socket);

        assertTrue("a local->tunnel write error must close the local socket",
                socket.awaitClosed());
        assertTrue("the stream must be released", connection.isClosed());
    }

    @Test
    public void serve_whenTunnelIsDown_closesTheAcceptedSocket() throws Exception {
        // H2PortForwardTransport:471-475: a connection accepted after the
        // tunnel died must be dropped, not left waiting on a stream that will
        // never carry bytes.
        transport.addLocal(3080, 80, "127.0.0.1");
        FakeServerSocket server = sockets.created.get(0);
        tunnel.connected = false;

        FakeSocket socket = new FakeSocket("hello");
        server.accept(socket);

        assertTrue("a down tunnel must close the accepted socket", socket.awaitClosed());
        assertEquals("no stream may be opened on a dead tunnel", 0, tunnel.openedHosts.size());
        assertEquals("no relay may stay tracked", 0, transport.activeRelayCount());
    }

    @Test
    public void incoming_relaysBytesBothWays() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addReverse(9000, 3000, "127.0.0.1");
        FakeSocket target = new FakeSocket("from-target");
        sockets.dialer.nextSocket = target;

        tunnel.control.incoming(9000, "tok-1");

        // target -> stream (the request direction)
        assertTrue(connection.awaitRequestBytes());
        assertEquals("from-target", connection.requestBytes());

        // stream -> target (the response direction)
        connection.pushResponse("from-server");
        assertTrue(target.awaitLocalBytes());
        assertEquals("from-server", target.localBytes());
    }

    @Test
    public void claimFailure_closesTheDialedTarget() throws Exception {
        transport.addReverse(9000, 3000, "127.0.0.1");
        FakeSocket target = new FakeSocket("");
        sockets.dialer.nextSocket = target;
        tunnel.failNextClaim = true;

        tunnel.control.incoming(9000, "tok-1");

        assertTrue("a rejected claim must not leak the dialed socket", target.awaitClosed());
        assertEquals(0, transport.activeRelayCount());
    }

    @Test
    public void controlStreamIsWiredOnlyOnceAcrossSeveralReversePorts() throws Exception {
        transport.addReverse(9000, 3000, "127.0.0.1");
        transport.addReverse(9001, 3001, "127.0.0.1");

        // The stream itself is a shared singleton (openControlStream is
        // idempotent), but the handler must be registered exactly once:
        // re-registering per bind would deliver every `incoming` several times,
        // and each duplicate would race for the same single-use token.
        assertEquals("the handler must be wired exactly once", 1, tunnel.control.handlerCount());

        tunnel.control.incoming(9001, "tok-2");
        assertTrue(tunnel.awaitClaims(1));
        assertEquals("exactly one claim per incoming", 1, tunnel.claimedTokens.size());
        assertEquals("one relay for the single claim", 1, transport.activeRelayCount());
    }

    @Test
    public void close_releasesReverseMappingsAndInFlightClaims() throws Exception {
        FakeConnection connection = tunnel.queueConnection();
        transport.addReverse(9000, 3000, "127.0.0.1");
        FakeSocket target = new FakeSocket("hello");
        sockets.dialer.nextSocket = target;
        tunnel.control.incoming(9000, "tok-1");
        assertTrue(connection.awaitRequestBytes());

        transport.close();

        assertEquals(0, transport.reverseForwardCount());
        assertTrue("the claimed target socket must be closed", target.awaitClosed());
        assertTrue("the claimed stream must be cancelled", connection.isClosed());
    }
}
