# ClawBench Android 桥注入名改名（AndroidNative → ClawBenchNative）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Android 原生 WebView 注入的 JS 桥从 `AndroidNative` 改名为 `ClawBenchNative`，与前端已落地的平台无关桥层契约对齐。

**Architecture:** 纯机械重命名。关键点是 `MainActivity.java:422` 的 `addJavascriptInterface(..., "AndroidNative")` 注入名与 `JSErrorInjector.buildScript("AndroidNative")` 必须与前端读取的 `window.ClawBenchNative` 一致；`login.html`（Android 静态登录页，直调同步 Java 桥）同步改名。无向后兼容（旧 APK 失去桥属预期）。

**Tech Stack:** Java / Android WebView / Gradle。

参考 spec：`docs/superpowers/specs/2026-08-10-clawbench-electron-design.md` §5。前置：Plan 1 已完成（前端已读 `ClawBenchNative`）。

---

## 任务分解

### Task 1: Java 注入名与注释改名

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/MainActivity.java`
- Modify: `android/app/src/main/java/com/clawbench/app/JSErrorInjector.java`
- Modify: `android/app/src/main/java/com/clawbench/app/BrowserActivity.java`

- [ ] **Step 1: `MainActivity.java`**

- 第 422 行：`webView.addJavascriptInterface(new WebAppInterface(this), "AndroidNative");` → `webView.addJavascriptInterface(new WebAppInterface(this), "ClawBenchNative");`
- 第 1761 行：`view.evaluateJavascript(JSErrorInjector.buildScript("AndroidNative"), null);` → `view.evaluateJavascript(JSErrorInjector.buildScript("ClawBenchNative"), null);`
- 注释更新（纯文本，保持语义）：
  - 第 342 行：`or from JS via AndroidNative.dismissSplash()...` → `... via ClawBenchNative.dismissSplash()...`
  - 第 879 行：`Called from the static login page via AndroidNative.connectToServer().` → `... via ClawBenchNative.connectToServer().`
  - 第 1802 行：`// AndroidNative.dismissSplash() once Vue finishes mounting,` → `// ClawBenchNative.dismissSplash() once Vue finishes mounting,`

- [ ] **Step 2: `JSErrorInjector.java`**

- 第 7 行注释：`Used by both MainActivity (AndroidNative) and BrowserActivity (BrowserNative).` → `Used by both MainActivity (ClawBenchNative) and BrowserActivity (BrowserNative).`
- 第 25 行注释：`("AndroidNative" for MainActivity, "BrowserNative" for BrowserActivity)` → `("ClawBenchNative" for MainActivity, "BrowserNative" for BrowserActivity)`

- [ ] **Step 3: `BrowserActivity.java`**

- 第 58 行注释：`- No AndroidNative bridge injected (clean browser environment)` → `- No ClawBenchNative bridge injected (clean browser environment)`

- [ ] **Step 4: 验证**

确认 Java 侧不再有旧名（除历史无关注释）：
```bash
rg -n "AndroidNative" android/app/src/main/java
```
Expected: 无输出（全部改为 ClawBenchNative）。

- [ ] **Step 5: 提交**

```bash
git add android/app/src/main/java/com/clawbench/app/MainActivity.java android/app/src/main/java/com/clawbench/app/JSErrorInjector.java android/app/src/main/java/com/clawbench/app/BrowserActivity.java
git commit -m "refactor(android): rename injected bridge to ClawBenchNative"
```

---

### Task 2: 静态登录页 `login.html` 改名

**Files:**
- Modify: `android/app/src/main/assets/login.html`

- [ ] **Step 1: 全部 `AndroidNative` → `ClawBenchNative`**

该文件用 `AndroidNative.getLanguage()`、`AndroidNative.getAppVersion()`、`AndroidNative.saveServer()`、`AndroidNative.connectToServer()`、`AndroidNative.getServerList()`、`AndroidNative.removeServer()` 等。全部替换为 `ClawBenchNative.*`：

```bash
sed -i 's/AndroidNative/ClawBenchNative/g' android/app/src/main/assets/login.html
```

login.html 直调同步 Java `@JavascriptInterface`，**无需** async 改造（与 Vue 应用的异步桥契约无关）。

- [ ] **Step 2: 验证**

```bash
rg -c "ClawBenchNative" android/app/src/main/assets/login.html
rg -c "AndroidNative" android/app/src/main/assets/login.html
```
Expected: ClawBenchNative 计数 = 原 AndroidNative 计数（8），AndroidNative 计数 = 0。

- [ ] **Step 3: 提交**

```bash
git add android/app/src/main/assets/login.html
git commit -m "refactor(android): static login page uses ClawBenchNative bridge"
```

---

### Task 3: 更新 Android 测试并验证构建

**Files:**
- Modify: `android/app/src/test/java/com/clawbench/app/JSErrorInjectorTest.java`
- Modify: `android/app/src/test/java/com/clawbench/app/MainActivityPreAuthTest.java`

- [ ] **Step 1: 测试改名**

打开两个测试文件，把其中 `AndroidNative` 引用替换为 `ClawBenchNative`（`JSErrorInjectorTest` 断言 `buildScript` 生成脚本引用名；`MainActivityPreAuthTest` 若 mock 注入名相关）。用全局替换：
```bash
sed -i 's/AndroidNative/ClawBenchNative/g' android/app/src/test/java/com/clawbench/app/JSErrorInjectorTest.java android/app/src/test/java/com/clawbench/app/MainActivityPreAuthTest.java
```

- [ ] **Step 2: 编译验证**

若有可用的 Gradle/Android 环境，运行 Android 单测：
```bash
cd android && ./gradlew testDebugUnitTest --tests "com.clawbench.app.JSErrorInjectorTest" 2>&1 | tail -20
```
Expected: BUILD SUCCESSFUL。
（若本机无 Android SDK，跳过此步并在报告中说明——构建验证由 CI/后续 APK 打包承担。）

- [ ] **Step 3: 提交**

```bash
git add android/app/src/test/java/com/clawbench/app/JSErrorInjectorTest.java android/app/src/test/java/com/clawbench/app/MainActivityPreAuthTest.java
git commit -m "test(android): update bridge tests to ClawBenchNative"
```

---

## 自检对照（spec §5 → task）

| Spec §5 要求 | Task |
|---|---|
| `MainActivity.java` 注入名改 ClawBenchNative | Task 1 |
| `JSErrorInjector` 同步 | Task 1 |
| 静态 login.html 改名 | Task 2 |
| 重打 APK（CI/发布流程，代码层在本 plan 完成改名） | Task 3 验证编译 |
| 上线顺序（先 web bundle 后 APK） | 记录于 spec §5，交付时遵守 |

## 后续

本 plan 为纯改名，无行为变更（Java `WebAppInterface` 中 frontend 已不调用的方法 `dismissSplash`/`setVolumeKeyMode`/`setTerminalSessionCount`/`stopBackgroundService` 保留不动，降风险）。完成后进入 Plan 3（Electron 客户端）。
