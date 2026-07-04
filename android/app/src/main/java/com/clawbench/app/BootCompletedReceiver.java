package com.clawbench.app;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.os.Build;
import android.os.PowerManager;

/**
 * Receiver for BOOT_COMPLETED that restarts BackgroundService
 * and schedules WorkManager fallback polling.
 *
 * This ensures SSH tunnels and native WS event push are restored
 * after device reboot. On Chinese ROMs that kill foreground services
 * during deep Doze, the WorkManager fallback provides periodic polling.
 *
 * On Android 12+ (API 31), starting a foreground service from background
 * (where BOOT_COMPLETED receivers run) requires the app to be on the
 * battery optimization allowlist. If the user hasn't granted this, we
 * fall back to WorkManager-only mode.
 */
public class BootCompletedReceiver extends BroadcastReceiver {

    private static final String TAG = "ClawBench";
    private static final String PREFS_NAME = "clawbench_prefs";
    private static final String KEY_SERVER_URL = "server_url";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (!Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) return;

        SharedPreferences prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        String serverUrl = prefs.getString(KEY_SERVER_URL, "");

        if (serverUrl.isEmpty()) {
            AppLog.d(TAG, "BootReceiver: no server URL configured, skipping");
            return;
        }

        AppLog.i(TAG, "BootReceiver: device booted, restoring background service");

        // On Android 12+, starting a foreground service from background requires
        // battery optimization exemption. Try to start the service, but fall back
        // to WorkManager-only mode if the system blocks it.
        boolean canStartForegroundService = true;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            PowerManager pm = (PowerManager) context.getSystemService(Context.POWER_SERVICE);
            canStartForegroundService = pm != null
                    && pm.isIgnoringBatteryOptimizations(context.getPackageName());
        }

        if (canStartForegroundService) {
            try {
                BackgroundService.startNativeEventWs(context);
            } catch (Exception e) {
                // ForegroundServiceStartNotAllowedException or other failures
                AppLog.w(TAG, "BootReceiver: cannot start foreground service, relying on WorkManager", e);
            }
        } else {
            AppLog.i(TAG, "BootReceiver: no battery optimization exemption, using WorkManager only");
        }

        // Always schedule WorkManager fallback — it works regardless of FG service
        // restrictions and covers the case where the service gets killed later
        BackgroundService.schedulePendingEventsWork(context);
    }
}
