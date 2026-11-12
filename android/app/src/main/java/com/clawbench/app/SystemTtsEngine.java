package com.clawbench.app;

import android.content.Context;
import android.os.Handler;
import android.os.Looper;
import android.speech.tts.TextToSpeech;
import android.speech.tts.UtteranceProgressListener;

import java.util.List;
import java.util.Locale;

/**
 * Reads text with the phone's own TextToSpeech engine.
 *
 * <p>Why this exists: the server-side engines produce the audio, but on a
 * phone the built-in engine is often clearer (it is the same voice the user
 * already hears elsewhere) and needs no network round trip. The user picks it
 * per device in Settings; this class is the native half of that switch.
 *
 * <p>Threading: the JS bridge calls in on the WebView's JavaBridge thread, but
 * TextToSpeech must be constructed and driven from a thread with a Looper.
 * Every public method therefore posts to the main thread and returns
 * immediately; state the caller can read synchronously ({@link #status()}) is
 * volatile.
 *
 * <p>State is reported back to the page as {@code clawbench-tts-state} events
 * (see MainActivity#dispatchTtsState). Exactly one terminal event (done or
 * error) is guaranteed per accepted utterance id — without that the frontend
 * would sit on "reading" forever.
 */
public final class SystemTtsEngine {

    /** Callback into the Activity, which forwards it into the WebView. */
    public interface Listener {
        void onState(String utteranceId, String state, String message);
    }

    static final String STATE_START = "start";
    static final String STATE_DONE = "done";
    static final String STATE_ERROR = "error";

    static final String STATUS_READY = "ready";
    static final String STATUS_INITIALIZING = "initializing";
    static final String STATUS_UNAVAILABLE = "unavailable";

    private static final int INTERNAL_IDLE = 0;
    private static final int INTERNAL_INITIALIZING = 1;
    private static final int INTERNAL_READY = 2;
    private static final int INTERNAL_UNAVAILABLE = 3;

    private final Context context;
    private final Listener listener;
    private final Handler main = new Handler(Looper.getMainLooper());

    private volatile int internalState = INTERNAL_IDLE;

    private TextToSpeech tts;
    private String pendingId;
    private String pendingText;

    private String currentId;
    private int lastIndex = -1;

    private final UtteranceProgressListener progress = new UtteranceProgressListener() {
        @Override
        public void onStart(String utteranceId) {
            // The start event is emitted right after the first speak() call
            // returns, not here: some engines delay onStart until audio
            // actually leaves the speaker, which would leave the UI in
            // "synthesizing" for the whole init window.
        }

        @Override
        public void onDone(String utteranceId) {
            handleFinished(utteranceId, null);
        }

        @Override
        public void onError(String utteranceId) {
            handleFinished(utteranceId, "utterance_error");
        }

        @Override
        public void onError(String utteranceId, int errorCode) {
            handleFinished(utteranceId, "utterance_error_" + errorCode);
        }
    };

    public SystemTtsEngine(Context context, Listener listener) {
        this.context = context.getApplicationContext();
        this.listener = listener;
    }

    /**
     * Current readiness. Never blocks: the first call kicks off initialization
     * and reports {@link #STATUS_INITIALIZING}.
     */
    public String status() {
        main.post(this::ensureInitialized);
        switch (internalState) {
            case INTERNAL_READY:
                return STATUS_READY;
            case INTERNAL_UNAVAILABLE:
                return STATUS_UNAVAILABLE;
            default:
                return STATUS_INITIALIZING;
        }
    }

    /**
     * Queue {@code text} for reading.
     *
     * <p>If the engine is still initializing the request is remembered and
     * spoken as soon as it is ready — the first read of an app session must
     * not fail just because binding the TTS service takes a moment.
     *
     * @return the status at call time ({@code ready} / {@code initializing} /
     *         {@code unavailable}); an asynchronous failure is reported as an
     *         {@code error} event carrying {@code utteranceId}
     */
    public String speak(String text, String utteranceId) {
        if (text == null || text.trim().isEmpty() || utteranceId == null || utteranceId.isEmpty()) {
            return "bad_request";
        }
        final String trimmed = text.trim();
        final String id = utteranceId;
        main.post(() -> speakOnMain(trimmed, id));
        return status();
    }

    /** Stop reading and forget anything queued. */
    public void stop() {
        main.post(this::stopOnMain);
    }

    /** Release the engine. Called when the Activity is destroyed. */
    public void shutdown() {
        main.post(() -> {
            stopOnMain();
            if (tts != null) {
                tts.shutdown();
                tts = null;
            }
            internalState = INTERNAL_IDLE;
        });
    }

    private void ensureInitialized() {
        if (internalState != INTERNAL_IDLE) {
            return;
        }
        internalState = INTERNAL_INITIALIZING;
        try {
            tts = new TextToSpeech(context, initStatus -> {
                if (initStatus == TextToSpeech.SUCCESS && tts != null) {
                    internalState = INTERNAL_READY;
                    tts.setOnUtteranceProgressListener(progress);
                    if (pendingText != null && pendingId != null) {
                        String text = pendingText;
                        String id = pendingId;
                        pendingText = null;
                        pendingId = null;
                        speakOnMain(text, id);
                    }
                    return;
                }
                internalState = INTERNAL_UNAVAILABLE;
                String id = pendingId;
                pendingId = null;
                pendingText = null;
                if (id != null) {
                    emit(id, STATE_ERROR, "init_failed");
                }
            });
        } catch (Throwable t) {
            internalState = INTERNAL_UNAVAILABLE;
            String id = pendingId;
            pendingId = null;
            pendingText = null;
            if (id != null) {
                emit(id, STATE_ERROR, "init_failed");
            }
        }
    }

    private void speakOnMain(String text, String id) {
        ensureInitialized();
        if (internalState == INTERNAL_UNAVAILABLE) {
            emit(id, STATE_ERROR, "unavailable");
            return;
        }
        if (internalState != INTERNAL_READY || tts == null) {
            pendingId = id;
            pendingText = text;
            return;
        }

        List<String> chunks = SystemTtsChunker.split(text, maxChunkLength());
        if (chunks.isEmpty()) {
            emit(id, STATE_DONE, "");
            return;
        }

        int language = tts.setLanguage(pickLocale(text));
        if (language == TextToSpeech.LANG_MISSING_DATA
                || language == TextToSpeech.LANG_NOT_SUPPORTED) {
            emit(id, STATE_ERROR, "language_not_supported");
            return;
        }

        currentId = id;
        lastIndex = chunks.size() - 1;
        for (int i = 0; i < chunks.size(); i++) {
            int result = tts.speak(
                    chunks.get(i),
                    i == 0 ? TextToSpeech.QUEUE_FLUSH : TextToSpeech.QUEUE_ADD,
                    null,
                    id + "#" + i);
            if (result != TextToSpeech.SUCCESS) {
                currentId = null;
                lastIndex = -1;
                emit(id, STATE_ERROR, "speak_failed");
                return;
            }
        }
        emit(id, STATE_START, "");
    }

    private void stopOnMain() {
        pendingId = null;
        pendingText = null;
        currentId = null;
        lastIndex = -1;
        if (tts != null) {
            tts.stop();
        }
    }

    /** Handles onDone/onError from a binder thread. */
    private void handleFinished(String utteranceId, String error) {
        if (utteranceId == null) {
            return;
        }
        int sep = utteranceId.lastIndexOf('#');
        if (sep < 0) {
            return;
        }
        final String id = utteranceId.substring(0, sep);
        final int index;
        try {
            index = Integer.parseInt(utteranceId.substring(sep + 1));
        } catch (NumberFormatException e) {
            return;
        }
        main.post(() -> {
            // A stop() or a newer speak() invalidates this callback; without
            // the check a cancelled run would still report "done" and make the
            // frontend advance its queue.
            if (!id.equals(currentId)) {
                return;
            }
            if (error == null && index != lastIndex) {
                return;
            }
            currentId = null;
            lastIndex = -1;
            emit(id, error == null ? STATE_DONE : STATE_ERROR, error == null ? "" : error);
        });
    }

    /** Must run on the main thread. */
    private void emit(String id, String state, String message) {
        if (listener != null) {
            listener.onState(id, state, message);
        }
    }

    /** Chinese text goes to the Chinese voice; pure Latin text to English. */
    static Locale pickLocale(String text) {
        for (int i = 0; i < text.length(); i++) {
            if (isCjk(text.charAt(i))) {
                return Locale.SIMPLIFIED_CHINESE;
            }
        }
        return Locale.US;
    }

    private static boolean isCjk(char c) {
        return (c >= 0x4E00 && c <= 0x9FFF)
                || (c >= 0x3400 && c <= 0x4DBF)
                || (c >= 0xF900 && c <= 0xFAFF);
    }

    /** Leaves headroom below the engine's hard limit (4000 on current Android). */
    private static int maxChunkLength() {
        return Math.max(200, Math.min(3000, TextToSpeech.getMaxSpeechInputLength() - 100));
    }
}
