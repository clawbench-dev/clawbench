package com.clawbench.app;

import org.junit.Test;
import static org.junit.Assert.*;

/**
 * Unit tests for BrowserSessionCredentials.
 * Note: SharedPreferences requires a real Context, so these test the data class only.
 */
public class BrowserSessionCredentialsTest {

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
}
