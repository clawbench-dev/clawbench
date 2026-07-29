package com.clawbench.app;

import org.junit.Before;
import org.junit.Test;

import java.util.List;

import static org.junit.Assert.*;

/**
 * Integration tests for the browser log capture pipeline:
 * - Console messages from WebView onConsoleMessage
 * - JS error/unhandledrejection via BrowserJavascriptInterface
 * Both write to the same BrowserLogBuffer.
 */
public class BrowserLogCaptureTest {

    private BrowserLogBuffer logBuffer;
    private BrowserJavascriptInterface jsInterface;

    @Before
    public void setUp() {
        logBuffer = new BrowserLogBuffer(100);
        jsInterface = new BrowserJavascriptInterface(logBuffer);
    }

    /**
     * Simulates mixed log capture: onConsoleMessage writes directly,
     * BrowserJavascriptInterface.log() writes via JS bridge.
     * Verifies both sources appear in the same buffer and can be filtered.
     */
    @Test
    public void logCaptureAndFilter() {
        // Simulate onConsoleMessage writing directly
        logBuffer.add('E', "WebView:ERROR", "console error (source.js:10)");
        logBuffer.add('W', "WebView:WARNING", "console warning (source.js:20)");
        logBuffer.add('D', "WebView:DEBUG", "console debug (source.js:30)");

        // Simulate JS bridge calls from error/unhandledrejection listeners
        jsInterface.log("E", "JS.error", "Uncaught TypeError: null is not an object");
        jsInterface.log("E", "JS.promise", "Unhandled Promise Rejection: NetworkError");

        // Simulate a normal bridge log
        jsInterface.log("I", "App", "Application started");

        // Verify total entries
        List<BrowserLogBuffer.Entry> all = logBuffer.getEntries();
        assertEquals(6, all.size());

        // Verify order is preserved (direct adds before bridge adds)
        assertEquals("console error (source.js:10)", all.get(0).msg);
        assertEquals("Uncaught TypeError: null is not an object", all.get(3).msg);

        // Filter errors only
        List<BrowserLogBuffer.Entry> errors = logBuffer.getFiltered('E');
        assertEquals(3, errors.size());
        assertEquals("console error (source.js:10)", errors.get(0).msg);
        assertEquals("Uncaught TypeError: null is not an object", errors.get(1).msg);
        assertEquals("Unhandled Promise Rejection: NetworkError", errors.get(2).msg);

        // Filter warnings only
        List<BrowserLogBuffer.Entry> warnings = logBuffer.getFiltered('W');
        assertEquals(1, warnings.size());
        assertEquals("console warning (source.js:20)", warnings.get(0).msg);

        // Filter info only
        List<BrowserLogBuffer.Entry> infos = logBuffer.getFiltered('I');
        assertEquals(1, infos.size());
        assertEquals("Application started", infos.get(0).msg);

        // Filter debug only
        List<BrowserLogBuffer.Entry> debugs = logBuffer.getFiltered('D');
        assertEquals(1, debugs.size());
        assertEquals("console debug (source.js:30)", debugs.get(0).msg);
    }

    /**
     * Verifies that when the buffer overflows, entries from both sources
     * (onConsoleMessage and JS bridge) are evicted correctly in FIFO order.
     */
    @Test
    public void bufferOverflowWithMixedSources() {
        BrowserLogBuffer smallBuffer = new BrowserLogBuffer(5);
        BrowserJavascriptInterface smallJsInterface = new BrowserJavascriptInterface(smallBuffer);

        // Fill buffer with direct adds
        smallBuffer.add('D', "Console", "msg0");
        smallBuffer.add('D', "Console", "msg1");
        smallBuffer.add('D', "Console", "msg2");
        smallBuffer.add('D', "Console", "msg3");
        smallBuffer.add('D', "Console", "msg4");

        // Now overflow with JS bridge adds
        smallJsInterface.log("E", "JS.error", "overflow error1");
        smallJsInterface.log("E", "JS.error", "overflow error2");

        List<BrowserLogBuffer.Entry> all = smallBuffer.getEntries();
        assertEquals(5, all.size());

        // First two direct adds should be evicted
        assertEquals("msg2", all.get(0).msg);
        assertEquals('D', all.get(0).level);
        assertEquals("msg3", all.get(1).msg);
        assertEquals("msg4", all.get(2).msg);
        assertEquals("overflow error1", all.get(3).msg);
        assertEquals('E', all.get(3).level);
        assertEquals("overflow error2", all.get(4).msg);
        assertEquals('E', all.get(4).level);

        // Filter should still work correctly after overflow
        List<BrowserLogBuffer.Entry> errors = smallBuffer.getFiltered('E');
        assertEquals(2, errors.size());
        assertEquals("overflow error1", errors.get(0).msg);
        assertEquals("overflow error2", errors.get(1).msg);
    }

    /**
     * Verifies that BrowserJavascriptInterface normalizes unknown levels to 'D'.
     */
    @Test
    public void jsInterfaceNormalizesUnknownLevel() {
        jsInterface.log("X", "Unknown", "unknown level message");
        jsInterface.log(null, "Null", "null level message");

        List<BrowserLogBuffer.Entry> debugs = logBuffer.getFiltered('D');
        assertEquals(2, debugs.size());
        assertEquals("unknown level message", debugs.get(0).msg);
        assertEquals("null level message", debugs.get(1).msg);
    }

    /**
     * Verifies that BrowserJavascriptInterface handles null tag and msg.
     */
    @Test
    public void jsInterfaceHandlesNulls() {
        jsInterface.log("E", null, null);

        List<BrowserLogBuffer.Entry> errors = logBuffer.getFiltered('E');
        assertEquals(1, errors.size());
        assertEquals("", errors.get(0).tag);
        assertEquals("", errors.get(0).msg);
    }
}
