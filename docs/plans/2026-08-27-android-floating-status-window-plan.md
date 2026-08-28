# Android 桌面悬浮状态窗 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 Android 端实现系统级桌面悬浮窗，当主界面在后台且会话运行中时实时展示会话/任务状态（原生 View 胶囊），点击回主界面。

**Architecture:** 复用 `BackgroundService` 已有的前台服务 + OkHttp 原生 WebSocket 事件通道。WS 事件在 `NativeEventListener.onMessage` 中增加派发，转发给新增的 `FloatingStatusController`（显隐状态机 + 拖动贴边），由 `FloatingStatusView`（`TYPE_APPLICATION_OVERLAY` 原生 View）渲染胶囊状态。主界面 `isForeground` 状态天然驱动显隐：前台隐藏、后台有任务出现。权限用 `Settings.canDrawOverlays()` + `SYSTEM_ALERT_WINDOW`，开关通过 WebView JS 桥存 SharedPreferences。

**Tech Stack:** Java、Android Framework（WindowManager/View）、OkHttp WS（已有）、Robolectric + JUnit 4 + Mockito（测试）

**关键现有机制（勿重复造轮子）：**
- `BackgroundService.connectNativeWs()` L2099 原生 WS；`NativeEventListener.onMessage` L2267 事件处理；`plainPreview()` L2670 纯文本预览（可复用）
- `MainActivity.isForeground` L114 static 标记；onPause L1617 → `startNativeEventWs`，onResume L1629 → `stopNativeEventWs`
- `BackgroundService.isNativePushEnabled` L288 / `setNativePushEnabled` L301 设置存储范式（SharedPreferences `native_push_enabled`）
- WebView JS 桥：`BrowserJavascriptInterface` / `WebAppInterface`，前端可调原生方法
- 测试范式：Unsafe 分配实例 + 反射测 private 方法（参考 `BackgroundServiceForegroundTypeTest`）

---

### Task 1: Manifest 权限 + 常量

**Files:**
- Modify: `android/app/src/main/AndroidManifest.xml`（uses-permission 区）
- Modify: `android/app/src/main/java/com/clawbench/app/FloatingStatusController.java`（新建时含常量，见 Task 3）

**Step 1: 添加权限声明**

在 `AndroidManifest.xml` L21（`<uses-feature>` 前）加：
```xml
<uses-permission android:name="android.permission.SYSTEM_ALERT_WINDOW" />
```

**Step 2: 验证**

Run: `./gradlew -p android :app:processDebugMainManifest`
Expected: BUILD SUCCESSFUL

**Step 3: Commit**

```bash
git add android/app/src/main/AndroidManifest.xml
git commit -m "feat(android): declare SYSTEM_ALERT_WINDOW permission for floating window"
```

---

### Task 2: FloatingStatusView（胶囊 View + 状态渲染）

**Files:**
- Create: `android/app/src/main/java/com/clawbench/app/FloatingStatusView.java`
- Create: `android/app/src/test/java/com/clawbench/app/FloatingStatusViewTest.java`

**Step 1: 写失败测试**

新建测试文件，验证状态→UI 文案映射（纯逻辑方法，可单测）。先定义 View 中一个可测的静态方法：

```java
// FloatingStatusView.java (骨架)
public class FloatingStatusView extends android.widget.FrameLayout {
    // 供单测的纯逻辑：状态 → 显示文案
    public static String statusLabel(String eventType, String status, String sessionTitle, String toolName) {
        if ("session_update".equals(eventType)) {
            if ("running".equals(status)) return "运行中 · " + sessionTitle;
            if ("completed".equals(status)) return "✓ 完成";
            if ("cancelled".equals(status)) return "已取消";
            if ("permission_pending".equals(status))
                return toolName.isEmpty() ? "等待授权" : "等待授权 · " + toolName;
        } else if ("task_update".equals(eventType)) {
            if ("running".equals(status)) return "任务运行中 · " + sessionTitle;
            if ("completed".equals(status)) return "✓ 任务完成";
            if ("failed".equals(status)) return "任务失败";
            if ("cancelled".equals(status)) return "任务已取消";
        }
        return "";
    }
    public static int statusColor(String eventType, String status) { ... } // 返回颜色 int
}
```

测试用例（`FloatingStatusViewTest.java`）：
- session running → 含"运行中"
- session permission_pending + toolName → 含"等待授权"且含 toolName
- session completed → "✓ 完成"
- task_update failed → "任务失败"
- 未知状态 → 空字符串

**Step 2: 运行验证失败**

Run: `./gradlew -p android :app:testDebugUnitTest --tests "com.clawbench.app.FloatingStatusViewTest"`
Expected: FAIL（类不存在/编译错误）

**Step 3: 实现 View**

实现 `FloatingStatusView`：继承 FrameLayout，含状态点 View + 单行 TextView，`render(eventType, status, title, preview, color)` 方法更新 UI；提供 `statusLabel`/`statusColor` 静态纯函数。预览文字用 `BackgroundService` 现有 `truncateForPush` 同款逻辑（复刻，因为它是 private static，抽取为工具或本地复刻）。胶囊背景圆角、状态点 200ms 脉冲缩放动画（`ObjectAnimator.ofFloat`）。

**Step 4: 运行验证通过**

Run: `./gradlew -p android :app:testDebugUnitTest --tests "com.clawbench.app.FloatingStatusViewTest"`
Expected: PASS（N 个测试全过）

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/FloatingStatusView.java android/app/src/test/java/com/clawbench/app/FloatingStatusViewTest.java
git commit -m "feat(android): floating status capsule view with state label mapping"
```

---

### Task 3: FloatingStatusController（显隐状态机 + 拖动贴边）

**Files:**
- Create: `android/app/src/main/java/com/clawbench/app/FloatingStatusController.java`
- Create: `android/app/src/test/java/com/clawbench/app/FloatingStatusControllerTest.java`

**Step 1: 写失败测试**

测试状态机核心决策逻辑（抽出纯逻辑方法 `shouldShow/isForeground/hasActiveSession` 组合）：

```java
// 决策纯函数（静态，可单测）
public static boolean shouldShow(boolean appForeground, boolean hasActive, boolean userDismissed) {
    return !appForeground && hasActive && !userDismissed;
}
public static boolean isActiveStatus(String eventType, String status) {
    if ("session_update".equals(eventType))
        return "running".equals(status) || "permission_pending".equals(status);
    if ("task_update".equals(eventType)) return "running".equals(status);
    return false;
}
```

测试用例：
- 前台 + 有任务 → false（前台永不显示）
- 后台 + 有任务 → true
- 后台 + 有任务 + 用户已关闭 → false
- 后台 + 无任务 → false
- session completed / task_update failed → isActiveStatus false
- session running / permission_pending → true

**Step 2: 运行验证失败**

Run: `./gradlew -p android :app:testDebugUnitTest --tests "com.clawbench.app.FloatingStatusControllerTest"`
Expected: FAIL（类不存在）

**Step 3: 实现 Controller**

`FloatingStatusController`：
- `create/destroyWindow(Context)`：`WindowManager` + `TYPE_APPLICATION_OVERLAY`（API 26+；26 以下用 `TYPE_PHONE`），`FLAG_NOT_FOCUSABLE | FLAG_NOT_TOUCH_MODAL | FLAG_LAYOUT_NO_LIMITS`
- `handleEvent(String eventType, JSONObject data)`：解析 status/session_id/session_title/tool_name/response_preview_plain → 更新 View + 状态机
- 显隐状态机：`setAppForeground(boolean)`；`setUserDismissed(boolean)`；完成态 3s 后淡出（`Handler.postDelayed`）→ `hideWithFade()`
- 拖动：`OnTouchListener`（ACTION_DOWN 记录起点 + 判定长按；ACTION_MOVE 超 touchSlop 转拖动，窗口半透明 0.85；ACTION_UP 吸附左/右边缘 8dp margin + 恢复不透明 + 存位置到 SharedPreferences）
- 点击：`ACTION_UP` 且未拖动 → 回调 `Runnable`（MainActivity 拉起）
- `isOverlayPermissionGranted(Context)`：`Settings.canDrawOverlays()`

**Step 4: 运行验证通过**

Run: `./gradlew -p android :app:testDebugUnitTest --tests "com.clawbench.app.FloatingStatusControllerTest"`
Expected: PASS

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/FloatingStatusController.java android/app/src/test/java/com/clawbench/app/FloatingStatusControllerTest.java
git commit -m "feat(android): floating status controller with show/hide state machine and drag-snap"
```

---

### Task 4: BackgroundService 事件派发 + 生命周期接入

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`（`onCreate` ~L100-160、`NativeEventListener.onMessage` L2305-2316 区、`onDestroy`）
- Modify: `android/app/src/test/java/com/clawbench/app/`（新增 `BackgroundServiceFloatingDispatchTest.java`）

**Step 1: 写失败测试**

用反射验证 `onMessage` 处理路径会调用 controller 派发。参照 `BackgroundServiceForegroundTypeTest` 范式（Unsafe 分配实例 + 反射）。测试：
- `BackgroundService` 持有 `FloatingStatusController` 字段（非 null）
- `onDestroy` 会释放 controller（字段可置 null）

**Step 2: 运行验证失败**

Run: `./gradlew -p android :app:testDebugUnitTest --tests "com.clawbench.app.BackgroundServiceFloatingDispatchTest"`
Expected: FAIL

**Step 3: 实现**

- `onCreate`：初始化 `floatingController = new FloatingStatusController(...)`（权限未授予则 no-op）
- `NativeEventListener.onMessage`：在 `shouldNotify` 判断后（约 L2316），把**所有** `session_update`/`task_update` 事件（不只 terminal）派发给 `floatingController.handleEvent(event, data)`
- `onDestroy`：`floatingController.destroy()`
- `setFloatingWindowEnabled(Context, boolean)` static 方法：存 SharedPreferences（`floating_window_enabled`），与 `isNativePushEnabled` 同范式；**注意**：悬浮窗开关仅在服务启动时读取生效
- 新增 `MainActivity.isForeground` 变化时调 `floatingController.setAppForeground(...)`——通过 `BackgroundService` 静态方法转发，在 `startNativeEventWs`/`stopNativeEventWs` 内联动（后台启动 WS 时 = 后台，前台停 WS 时 = 前台），避免直接依赖 Activity

**Step 4: 运行验证通过**

Run: `./gradlew -p android :app:testDebugUnitTest`
Expected: PASS（全部含新测试）

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/BackgroundService.java android/app/src/test/java/com/clawbench/app/BackgroundServiceFloatingDispatchTest.java
git commit -m "feat(android): dispatch WS events to floating controller and wire lifecycle"
```

---

### Task 5: MainActivity 权限申请 + 拉起回主界面

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/MainActivity.java`
- Modify: `android/app/src/test/java/com/clawbench/app/MainActivityOverlayPermissionTest.java`（新）

**Step 1: 写失败测试**

反射验证：
- `MainActivity` 存在 `requestOverlayPermission()` 方法
- 存在跳转 `Settings.ACTION_MANAGE_OVERLAY_PERMISSION` 的 intent 构建逻辑

**Step 2: 运行验证失败**

Run: `./gradlew -p android :app:testDebugUnitTest --tests "com.clawbench.app.MainActivityOverlayPermissionTest"`
Expected: FAIL

**Step 3: 实现**

- `requestOverlayPermission()`：检查 `Settings.canDrawOverlays(this)`，未授权则 `startActivity(Settings.ACTION_MANAGE_OVERLAY_PERMISSION)`，`onActivityResult`/`onResume` 复查
- 点击悬浮窗回调：`startActivity(intent)` 带 `FLAG_ACTIVITY_REORDER_TO_FRONT` + `FLAG_ACTIVITY_SINGLE_TOP`，`putExtra("session_id", id)`，MainActivity 在 `handleResumeIntent` 或新方法中解析 session_id 传给前端 JS（`webView.evaluateJavascript` 深链定位）
- 悬浮窗开关：暴露 JS 桥方法 `setFloatingWindowEnabled(boolean)`（加在现有 `BrowserJavascriptInterface`/`WebAppInterface`），前端设置页调用
- 权限被拒时 `AppLog.w` 记录，悬浮窗静默不显示

**Step 4: 运行验证通过**

Run: `./gradlew -p android :app:testDebugUnitTest --tests "com.clawbench.app.MainActivityOverlayPermissionTest"`
Expected: PASS

**Step 5: Commit**

```bash
git add android/app/src/main/java/com/clawbench/app/MainActivity.java android/app/src/test/java/com/clawbench/app/MainActivityOverlayPermissionTest.java
git commit -m "feat(android): overlay permission flow, tap-to-open main activity, JS toggle bridge"
```

---

### Task 6: 全量验证

**Step 1: 运行全部 Android 单元测试**

Run: `./gradlew -p android :app:testDebugUnitTest`
Expected: PASS，全部测试通过

**Step 2: 编译 Debug APK 验证 Manifest 合并**

Run: `./gradlew -p android :app:assembleDebug`
Expected: BUILD SUCCESSFUL

**Step 3: 检查代码风格（AppLog 使用）**

所有新代码用 `AppLog.d/i/w/e`，禁止 `android.util.Log`。Grep 确认：`grep -rn "android.util.Log" android/app/src/main/java/com/clawbench/app/FloatingStatus*` 无输出。

**Step 4: 提交收尾**

```bash
git status
git add -A
git commit -m "feat(android): finalize floating status window implementation"
```

---

## 手动验证清单（真机/模拟器，需 overlay 权限）

1. 安装 APK → 设置页开启悬浮窗开关 → 系统授权悬浮窗
2. 主界面发一条消息 → 切到后台 → 悬浮窗出现，绿点 + "运行中"
3. 拖动 → 松手吸附边缘；重启 APP 位置恢复
4. 点悬浮窗 → 回主界面并定位到该会话
5. 会话完成 → "✓ 完成" 3s → 淡出
6. 主界面回前台 → 悬浮窗立即消失
7. 权限拒绝场景 → 无崩溃，日志有 `AppLog.w`
