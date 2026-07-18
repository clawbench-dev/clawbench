# Native Push Notification Toggle Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an independent "native push notification" toggle in the notification settings that, when disabled, stops the native WebSocket, cancels WorkManager fallback polling, and prevents the BackgroundService from starting WS on lifecycle events — saving battery and CPU.

**Architecture:** Add a `nativePushEnabled` local setting (persisted in localStorage on web side, SharedPreferences on Android side). The Android `WebAppInterface` gets a new `setNativePushEnabled(boolean)` JS bridge method. When toggled OFF, the frontend calls this bridge method which: (1) stops the native WS, (2) cancels PendingEventsWorker, (3) saves the preference. When toggled ON, the preference is saved and WS will start on next `onPause()`. The `BootCompletedReceiver` and `MainActivity.onPause()` check this preference before starting native WS.

**Tech Stack:** Vue 3 (settings UI), Java/Android (BackgroundService, WebAppInterface, BootCompletedReceiver), JS Bridge communication

---

### Task 1: Add i18n strings for native push toggle

**Files:**
- Modify: `web/src/i18n/locales/zh.ts`
- Modify: `web/src/i18n/locales/en.ts`

**Step 1: Add i18n keys**

In `zh.ts`, add inside the `settings.items` object (after `dingtalkAgentIdDesc`):
```typescript
nativePushEnabled: '原生推送通知',
nativePushEnabledDesc: '后台接收 AI 会话和任务完成通知（关闭后可节省电量）',
```

In `en.ts`, add the same location:
```typescript
nativePushEnabled: 'Native Push Notifications',
nativePushEnabledDesc: 'Receive AI session and task completion notifications in background (disabling saves battery)',
```

**Step 2: Verify build**

Run: `cd web && npx vue-tsc --noEmit`
Expected: No type errors

**Step 3: Commit**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat: add i18n strings for native push notification toggle"
```

---

### Task 2: Add nativePushEnabled to settings field map and local config

**Files:**
- Modify: `web/src/components/settings/settingsFieldMap.ts`
- Modify: `web/src/composables/useSettingsConfig.ts`

**Step 1: Add the notification category item in settingsFieldMap.ts**

In the `notification` drill-down category, add `nativePushEnabled` as a standalone switch before the DingTalk fields. Since the notification category is a drill-down, add a new `commonFields` entry at the beginning:

```typescript
notification: {
    categoryId: 'notification',
    enableKey: 'dingtalk.enabled',
    enableLabelKey: 'settings.items.dingtalkEnabled',
    commonFields: [
      { labelKey: 'settings.items.nativePushEnabled', descriptionKey: 'settings.items.nativePushEnabledDesc', key: 'nativePushEnabled', type: 'switch', source: 'local', appOnly: true },
      { labelKey: 'settings.items.dingtalkAppKey', descriptionKey: 'settings.items.dingtalkAppKeyDesc', key: 'dingtalk.app_key', type: 'text', source: 'server' },
      { labelKey: 'settings.items.dingtalkAppSecret', descriptionKey: 'settings.items.dingtalkAppSecretDesc', key: 'dingtalk.app_secret', type: 'password', source: 'server' },
      { labelKey: 'settings.items.dingtalkAgentId', descriptionKey: 'settings.items.dingtalkAgentIdDesc', key: 'dingtalk.agent_id', type: 'number', source: 'server' },
    ],
    optionSubFields: [],
    requiredFields: ['dingtalk.app_key', 'dingtalk.app_secret', 'dingtalk.agent_id'],
  },
```

**Step 2: Add nativePushEnabled to useSettingsConfig.ts local defaults and legacy keys**

Add to `localDefaults`:
```typescript
nativePushEnabled: true,
```

Add to `legacyKeys`:
```typescript
nativePushEnabled: {
    key: '',
    format: 'raw',
    sideEffect(value: boolean) {
      // Notify Android native side via JS bridge
      try {
        const native = (window as unknown as { AndroidNative?: { setNativePushEnabled?: (v: boolean) => void } }).AndroidNative
        native?.setNativePushEnabled?.(value)
      } catch { /* not in app mode */ }
    },
  },
```

**Step 3: Verify build**

Run: `cd web && npx vue-tsc --noEmit`
Expected: No type errors

**Step 4: Commit**

```bash
git add web/src/components/settings/settingsFieldMap.ts web/src/composables/useSettingsConfig.ts
git commit -m "feat: add nativePushEnabled to settings field map and local config"
```

---

### Task 3: Add Android JS bridge method and SharedPreferences key

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`
- Modify: `android/app/src/main/java/com/clawbench/app/MainActivity.java` (WebAppInterface inner class)

**Step 1: Add SharedPreferences key and helper methods in BackgroundService.java**

Add a new constant:
```java
private static final String KEY_NATIVE_PUSH_ENABLED = "native_push_enabled";
```

Add static helper methods:
```java
/**
 * Check whether native push notifications are enabled.
 * Defaults to true for existing users (no migration needed).
 */
public static boolean isNativePushEnabled(Context context) {
    return context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getBoolean(KEY_NATIVE_PUSH_ENABLED, true);
}

/**
 * Enable or disable native push notifications.
 * When disabled, stops the native WS and cancels WorkManager polling.
 * When enabled, the next onPause() will start the native WS.
 */
public static void setNativePushEnabled(Context context, boolean enabled) {
    context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putBoolean(KEY_NATIVE_PUSH_ENABLED, enabled)
            .apply();
    if (!enabled) {
        // Stop native WS and cancel WorkManager polling
        stopNativeEventWs(context);
        cancelPendingEventsWork(context);
    }
    AppLog.i(TAG, "NativePush: set enabled=" + enabled);
}
```

Add a `cancelPendingEventsWork` method:
```java
/**
 * Cancel the WorkManager periodic polling for pending events.
 * Called when native push is disabled by the user.
 */
public static void cancelPendingEventsWork(Context context) {
    java.util.List<androidx.work.WorkManager> unused = new java.util.ArrayList<>();
    try {
        androidx.work.WorkManager workManager = androidx.work.WorkManager.getInstance(context);
        workManager.cancelUniqueWork("clawbench_pending_events");
        AppLog.i(TAG, "NativePush: cancelled PendingEventsWorker");
    } catch (Exception e) {
        AppLog.w(TAG, "NativePush: failed to cancel PendingEventsWorker", e);
    }
}
```

**Step 2: Add JS bridge method in WebAppInterface (MainActivity.java)**

Add to the `WebAppInterface` class:
```java
/**
 * Enable or disable native push notifications from the WebView settings UI.
 * When disabled, stops the native WS connection and WorkManager polling.
 * When enabled, allows the next onPause() to start native WS.
 */
@JavascriptInterface
public void setNativePushEnabled(boolean enabled) {
    AppLog.i(TAG, "JSBridge: setNativePushEnabled=" + enabled);
    BackgroundService.setNativePushEnabled(activity, enabled);
}

/**
 * Check whether native push notifications are currently enabled.
 * Used by the WebView to read the initial state on settings page load.
 */
@JavascriptInterface
public boolean isNativePushEnabled() {
    return BackgroundService.isNativePushEnabled(activity);
}
```

**Step 3: Verify build**

Run: `cd android && JAVA_HOME=/usr/lib/jvm/jdk-17.0.12 ./gradlew compileDebugJavaWithJavac`
Expected: BUILD SUCCESSFUL

**Step 4: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/BackgroundService.java android/app/src/main/java/com/clawbench/app/MainActivity.java
git commit -m "feat: add native push enabled JS bridge and SharedPreferences"
```

---

### Task 4: Guard lifecycle WS start with native push enabled check

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/MainActivity.java`
- Modify: `android/app/src/main/java/com/clawbench/app/BootCompletedReceiver.java`

**Step 1: Guard onPause WS start in MainActivity.java**

In `onPause()`, wrap the `startNativeEventWs` call with the enabled check:

```java
@Override
protected void onPause() {
    super.onPause();
    isForeground = false;
    pauseWebView();
    // App going to background — start native WS so we still get
    // notifications when Android kills the WebView process.
    if (webViewConnected && BackgroundService.isNativePushEnabled(this)) {
        BackgroundService.startNativeEventWs(this);
    }
}
```

**Step 2: Guard BootCompletedReceiver**

In `BootCompletedReceiver.onReceive()`, add the check before starting native WS:

```java
if (canStartForegroundService && BackgroundService.isNativePushEnabled(context)) {
    try {
        BackgroundService.startNativeEventWs(context);
    } catch (Exception e) {
        AppLog.w(TAG, "BootReceiver: cannot start foreground service, relying on WorkManager", e);
    }
} else if (canStartForegroundService) {
    AppLog.i(TAG, "BootReceiver: native push disabled, skipping WS start");
}

// Only schedule WorkManager if native push is enabled
if (BackgroundService.isNativePushEnabled(context)) {
    BackgroundService.schedulePendingEventsWork(context);
}
```

**Step 3: Verify build**

Run: `cd android && JAVA_HOME=/usr/lib/jvm/jdk-17.0.12 ./gradlew compileDebugJavaWithJavac`
Expected: BUILD SUCCESSFUL

**Step 4: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/MainActivity.java android/app/src/main/java/com/clawbench/app/BootCompletedReceiver.java
git commit -m "feat: guard lifecycle WS start with native push enabled check"
```

---

### Task 5: Sync initial nativePushEnabled from Android SharedPreferences to frontend

**Files:**
- Modify: `web/src/composables/useSettingsConfig.ts`

**Step 1: Add sync logic in useSettingsConfig**

In `syncNativeSettings()`, read the native push enabled state from Android:

```typescript
function syncNativeSettings() {
    try {
        const native = (window as unknown as { AndroidNative?: { isNativePushEnabled?: () => boolean } }).AndroidNative
        if (native?.isNativePushEnabled) {
            const nativeValue = native.isNativePushEnabled()
            if (localConfig.nativePushEnabled !== nativeValue) {
                localConfig.nativePushEnabled = nativeValue
                try {
                    localStorage.setItem(LOCAL_PREFIX + 'nativePushEnabled', JSON.stringify(nativeValue))
                } catch { /* ignore */ }
            }
        }
    } catch { /* not in app mode */ }
}
```

**Step 2: Verify build**

Run: `cd web && npx vue-tsc --noEmit`
Expected: No type errors

**Step 3: Commit**

```bash
git add web/src/composables/useSettingsConfig.ts
git commit -m "feat: sync nativePushEnabled from Android SharedPreferences to frontend"
```

---

### Task 6: Write tests

**Files:**
- Create: `web/src/composables/__tests__/useSettingsConfig.nativePush.test.ts`

**Step 1: Write the test**

Test that the `nativePushEnabled` local setting:
1. Has the correct default value (`true`)
2. Can be toggled via `setLocalConfig`
3. Calls `AndroidNative.setNativePushEnabled()` via the side effect when in app mode

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'

describe('nativePushEnabled setting', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('defaults to true', () => {
    const { localConfig } = useSettingsConfig()
    expect(localConfig.nativePushEnabled).toBe(true)
  })

  it('can be set to false via setLocalConfig', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()
    setLocalConfig('nativePushEnabled', false)
    expect(localConfig.nativePushEnabled).toBe(false)
  })

  it('calls AndroidNative.setNativePushEnabled side effect', () => {
    const mockSetNativePushEnabled = vi.fn()
    const originalAndroidNative = (window as any).AndroidNative
    ;(window as any).AndroidNative = { setNativePushEnabled: mockSetNativePushEnabled }

    const { setLocalConfig } = useSettingsConfig()
    setLocalConfig('nativePushEnabled', false)
    expect(mockSetNativePushEnabled).toHaveBeenCalledWith(false)

    setLocalConfig('nativePushEnabled', true)
    expect(mockSetNativePushEnabled).toHaveBeenCalledWith(true)

    // Restore
    if (originalAndroidNative) {
      ;(window as any).AndroidNative = originalAndroidNative
    } else {
      delete (window as any).AndroidNative
    }
  })
})
```

**Step 2: Run tests**

Run: `cd web && npx vitest run src/composables/__tests__/useSettingsConfig.nativePush.test.ts`
Expected: All tests pass

**Step 3: Commit**

```bash
git add web/src/composables/__tests__/useSettingsConfig.nativePush.test.ts
git commit -m "test: add tests for nativePushEnabled setting"
```

---

### Task 7: Final verification

**Step 1: Run full frontend typecheck**

Run: `cd web && npx vue-tsc --noEmit`
Expected: No errors

**Step 2: Run full frontend test suite**

Run: `cd web && npx vitest run`
Expected: All tests pass

**Step 3: Run Android lint**

Run: `./scripts/lint-android.sh`
Expected: No violations (new Java code uses AppLog, not android.util.Log)

**Step 4: Final commit (if any lint fixes needed)**

```bash
git add -A
git commit -m "chore: fix lint issues from native push feature"
```
