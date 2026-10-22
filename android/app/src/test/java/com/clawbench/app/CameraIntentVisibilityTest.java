package com.clawbench.app;

import org.junit.Test;
import org.w3c.dom.Document;
import org.w3c.dom.Element;
import org.w3c.dom.NodeList;

import java.io.File;

import javax.xml.parsers.DocumentBuilder;
import javax.xml.parsers.DocumentBuilderFactory;

import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

/**
 * Regression guard for the 拍照 entry on Android 11+.
 *
 * `launchFileChooserWithCamera` decides whether a camera app exists with
 * `cameraIntent.resolveActivity(getPackageManager())`. Since API 30 that call is
 * subject to package visibility: without a matching `<queries>` element it
 * returns null even when a camera IS installed, so the code took its "no camera
 * app" branch and silently opened the photo picker instead. The user pressed
 * 拍照 and got the gallery, with no error anywhere.
 *
 * The fix lives entirely in the manifest, so that is what this test pins. It
 * parses the SOURCE manifest (the one we edit) rather than the merged one, so a
 * failure points at the file to change. It parses rather than greps: a
 * substring hit would also match a comment or an action declared on the wrong
 * element, neither of which grants visibility.
 */
public class CameraIntentVisibilityTest {

    private static final String CAMERA_ACTION = "android.media.action.IMAGE_CAPTURE";
    private static final String ANDROID_NS = "http://schemas.android.com/apk/res/android";

    @Test
    public void manifestDeclaresQueriesForImageCapture() throws Exception {
        Document doc = parseSourceManifest();

        NodeList queriesNodes = doc.getElementsByTagName("queries");
        assertTrue(
                "AndroidManifest.xml has no <queries> element. Without it, "
                        + "resolveActivity(ACTION_IMAGE_CAPTURE) returns null on API 30+ and 拍照 "
                        + "silently falls back to the photo picker.",
                queriesNodes.getLength() > 0);

        // The action must sit inside <queries><intent><action>, which is the
        // only shape the platform honours. An <action> under <application> or a
        // stray <intent> outside <queries> grants nothing.
        for (int i = 0; i < queriesNodes.getLength(); i++) {
            Element queries = (Element) queriesNodes.item(i);
            NodeList intents = queries.getElementsByTagName("intent");
            for (int j = 0; j < intents.getLength(); j++) {
                NodeList actions = ((Element) intents.item(j)).getElementsByTagName("action");
                for (int k = 0; k < actions.getLength(); k++) {
                    String name = ((Element) actions.item(k)).getAttributeNS(ANDROID_NS, "name");
                    if (CAMERA_ACTION.equals(name)) {
                        return;
                    }
                }
            }
        }
        fail("<queries> declares no <intent> whose <action> is " + CAMERA_ACTION
                + ". Only actions declared that way are visible to resolveActivity().");
    }

    /**
     * Locate and parse `src/main/AndroidManifest.xml`.
     *
     * Gradle's unit-test working directory is the module (android/app), but that
     * is a build detail; walk up a few levels and anchor on the file itself.
     */
    private static Document parseSourceManifest() throws Exception {
        File file = findSourceManifest();
        DocumentBuilderFactory factory = DocumentBuilderFactory.newInstance();
        factory.setNamespaceAware(true);
        DocumentBuilder builder = factory.newDocumentBuilder();
        return builder.parse(file);
    }

    private static File findSourceManifest() {
        File dir = new File(System.getProperty("user.dir", ".")).getAbsoluteFile();
        for (int depth = 0; depth < 5 && dir != null; depth++) {
            File candidate = new File(dir, "src/main/AndroidManifest.xml");
            if (candidate.isFile()) {
                return candidate;
            }
            dir = dir.getParentFile();
        }
        // Fail loudly: a guard that silently passes when it cannot find its
        // subject is worse than no guard at all.
        fail("could not locate src/main/AndroidManifest.xml from " + System.getProperty("user.dir"));
        return null;
    }
}
