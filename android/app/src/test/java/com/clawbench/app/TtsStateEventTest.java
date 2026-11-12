package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for the TTS state event the native side pushes into the WebView.
 *
 * <p>The renderer advances its read-aloud queue on this event and matches it by
 * utterance id, so a renamed event or field would compile on both sides and
 * leave the UI stuck on "reading" forever.
 */
public class TtsStateEventTest {

    @Test
    public void script_usesTheEventNameTheRendererListensFor() {
        String script = MainActivity.buildTtsStateScript("42", "done", "");
        assertTrue(script.contains("'clawbench-tts-state'"));
    }

    @Test
    public void payload_carriesIdStateAndMessage() {
        String script = MainActivity.buildTtsStateScript("42", "error", "unavailable");
        assertTrue(script.contains("\"id\":\"42\""));
        assertTrue(script.contains("\"state\":\"error\""));
        assertTrue(script.contains("\"message\":\"unavailable\""));
    }


    @Test
    public void initScript_usesTheEventNameTheSettingsProbeListensFor() {
        String script = MainActivity.buildTtsInitScript("no_chinese");
        assertTrue(script.contains("'clawbench-tts-init'"));
        assertTrue(script.contains("\"state\":\"no_chinese\""));
    }

    @Test
    public void quotesAndNewlines_areEscaped() {
        // A message about a `"` must not turn the injected script into a
        // syntax error: the page would never hear "done".
        String script = MainActivity.buildTtsStateScript("1", "done", "say \"hi\"\nnext\\line");
        assertTrue(script.contains("say \\\"hi\\\"\\nnext\\\\line"));
    }

    @Test
    public void lineSeparators_areEscaped() {
        String script = MainActivity.jsStringLiteral("a\u2028b\u2029c");
        assertEquals("\"a\\u2028b\\u2029c\"", script);
    }

    @Test
    public void controlCharacters_areEscaped() {
        assertEquals("\"a\\u0001b\"", MainActivity.jsStringLiteral("a\u0001b"));
    }

    @Test
    public void nullBecomesJsNull() {
        assertEquals("null", MainActivity.jsStringLiteral(null));
    }
}
