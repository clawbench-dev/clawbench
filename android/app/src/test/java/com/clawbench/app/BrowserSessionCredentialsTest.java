package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import static org.junit.Assert.*;

@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class BrowserSessionCredentialsTest {

    private Context getContext() {
        return RuntimeEnvironment.getApplication();
    }

    @Test
    public void credsDataClass() {
        BrowserSessionCredentials.Creds creds = new BrowserSessionCredentials.Creds(
                "sid1", "http://localhost:20000", "clawbench_session=abc123; clawbench_project=%2Fhome%2Fuser%2Fproject");
        assertEquals("sid1", creds.sessionId);
        assertEquals("http://localhost:20000", creds.serverUrl);
        assertEquals("clawbench_session=abc123; clawbench_project=%2Fhome%2Fuser%2Fproject", creds.allCookies);
    }

    @Test
    public void credsNullSafety() {
        BrowserSessionCredentials.Creds creds = new BrowserSessionCredentials.Creds(null, null, null);
        assertNull(creds.sessionId);
        assertNull(creds.serverUrl);
        assertNull(creds.allCookies);
    }

    @Test
    public void setGetRoundTrip() {
        Context ctx = getContext();
        BrowserSessionCredentials.clear(ctx);

        BrowserSessionCredentials.set(ctx, "session42", "http://localhost:3000", "cookie1=val1; cookie2=val2");
        BrowserSessionCredentials.Creds creds = BrowserSessionCredentials.get(ctx);

        assertNotNull(creds);
        assertEquals("session42", creds.sessionId);
        assertEquals("http://localhost:3000", creds.serverUrl);
        assertEquals("cookie1=val1; cookie2=val2", creds.allCookies);
    }

    @Test
    public void getReturnsNullWhenNoCredentialsStored() {
        Context ctx = getContext();
        BrowserSessionCredentials.clear(ctx);

        assertNull(BrowserSessionCredentials.get(ctx));
    }

    @Test
    public void getAndClearReadsThenClears() {
        Context ctx = getContext();
        BrowserSessionCredentials.clear(ctx);

        BrowserSessionCredentials.set(ctx, "sid99", "http://host:1234", "all=cookies");
        BrowserSessionCredentials.Creds creds = BrowserSessionCredentials.getAndClear(ctx);

        assertNotNull(creds);
        assertEquals("sid99", creds.sessionId);
        assertEquals("http://host:1234", creds.serverUrl);
        assertEquals("all=cookies", creds.allCookies);

        // After getAndClear, subsequent get should return null
        assertNull(BrowserSessionCredentials.get(ctx));
    }

    @Test
    public void getAndClearReturnsNullWhenEmpty() {
        Context ctx = getContext();
        BrowserSessionCredentials.clear(ctx);

        assertNull(BrowserSessionCredentials.getAndClear(ctx));
    }

    @Test
    public void clearRemovesAllCredentials() {
        Context ctx = getContext();
        BrowserSessionCredentials.set(ctx, "sid1", "http://a", "cookies");

        BrowserSessionCredentials.clear(ctx);

        assertNull(BrowserSessionCredentials.get(ctx));
    }

    @Test
    public void legacySessionCookieMigration() {
        Context ctx = getContext();
        BrowserSessionCredentials.clear(ctx);

        // Write using the legacy "session_cookie" key directly
        SharedPreferences prefs = ctx.getSharedPreferences("browser_session_creds", Context.MODE_PRIVATE);
        prefs.edit()
                .putString("session_id", "legacySid")
                .putString("server_url", "http://legacy")
                .putString("session_cookie", "legacy_cookie=abc")
                .commit();

        // get() should fall back to "session_cookie" when KEY_ALL_COOKIES is absent
        BrowserSessionCredentials.Creds creds = BrowserSessionCredentials.get(ctx);
        assertNotNull(creds);
        assertEquals("legacySid", creds.sessionId);
        assertEquals("http://legacy", creds.serverUrl);
        assertEquals("legacy_cookie=abc", creds.allCookies);
    }

    @Test
    public void newKeyTakesPrecedenceOverLegacy() {
        Context ctx = getContext();
        BrowserSessionCredentials.clear(ctx);

        // Write both new and legacy keys
        SharedPreferences prefs = ctx.getSharedPreferences("browser_session_creds", Context.MODE_PRIVATE);
        prefs.edit()
                .putString("session_id", "sid")
                .putString("server_url", "http://new")
                .putString("all_cookies", "new_cookies=xyz")
                .putString("session_cookie", "legacy_cookie=old")
                .commit();

        BrowserSessionCredentials.Creds creds = BrowserSessionCredentials.get(ctx);
        assertNotNull(creds);
        assertEquals("new_cookies=xyz", creds.allCookies);
    }

    @Test
    public void setWithNullParametersStoresEmptyStrings() {
        Context ctx = getContext();
        BrowserSessionCredentials.clear(ctx);

        BrowserSessionCredentials.set(ctx, null, null, null);

        // Should not return null because set stores empty strings for null values,
        // but both sessionId and serverUrl are empty, so get returns null
        assertNull(BrowserSessionCredentials.get(ctx));

        // Verify stored values directly
        SharedPreferences prefs = ctx.getSharedPreferences("browser_session_creds", Context.MODE_PRIVATE);
        assertEquals("", prefs.getString("session_id", "MISSING"));
        assertEquals("", prefs.getString("server_url", "MISSING"));
        assertEquals("", prefs.getString("all_cookies", "MISSING"));
    }

    @Test
    public void setWithNullSessionIdButValidServerUrl() {
        Context ctx = getContext();
        BrowserSessionCredentials.clear(ctx);

        BrowserSessionCredentials.set(ctx, null, "http://server", "cookies");

        BrowserSessionCredentials.Creds creds = BrowserSessionCredentials.get(ctx);
        assertNotNull(creds);
        assertEquals("", creds.sessionId);
        assertEquals("http://server", creds.serverUrl);
        assertEquals("cookies", creds.allCookies);
    }
}
