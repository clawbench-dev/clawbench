package com.clawbench.app;

import android.content.SharedPreferences;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.doReturn;

/**
 * Unit tests for per-server password handling in MainActivity.
 *
 * <p>The regression: a single global {@code KEY_SSH_PASSWORD} slot meant the
 * tunnel and the cold-start auto-login used whichever password was written
 * last — so with more than one saved server, all but the most recent were
 * unauthenticated (and a cookie-only server inherited the previous password).
 *
 * <p>Covers:
 * <ul>
 *   <li>{@code savedPasswordFor(url)}: per-URL lookup + legacy-global guard</li>
 *   <li>{@code setActivePassword(url, pw)}: clearing instead of retaining a stale value</li>
 *   <li>{@code removeServer}: retiring the active pointer and password slot</li>
 * </ul>
 *
 * <p>The Activity is created with {@code Unsafe.allocateInstance} and spied so
 * {@code getSharedPreferences} (used by {@code BackgroundService.setPassword})
 * can be redirected at an in-memory store.
 */
public class MainActivityServerPasswordTest {

    private static final String URL_A = "https://a.example.com:20000";
    private static final String URL_B = "https://b.example.com:20000";

    private MainActivity activity;
    private InMemoryPrefs prefs;
    private Object webAppInterface;

    @Before
    public void setUp() throws Exception {
        activity = org.mockito.Mockito.spy(allocate(MainActivity.class));

        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        prefs = new InMemoryPrefs();
        setField(activity, "prefs", prefs);
        // BackgroundService.setPassword() reaches SharedPreferences through the
        // Context; the spy has no attached base context, so route it to the same
        // in-memory store the Activity itself uses.
        doReturn(prefs).when(activity).getSharedPreferences(anyString(), anyInt());

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
        } catch (Exception ignored) {}
    }

    // =====================================================
    // savedPasswordFor: per-URL lookup
    // =====================================================

    @Test
    public void savedPasswordFor_returnsPasswordOfTheNamedServer() throws Exception {
        setServerList("[{\"url\":\"" + URL_A + "\",\"password\":\"pass-a\"},"
                + "{\"url\":\"" + URL_B + "\",\"password\":\"pass-b\"}]");

        assertEquals("pass-a", savedPasswordFor(URL_A));
        assertEquals("pass-b", savedPasswordFor(URL_B));
    }

    @Test
    public void savedPasswordFor_unknownServer_returnsEmpty() throws Exception {
        setServerList("[{\"url\":\"" + URL_A + "\",\"password\":\"pass-a\"}]");

        assertEquals("", savedPasswordFor("https://other.example.com:20000"));
    }

    @Test
    public void savedPasswordFor_emptyUrl_returnsEmpty() throws Exception {
        setServerList("[{\"url\":\"" + URL_A + "\",\"password\":\"pass-a\"}]");

        assertEquals("", savedPasswordFor(""));
        assertEquals("", savedPasswordFor(null));
    }

    @Test
    public void savedPasswordFor_malformedList_returnsEmptyInsteadOfThrowing() throws Exception {
        setServerList("not json at all");

        assertEquals("", savedPasswordFor(URL_A));
    }

    // =====================================================
    // savedPasswordFor: legacy global slot guard
    // =====================================================

    @Test
    public void savedPasswordFor_claimsLegacyGlobalOnlyForTheActiveServer() throws Exception {
        // The global slot was written when URL_A was active; it must not be
        // handed to URL_B.
        setServerList("[]");
        putString("server_url", URL_A);
        putString("ssh_password", "legacy-a");

        assertEquals("legacy-a", savedPasswordFor(URL_A));
        assertEquals("", savedPasswordFor(URL_B));
    }

    @Test
    public void savedPasswordFor_listEntryOutranksLegacyGlobal() throws Exception {
        setServerList("[{\"url\":\"" + URL_A + "\",\"password\":\"list-a\"}]");
        putString("server_url", URL_A);
        putString("ssh_password", "legacy-a");

        assertEquals("list-a", savedPasswordFor(URL_A));
    }

    // =====================================================
    // setActivePassword: clearing a stale value
    // =====================================================

    @Test
    public void setActivePassword_emptyPasswordForCookieOnlyServer_clearsSlot() throws Exception {
        // URL_B has no password (cookie-only). Connecting to it must NOT leave
        // URL_A's password in the tunnel's slot.
        setServerList("[{\"url\":\"" + URL_A + "\",\"password\":\"pass-a\"},{\"url\":\"" + URL_B + "\"}]");

        setActivePassword(URL_B, "");

        assertEquals("", prefs.getString("ssh_password", ""));
    }

    @Test
    public void setActivePassword_emptyPassword_usesTheServersOwnListEntry() throws Exception {
        // Switching back to URL_A with an empty argument (password field hidden)
        // must reuse URL_A's saved password, not clear the slot.
        setServerList("[{\"url\":\"" + URL_A + "\",\"password\":\"pass-a\"},{\"url\":\"" + URL_B + "\"}]");

        setActivePassword(URL_A, "");

        assertEquals("pass-a", prefs.getString("ssh_password", ""));
    }

    @Test
    public void setActivePassword_explicitPassword_isUsedVerbatim() throws Exception {
        setServerList("[]");

        setActivePassword(URL_A, "typed-pw");

        assertEquals("typed-pw", prefs.getString("ssh_password", ""));
    }

    // =====================================================
    // removeServer: retiring the active pointer
    // =====================================================

    @Test
    public void removeServer_activeServer_clearsUrlAndPassword() throws Exception {
        setServerList("[{\"url\":\"" + URL_A + "\",\"password\":\"pass-a\"},"
                + "{\"url\":\"" + URL_B + "\",\"password\":\"pass-b\"}]");
        putString("server_url", URL_A);
        putString("ssh_password", "pass-a");

        invokeRemoveServer(URL_A);

        assertEquals("", prefs.getString("server_url", ""));
        assertEquals("", prefs.getString("ssh_password", ""));
        // The other server survives, with its own password intact.
        assertEquals("pass-b", savedPasswordFor(URL_B));
        assertEquals("", savedPasswordFor(URL_A));
    }

    @Test
    public void removeServer_inactiveServer_keepsActivePointer() throws Exception {
        setServerList("[{\"url\":\"" + URL_A + "\",\"password\":\"pass-a\"},"
                + "{\"url\":\"" + URL_B + "\",\"password\":\"pass-b\"}]");
        putString("server_url", URL_A);

        invokeRemoveServer(URL_B);

        assertEquals(URL_A, prefs.getString("server_url", ""));
        assertEquals("pass-a", savedPasswordFor(URL_A));
    }

    // =====================================================
    // Helpers
    // =====================================================

    private void setServerList(String json) {
        putString("server_list", json);
    }

    private void putString(String key, String value) {
        prefs.edit().putString(key, value).apply();
    }

    private String savedPasswordFor(String url) throws Exception {
        Method m = findMethod(activity.getClass(), "savedPasswordFor", String.class);
        m.setAccessible(true);
        return (String) m.invoke(activity, url);
    }

    private void setActivePassword(String url, String password) throws Exception {
        Method m = findMethod(activity.getClass(), "setActivePassword", String.class, String.class);
        m.setAccessible(true);
        m.invoke(activity, url, password);
    }

    private void invokeRemoveServer(String url) throws Exception {
        Method m = findMethod(webAppInterface.getClass(), "removeServer", String.class);
        m.setAccessible(true);
        m.invoke(webAppInterface, url);
    }

    private static void setField(Object target, String fieldName, Object value) throws Exception {
        Field f = findField(target.getClass(), fieldName);
        f.setAccessible(true);
        f.set(target, value);
    }

    private static Field findField(Class<?> clazz, String fieldName) throws Exception {
        Class<?> c = clazz;
        while (c != null) {
            try {
                return c.getDeclaredField(fieldName);
            } catch (NoSuchFieldException e) {
                c = c.getSuperclass();
            }
        }
        throw new NoSuchFieldException(fieldName);
    }

    private static Method findMethod(Class<?> clazz, String name, Class<?>... params) throws Exception {
        Class<?> c = clazz;
        while (c != null) {
            try {
                return c.getDeclaredMethod(name, params);
            } catch (NoSuchMethodException e) {
                c = c.getSuperclass();
            }
        }
        throw new NoSuchMethodException(name);
    }

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        try {
            Constructor<T> ctor = clazz.getDeclaredConstructor();
            ctor.setAccessible(true);
            return ctor.newInstance();
        } catch (Exception e) {
            Field unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
            unsafeField.setAccessible(true);
            Object unsafe = unsafeField.get(null);
            Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
            allocate.setAccessible(true);
            return (T) allocate.invoke(unsafe, clazz);
        }
    }

    /**
     * Minimal in-memory SharedPreferences covering the string + edit/apply path
     * the password logic uses.
     */
    private static class InMemoryPrefs implements SharedPreferences {
        private final Map<String, String> values = new HashMap<>();

        @Override
        public String getString(String key, String defValue) {
            return values.containsKey(key) ? values.get(key) : defValue;
        }

        @Override
        public Editor edit() {
            return new Editor() {
                private final Map<String, String> pending = new HashMap<>();
                private final Set<String> removals = new HashSet<>();

                @Override
                public Editor putString(String key, String value) {
                    pending.put(key, value);
                    removals.remove(key);
                    return this;
                }

                @Override
                public Editor remove(String key) {
                    removals.add(key);
                    pending.remove(key);
                    return this;
                }

                @Override
                public Editor clear() {
                    removals.addAll(values.keySet());
                    return this;
                }

                @Override
                public Editor putStringSet(String key, Set<String> v) { return this; }
                @Override
                public Editor putInt(String key, int v) { return this; }
                @Override
                public Editor putLong(String key, long v) { return this; }
                @Override
                public Editor putFloat(String key, float v) { return this; }
                @Override
                public Editor putBoolean(String key, boolean v) { return this; }

                @Override
                public boolean commit() {
                    apply();
                    return true;
                }

                @Override
                public void apply() {
                    for (String k : removals) values.remove(k);
                    values.putAll(pending);
                }
            };
        }

        @Override
        public Map<String, ?> getAll() { return new HashMap<>(values); }
        @Override
        public Set<String> getStringSet(String k, Set<String> d) { return d; }
        @Override
        public int getInt(String k, int d) { return d; }
        @Override
        public long getLong(String k, long d) { return d; }
        @Override
        public float getFloat(String k, float d) { return d; }
        @Override
        public boolean getBoolean(String k, boolean d) { return d; }
        @Override
        public boolean contains(String k) { return values.containsKey(k); }
        @Override
        public void registerOnSharedPreferenceChangeListener(OnSharedPreferenceChangeListener l) {}
        @Override
        public void unregisterOnSharedPreferenceChangeListener(OnSharedPreferenceChangeListener l) {}
    }
}
