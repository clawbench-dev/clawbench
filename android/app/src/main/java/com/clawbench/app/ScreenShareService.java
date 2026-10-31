package com.clawbench.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.content.res.Configuration;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.PixelFormat;
import android.graphics.Rect;
import android.hardware.display.DisplayManager;
import android.hardware.display.VirtualDisplay;
import android.media.AudioAttributes;
import android.media.AudioFormat;
import android.media.AudioPlaybackCaptureConfiguration;
import android.media.AudioRecord;
import android.media.projection.MediaProjection;
import android.media.projection.MediaProjectionManager;
import android.os.Build;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.IBinder;
import android.util.DisplayMetrics;
import android.view.WindowManager;

import androidx.core.app.NotificationCompat;
import androidx.core.content.ContextCompat;

import java.io.ByteArrayOutputStream;
import java.nio.ByteBuffer;

/**
 * Foreground service that owns one screen-share capture session.
 *
 * <p>Android WebView has no {@code getDisplayMedia}: screen capture is only
 * reachable through {@link MediaProjection}, which requires (a) a user consent
 * dialog, (b) a foreground service of type {@code mediaProjection} running
 * <em>before</em> the projection is created, and (c) a projection callback
 * registered before the virtual display exists. Those three rules are why this
 * is a service rather than a few calls inside the activity: the app is
 * backgrounded for the entire time the user is sharing (they are looking at
 * other apps), and an activity-scoped capture would be torn down.
 *
 * <p>The service does not talk to the WebView. It publishes into
 * {@link ScreenShareStatus}, and the bridge in {@code MainActivity} drains that
 * from the page's own thread — the same decoupling that lets the capture
 * survive a WebView reload.
 *
 * <p>Two producers run here:
 * <ul>
 *   <li><b>Video</b>: {@link VirtualDisplay} mirrors the screen into an
 *       {@link android.media.ImageReader}; each frame is scaled and JPEG-encoded
 *       on the reader's handler thread. Encoding inline is the frame-rate
 *       limiter: if it cannot keep up, {@code acquireLatestImage()} simply skips
 *       the frames in between instead of queueing them.</li>
 *   <li><b>System audio</b>: {@link AudioPlaybackCaptureConfiguration} on API 29+.
 *       Only apps that permit playback capture are included — a voice call is
 *       never capturable, by platform design.</li>
 * </ul>
 */
public class ScreenShareService extends Service {

    private static final String TAG = "ScreenShare";
    private static final String ACTION_START = "com.clawbench.app.action.SCREEN_SHARE_START";
    private static final String ACTION_STOP = "com.clawbench.app.action.SCREEN_SHARE_STOP";
    private static final String EXTRA_RESULT_CODE = "resultCode";
    private static final String EXTRA_RESULT_DATA = "resultData";
    private static final String EXTRA_OPTIONS = "options";

    private static final String CHANNEL_ID = "clawbench_screen_share";
    private static final int NOTIFICATION_ID = 0x5C01;

    /** 48 kHz stereo 16-bit: the format playback capture is defined against. */
    private static final int AUDIO_SAMPLE_RATE = 48000;
    private static final int AUDIO_CHANNELS = 2;
    /** 100 ms per delivered chunk; matches the frontend's jitter-buffer unit. */
    private static final int AUDIO_CHUNK_BYTES =
            AUDIO_SAMPLE_RATE * AUDIO_CHANNELS * 2 / 10;

    private MediaProjection projection;
    private VirtualDisplay virtualDisplay;
    private android.media.ImageReader imageReader;
    private HandlerThread captureThread;
    private Handler captureHandler;

    private Thread audioThread;
    private volatile boolean audioRunning;
    private AudioRecord audioRecord;

    private ScreenShareOptions options = ScreenShareOptions.parse(null);
    private boolean systemAudioActive;

    /** Reused across frames so the encoder does not allocate 10 bitmaps a second. */
    private Bitmap sourceBitmap;
    private Bitmap targetBitmap;
    private final Canvas targetCanvas = new Canvas();
    private final Paint scalePaint = new Paint(Paint.FILTER_BITMAP_FLAG);
    private final ByteArrayOutputStream jpegBuffer = new ByteArrayOutputStream();
    private long lastEncodeMs;

    private int captureWidth;
    private int captureHeight;

    // ── Lifecycle ────────────────────────────────────────────────────────────

    /** Begin a session. Must be called with the consent result from the system dialog. */
    static void start(Context context, int resultCode, Intent resultData, ScreenShareOptions opts) {
        Intent intent = new Intent(context, ScreenShareService.class);
        intent.setAction(ACTION_START);
        intent.putExtra(EXTRA_RESULT_CODE, resultCode);
        intent.putExtra(EXTRA_RESULT_DATA, resultData);
        intent.putExtra(EXTRA_OPTIONS, opts.toJson());
        ContextCompat.startForegroundService(context, intent);
    }

    /** End the session. Safe to call when nothing is running. */
    static void stop(Context context) {
        try {
            context.stopService(new Intent(context, ScreenShareService.class));
        } catch (Exception e) {
            AppLog.w(TAG, "stopService failed", e);
        }
    }

    @Override
    public void onCreate() {
        super.onCreate();
        createNotificationChannel();
        captureThread = new HandlerThread("clawbench-screen-capture");
        captureThread.start();
        captureHandler = new Handler(captureThread.getLooper());
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? null : intent.getAction();
        if (ACTION_STOP.equals(action)) {
            stopSelf();
            return START_NOT_STICKY;
        }
        if (!ACTION_START.equals(action)) {
            // Restarted by the system with no intent (or an unknown action):
            // there is no consent data to resume with, so a capture cannot be
            // reconstructed. End rather than sit in the foreground doing nothing.
            stopSelf();
            return START_NOT_STICKY;
        }

        // Android requires startForeground() within 5 s of startForegroundService,
        // and (API 29+) the service must already be foreground with the
        // mediaProjection type before getMediaProjection() is called.
        startForegroundCompat(buildNotification());

        options = ScreenShareOptions.parse(intent.getStringExtra(EXTRA_OPTIONS));
        int resultCode = intent.getIntExtra(EXTRA_RESULT_CODE, 0);
        Intent resultData = intent.getParcelableExtra(EXTRA_RESULT_DATA);
        if (resultData == null) {
            fail("no projection consent data");
            return START_NOT_STICKY;
        }
        beginProjection(resultCode, resultData);
        return START_NOT_STICKY;
    }

    @Override
    public void onDestroy() {
        stopAudio();
        releaseDisplay();
        if (projection != null) {
            try {
                projection.stop();
            } catch (Exception e) {
                AppLog.w(TAG, "projection.stop failed", e);
            }
            projection = null;
        }
        if (captureThread != null) {
            captureThread.quitSafely();
            captureThread = null;
            captureHandler = null;
        }
        // The bridge keeps its listener until the session ends; clearing it here
        // means a late frame from a dying thread cannot touch a WebView that may
        // already be gone.
        ScreenShareStatus.setTickListener(null);
        ScreenShareStatus.reset();
        super.onDestroy();
    }

    @Override
    public void onConfigurationChanged(Configuration newConfig) {
        super.onConfigurationChanged(newConfig);
        // A phone rotates mid-share more often than not. The virtual display keeps
        // the size it was created with, so without this the picture would be
        // stretched for the rest of the session.
        if (projection != null) {
            Handler handler = captureHandler;
            if (handler != null) handler.post(this::recreateDisplay);
        }
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    // ── Projection ───────────────────────────────────────────────────────────

    private void beginProjection(int resultCode, Intent resultData) {
        MediaProjectionManager manager =
                (MediaProjectionManager) getSystemService(Context.MEDIA_PROJECTION_SERVICE);
        if (manager == null) {
            fail("MediaProjectionManager unavailable");
            return;
        }
        try {
            projection = manager.getMediaProjection(resultCode, resultData);
        } catch (Exception e) {
            fail("getMediaProjection failed: " + e.getMessage());
            return;
        }
        if (projection == null) {
            fail("projection not granted");
            return;
        }
        // Required before createVirtualDisplay() on API 34+, and the only way to
        // learn that the user revoked the share from the system UI.
        projection.registerCallback(new MediaProjection.Callback() {
            @Override
            public void onStop() {
                AppLog.i(TAG, "projection stopped by the system");
                stopSelf();
            }
        }, captureHandler);

        if (!createDisplay()) {
            return;
        }
        if (options.systemAudio && Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startAudio();
        }
        ScreenShareStatus.setRunning(captureWidth, captureHeight, systemAudioActive);
        AppLog.i(TAG, "capture running " + captureWidth + "x" + captureHeight
                + " fps=" + options.fps + " systemAudio=" + systemAudioActive);
    }

    private boolean createDisplay() {
        DisplayMetrics metrics = readDisplayMetrics();
        if (metrics == null || metrics.widthPixels <= 0 || metrics.heightPixels <= 0) {
            fail("cannot read the display size");
            return false;
        }
        captureWidth = metrics.widthPixels;
        captureHeight = metrics.heightPixels;

        try {
            imageReader = android.media.ImageReader.newInstance(
                    captureWidth, captureHeight, PixelFormat.RGBA_8888, 2);
        } catch (Exception e) {
            fail("ImageReader failed: " + e.getMessage());
            return false;
        }
        imageReader.setOnImageAvailableListener(this::onImageAvailable, captureHandler);

        try {
            virtualDisplay = projection.createVirtualDisplay(
                    "clawbench-screen-share",
                    captureWidth,
                    captureHeight,
                    metrics.densityDpi,
                    DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR,
                    imageReader.getSurface(),
                    null,
                    captureHandler);
        } catch (Exception e) {
            fail("createVirtualDisplay failed: " + e.getMessage());
            return false;
        }
        return virtualDisplay != null;
    }

    private void recreateDisplay() {
        if (projection == null) return;
        releaseDisplay();
        if (createDisplay()) {
            ScreenShareStatus.setRunning(captureWidth, captureHeight, systemAudioActive);
        }
    }

    private void releaseDisplay() {
        if (imageReader != null) {
            // Detach the listener first: releasing the reader while a callback is
            // in flight would deliver an already-closed Image.
            try {
                imageReader.setOnImageAvailableListener(null, null);
            } catch (Exception ignored) {
                // Reader already closed.
            }
            try {
                imageReader.close();
            } catch (Exception ignored) {
                // Already closed.
            }
            imageReader = null;
        }
        if (virtualDisplay != null) {
            try {
                virtualDisplay.release();
            } catch (Exception ignored) {
                // Already released.
            }
            virtualDisplay = null;
        }
        if (sourceBitmap != null) {
            sourceBitmap.recycle();
            sourceBitmap = null;
        }
        if (targetBitmap != null) {
            targetBitmap.recycle();
            targetBitmap = null;
        }
    }

    @SuppressWarnings("deprecation")
    private DisplayMetrics readDisplayMetrics() {
        WindowManager wm = (WindowManager) getSystemService(Context.WINDOW_SERVICE);
        if (wm == null) return null;
        DisplayMetrics metrics = new DisplayMetrics();
        // getRealMetrics includes the navigation bar and is the size the mirrored
        // picture should match; getMetrics() would crop it. Deprecated on API 30
        // but still the only call that works across our whole minSdk range.
        wm.getDefaultDisplay().getRealMetrics(metrics);
        return metrics;
    }

    // ── Video ────────────────────────────────────────────────────────────────

    private void onImageAvailable(android.media.ImageReader reader) {
        android.media.Image image = null;
        try {
            image = reader.acquireLatestImage();
            if (image == null) return;

            long now = System.currentTimeMillis();
            long intervalMs = Math.max(1, 1000L / Math.max(1, options.fps));
            if (lastEncodeMs != 0 && now - lastEncodeMs < intervalMs) {
                return; // Too soon: drop this one, the next frame will do.
            }
            lastEncodeMs = now;

            android.media.Image.Plane plane = image.getPlanes()[0];
            int pixelStride = plane.getPixelStride();
            int rowStride = plane.getRowStride();
            int rowPadding = rowStride - pixelStride * captureWidth;
            int paddedWidth = captureWidth + rowPadding / Math.max(1, pixelStride);

            ByteBuffer buffer = plane.getBuffer();
            if (sourceBitmap == null
                    || sourceBitmap.getWidth() != paddedWidth
                    || sourceBitmap.getHeight() != captureHeight) {
                if (sourceBitmap != null) sourceBitmap.recycle();
                sourceBitmap = Bitmap.createBitmap(paddedWidth, captureHeight, Bitmap.Config.ARGB_8888);
            }
            buffer.rewind();
            sourceBitmap.copyPixelsFromBuffer(buffer);

            int[] target = scaleTo(captureWidth, captureHeight, options.maxWidth);
            if (targetBitmap == null
                    || targetBitmap.getWidth() != target[0]
                    || targetBitmap.getHeight() != target[1]) {
                if (targetBitmap != null) targetBitmap.recycle();
                targetBitmap = Bitmap.createBitmap(target[0], target[1], Bitmap.Config.ARGB_8888);
                targetCanvas.setBitmap(targetBitmap);
            }
            // One draw handles both the row-padding crop and the downscale, and
            // reuses the target bitmap instead of allocating per frame.
            targetCanvas.drawBitmap(
                    sourceBitmap,
                    new Rect(0, 0, captureWidth, captureHeight),
                    new Rect(0, 0, targetBitmap.getWidth(), targetBitmap.getHeight()),
                    scalePaint);

            jpegBuffer.reset();
            if (!targetBitmap.compress(Bitmap.CompressFormat.JPEG, options.quality, jpegBuffer)) {
                return;
            }
            ScreenShareStatus.putFrame(jpegBuffer.toByteArray());
        } catch (Exception e) {
            AppLog.w(TAG, "frame encode failed", e);
        } finally {
            if (image != null) {
                try {
                    image.close();
                } catch (Exception ignored) {
                    // Reader closed underneath us.
                }
            }
        }
    }

    /**
     * Longest side of the encoded frame, capped at {@code maxWidth}.
     *
     * <p>Phones are tall: capping the <em>longest</em> side (rather than the
     * width) keeps a 1080x2400 portrait screen at a sane pixel count instead of
     * encoding 2400-pixel-tall frames the viewer will downscale anyway.
     */
    static int[] scaleTo(int width, int height, int maxLongSide) {
        int longest = Math.max(width, height);
        if (longest <= maxLongSide) return new int[]{width, height};
        double factor = (double) maxLongSide / longest;
        return new int[]{
                Math.max(2, (int) Math.round(width * factor)),
                Math.max(2, (int) Math.round(height * factor)),
        };
    }

    // ── System audio ─────────────────────────────────────────────────────────

    private void startAudio() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return;
        if (ContextCompat.checkSelfPermission(this, android.Manifest.permission.RECORD_AUDIO)
                != android.content.pm.PackageManager.PERMISSION_GRANTED) {
            // Degrade rather than fail: the picture is still worth sharing.
            AppLog.w(TAG, "RECORD_AUDIO missing — sharing without system audio");
            return;
        }
        audioRunning = true;
        audioThread = new Thread(this::audioLoop, "clawbench-screen-audio");
        audioThread.start();
    }

    private void stopAudio() {
        audioRunning = false;
        Thread thread = audioThread;
        audioThread = null;
        if (thread != null) {
            try {
                thread.join(500);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }
        AudioRecord record = audioRecord;
        audioRecord = null;
        if (record != null) {
            try {
                record.stop();
            } catch (Exception ignored) {
                // Never started.
            }
            try {
                record.release();
            } catch (Exception ignored) {
                // Already released.
            }
        }
    }

    private void audioLoop() {
        AudioRecord record = null;
        try {
            AudioPlaybackCaptureConfiguration config =
                    new AudioPlaybackCaptureConfiguration.Builder(projection)
                            // Media and games are the two usages that carry content a
                            // viewer would want. Voice communication is excluded by the
                            // platform itself and cannot be added.
                            .addMatchingUsage(AudioAttributes.USAGE_MEDIA)
                            .addMatchingUsage(AudioAttributes.USAGE_GAME)
                            .addMatchingUsage(AudioAttributes.USAGE_UNKNOWN)
                            .build();
            AudioFormat format = new AudioFormat.Builder()
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .setSampleRate(AUDIO_SAMPLE_RATE)
                    .setChannelMask(AudioFormat.CHANNEL_IN_STEREO)
                    .build();
            int minBuffer = AudioRecord.getMinBufferSize(
                    AUDIO_SAMPLE_RATE, AudioFormat.CHANNEL_IN_STEREO, AudioFormat.ENCODING_PCM_16BIT);
            if (minBuffer <= 0) minBuffer = AUDIO_CHUNK_BYTES;

            record = new AudioRecord.Builder()
                    .setAudioFormat(format)
                    .setBufferSizeInBytes(Math.max(minBuffer * 2, AUDIO_CHUNK_BYTES * 2))
                    .setAudioPlaybackCaptureConfig(config)
                    .build();
            if (record.getState() != AudioRecord.STATE_INITIALIZED) {
                AppLog.w(TAG, "AudioRecord not initialized — no system audio");
                return;
            }
            audioRecord = record;
            record.startRecording();
            systemAudioActive = true;
            ScreenShareStatus.setRunning(captureWidth, captureHeight, true);

            byte[] chunk = new byte[AUDIO_CHUNK_BYTES];
            int filled = 0;
            while (audioRunning) {
                int read = record.read(chunk, filled, chunk.length - filled);
                if (read <= 0) {
                    // -6 (dead object) and friends mean the projection is gone.
                    if (read < 0) break;
                    continue;
                }
                filled += read;
                if (filled == chunk.length) {
                    byte[] out = new byte[AUDIO_CHUNK_BYTES];
                    System.arraycopy(chunk, 0, out, 0, chunk.length);
                    ScreenShareStatus.putAudio(out);
                    filled = 0;
                }
            }
        } catch (Exception e) {
            AppLog.w(TAG, "system audio capture failed", e);
        } finally {
            if (record != null) {
                try {
                    record.stop();
                } catch (Exception ignored) {
                    // Never started.
                }
                try {
                    record.release();
                } catch (Exception ignored) {
                    // Already released.
                }
            }
        }
    }

    // ── Notification ─────────────────────────────────────────────────────────

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return;
        NotificationManager nm = getSystemService(NotificationManager.class);
        if (nm == null) return;
        NotificationChannel channel = new NotificationChannel(
                CHANNEL_ID,
                getString(R.string.notif_channel_screen_share),
                NotificationManager.IMPORTANCE_LOW);
        channel.setDescription(getString(R.string.notif_channel_screen_share_desc));
        channel.setShowBadge(false);
        nm.createNotificationChannel(channel);
    }

    private Notification buildNotification() {
        Intent open = new Intent(this, MainActivity.class);
        open.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        PendingIntent contentIntent = PendingIntent.getActivity(
                this, 0, open,
                Build.VERSION.SDK_INT >= Build.VERSION_CODES.M
                        ? PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE
                        : PendingIntent.FLAG_UPDATE_CURRENT);
        return new NotificationCompat.Builder(this, CHANNEL_ID)
                .setContentTitle(getString(R.string.notif_screen_share_title))
                .setContentText(getString(R.string.notif_screen_share_text))
                .setSmallIcon(R.drawable.ic_notification)
                .setContentIntent(contentIntent)
                .setOngoing(true)
                .setPriority(NotificationCompat.PRIORITY_LOW)
                .build();
    }

    private void startForegroundCompat(Notification notification) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, notification,
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PROJECTION);
        } else {
            startForeground(NOTIFICATION_ID, notification);
        }
    }

    private void fail(String message) {
        AppLog.w(TAG, message);
        ScreenShareStatus.setError(message);
        stopSelf();
    }
}
