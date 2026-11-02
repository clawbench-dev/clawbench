package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Method;

import static org.junit.Assert.assertEquals;

/**
 * Wiring test for the duplicate-start guard in {@link MainActivity}.
 *
 * <p>{@link ScreenShareStartPolicyTest} pins the decision table, but a decision
 * nobody consults protects nothing: deleting the guard from
 * {@code requestScreenCapture} leaves that test fully green. This exercises the
 * call site.
 *
 * <p>A second start while a session is live would create a second
 * MediaProjection and overwrite the service's projection/imageReader/
 * virtualDisplay without releasing the old ones — the abandoned virtual display
 * can never be released afterwards, so the system's "screen is being shared"
 * indicator stays on until the process is killed.
 *
 * <p>The activity is allocated with {@code Unsafe.allocateInstance} (same
 * pattern as {@code BackgroundServiceForceReconnectTest}): with the guard in
 * place the method returns before touching any Android framework object, which
 * is exactly what makes it testable on the JVM. Without the guard it proceeds
 * into the permission/consent path and throws, so the mutation is caught either
 * way.
 */
public class MainActivityScreenShareGuardTest {

    private MainActivity activity;

    @Before
    public void setUp() throws Exception {
        var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);
        Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
        allocate.setAccessible(true);
        activity = (MainActivity) allocate.invoke(unsafe, MainActivity.class);
        ScreenShareStatus.reset();
    }

    @After
    public void tearDown() {
        ScreenShareStatus.reset();
    }

    @Test
    public void ignoresAStartWhileAlreadyRunning() {
        ScreenShareStatus.setRunning(1080, 2400, false);

        activity.requestScreenCapture(ScreenShareOptions.parse(null));

        // A new consent flow would have flipped this to "requesting".
        assertEquals(ScreenShareStatus.RUNNING, ScreenShareStatus.getState());
    }

    @Test
    public void ignoresAStartWhileTheConsentDialogIsUp() {
        ScreenShareStatus.beginRequesting();

        activity.requestScreenCapture(ScreenShareOptions.parse(null));

        assertEquals(ScreenShareStatus.REQUESTING, ScreenShareStatus.getState());
    }
}
