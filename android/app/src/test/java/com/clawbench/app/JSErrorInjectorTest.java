package com.clawbench.app;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.assertThrows;

import org.junit.Test;

public class JSErrorInjectorTest {

    @Test
    public void buildScript_containsGuardFlag() {
        String script = JSErrorInjector.buildScript("ClawBenchNative");
        assertTrue("Should contain __clawbenchErrorInjected guard",
                script.contains("__clawbenchErrorInjected"));
    }

    @Test
    public void buildScript_interpolatesInterfaceName_androidNative() {
        String script = JSErrorInjector.buildScript("ClawBenchNative");
        assertTrue("Should reference ClawBenchNative",
                script.contains("typeof ClawBenchNative!=='undefined'"));
        assertTrue("Should call ClawBenchNative.log",
                script.contains("ClawBenchNative.log("));
    }

    @Test
    public void buildScript_interpolatesInterfaceName_browserNative() {
        String script = JSErrorInjector.buildScript("BrowserNative");
        assertTrue("Should reference BrowserNative",
                script.contains("typeof BrowserNative!=='undefined'"));
        assertTrue("Should call BrowserNative.log",
                script.contains("BrowserNative.log("));
    }

    @Test
    public void buildScript_containsAllThreeListeners() {
        String script = JSErrorInjector.buildScript("TestNative");
        // Bubble-phase error listener (e.message guard)
        assertTrue("Should contain bubble-phase error listener with e.message guard",
                script.contains("addEventListener('error'") && script.contains("e.message"));
        // Capture-phase error listener (e.target !== window)
        assertTrue("Should contain capture-phase error listener",
                script.contains("e.target!==window") && script.contains("},true)"));
        // Unhandledrejection listener
        assertTrue("Should contain unhandledrejection listener",
                script.contains("addEventListener('unhandledrejection'"));
    }

    @Test
    public void buildScript_usesErrorStack() {
        String script = JSErrorInjector.buildScript("TestNative");
        assertTrue("Should use e.error.stack for richer traces",
                script.contains("e.error&&e.error.stack"));
    }

    @Test
    public void buildScript_wrapsJsonStringifyInTryCatch() {
        String script = JSErrorInjector.buildScript("TestNative");
        assertTrue("Should wrap JSON.stringify in try-catch for circular references",
                script.contains("try{return JSON.stringify(r)}catch"));
    }

    @Test
    public void buildScript_rejectsNullInterfaceName() {
        assertThrows(IllegalArgumentException.class,
                () -> JSErrorInjector.buildScript(null));
    }

    @Test
    public void buildScript_rejectsInvalidInterfaceName() {
        assertThrows(IllegalArgumentException.class,
                () -> JSErrorInjector.buildScript("foo-bar"));
        assertThrows(IllegalArgumentException.class,
                () -> JSErrorInjector.buildScript("123invalid"));
        assertThrows(IllegalArgumentException.class,
                () -> JSErrorInjector.buildScript("obj.prop"));
    }

    @Test
    public void buildScript_acceptsValidIdentifiers() {
        // Should not throw for valid JS identifiers
        assertNotNull(JSErrorInjector.buildScript("ClawBenchNative"));
        assertNotNull(JSErrorInjector.buildScript("BrowserNative"));
        assertNotNull(JSErrorInjector.buildScript("$jquery"));
        assertNotNull(JSErrorInjector.buildScript("_private"));
    }
}
