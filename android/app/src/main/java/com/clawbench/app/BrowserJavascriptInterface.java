package com.clawbench.app;

import android.webkit.JavascriptInterface;

/**
 * Lightweight JavascriptInterface for BrowserActivity.
 * Provides log relay from injected window.error/unhandledrejection listeners.
 */
public class BrowserJavascriptInterface {

    private final BrowserLogBuffer logBuffer;

    public BrowserJavascriptInterface(BrowserLogBuffer logBuffer) {
        this.logBuffer = logBuffer;
    }

    @JavascriptInterface
    public void log(String level, String tag, String msg) {
        char c;
        switch (level != null ? level : "") {
            case "E": c = 'E'; break;
            case "W": c = 'W'; break;
            case "I": c = 'I'; break;
            default:  c = 'D'; break;
        }
        logBuffer.add(c, tag != null ? tag : "", msg != null ? msg : "");
    }
}
