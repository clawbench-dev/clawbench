package com.clawbench.app;

import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.os.Build;

/**
 * Utilities for Chinese OEM-specific optimizations.
 *
 * Chinese ROMs (MIUI, EMUI/HarmonyOS, ColorOS, OriginOS, FunTouchOS)
 * aggressively kill background services and require manual auto-start /
 * battery optimization whitelisting. This utility:
 *
 * 1. Detects the OEM via Build.MANUFACTURER / Build.BRAND
 * 2. Provides intents to OEM-specific auto-start / battery settings
 * 3. Tracks whether the user has been prompted (to avoid nagging)
 *
 * Usage from Activity:
 *   Intent intent = OemUtils.getAutoStartIntent(context);
 *   if (intent != null) startActivity(intent);
 */
public final class OemUtils {

    private static final String TAG = "ClawBench";
    private static final String PREFS_NAME = "clawbench_prefs";
    private static final String KEY_OEM_AUTOSTART_PROMPTED = "oem_autostart_prompted";

    private OemUtils() {} // utility class

    // --- OEM detection ---

    public enum Oem {
        XIAOMI,   // MIUI / HyperOS
        HUAWEI,   // EMUI / HarmonyOS
        OPPO,     // ColorOS
        VIVO,     // OriginOS / FunTouchOS
        SAMSUNG,  // OneUI
        OTHER
    }

    private static volatile Oem detectedOem;

    /**
     * Detect the current device OEM. Cached after first call.
     * Safe for concurrent use — benign race, result is deterministic per device.
     */
    public static Oem detectOem() {
        if (detectedOem != null) return detectedOem;

        // Null-safety: some custom ROMs/emulators may return null
        String manufacturerRaw = Build.MANUFACTURER;
        String brandRaw = Build.BRAND;
        String manufacturer = manufacturerRaw != null ? manufacturerRaw.toLowerCase() : "";
        String brand = brandRaw != null ? brandRaw.toLowerCase() : "";

        Oem oem;
        if (manufacturer.contains("xiaomi") || brand.contains("xiaomi") || brand.contains("redmi")) {
            oem = Oem.XIAOMI;
        } else if (manufacturer.contains("huawei") || brand.contains("huawei") || brand.contains("honor")) {
            oem = Oem.HUAWEI;
        } else if (manufacturer.contains("oppo") || brand.contains("oppo") || brand.contains("realme") || brand.contains("oneplus")) {
            oem = Oem.OPPO;
        } else if (manufacturer.contains("vivo") || brand.contains("vivo") || brand.contains("iqoo")) {
            oem = Oem.VIVO;
        } else if (manufacturer.contains("samsung") || brand.contains("samsung")) {
            oem = Oem.SAMSUNG;
        } else {
            oem = Oem.OTHER;
        }

        detectedOem = oem;
        AppLog.i(TAG, "OemUtils: detected OEM=" + oem
                + " (manufacturer=" + manufacturerRaw + ", brand=" + brandRaw + ")");
        return oem;
    }

    /**
     * Returns true if the device is a Chinese OEM with known aggressive
     * background process management (Xiaomi, Huawei, OPPO, vivo).
     */
    public static boolean isChineseOem() {
        Oem oem = detectOem();
        return oem == Oem.XIAOMI || oem == Oem.HUAWEI || oem == Oem.OPPO || oem == Oem.VIVO;
    }

    // --- Auto-start intent ---

    /**
     * Get an intent to the OEM-specific auto-start / startup manager settings.
     * Returns null if the OEM is not recognized or no known intent resolves
     * on this device (component not installed).
     */
    public static Intent getAutoStartIntent(Context context) {
        Oem oem = detectOem();
        Intent intent = null;

        switch (oem) {
            case XIAOMI:
                intent = tryComponent(context,
                        "com.miui.securitycenter",
                        "com.miui.permcenter.autostart.AutoStartManagementActivity");
                if (intent == null) {
                    // Fallback: MIUI security center main
                    intent = tryComponent(context,
                            "com.miui.securitycenter",
                            "com.miui.securitycenter.MainActivity");
                }
                break;

            case HUAWEI:
                intent = tryComponent(context,
                        "com.huawei.systemmanager",
                        "com.huawei.systemmanager.optimize.process.ProtectActivity");
                if (intent == null) {
                    // HarmonyOS startup manager
                    intent = tryComponent(context,
                            "com.huawei.systemmanager",
                            "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity");
                }
                break;

            case OPPO:
                intent = tryComponent(context,
                        "com.coloros.safecenter",
                        "com.coloros.safecenter.permission.startup.StartupAppListActivity");
                if (intent == null) {
                    intent = tryComponent(context,
                            "com.oppo.safe",
                            "com.oppo.safe.permission.startup.StartupAppListActivity");
                }
                break;

            case VIVO:
                intent = tryComponent(context,
                        "com.vivo.abe",
                        "com.vivo.applicationbehave.VivoStartActivity");
                if (intent == null) {
                    intent = tryComponent(context,
                            "com.iqoo.secure",
                            "com.iqoo.secure.ui.phoneoptimize.AddWhiteListActivity");
                }
                break;

            default:
                break;
        }

        if (intent != null) {
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            AppLog.i(TAG, "OemUtils: auto-start intent for " + oem + ": " + intent.getComponent());
        }
        return intent;
    }

    // --- Battery optimization intent ---

    /**
     * Get an intent to the OEM-specific battery optimization / power manager
     * settings. These are different from the standard Android
     * ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS — they control
     * "background cleanup" / "power saving" which is more aggressive
     * on Chinese ROMs.
     *
     * Returns null if no known intent resolves on this device.
     */
    public static Intent getBatterySettingsIntent(Context context) {
        Oem oem = detectOem();
        Intent intent = null;

        switch (oem) {
            case XIAOMI:
                // MIUI battery saver
                intent = tryComponent(context,
                        "com.miui.powercenter",
                        "com.miui.powercenter.ui.Settings");
                break;

            case HUAWEI:
                // EMUI/HarmonyOS battery manager
                intent = tryComponent(context,
                        "com.huawei.systemmanager",
                        "com.huawei.systemmanager.power.ui.HwPowerManagerActivity");
                break;

            case OPPO:
                intent = tryComponent(context,
                        "com.coloros.safecenter",
                        "com.coloros.safecenter.permission.background.BackgroundAppListActivity");
                break;

            case VIVO:
                intent = tryComponent(context,
                        "com.vivo.abe",
                        "com.vivo.applicationbehave.VivoBgStartActivity");
                break;

            default:
                break;
        }

        if (intent != null) {
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            AppLog.i(TAG, "OemUtils: battery settings intent for " + oem + ": " + intent.getComponent());
        }
        return intent;
    }

    // --- Prompt tracking ---

    /**
     * Check if the auto-start prompt has already been shown to the user.
     */
    public static boolean isAutoStartPrompted(Context context) {
        return context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .getBoolean(KEY_OEM_AUTOSTART_PROMPTED, false);
    }

    /**
     * Mark the auto-start prompt as shown (so we don't nag again).
     */
    public static void setAutoStartPrompted(Context context) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit()
                .putBoolean(KEY_OEM_AUTOSTART_PROMPTED, true)
                .apply();
    }

    // --- Internal helpers ---

    /**
     * Build an Intent for a specific component, validating that the activity
     * actually exists on this device via PackageManager.resolveActivity().
     * Returns null if the component doesn't resolve.
     */
    private static Intent tryComponent(Context context, String packageName, String className) {
        Intent intent = new Intent();
        intent.setComponent(new ComponentName(packageName, className));
        if (context.getPackageManager().resolveActivity(intent, 0) != null) {
            return intent;
        }
        return null;
    }
}
