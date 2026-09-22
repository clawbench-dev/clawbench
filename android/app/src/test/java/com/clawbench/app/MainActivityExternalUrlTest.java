package com.clawbench.app;

import android.app.Application;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.Shadows;
import org.robolectric.annotation.Config;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;

/**
 * Unit tests for WebAppInterface.openExternalUrl — the settings "About" page's
 * project homepage / issue-tracker links.
 *
 * <p>Regression focus: the page cannot use {@code window.open(url, '_blank')}
 * for these. The WebView never calls {@code setSupportMultipleWindows}, so the
 * popup is silently dropped — no navigation, no error, and
 * {@code shouldOverrideUrlLoading} is never consulted. The bridge method is the
 * only path that reaches the system browser.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class MainActivityExternalUrlTest {

    private Application appContext;
    private MainActivity activity;
    private Object webAppInterface;

    @Before
    public void setUp() throws Exception {
        appContext = RuntimeEnvironment.getApplication();
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().clear().commit();

        activity = Robolectric.buildActivity(MainActivity.class).get();
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
    }

    private void openExternalUrl(String url) throws Exception {
        var method = webAppInterface.getClass().getMethod("openExternalUrl", String.class);
        method.invoke(webAppInterface, url);
        // The method hops to the UI thread; Robolectric's main looper is paused
        // by default, so drain it before asserting on the started activity.
        Shadows.shadowOf(android.os.Looper.getMainLooper()).idle();
    }

    @Test
    public void openExternalUrl_httpsUrl_startsSystemBrowser() throws Exception {
        openExternalUrl("https://github.com/xulongzhe/clawbench/issues/new/choose");

        Intent started = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull("the URL must reach the system browser", started);
        assertEquals(Intent.ACTION_VIEW, started.getAction());
        assertEquals(Uri.parse("https://github.com/xulongzhe/clawbench/issues/new/choose"),
                started.getData());
        // Started from the WebView context rather than an Activity.
        assertEquals(true,
                (started.getFlags() & Intent.FLAG_ACTIVITY_NEW_TASK) != 0);
    }

    @Test
    public void openExternalUrl_httpUrl_isAccepted() throws Exception {
        openExternalUrl("http://example.com/page");

        Intent started = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull(started);
        assertEquals(Uri.parse("http://example.com/page"), started.getData());
    }

    @Test
    public void openExternalUrl_nonHttpScheme_isRejected() throws Exception {
        // A page-supplied scheme would otherwise become an arbitrary OS
        // protocol-handler launch (intent:, file:, javascript:).
        for (String url : new String[] {
                "intent://evil#Intent;scheme=android_secret_code;end",
                "file:///etc/passwd",
                "javascript:alert(1)",
                "content://com.example/secret",
        }) {
            openExternalUrl(url);
            assertNull("scheme must be refused: " + url,
                    Shadows.shadowOf(appContext).getNextStartedActivity());
        }
    }

    @Test
    public void openExternalUrl_emptyOrNull_isIgnored() throws Exception {
        openExternalUrl("");
        assertNull(Shadows.shadowOf(appContext).getNextStartedActivity());

        openExternalUrl(null);
        assertNull(Shadows.shadowOf(appContext).getNextStartedActivity());
    }

    @Test
    public void openExternalUrl_uppercaseScheme_isAccepted() throws Exception {
        // Uri.getScheme() preserves the case as written; a case-sensitive
        // comparison would silently refuse a perfectly valid URL.
        openExternalUrl("HTTPS://github.com/xulongzhe/clawbench");

        Intent started = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull(started);
    }
}
