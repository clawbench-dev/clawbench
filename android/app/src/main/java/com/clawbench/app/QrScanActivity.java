package com.clawbench.app;

import android.Manifest;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Bundle;
import android.util.DisplayMetrics;
import android.view.View;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.appcompat.app.AppCompatActivity;
import androidx.camera.core.AspectRatio;
import androidx.camera.core.CameraSelector;
import androidx.camera.core.ImageAnalysis;
import androidx.camera.core.ImageProxy;
import androidx.camera.core.Preview;
import androidx.camera.lifecycle.ProcessCameraProvider;
import androidx.camera.view.PreviewView;
import androidx.core.app.ActivityCompat;
import androidx.core.content.ContextCompat;

import com.google.android.material.button.MaterialButton;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.mlkit.vision.barcode.BarcodeScanner;
import com.google.mlkit.vision.barcode.BarcodeScannerOptions;
import com.google.mlkit.vision.barcode.BarcodeScanning;
import com.google.mlkit.vision.barcode.common.Barcode;
import com.google.mlkit.vision.common.InputImage;

import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Camera-based QR code scanning Activity.
 * Scans for clawbench://connect deep link QR codes.
 * Returns the scanned URI string as result data.
 */
public class QrScanActivity extends AppCompatActivity {

    private static final int CAMERA_PERMISSION_REQUEST = 1001;

    private PreviewView previewView;
    private View scanningOverlay;
    private MaterialButton flashBtn;
    private ExecutorService cameraExecutor;
    private ProcessCameraProvider cameraProvider;
    private boolean scanned = false;
    private boolean flashOn = false;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        previewView = new PreviewView(this);
        scanningOverlay = createScanningOverlay();

        // Simple layout: camera preview fills screen, overlay on top
        setContentView(createContentView());

        cameraExecutor = Executors.newSingleThreadExecutor();

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA)
                == PackageManager.PERMISSION_GRANTED) {
            startCamera();
        } else {
            ActivityCompat.requestPermissions(this,
                    new String[]{Manifest.permission.CAMERA}, CAMERA_PERMISSION_REQUEST);
        }
    }

    private View createContentView() {
        android.widget.FrameLayout root = new android.widget.FrameLayout(this);
        root.setLayoutParams(new android.widget.FrameLayout.LayoutParams(
                android.widget.FrameLayout.LayoutParams.MATCH_PARENT,
                android.widget.FrameLayout.LayoutParams.MATCH_PARENT));

        previewView.setLayoutParams(new android.widget.FrameLayout.LayoutParams(
                android.widget.FrameLayout.LayoutParams.MATCH_PARENT,
                android.widget.FrameLayout.LayoutParams.MATCH_PARENT));
        root.addView(previewView);
        root.addView(scanningOverlay);

        // Close button (top-left)
        MaterialButton closeBtn = new MaterialButton(this);
        closeBtn.setText("✕");
        closeBtn.setTextSize(18);
        closeBtn.setBackgroundColor(0x66000000);
        closeBtn.setTextColor(0xFFFFFFFF);
        closeBtn.setCornerRadius(24);
        closeBtn.setOnClickListener(v -> finish());
        android.widget.FrameLayout.LayoutParams closeLp =
                new android.widget.FrameLayout.LayoutParams(96, 96);
        closeLp.topMargin = 48;
        closeLp.leftMargin = 24;
        closeLp.gravity = android.view.Gravity.TOP | android.view.Gravity.START;
        root.addView(closeBtn, closeLp);

        return root;
    }

    private View createScanningOverlay() {
        // Simple text overlay
        android.widget.TextView hint = new android.widget.TextView(this);
        hint.setText("将二维码对准框内即可自动扫描");
        hint.setTextColor(0xCCFFFFFF);
        hint.setTextSize(14);
        hint.setGravity(android.view.Gravity.CENTER);
        android.widget.FrameLayout.LayoutParams lp =
                new android.widget.FrameLayout.LayoutParams(
                        android.widget.FrameLayout.LayoutParams.WRAP_CONTENT,
                        android.widget.FrameLayout.LayoutParams.WRAP_CONTENT);
        lp.gravity = android.view.Gravity.BOTTOM | android.view.Gravity.CENTER_HORIZONTAL;
        lp.bottomMargin = 120;
        hint.setLayoutParams(lp);
        return hint;
    }

    private void startCamera() {
        ListenableFuture<ProcessCameraProvider> cameraProviderFuture =
                ProcessCameraProvider.getInstance(this);

        cameraProviderFuture.addListener(() -> {
            try {
                cameraProvider = cameraProviderFuture.get();
                bindCameraUseCases();
            } catch (ExecutionException | InterruptedException e) {
                AppLog.e("QrScan", "Camera init failed", e);
                Toast.makeText(this, "相机初始化失败", Toast.LENGTH_SHORT).show();
                finish();
            }
        }, ContextCompat.getMainExecutor(this));
    }

    private void bindCameraUseCases() {
        Preview preview = new Preview.Builder().build();
        preview.setSurfaceProvider(previewView.getSurfaceProvider());

        BarcodeScannerOptions options = new BarcodeScannerOptions.Builder()
                .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
                .build();
        BarcodeScanner scanner = BarcodeScanning.getClient(options);

        ImageAnalysis imageAnalysis = new ImageAnalysis.Builder()
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .build();

        imageAnalysis.setAnalyzer(cameraExecutor, imageProxy -> {
            if (scanned) {
                imageProxy.close();
                return;
            }
            InputImage inputImage = InputImage.fromMediaImage(
                    imageProxy.getImage(), imageProxy.getImageInfo().getRotationDegrees());
            scanner.process(inputImage)
                    .addOnSuccessListener(barcodes -> {
                        for (Barcode barcode : barcodes) {
                            String rawValue = barcode.getRawValue();
                            if (rawValue != null && rawValue.startsWith("clawbench://connect")) {
                                scanned = true;
                                Intent result = new Intent();
                                result.putExtra("qr_data", rawValue);
                                setResult(RESULT_OK, result);
                                finish();
                                return;
                            }
                        }
                    })
                    .addOnFailureListener(e -> AppLog.e("QrScan", "Barcode scan failed", e))
                    .addOnCompleteListener(task -> imageProxy.close());
        });

        CameraSelector cameraSelector = new CameraSelector.Builder()
                .requireLensFacing(CameraSelector.LENS_FACING_BACK)
                .build();

        try {
            cameraProvider.unbindAll();
            cameraProvider.bindToLifecycle(this, cameraSelector, preview, imageAnalysis);
        } catch (Exception e) {
            AppLog.e("QrScan", "Bind camera failed", e);
        }
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, @NonNull String[] permissions,
                                            @NonNull int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        if (requestCode == CAMERA_PERMISSION_REQUEST) {
            if (grantResults.length > 0 && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
                startCamera();
            } else {
                Toast.makeText(this, "需要相机权限才能扫码", Toast.LENGTH_SHORT).show();
                finish();
            }
        }
    }

    @Override
    protected void onDestroy() {
        super.onDestroy();
        if (cameraExecutor != null) {
            cameraExecutor.shutdown();
        }
    }
}
