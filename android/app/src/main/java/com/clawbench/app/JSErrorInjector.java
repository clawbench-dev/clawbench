package com.clawbench.app;

import java.util.regex.Pattern;

/**
 * Shared JS error listener injection for WebViews.
 * Used by both MainActivity (AndroidNative) and BrowserActivity (BrowserNative).
 *
 * Captures:
 * - Uncaught runtime JS errors (via window.addEventListener 'error' bubble phase)
 * - Resource loading failures: img/script/link/etc 404s (via capture phase)
 * - Unhandled Promise rejections (via 'unhandledrejection')
 */
public final class JSErrorInjector {

    /** Valid JS identifier pattern for safety validation. */
    private static final Pattern JS_IDENTIFIER = Pattern.compile("[A-Za-z_$][A-Za-z0-9_$]*");

    private JSErrorInjector() {} // utility class

    /**
     * Build the JS script that injects global error listeners into the WebView.
     *
     * @param nativeInterfaceName The JavascriptInterface name to call
     *                           ("AndroidNative" for MainActivity, "BrowserNative" for BrowserActivity)
     * @return JavaScript code to evaluate in onPageStarted
     * @throws IllegalArgumentException if nativeInterfaceName is not a valid JS identifier
     */
    public static String buildScript(String nativeInterfaceName) {
        if (nativeInterfaceName == null || !JS_IDENTIFIER.matcher(nativeInterfaceName).matches()) {
            throw new IllegalArgumentException("Invalid JS identifier: " + nativeInterfaceName);
        }
        return "(function(){" +
            "if(window.__clawbenchErrorInjected) return;" +
            "window.__clawbenchErrorInjected=1;" +
            // Bubble phase: captures runtime JS errors (syntax, thrown exceptions, etc.)
            // Guard e.message to skip resource errors that lack a message property
            "window.addEventListener('error',function(e){" +
            "  if(typeof " + nativeInterfaceName + "!=='undefined' && e.message){" +
            "    var s=e.error&&e.error.stack?e.error.stack:(e.message+' at '+e.filename+':'+e.lineno);" +
            "    " + nativeInterfaceName + ".log('E','JS.Uncaught',s);" +
            "  }" +
            "});" +
            // Capture phase: captures resource loading failures (img/script/link 404s, etc.)
            // Resource error events fire on the element and do NOT bubble, so capture phase is required.
            "window.addEventListener('error',function(e){" +
            "  if(typeof " + nativeInterfaceName + "!=='undefined' && e.target && e.target!==window){" +
            "    var tag=e.target.tagName||'?';" +
            "    var src=e.target.src||e.target.href||'';" +
            "    " + nativeInterfaceName + ".log('E','JS.Resource','Failed to load <'+tag+'> src='+src);" +
            "  }" +
            "},true);" +
            // Unhandled Promise rejections with improved serialization
            // JSON.stringify wrapped in try-catch to handle circular references
            "window.addEventListener('unhandledrejection',function(e){" +
            "  if(typeof " + nativeInterfaceName + "!=='undefined'){" +
            "    var r=e.reason;" +
            "    var msg=r instanceof Error?(r.stack||r.message):" +
            "      (typeof r==='object'&&r?(function(){try{return JSON.stringify(r)}catch(x){return String(r)}})():String(r));" +
            "    " + nativeInterfaceName + ".log('E','JS.Promise',msg);" +
            "  }" +
            "});" +
            "})();";
    }
}
