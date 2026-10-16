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

# Custom WebView subclass, inflated BY NAME from activity_main.xml
# (<com.clawbench.app.ClawBenchWebView ...>). The framework instantiates it
# reflectively via its (Context, AttributeSet) constructor, so R8 must not
# rename or strip it — otherwise the release build fails to inflate the layout
# and the app shows no WebView at all. Debug builds are unaffected (no
# shrinking), so this only ever breaks on a release APK.
-keep class com.clawbench.app.ClawBenchWebView { *; }
