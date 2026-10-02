package com.clawbench.app;

import android.webkit.WebView;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import java.lang.ref.WeakReference;
import java.lang.reflect.Field;

import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;

/**
 * A RECREATED {@code BackgroundService} must still be able to reach the
 * Activity's WebView.
 *
 * <p>The service is stopped whenever no ports need forwarding and started again
 * on the next add (observed in a real session: stop at 22:16:31.565, recreate at
 * 22:16:38.830). {@code webViewRef} is an instance field and the only writer is
 * {@code MainActivity.setupWebView()} — which does NOT run again for a service
 * restart, because the Activity and its WebView never went away. A recreated
 * instance therefore had a null {@code webViewRef}, so
 * {@code notifyPortForwardResult()} silently skipped the dispatch: the frontend
 * never received {@code clawbench-port-forward-result} and the status dot stayed
 * red until the user refreshed, even though the forward worked.
 *
 * <p>These tests drive {@code onCreate()} directly and assert the re-seed. They
 * deliberately do not go through {@code startForegroundCompat}: that needs a
 * notification channel and is irrelevant to the reference hand-off.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class BackgroundServiceWebViewReseedTest {

    @Before
    public void setUp() {
        setStaticField("instance", null);
        MainActivity.instance = null;
    }

    @After
    public void tearDown() {
        setStaticField("instance", null);
        setStaticField("isRunning", false);
        MainActivity.instance = null;
    }

    @Test
    public void onCreate_adoptsTheLiveActivitysWebView() {
        // The Activity outlives the service: seed it as setupWebView() would
        // have, then start a FRESH service and confirm it picks the WebView up.
        MainActivity activity = Robolectric.buildActivity(MainActivity.class).get();
        WebView webView = new WebView(activity);
        activity.webView = webView;
        MainActivity.instance = activity;

        BackgroundService service = Robolectric.buildService(BackgroundService.class).get();
        service.onCreate();

        assertNotNull("a recreated service must re-seed webViewRef from the live Activity",
                getInstanceField(service, "webViewRef"));
        assertSame("the adopted WebView must be the Activity's own",
                webView, webView(service));
    }

    @Test
    public void onCreate_withoutAnActivity_leavesTheRefNull() {
        // No Activity (cold service start before the UI exists): there is
        // nothing to adopt, and the notify path must simply no-op rather than
        // throw on a half-built instance.
        MainActivity.instance = null;

        BackgroundService service = Robolectric.buildService(BackgroundService.class).get();
        service.onCreate();

        assertNull("no live Activity means no WebView to adopt",
                getInstanceField(service, "webViewRef"));
    }

    @Test
    public void onCreate_withAnActivityThatHasNoWebViewYet_leavesTheRefNull() {
        // The Activity can exist while its WebView has not been built (it is
        // created in setupWebView, later in onCreate). Adopting a null here
        // would be indistinguishable from "no Activity" — both must stay null
        // so a later updateWebViewRef() is what fills it in.
        MainActivity activity = Robolectric.buildActivity(MainActivity.class).get();
        activity.webView = null;
        MainActivity.instance = activity;

        BackgroundService service = Robolectric.buildService(BackgroundService.class).get();
        service.onCreate();

        assertNull("an Activity without a WebView must not seed a null reference",
                getInstanceField(service, "webViewRef"));
    }

    // ── helpers ──────────────────────────────────────────────

    private static WebView webView(BackgroundService service) {
        Object ref = getInstanceField(service, "webViewRef");
        return ref == null ? null : ((WeakReference<WebView>) ref).get();
    }

    private static Object getInstanceField(Object target, String name) {
        try {
            Field f = BackgroundService.class.getDeclaredField(name);
            f.setAccessible(true);
            return f.get(target);
        } catch (Exception e) {
            throw new AssertionError(e);
        }
    }

    private static void setStaticField(String name, Object value) {
        try {
            Field f = BackgroundService.class.getDeclaredField(name);
            f.setAccessible(true);
            f.set(null, value);
        } catch (Exception e) {
            throw new AssertionError(e);
        }
    }
}
