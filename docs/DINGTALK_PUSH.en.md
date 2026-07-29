# DingTalk Bot Push Configuration

ClawBench supports push notifications via DingTalk bot. Receive instant messages when AI sessions complete, permission approvals are needed, or scheduled task status changes.

---

## Setup Steps

### 1. Create a DingTalk App

1. Log in to the [DingTalk Open Platform](https://open-dev.dingtalk.com/)
2. Click **Create App** → select **Enterprise Internal App**
3. Fill in the app name and description. After creation, note down the **AppKey** and **AppSecret**

### 2. Enable Robot Capability

1. Go to the app → **App Features** → **Robot**
2. Click **Enable** to activate the robot
3. Set message receiving mode to **Stream Mode** (no public callback URL required)

### 3. Configure Event Subscription

1. Go to **Events & Callbacks** → **Event Configuration**
2. Select **Stream Push** as the push method (consistent with the robot message receiving mode)

### 4. Configure Permissions

Go to **Permission Management** and enable the following permissions:

| Permission | Permission ID | Description |
|------------|---------------|-------------|
| Basic permission for calling SNS API | `snsapi_base` | Basic permission |
| Basic permission for calling enterprise API | `qyapi_base` | Basic permission |
| Enterprise robot message sending permission | `qyapi_robot_sendmsg` | Send single-chat messages |

### 5. Publish the App

1. **Version Management** → Create version → Submit for review
2. After approval, publish the app
3. Add users who need push notifications to the **App Visibility Scope**

### 6. Configure in ClawBench

1. Open the ClawBench settings panel
2. Select **DingTalk Push** as the push mode
3. Enter the AppKey and AppSecret
4. Click **Test Connection** to verify the configuration
5. Save to apply (takes effect immediately)

---

## Usage

### Subscribe to Notifications

Send any message to the bot in DingTalk to auto-subscribe. You will receive a reply confirming subscription.

### View Session List

Send a message without any prefix, and the bot will return a list of recent sessions:

```
Session List
Send @SessionID <message> to send a message to a session:

/path/to/project
- @a1b2c3d4 Fix login bug *
- @e5f67890 Add user feature

/path/to/another
- @b3c4d5e6 Code refactor
```

`*` indicates a running session.

### Send Message to Session

Send a message in the format `@SessionID message content` to append a message to the specified session:

```
@a1b2c3d4 Please continue fixing
```

### Push Notification Types

| Event | Description |
|-------|-------------|
| Session Completed | AI session finished, includes response preview |
| Session Cancelled | AI session cancelled by user |
| Permission Pending | AI requests approval for an action (e.g., Bash command, file modification) |
| Task Started | Scheduled task started execution |
| Task Completed | Scheduled task finished successfully, includes response preview |
| Task Failed | Scheduled task execution failed |
| Task Cancelled | Scheduled task cancelled |

> When a WebSocket client is connected to ClawBench (user is actively using the web UI), push notifications are automatically suppressed to avoid duplicate alerts.

### Manage Subscriptions

You can view and manage DingTalk subscribers in the ClawBench settings panel, including manual add and delete.

---

## FAQ

**Q: No notifications after saving configuration?**

A: Check the following:
1. Has the app been approved and published?
2. Is the event push method set to Stream Push?
3. Is the user within the app's visibility scope?
4. Is the robot's Stream message receiving mode enabled?

**Q: Test connection succeeds but sending messages gets no response?**

A: Confirm that the robot has Stream message receiving mode enabled, and that the event subscription uses Stream push.

**Q: How to switch push mode?**

A: Select a different push mode in the settings panel. DingTalk and Feishu are mutually exclusive — switching does not delete subscriber data; you can switch back anytime.
