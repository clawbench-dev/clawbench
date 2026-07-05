package com.clawbench.app;

import android.Manifest;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.ImageFormat;
import android.graphics.SurfaceTexture;
import android.hardware.camera2.CameraCaptureSession;
import android.hardware.camera2.CameraCharacteristics;
import android.hardware.camera2.CameraDevice;
import android.hardware.camera2.CameraManager;
import android.hardware.camera2.CaptureRequest;
import android.hardware.camera2.params.StreamConfigurationMap;
import android.media.Image;
import android.media.ImageReader;
import android.os.Bundle;
import android.os.Handler;
import android.os.HandlerThread;
import android.util.Size;
import android.view.Surface;
import android.view.TextureView;
import android.view.View;
import android.widget.FrameLayout;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.app.ActivityCompat;
import androidx.core.content.ContextCompat;

import com.google.android.material.button.MaterialButton;
import com.google.zxing.BinaryBitmap;
import com.google.zxing.DecodeHintType;
import com.google.zxing.MultiFormatReader;
import com.google.zxing.PlanarYUVLuminanceSource;
import com.google.zxing.Result;
import com.google.zxing.common.HybridBinarizer;

import java.nio.ByteBuffer;
import java.util.HashMap;
import java.util.Map;

/**
 * Camera-based QR code scanning Activity using camera2 + ZXing.
 * Scans for clawbench://connect deep link QR codes.
 * Returns the scanned URI string as result data.
 */
public class QrScanActivity extends AppCompatActivity {

    private static final String TAG = "QrScan";
    private static final int CAMERA_PERMISSION_REQUEST = 1001;
    private static final int MAX_PREVIEW_WIDTH = 1280;
    private static final int MAX_PREVIEW_HEIGHT = 720;

    private TextureView textureView;
    private CameraDevice cameraDevice;
    private CameraCaptureSession captureSession;
    private ImageReader imageReader;
    private Handler backgroundHandler;
    private HandlerThread backgroundThread;
    private MultiFormatReader zxingReader;
    private volatile boolean scanned = false;
    private volatile boolean cameraOpening = false;
    private Size previewSize;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        // Install a temporary uncaught exception handler to log crash details
        Thread.setDefaultUncaughtExceptionHandler((thread, throwable) -> {
            AppLog.e(TAG, "UNCAUGHT EXCEPTION in QrScanActivity", throwable);
            // Also write to a crash file that persists
            try {
                java.io.FileWriter fw = new java.io.FileWriter(
                        new java.io.File(getCacheDir(), "qr_scan_crash.txt"), true);
                fw.append(new java.text.SimpleDateFormat("yyyy-MM-dd HH:mm:ss", java.util.Locale.US)
                        .format(new java.util.Date())).append("\n");
                fw.append(thread.toString()).append("\n");
                fw.append(throwable.toString()).append("\n");
                for (StackTraceElement ste : throwable.getStackTrace()) {
                    fw.append("  at ").append(ste.toString()).append("\n");
                }
                if (throwable.getCause() != null) {
                    fw.append("Caused by: ").append(throwable.getCause().toString()).append("\n");
                    for (StackTraceElement ste : throwable.getCause().getStackTrace()) {
                        fw.append("  at ").append(ste.toString()).append("\n");
                    }
                }
                fw.append("\n");
                fw.close();
            } catch (Exception ignored) {}
            // Let the default handler kill the process
            android.os.Process.killProcess(android.os.Process.myPid());
        });

        setContentView(createContentView());

        // ZXing reader, QR only
        try {
            Map<DecodeHintType, Object> hints = new HashMap<>();
            hints.put(DecodeHintType.POSSIBLE_FORMATS,
                    java.util.Arrays.asList(com.google.zxing.BarcodeFormat.QR_CODE));
            zxingReader = new MultiFormatReader();
            zxingReader.setHints(hints);
        } catch (Exception e) {
            AppLog.e(TAG, "ZXing init failed", e);
            Toast.makeText(this, "扫码组件初始化失败", Toast.LENGTH_SHORT).show();
            finish();
            return;
        }

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA)
                == PackageManager.PERMISSION_GRANTED) {
            textureView.setSurfaceTextureListener(surfaceTextureListener);
        } else {
            ActivityCompat.requestPermissions(this,
                    new String[]{Manifest.permission.CAMERA}, CAMERA_PERMISSION_REQUEST);
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        if (textureView.isAvailable() && cameraDevice == null && !cameraOpening) {
            openCamera(textureView.getWidth(), textureView.getHeight());
        }
    }

    @Override
    protected void onPause() {
        super.onPause();
        closeCamera();
    }

    private View createContentView() {
        FrameLayout root = new FrameLayout(this);
        root.setLayoutParams(new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT));

        textureView = new TextureView(this);
        textureView.setLayoutParams(new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT));
        root.addView(textureView);

        // Hint text
        TextView hint = new TextView(this);
        hint.setText("将二维码对准框内即可自动扫描");
        hint.setTextColor(0xCCFFFFFF);
        hint.setTextSize(14);
        hint.setGravity(android.view.Gravity.CENTER);
        FrameLayout.LayoutParams hintLp = new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.WRAP_CONTENT, FrameLayout.LayoutParams.WRAP_CONTENT);
        hintLp.gravity = android.view.Gravity.BOTTOM | android.view.Gravity.CENTER_HORIZONTAL;
        hintLp.bottomMargin = 120;
        hint.setLayoutParams(hintLp);
        root.addView(hint);

        // Close button (top-left)
        MaterialButton closeBtn = new MaterialButton(this);
        closeBtn.setText("✕");
        closeBtn.setTextSize(18);
        closeBtn.setBackgroundColor(0x66000000);
        closeBtn.setTextColor(0xFFFFFFFF);
        closeBtn.setCornerRadius(24);
        closeBtn.setOnClickListener(v -> finish());
        FrameLayout.LayoutParams closeLp = new FrameLayout.LayoutParams(96, 96);
        closeLp.topMargin = 48;
        closeLp.leftMargin = 24;
        closeLp.gravity = android.view.Gravity.TOP | android.view.Gravity.START;
        root.addView(closeBtn, closeLp);

        return root;
    }

    private final TextureView.SurfaceTextureListener surfaceTextureListener =
            new TextureView.SurfaceTextureListener() {
                @Override
                public void onSurfaceTextureAvailable(@NonNull SurfaceTexture surface, int width, int height) {
                    openCamera(width, height);
                }

                @Override
                public void onSurfaceTextureSizeChanged(@NonNull SurfaceTexture surface, int width, int height) {}

                @Override
                public boolean onSurfaceTextureDestroyed(@NonNull SurfaceTexture surface) {
                    return true;
                }

                @Override
                public void onSurfaceTextureUpdated(@NonNull SurfaceTexture surface) {}
            };

    private void startBackgroundThread() {
        backgroundThread = new HandlerThread("CameraBackground");
        backgroundThread.start();
        backgroundHandler = new Handler(backgroundThread.getLooper());
    }

    private void stopBackgroundThread() {
        if (backgroundThread != null) {
            backgroundThread.quitSafely();
            try {
                backgroundThread.join();
            } catch (InterruptedException e) {
                // ignore
            }
            backgroundThread = null;
            backgroundHandler = null;
        }
    }

    @SuppressWarnings("MissingPermission")
    private void openCamera(int width, int height) {
        if (cameraOpening || cameraDevice != null) return;
        cameraOpening = true;

        if (backgroundThread == null) {
            startBackgroundThread();
        }
        CameraManager manager = (CameraManager) getSystemService(CAMERA_SERVICE);
        try {
            String cameraId = null;
            for (String id : manager.getCameraIdList()) {
                CameraCharacteristics chars = manager.getCameraCharacteristics(id);
                Integer facing = chars.get(CameraCharacteristics.LENS_FACING);
                if (facing != null && facing == CameraCharacteristics.LENS_FACING_BACK) {
                    cameraId = id;
                    break;
                }
            }
            if (cameraId == null) {
                cameraOpening = false;
                Toast.makeText(this, "未找到后置摄像头", Toast.LENGTH_SHORT).show();
                finish();
                return;
            }

            CameraCharacteristics characteristics = manager.getCameraCharacteristics(cameraId);
            StreamConfigurationMap map = characteristics.get(
                    CameraCharacteristics.SCALER_STREAM_CONFIGURATION_MAP);
            if (map == null) {
                cameraOpening = false;
                Toast.makeText(this, "相机配置不可用", Toast.LENGTH_SHORT).show();
                finish();
                return;
            }

            // Choose optimal preview size (capped at 1280x720 for QR scanning)
            Size[] sizes = map.getOutputSizes(SurfaceTexture.class);
            previewSize = chooseOptimalSize(sizes, width, height);

            // ImageReader for frame capture (YUV_420_888)
            int readerWidth = previewSize.getWidth();
            int readerHeight = previewSize.getHeight();
            imageReader = ImageReader.newInstance(readerWidth, readerHeight,
                    ImageFormat.YUV_420_888, 2);
            imageReader.setOnImageAvailableListener(onImageAvailableListener, backgroundHandler);

            manager.openCamera(cameraId, new CameraDevice.StateCallback() {
                @Override
                public void onOpened(@NonNull CameraDevice camera) {
                    cameraDevice = camera;
                    cameraOpening = false;
                    createCameraPreviewSession();
                }

                @Override
                public void onDisconnected(@NonNull CameraDevice camera) {
                    camera.close();
                    cameraDevice = null;
                    cameraOpening = false;
                }

                @Override
                public void onError(@NonNull CameraDevice camera, int error) {
                    camera.close();
                    cameraDevice = null;
                    cameraOpening = false;
                    AppLog.e(TAG, "Camera open error: " + error);
                    runOnUiThread(() -> {
                        Toast.makeText(QrScanActivity.this, "相机打开失败", Toast.LENGTH_SHORT).show();
                        finish();
                    });
                }
            }, backgroundHandler);
        } catch (Exception e) {
            cameraOpening = false;
            AppLog.e(TAG, "openCamera failed", e);
            Toast.makeText(this, "相机初始化失败", Toast.LENGTH_SHORT).show();
            finish();
        }
    }

    private void createCameraPreviewSession() {
        try {
            SurfaceTexture texture = textureView.getSurfaceTexture();
            if (texture == null) {
                AppLog.e(TAG, "SurfaceTexture is null in createCameraPreviewSession");
                return;
            }
            texture.setDefaultBufferSize(previewSize.getWidth(), previewSize.getHeight());
            Surface previewSurface = new Surface(texture);
            Surface readerSurface = imageReader.getSurface();

            CaptureRequest.Builder builder = cameraDevice.createCaptureRequest(
                    CameraDevice.TEMPLATE_PREVIEW);
            builder.addTarget(previewSurface);
            builder.addTarget(readerSurface);

            cameraDevice.createCaptureSession(
                    java.util.Arrays.asList(previewSurface, readerSurface),
                    new CameraCaptureSession.StateCallback() {
                        @Override
                        public void onConfigured(@NonNull CameraCaptureSession session) {
                            if (cameraDevice == null) return;
                            captureSession = session;
                            try {
                                builder.set(CaptureRequest.CONTROL_AF_MODE,
                                        CaptureRequest.CONTROL_AF_MODE_CONTINUOUS_PICTURE);
                                session.setRepeatingRequest(builder.build(), null, backgroundHandler);
                            } catch (Exception e) {
                                AppLog.e(TAG, "setRepeatingRequest failed", e);
                            }
                        }

                        @Override
                        public void onConfigureFailed(@NonNull CameraCaptureSession session) {
                            AppLog.e(TAG, "Camera capture session configure failed");
                            runOnUiThread(() -> {
                                Toast.makeText(QrScanActivity.this, "相机配置失败", Toast.LENGTH_SHORT).show();
                                finish();
                            });
                        }
                    }, backgroundHandler);
        } catch (Exception e) {
            AppLog.e(TAG, "createCameraPreviewSession failed", e);
        }
    }

    private final ImageReader.OnImageAvailableListener onImageAvailableListener = reader -> {
        if (scanned) return;
        Image image = null;
        try {
            image = reader.acquireLatestImage();
            if (image == null) return;

            Image.Plane[] planes = image.getPlanes();
            Image.Plane yPlane = planes[0];
            ByteBuffer yBuffer = yPlane.getBuffer();
            int yRowStride = yPlane.getRowStride();

            int width = image.getWidth();
            int height = image.getHeight();

            // Build Y-only byte array for ZXing (luminance)
            byte[] yData = new byte[width * height];
            for (int row = 0; row < height; row++) {
                yBuffer.position(row * yRowStride);
                yBuffer.get(yData, row * width, width);
            }

            PlanarYUVLuminanceSource source = new PlanarYUVLuminanceSource(
                    yData, width, height, 0, 0, width, height, false);
            BinaryBitmap bitmap = new BinaryBitmap(new HybridBinarizer(source));

            Result result = zxingReader.decode(bitmap);
            String text = result.getText();
            if (text != null && text.startsWith("clawbench://connect")) {
                scanned = true;
                Intent intent = new Intent();
                intent.putExtra("qr_data", text);
                setResult(RESULT_OK, intent);
                runOnUiThread(this::finish);
            }
        } catch (com.google.zxing.NotFoundException e) {
            // No QR in frame — normal, skip
        } catch (Exception e) {
            AppLog.e(TAG, "Decode error", e);
        } finally {
            if (image != null) image.close();
        }
    };

    private static Size chooseOptimalSize(Size[] choices, int textureWidth, int textureHeight) {
        if (choices == null || choices.length == 0) {
            return new Size(textureWidth, textureHeight);
        }
        // Collect sizes close to the target aspect ratio, capped at MAX_PREVIEW resolution
        float targetRatio = (float) textureWidth / textureHeight;
        Size best = null;
        float minDiff = Float.MAX_VALUE;
        for (Size s : choices) {
            // Skip resolutions above our cap — wasteful for QR scanning
            if (s.getWidth() > MAX_PREVIEW_WIDTH || s.getHeight() > MAX_PREVIEW_HEIGHT) continue;
            float ratio = (float) s.getWidth() / s.getHeight();
            float diff = Math.abs(ratio - targetRatio);
            if (diff < minDiff || (diff == minDiff && best != null
                    && s.getWidth() < best.getWidth())) {
                best = s;
                minDiff = diff;
            }
        }
        // Fallback: if all sizes exceed the cap, pick the smallest available
        if (best == null) {
            for (Size s : choices) {
                if (best == null || s.getWidth() * s.getHeight() < best.getWidth() * best.getHeight()) {
                    best = s;
                }
            }
        }
        return best != null ? best : choices[0];
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, @NonNull String[] permissions,
                                            @NonNull int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        if (requestCode == CAMERA_PERMISSION_REQUEST) {
            if (grantResults.length > 0 && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
                textureView.setSurfaceTextureListener(surfaceTextureListener);
            } else {
                Toast.makeText(this, "需要相机权限才能扫码", Toast.LENGTH_SHORT).show();
                finish();
            }
        }
    }

    @Override
    protected void onDestroy() {
        super.onDestroy();
        closeCamera();
        stopBackgroundThread();
    }

    private void closeCamera() {
        if (captureSession != null) {
            captureSession.close();
            captureSession = null;
        }
        if (cameraDevice != null) {
            cameraDevice.close();
            cameraDevice = null;
        }
        if (imageReader != null) {
            imageReader.close();
            imageReader = null;
        }
    }
}
