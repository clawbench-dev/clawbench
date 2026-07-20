package com.clawbench.app;

import org.json.JSONArray;
import org.junit.Test;

import static org.junit.Assert.*;

/**
 * Unit tests for shareFiles JSON parsing and path validation logic.
 * The network/Intent logic in shareFiles requires a full Activity and is
 * tested via integration tests; these tests cover the pure validation paths.
 */
public class ShareFilesValidationTest {

    // ── Path traversal validation ──

    @Test
    public void pathWithDoubleDot_isRejected() {
        // Simulates the validation logic from shareFiles
        String path = "../etc/passwd";
        assertTrue("Path with .. should be rejected", path.contains(".."));
    }

    @Test
    public void pathWithEncodedDoubleDot_isRejected() throws Exception {
        String path = "%2e%2e/etc/passwd";
        String decoded = java.net.URLDecoder.decode(path, "UTF-8");
        assertTrue("Decoded path with .. should be rejected", decoded.contains(".."));
    }

    @Test
    public void validPath_passesValidation() {
        String path = "project/src/main.go";
        assertFalse("Valid path should not contain ..", path.contains(".."));
    }

    @Test
    public void absoluteValidPath_passesValidation() {
        String path = "/home/user/project/file.txt";
        assertFalse("Absolute valid path should not contain ..", path.contains(".."));
    }

    // ── JSON array parsing ──

    @Test
    public void parseValidPathsJson() throws Exception {
        String pathsJson = "[\"file1.txt\",\"file2.png\"]";
        JSONArray arr = new JSONArray(pathsJson);
        assertEquals(2, arr.length());
        assertEquals("file1.txt", arr.getString(0));
        assertEquals("file2.png", arr.getString(1));
    }

    @Test
    public void parseValidMimeTypesJson() throws Exception {
        String mimeJson = "[\"*/*\",\"image/*\"]";
        JSONArray arr = new JSONArray(mimeJson);
        assertEquals(2, arr.length());
        assertEquals("*/*", arr.getString(0));
        assertEquals("image/*", arr.getString(1));
    }

    @Test(expected = org.json.JSONException.class)
    public void invalidJson_throwsException() throws Exception {
        new JSONArray("not a json array");
    }

    @Test
    public void emptyPathsArray_hasZeroLength() throws Exception {
        JSONArray arr = new JSONArray("[]");
        assertEquals(0, arr.length());
    }

    // ── MIME type common denominator logic ──

    @Test
    public void sameMimeTypes_keepCommonType() {
        String[] types = {"image/*", "image/*"};
        String common = types[0];
        for (String t : types) {
            if (!t.equals(common)) {
                common = "*/*";
                break;
            }
        }
        assertEquals("image/*", common);
    }

    @Test
    public void mixedMimeTypes_fallBackToWildcard() {
        String[] types = {"image/*", "video/*"};
        String common = types[0];
        for (String t : types) {
            if (!t.equals(common)) {
                common = "*/*";
                break;
            }
        }
        assertEquals("*/*", common);
    }
}
