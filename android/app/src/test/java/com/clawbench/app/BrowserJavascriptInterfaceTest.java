package com.clawbench.app;

import org.junit.Test;
import static org.junit.Assert.*;

public class BrowserJavascriptInterfaceTest {

    @Test
    public void logAddsToBuffer() {
        BrowserLogBuffer buffer = new BrowserLogBuffer(100);
        BrowserJavascriptInterface bridge = new BrowserJavascriptInterface(buffer);
        bridge.log("E", "JSTag", "js error");
        assertEquals(1, buffer.getEntries().size());
        assertEquals('E', buffer.getEntries().get(0).level);
        assertEquals("JSTag", buffer.getEntries().get(0).tag);
        assertEquals("js error", buffer.getEntries().get(0).msg);
    }

    @Test
    public void logDefaultsToDebug() {
        BrowserLogBuffer buffer = new BrowserLogBuffer(100);
        BrowserJavascriptInterface bridge = new BrowserJavascriptInterface(buffer);
        bridge.log("X", "T", "msg");
        assertEquals('D', buffer.getEntries().get(0).level);
    }

    @Test
    public void logNullSafety() {
        BrowserLogBuffer buffer = new BrowserLogBuffer(100);
        BrowserJavascriptInterface bridge = new BrowserJavascriptInterface(buffer);
        bridge.log(null, null, null);
        assertEquals(1, buffer.getEntries().size());
        assertEquals('D', buffer.getEntries().get(0).level);
    }
}
