# Keep JavaScript interface methods (inner class of MainActivity)
-keepclassmembers class com.clawbench.app.MainActivity$WebAppInterface {
    @android.webkit.JavascriptInterface <methods>;
}

# Keep JSch classes used for SSH tunneling
-keep class com.jcraft.jsch.** { *; }
-dontwarn com.jcraft.jsch.**

# OkHttp (used for native WebSocket in BackgroundService)
-dontwarn okhttp3.**
-dontwarn okio.**

# WorkManager — custom Worker subclass must be kept (instantiated via reflection)
-keep class com.clawbench.app.PendingEventsWorker { *; }
-dontwarn androidx.work.**

# ML Kit Barcode Scanning
-keep class com.google.mlkit.vision.barcode.** { *; }
-dontwarn com.google.mlkit.vision.barcode.**

# CameraX
-keep class androidx.camera.** { *; }
-dontwarn androidx.camera.**
