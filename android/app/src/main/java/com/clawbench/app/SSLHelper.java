package com.clawbench.app;

import javax.net.ssl.HttpsURLConnection;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509TrustManager;
import java.security.SecureRandom;
import java.security.cert.X509Certificate;

/**
 * Shared SSL trust helper for self-signed certificates.
 * Used by BrowserActivity's SandboxWebViewClient and LogBottomSheet.
 */
public class SSLHelper {

    private static final SSLContext trustAllContext;

    static {
        SSLContext ctx = null;
        try {
            ctx = SSLContext.getInstance("TLS");
            ctx.init(null, new TrustManager[]{new X509TrustManager() {
                public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
                public void checkClientTrusted(X509Certificate[] certs, String authType) {}
                public void checkServerTrusted(X509Certificate[] certs, String authType) {}
            }}, new SecureRandom());
        } catch (Exception e) {
            AppLog.e("SSLHelper", "Failed to initialize trust-all SSLContext", e);
        }
        trustAllContext = ctx;
    }

    public static void setupTrustAll(HttpsURLConnection conn) {
        if (trustAllContext != null) {
            conn.setSSLSocketFactory(trustAllContext.getSocketFactory());
            conn.setHostnameVerifier((hostname, session) -> true);
        }
    }
}
