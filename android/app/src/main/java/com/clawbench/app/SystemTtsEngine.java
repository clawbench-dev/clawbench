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

        /**
         * Fires once when initialization finishes, with {@code ready},
         * {@code no_chinese} or {@code unavailable}.
         *
         * Needed because initialization is asynchronous: a caller that asks
         * "can this phone read?" right after turning the feature on would
         * otherwise only ever see {@code initializing} and have to poll.
         */
        void onInit(String status);
    }

    static final String STATE_START = "start";
    static final String STATE_DONE = "done";
    static final String STATE_ERROR = "error";

    static final String STATUS_READY = "ready";
    static final String STATUS_INITIALIZING = "initializing";
    static final String STATUS_UNAVAILABLE = "unavailable";
    /**
     * The engine works but has no Chinese voice installed.
     *
     * Kept separate from {@code unavailable}: English text can still be read,
     * so the read path may use it, while the settings switch (which exists for
     * Chinese replies) refuses to turn on.
     */
    static final String STATUS_NO_CHINESE = "no_chinese";

    private static final int INTERNAL_IDLE = 0;
    private static final int INTERNAL_INITIALIZING = 1;
    private static final int INTERNAL_READY = 2;
    private static final int INTERNAL_UNAVAILABLE = 3;
    private static final int INTERNAL_NO_CHINESE = 4;

    private static final String TAG = "SystemTts";

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
            case INTERNAL_NO_CHINESE:
                return STATUS_NO_CHINESE;
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
                    tts.setOnUtteranceProgressListener(progress);
                    // Initialization succeeding only means "an engine exists".
                    // Whether it can read Chinese is a separate question, and
                    // the one the settings switch actually cares about.
                    internalState = hasChineseVoice(tts) ? INTERNAL_READY : INTERNAL_NO_CHINESE;
                    AppLog.i(TAG, "init ok status=" + internalState + " " + describeEngines(tts));
                    emitInit();
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
                // -1 = ERROR. Usually "no default engine set" or, on API 30+,
                // the engine being invisible without the TTS_SERVICE query in
                // the manifest.
                AppLog.w(TAG, "init failed status=" + initStatus);
                emitInit();
                String id = pendingId;
                pendingId = null;
                pendingText = null;
                if (id != null) {
                    emit(id, STATE_ERROR, "init_failed");
                }
            });
        } catch (Throwable t) {
            internalState = INTERNAL_UNAVAILABLE;
            AppLog.w(TAG, "init threw: " + t);
            emitInit();
            String id = pendingId;
            pendingId = null;
            pendingText = null;
            if (id != null) {
                emit(id, STATE_ERROR, "init_failed");
            }
        }
    }

    /** Must run on the main thread. */
    private void emitInit() {
        if (listener == null) {
            return;
        }
        String status;
        switch (internalState) {
            case INTERNAL_READY:
                status = STATUS_READY;
                break;
            case INTERNAL_NO_CHINESE:
                status = STATUS_NO_CHINESE;
                break;
            default:
                status = STATUS_UNAVAILABLE;
                break;
        }
        listener.onInit(status);
    }

    /** True when the engine advertises a Chinese voice (any variant). */
    static boolean hasChineseVoice(TextToSpeech engine) {
        return engine.isLanguageAvailable(Locale.SIMPLIFIED_CHINESE) >= TextToSpeech.LANG_AVAILABLE
                || engine.isLanguageAvailable(Locale.CHINESE) >= TextToSpeech.LANG_AVAILABLE
                || engine.isLanguageAvailable(Locale.TRADITIONAL_CHINESE) >= TextToSpeech.LANG_AVAILABLE;
    }

    /**
     * Retry the remaining Chinese variants after the preferred one failed.
     *
     * <p>Engines differ in how they report locales: some only expose `zh`,
     * some only `zh_TW`. A phone that can read Chinese must not be rejected
     * because `zh_CN` specifically is missing.
     */
    static boolean applyLanguageFallback(TextToSpeech engine, String text) {
        Locale[] candidates = containsCjk(text)
                ? new Locale[]{Locale.CHINESE, Locale.TRADITIONAL_CHINESE}
                : new Locale[]{Locale.ENGLISH};
        for (Locale locale : candidates) {
            if (engine.setLanguage(locale) >= TextToSpeech.LANG_AVAILABLE) {
                return true;
            }
        }
        return false;
    }

    /** Engine list and language availability, for the client log. */
    static String describeEngines(TextToSpeech engine) {
        try {
            return "zhCN=" + engine.isLanguageAvailable(Locale.SIMPLIFIED_CHINESE)
                    + " zh=" + engine.isLanguageAvailable(Locale.CHINESE)
                    + " zhTW=" + engine.isLanguageAvailable(Locale.TRADITIONAL_CHINESE)
                    + " en=" + engine.isLanguageAvailable(Locale.US)
                    + " engines=" + engine.getEngines();
        } catch (Throwable t) {
            return "describe failed: " + t;
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
            // Retry with the bare / traditional variants before giving up: some
            // engines advertise `zh` or `zh_TW` but not `zh_CN`, and can still
            // read simplified text.
            if (!applyLanguageFallback(tts, text)) {
                AppLog.w(TAG, "no usable voice for text, " + describeEngines(tts));
                emit(id, STATE_ERROR, "language_not_supported");
                return;
            }
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
        return containsCjk(text) ? Locale.SIMPLIFIED_CHINESE : Locale.US;
    }

    static boolean containsCjk(String text) {
        for (int i = 0; i < text.length(); i++) {
            if (isCjk(text.charAt(i))) {
                return true;
            }
        }
        return false;
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
