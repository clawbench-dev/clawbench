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

# ZXing (QR code decoding) — must keep explicitly because R8 shrink phase
# removes classes not reachable from known entry points; QrScanActivity is
# launched via Intent (not direct code reference), and ZXing readers are
# instantiated reflectively via ServiceLoader.
-keep class com.clawbench.app.QrScanActivity { *; }
-keep class com.google.zxing.** { *; }
-keep class * implements com.google.zxing.Reader { *; }
-dontwarn com.google.zxing.**
