package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;

/**
 * Unit tests for the theme color sync from the WebView to the floating window.
 *
 * Verifies that setTheme (new 5-arg bridge) persists the full palette (bg /
 * text / textSecondary / accent) into SharedPreferences, and that the legacy
 * 1-arg setTheme keeps the github-dark defaults for the color slots.
 * FloatingThemeColors.get() is the read-back path used by FloatingStatusView
 * and FloatingStatusPanelView.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class MainActivityThemeTest {

    private MainActivity activity;
    private Context appContext;
    private SharedPreferences prefs;

    @Before
    public void setUp() throws Exception {
        activity = allocate(MainActivity.class);
        // Set the static instance field (some methods read MainActivity.instance)
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        appContext = RuntimeEnvironment.getApplication();
        prefs = appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE);
        prefs.edit().clear().commit();
        // Wire the activity's prefs field to the Robolectric prefs.
        setField(activity, "prefs", prefs);
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
    // setTheme bridge → SharedPreferences persistence
    // =====================================================

    @Test
    public void setThemeWithColors_persistsPalette() throws Exception {
        MainActivity.WebAppInterface bridge = allocate(MainActivity.WebAppInterface.class);
        setField(bridge, "activity", activity);

        bridge.setTheme("github-dark", "#161b22", "#c9d1d9", "#8b949e", "#58a6ff");

        assertEquals("github-dark", prefs.getString("theme_base", ""));
        assertEquals("#161b22", prefs.getString("theme_bg", ""));
        assertEquals("#c9d1d9", prefs.getString("theme_text", ""));
        assertEquals("#8b949e", prefs.getString("theme_text_secondary", ""));
        assertEquals("#58a6ff", prefs.getString("theme_accent", ""));
    }

    @Test
    public void setThemeWithColors_readBackViaFloatingThemeColors() throws Exception {
        MainActivity.WebAppInterface bridge = allocate(MainActivity.WebAppInterface.class);
        setField(bridge, "activity", activity);

        bridge.setTheme("dracula", "#21222c", "#f8f8f2", "#6272a4", "#bd93f9");

        int[] colors = FloatingThemeColors.get(appContext);
        // Array layout: [bg, text, textSecondary, accent]
        assertEquals(0xFF21222C, colors[0]);
        assertEquals(0xFFF8F8F2, colors[1]);
        assertEquals(0xFF6272A4, colors[2]);
        assertEquals(0xFFBD93F9, colors[3]);
    }

    @Test
    public void setThemeWithoutColors_keepsDefaults() throws Exception {
        MainActivity.WebAppInterface bridge = allocate(MainActivity.WebAppInterface.class);
        setField(bridge, "activity", activity);

        // Legacy 1-arg signature (login.html still calls it) must still persist
        // the theme id and leave the color slots empty → github-dark defaults.
        bridge.setTheme("github-light");

        assertEquals("github-light", prefs.getString("theme_base", ""));

        int[] colors = FloatingThemeColors.get(appContext);
        assertEquals(0xFF161B22, colors[0]);
        assertEquals(0xFFC9D1D9, colors[1]);
        assertEquals(0xFF8B949E, colors[2]);
        assertEquals(0xFF58A6FF, colors[3]);
    }

    // =====================================================
    // FloatingThemeColors parse helper
    // =====================================================

    @Test
    public void parseColor_handlesHashRRGGBB() {
        assertEquals(0xFF112233, FloatingThemeColors.parseColor("#112233", 0));
        assertEquals(0xFFABCDEF, FloatingThemeColors.parseColor("#abcdef", 0));
    }

    @Test
    public void parseColor_invalidInput_fallsBackToDefault() {
        assertEquals(0xFF123456, FloatingThemeColors.parseColor(null, 0xFF123456));
        assertEquals(0xFF123456, FloatingThemeColors.parseColor("", 0xFF123456));
        assertEquals(0xFF123456, FloatingThemeColors.parseColor("not-a-color", 0xFF123456));
        assertEquals(0xFF123456, FloatingThemeColors.parseColor("#12", 0xFF123456));
    }

    @Test
    public void borderColorFromBackground_darkBg_lightsUp() {
        // github-dark background: border must be lighter than the bg so it is
        // visible against a dark backdrop.
        int bg = 0xFF161B22;
        int border = FloatingThemeColors.borderColorFromBackground(bg);
        assertTrue("dark bg border must be lighter than bg", brightness(border) > brightness(bg));
        assertEquals("border must keep the bg's hue family", 0xFF000000,
                border & 0xFF000000);
    }

    @Test
    public void borderColorFromBackground_lightBg_darkens() {
        // github-light background: border must be darker than the bg.
        int bg = 0xFFF8F9FA;
        int border = FloatingThemeColors.borderColorFromBackground(bg);
        assertTrue("light bg border must be darker than bg", brightness(border) < brightness(bg));
    }

    @Test
    public void borderColorFromBackground_veryDarkBg_staysOpaque() {
        int bg = 0xFF000000;
        int border = FloatingThemeColors.borderColorFromBackground(bg);
        assertTrue("even a black bg must yield a visible (lighter) border",
                brightness(border) > brightness(bg));
        assertEquals("alpha must remain opaque", 0xFF000000, border & 0xFF000000);
    }

    private static int brightness(int argb) {
        int r = (argb >> 16) & 0xFF;
        int g = (argb >> 8) & 0xFF;
        int b = argb & 0xFF;
        return (r + g + b) / 3;
    }

    // --- Helpers (same pattern as MainActivityNotificationTest) ---

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        try {
            Constructor<T> ctor = clazz.getDeclaredConstructor();
            ctor.setAccessible(true);
            return ctor.newInstance();
        } catch (Exception e) {
            var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
            unsafeField.setAccessible(true);
            Object unsafe = unsafeField.get(null);
            Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
            allocate.setAccessible(true);
            return (T) allocate.invoke(unsafe, clazz);
        }
    }

    private static void setField(Object target, String fieldName, Object value) throws Exception {
        Field field = null;
        Class<?> clazz = target.getClass();
        while (clazz != null) {
            try {
                field = clazz.getDeclaredField(fieldName);
                break;
            } catch (NoSuchFieldException e) {
                clazz = clazz.getSuperclass();
            }
        }
        if (field == null) throw new NoSuchFieldException(fieldName);
        field.setAccessible(true);
        field.set(target, value);
    }
}
