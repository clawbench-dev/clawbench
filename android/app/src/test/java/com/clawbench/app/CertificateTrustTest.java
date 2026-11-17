package com.clawbench.app;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

import java.util.Collections;
import java.util.HashSet;
import java.util.Set;

/**
 * The per-address memory of accepted self-signed certificates.
 *
 * <p>The failure this guards against is the one that prompted the class: a
 * confirmation that only lasts for a single connect attempt. Keying must be
 * stable across restarts (no in-memory state), and it must not blur two servers
 * on the same host but different ports.
 */
public class CertificateTrustTest {

    @Test
    public void keepsSchemeHostAndPort() {
        assertEquals("https://192.168.1.50:20000", CertificateTrust.originOf("https://192.168.1.50:20000/login"));
        assertEquals("https://gdp:8443", CertificateTrust.originOf("https://GDP:8443/"));
        assertEquals("http://gdp:20000", CertificateTrust.originOf("http://gdp:20000"));
    }

    @Test
    public void dropsTheDefaultPort() {
        assertEquals("https://gdp", CertificateTrust.originOf("https://gdp/"));
        assertEquals("https://gdp", CertificateTrust.originOf("https://gdp:443/"));
        assertEquals("http://gdp", CertificateTrust.originOf("http://gdp:80/"));
    }

    @Test
    public void foldsWebSocketUrlsOntoTheirHttpOrigin() {
        // A wss socket to the same server presents the same certificate.
        assertEquals("https://gdp:20000", CertificateTrust.originOf("wss://gdp:20000/ws"));
        assertEquals("http://gdp:20000", CertificateTrust.originOf("ws://gdp:20000/ws"));
    }

    @Test
    public void returnsEmptyForUnparseableOrUnsupportedUrls() {
        for (String url : new String[] {null, "", "   ", "not a url", "gdp:20000", "file:///x", "ftp://gdp/"}) {
            assertEquals(String.valueOf(url), "", CertificateTrust.originOf(url));
        }
    }

    @Test
    public void matchesAnAddressThatWasAcceptedBefore() {
        Set<String> trusted = Collections.singleton("https://gdp:20000");
        assertTrue(CertificateTrust.isTrusted(trusted, "https://gdp:20000"));
        assertTrue(CertificateTrust.isTrusted(trusted, "https://gdp:20000/"));
        assertTrue(CertificateTrust.isTrusted(trusted, "wss://gdp:20000/ws"));
    }

    @Test
    public void doesNotMatchADifferentPortSchemeOrHost() {
        Set<String> trusted = Collections.singleton("https://gdp:20000");
        assertFalse(CertificateTrust.isTrusted(trusted, "https://gdp:20300"));
        assertFalse(CertificateTrust.isTrusted(trusted, "http://gdp:20000"));
        assertFalse(CertificateTrust.isTrusted(trusted, "https://other:20000"));
    }

    @Test
    public void doesNotMatchWhenNothingIsRemembered() {
        assertFalse(CertificateTrust.isTrusted(null, "https://gdp:20000"));
        assertFalse(CertificateTrust.isTrusted(Collections.emptySet(), "https://gdp:20000"));
        // An unparseable URL must never match, even with entries present.
        assertFalse(CertificateTrust.isTrusted(Collections.singleton(""), "garbage"));
    }

    @Test
    public void addingLeavesTheInputSetUntouched() {
        // SharedPreferences.getStringSet returns a read-only view; mutating it
        // in place throws, so the helper must always hand back a copy.
        Set<String> original = new HashSet<>(Collections.singleton("https://old:20000"));
        Set<String> next = CertificateTrust.withTrusted(original, "https://new:20000");

        assertTrue(next.contains("https://old:20000"));
        assertTrue(next.contains("https://new:20000"));
        assertEquals(1, original.size());
        assertFalse(original.contains("https://new:20000"));
    }

    @Test
    public void addingAnUnparseableUrlChangesNothing() {
        Set<String> original = Collections.singleton("https://old:20000");
        assertEquals(original, CertificateTrust.withTrusted(original, "garbage"));
    }

    @Test
    public void removingForgetsOnlyThatAddress() {
        Set<String> original = new HashSet<>();
        original.add("https://a:20000");
        original.add("https://b:20000");

        Set<String> next = CertificateTrust.withoutTrusted(original, "https://a:20000/");

        assertFalse(next.contains("https://a:20000"));
        assertTrue(next.contains("https://b:20000"));
        assertEquals(2, original.size());
    }
}
