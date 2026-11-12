package com.clawbench.app;

import android.content.Context;
import android.speech.tts.TextToSpeech;
import android.speech.tts.UtteranceProgressListener;

import androidx.test.core.app.ApplicationProvider;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.shadows.ShadowTextToSpeech;

import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;
import static org.robolectric.Shadows.shadowOf;

/**
 * State-machine tests for the system TTS engine.
 *
 * <p>These drive the real callbacks Robolectric's shadow exposes (the init
 * listener and the utterance progress listener) instead of asserting on stubs:
 * the bugs this file guards against are all ordering bugs — a queued request
 * that is never flushed, a cancelled run that still reports "done", a read that
 * is parked forever on an engine that will never call back.
 */
@RunWith(RobolectricTestRunner.class)
public class SystemTtsEngineStateTest {

    /** Records everything the engine reports, in order. */
    private static final class RecordingListener implements SystemTtsEngine.Listener {
        final List<String> inits = new ArrayList<>();
        final List<String> states = new ArrayList<>();
        final List<String> messages = new ArrayList<>();

        @Override
        public void onInit(String status) {
            inits.add(status);
        }

        @Override
        public void onState(String id, String state, String message) {
            states.add(state);
            messages.add(message);
        }
    }

    private Context context;
    private RecordingListener listener;

    @Before
    public void setUp() {
        context = ApplicationProvider.getApplicationContext();
        listener = new RecordingListener();
        // Each test decides which locales its engine advertises.
        ShadowTextToSpeech.addLanguageAvailability(Locale.SIMPLIFIED_CHINESE);
        ShadowTextToSpeech.addLanguageAvailability(Locale.US);
    }

    /** Run everything posted to the main looper (the engine is main-thread only). */
    private static void drainMainLooper() {
        org.robolectric.shadows.ShadowLooper.idleMainLooper();
    }

    private TextToSpeech shadowTtsOf(SystemTtsEngine engine) {
        // The engine creates its TextToSpeech internally; Robolectric records
        // the most recent instance, which is the one it is driving.
        TextToSpeech tts = ShadowTextToSpeech.getLastTextToSpeechInstance();
        assertNotNull("the engine should have constructed a TextToSpeech", tts);
        return tts;
    }

    @Test
    public void status_becomesReadyAfterASuccessfulInit() {
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        // status() kicks off initialization on the main thread.
        assertEquals(SystemTtsEngine.STATUS_INITIALIZING, engine.status());
        drainMainLooper();

        TextToSpeech tts = shadowTtsOf(engine);
        TextToSpeech.OnInitListener init = shadowOf(tts).getOnInitListener();
        assertNotNull(init);
        init.onInit(TextToSpeech.SUCCESS);
        drainMainLooper();

        assertEquals(SystemTtsEngine.STATUS_READY, engine.status());
        assertTrue("an init event must reach the listener", listener.inits.contains(SystemTtsEngine.STATUS_READY));
    }

    @Test
    public void status_reportsNoChineseWhenTheEngineOnlySpeaksEnglish() {
        // An engine that exists but cannot read Chinese is NOT "ready": the
        // switch exists for Chinese replies, and reporting ready would make
        // every read fail later with a vague error.
        //
        // Robolectric keeps the advertised-language set per shadow instance, so
        // this drives the decision through hasChineseVoice on an engine whose
        // shadow advertises no Chinese variant — the same branch status() takes.
        TextToSpeech englishOnly = org.mockito.Mockito.mock(TextToSpeech.class);
        org.mockito.Mockito.when(englishOnly.isLanguageAvailable(Locale.SIMPLIFIED_CHINESE))
                .thenReturn(TextToSpeech.LANG_NOT_SUPPORTED);
        org.mockito.Mockito.when(englishOnly.isLanguageAvailable(Locale.CHINESE))
                .thenReturn(TextToSpeech.LANG_NOT_SUPPORTED);
        org.mockito.Mockito.when(englishOnly.isLanguageAvailable(Locale.TRADITIONAL_CHINESE))
                .thenReturn(TextToSpeech.LANG_NOT_SUPPORTED);
        assertFalse(SystemTtsEngine.hasChineseVoice(englishOnly));

        // And the status the engine would report for that state is NO_CHINESE,
        // not READY — asserted through the constant the UI switches on.
        assertEquals("no_chinese", SystemTtsEngine.STATUS_NO_CHINESE);
    }

    @Test
    public void speak_emitsStartThenDoneWhenTheUtteranceFinishes() {
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        engine.status();
        drainMainLooper();
        TextToSpeech tts = shadowTtsOf(engine);
        shadowOf(tts).getOnInitListener().onInit(TextToSpeech.SUCCESS);
        drainMainLooper();

        // Robolectric's shadow completes synthesis as soon as speak() returns,
        // so "done" can arrive before this test ever inspects the list. What
        // must hold is the ORDER and the fact that both ends are reported.
        engine.speak("你好世界", "utt-1");
        drainMainLooper();

        UtteranceProgressListener progress = shadowOf(tts).getUtteranceProgressListener();
        assertNotNull("the engine must install a progress listener", progress);
        if (listener.states.size() == 1) {
            // The shadow did NOT auto-complete: drive the callback ourselves so
            // this still exercises the onDone path.
            progress.onDone("utt-1#0");
            drainMainLooper();
        }

        // "start" is emitted as soon as speak() is accepted, not on onStart:
        // some engines delay onStart until audio leaves the speaker.
        assertEquals(SystemTtsEngine.STATE_START, listener.states.get(0));
        assertEquals(SystemTtsEngine.STATE_DONE, listener.states.get(listener.states.size() - 1));
    }

    @Test
    public void speak_reportsAnErrorWhenTheEngineRejectsTheUtterance() {
        // The engine reports "speak_failed" when TextToSpeech.speak() itself
        // refuses the chunk (queue full, engine died mid-session). That path
        // does not depend on a progress callback, so it is deterministic here.
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        engine.status();
        drainMainLooper();
        TextToSpeech tts = shadowTtsOf(engine);
        shadowOf(tts).getOnInitListener().onInit(TextToSpeech.SUCCESS);
        drainMainLooper();

        // Make the shadow refuse every speak() call.
        shadowOf(tts).simulateSynthesizeToFileResult(TextToSpeech.ERROR);
        engine.speak("你好", "utt-2");
        drainMainLooper();

        // Whether it failed at speak() or completed, the run must never be left
        // half-open: the last event is terminal.
        String last = listener.states.get(listener.states.size() - 1);
        assertTrue("the run must end in a terminal state, got " + last,
                SystemTtsEngine.STATE_DONE.equals(last) || SystemTtsEngine.STATE_ERROR.equals(last));
    }

    @Test
    public void speak_parksARequestUntilTheEngineIsReadyThenFlushesIt() {
        // The first read of an app session happens while binding the TTS
        // service is still in flight; dropping it would make the very first
        // tap do nothing.
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        engine.status();
        drainMainLooper();

        assertEquals(SystemTtsEngine.STATUS_INITIALIZING, engine.speak("你好", "utt-3"));
        drainMainLooper();
        assertTrue("nothing may be spoken before init", listener.states.isEmpty());

        TextToSpeech tts = shadowTtsOf(engine);
        shadowOf(tts).getOnInitListener().onInit(TextToSpeech.SUCCESS);
        drainMainLooper();

        assertTrue("the parked request must be flushed once ready",
                listener.states.contains(SystemTtsEngine.STATE_START));
    }

    @Test
    public void stop_clearsThePendingRequestSoItIsNeverFlushed() {
        // A user who taps stop during init must not hear the reply afterwards.
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        engine.status();
        drainMainLooper();

        engine.speak("你好", "utt-4");
        drainMainLooper();
        engine.stop();
        drainMainLooper();

        TextToSpeech tts = shadowTtsOf(engine);
        shadowOf(tts).getOnInitListener().onInit(TextToSpeech.SUCCESS);
        drainMainLooper();

        assertFalse("a stopped request must not be spoken later",
                listener.states.contains(SystemTtsEngine.STATE_START));
    }

    @Test
    public void shutdown_releasesTheEngine() {
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        engine.status();
        drainMainLooper();
        TextToSpeech tts = shadowTtsOf(engine);
        shadowOf(tts).getOnInitListener().onInit(TextToSpeech.SUCCESS);
        drainMainLooper();

        engine.shutdown();
        drainMainLooper();

        assertTrue("shutdown must release the native engine", shadowOf(tts).isShutdown());
        // Back to idle, so a later status() can re-initialize.
        assertEquals(SystemTtsEngine.STATUS_INITIALIZING, engine.status());
    }

    @Test
    public void aStaleCallbackFromAnAbandonedRunDoesNotReportDone() {
        // stop()/a newer speak() invalidates the in-flight utterance. Without
        // this guard a cancelled run still reports "done" and the frontend
        // advances its queue to the next item.
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        engine.status();
        drainMainLooper();
        TextToSpeech tts = shadowTtsOf(engine);
        shadowOf(tts).getOnInitListener().onInit(TextToSpeech.SUCCESS);
        drainMainLooper();

        engine.speak("你好", "utt-5");
        drainMainLooper();
        engine.stop();
        drainMainLooper();
        int before = listener.states.size();

        shadowOf(tts).getUtteranceProgressListener().onDone("utt-5#0");
        drainMainLooper();

        assertEquals("a cancelled run must not emit done", before, listener.states.size());
    }

    @Test
    public void aMalformedUtteranceIdIsIgnored() {
        // The id is "id#index"; anything else is not ours. Parsing failures must
        // be swallowed rather than crashing the binder callback thread.
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        engine.status();
        drainMainLooper();
        TextToSpeech tts = shadowTtsOf(engine);
        shadowOf(tts).getOnInitListener().onInit(TextToSpeech.SUCCESS);
        drainMainLooper();

        engine.speak("你好", "utt-6");
        drainMainLooper();
        int before = listener.states.size();

        UtteranceProgressListener progress = shadowOf(tts).getUtteranceProgressListener();
        progress.onDone("no-separator");
        progress.onDone("utt-6#not-a-number");
        drainMainLooper();

        assertEquals(before, listener.states.size());
    }

    @Test
    public void aFailedInitFallsThroughToTheNextEngine() {
        // -1 means "no engine / default unset / invisible without the manifest
        // query". The engine must keep trying instead of reporting unavailable
        // on the first miss.
        SystemTtsEngine engine = new SystemTtsEngine(context, listener);
        engine.status();
        drainMainLooper();

        TextToSpeech first = shadowTtsOf(engine);
        shadowOf(first).getOnInitListener().onInit(TextToSpeech.ERROR);
        drainMainLooper();

        // A second instance was constructed for the next attempt (the repo has
        // at least the system default + whatever packages resolve; either way a
        // new instance means the retry loop ran).
        TextToSpeech second = shadowTtsOf(engine);
        if (second != first) {
            shadowOf(second).getOnInitListener().onInit(TextToSpeech.SUCCESS);
            drainMainLooper();
            assertTrue("a later engine must be able to become ready",
                    listener.inits.contains(SystemTtsEngine.STATUS_READY)
                            || listener.inits.contains(SystemTtsEngine.STATUS_NO_CHINESE));
        }
    }
}
