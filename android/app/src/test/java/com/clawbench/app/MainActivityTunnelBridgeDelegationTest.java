package com.clawbench.app;

import android.content.Intent;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.mockito.ArgumentCaptor;
import org.mockito.Mockito;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.concurrent.ConcurrentHashMap;

import static org.junit.Assert.assertEquals;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.spy;

/**
 * Intent-level assertions for the {@code MainActivity.WebAppInterface} tunnel
 * bridge methods.
 *
 * <p>Split from {@link MainActivityTunnelBridgeTest} because that class runs
 * with {@code returnDefaultValues = true}, where {@link Intent} accessors
 * return defaults (so {@code getAction()} is null and extras are unreadable).
 * Under Robolectric the Intent is real, which is the only way to assert the
 * action string and extras that decide whether a reverse add is routed into the
 * forward path.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class MainActivityTunnelBridgeDelegationTest {

    private MainActivity activity;
    private Object webAppInterface;

    @Before
    public void setUp() throws Exception {
        activity = spy(allocate(MainActivity.class));
        // Unsafe allocation leaves the ContextWrapper's base null, and the
        // bridge builds `new Intent(context, BackgroundService.class)`, which
        // calls getPackageName(). Attach a real application context.
        Method attach = android.content.ContextWrapper.class
                .getDeclaredMethod("attachBaseContext", android.content.Context.class);
        attach.setAccessible(true);
        attach.invoke(activity,
                androidx.test.core.app.ApplicationProvider.getApplicationContext());
        doAnswer(inv -> {
            Runnable r = inv.getArgument(0);
            if (r != null) r.run();
            return null;
        }).when(activity).runOnUiThread(any(Runnable.class));

        Field forwarded = MainActivity.class.getDeclaredField("forwardedPorts");
        forwarded.setAccessible(true);
        forwarded.set(activity, new ConcurrentHashMap<Integer, String>());

        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        Class<?> waiClass = Class.forName("com.clawbench.app.MainActivity$WebAppInterface");
        Constructor<?> constructor = waiClass.getDeclaredConstructor(MainActivity.class);
        constructor.setAccessible(true);
        webAppInterface = constructor.newInstance(activity);
    }

    @After
    public void tearDown() throws Exception {
        try {
            Field instanceField = MainActivity.class.getDeclaredField("instance");
            instanceField.setAccessible(true);
            instanceField.set(null, null);
        } catch (Exception ignored) {
        }
        try {
            Field bsInstance = BackgroundService.class.getDeclaredField("instance");
            bsInstance.setAccessible(true);
            bsInstance.set(null, null);
        } catch (Exception ignored) {
        }
    }

    @Test
    public void removeForwardedPort_delegatesRemovePortAction() throws Exception {
        // MainActivity:2795-2800: the service releases the listener under the
        // REMOVE_PORT action carrying the local port.
        doReturn(null).when(activity).startService(any(Intent.class));

        invoke("removeForwardedPort", 3080);

        Intent intent = captureStartService();
        assertEquals("REMOVE_PORT", intent.getAction());
        assertEquals(3080, intent.getIntExtra("port", -1));
    }

    @Test
    public void addReverseForwardedPort_delegatesAddReverseAction() throws Exception {
        // MainActivity:2811-2829: serverPort must reach the service as
        // "serverPort" under ADD_REVERSE_PORT. A collision with ADD_PORT would
        // silently route a reverse add into a forward add.
        invoke("addReverseForwardedPort", 9000, 3000, "10.0.0.7");

        ArgumentCaptor<Intent> captor = ArgumentCaptor.forClass(Intent.class);
        Mockito.verify(activity).startForegroundService(captor.capture());
        Intent intent = captor.getValue();
        assertEquals("ADD_REVERSE_PORT", intent.getAction());
        assertEquals(9000, intent.getIntExtra("serverPort", -1));
        assertEquals(3000, intent.getIntExtra("targetPort", -1));
        assertEquals("10.0.0.7", intent.getStringExtra("host"));
    }

    @Test
    public void addReverseForwardedPort_nullHost_becomesEmptyString() throws Exception {
        // MainActivity:2814 normalizes a null host so the service never has to
        // null-check it (the SSH adapter treats "" as loopback).
        invoke("addReverseForwardedPort", 9000, 3000, null);

        ArgumentCaptor<Intent> captor = ArgumentCaptor.forClass(Intent.class);
        Mockito.verify(activity).startForegroundService(captor.capture());
        assertEquals("", captor.getValue().getStringExtra("host"));
    }

    @Test
    public void removeReverseForwardedPort_delegatesRemoveReverseAction() throws Exception {
        // MainActivity:2831-2833.
        doReturn(null).when(activity).startService(any(Intent.class));

        invoke("removeReverseForwardedPort", 9000);

        Intent intent = captureStartService();
        assertEquals("REMOVE_REVERSE_PORT", intent.getAction());
        assertEquals(9000, intent.getIntExtra("serverPort", -1));
    }

    private Intent captureStartService() {
        ArgumentCaptor<Intent> captor = ArgumentCaptor.forClass(Intent.class);
        Mockito.verify(activity).startService(captor.capture());
        return captor.getValue();
    }

    private Object invoke(String method, Object... args) throws Exception {
        Class<?>[] signature = method.equals("addReverseForwardedPort")
                ? new Class<?>[]{int.class, int.class, String.class}
                : new Class<?>[]{int.class};
        Method m = webAppInterface.getClass().getDeclaredMethod(method, signature);
        m.setAccessible(true);
        return m.invoke(webAppInterface, args);
    }

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);
        var allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
        allocate.setAccessible(true);
        return (T) allocate.invoke(unsafe, clazz);
    }
}
