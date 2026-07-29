# Feishu Bot Push Configuration

ClawBench supports push notifications via Feishu bot. Receive instant messages when AI sessions complete, permission approvals are needed, or scheduled task status changes.

---

## Setup Steps

### 1. Create a Feishu App

1. Log in to the [Feishu Open Platform](https://open.feishu.cn/)
2. Click **Create Enterprise Self-built App**
3. Fill in the app name and description. After creation, note down the **App ID** and **App Secret**

### 2. Enable Robot Capability

1. Go to the app → **App Features** → **Robot**
2. Click **Enable** to activate the robot
3. Enable **Receive P2P Messages** (allow users to chat with the bot)

### 3. Configure Event Subscription

1. Go to **Events & Callbacks** → **Event Configuration**
2. Select **Use Long Connection to Receive Events** (WebSocket mode, no public callback URL required)
3. Search and add the event: `im.message.receive_v1` (receive messages)

### 4. Configure Permissions

Go to **Permission Management** and enable the following permissions:

| Permission | Permission ID | Description |
|------------|---------------|-------------|
| Get and send P2P and group messages | `im:message` | Read and send messages |
| Read P2P messages sent to the bot | `im:message.p2p_msg:readonly` | Receive P2P messages |
| Send messages as the app | `im:message:send_as_bot` | Bot sends messages |

### 5. Publish the App

1. **Version Management** → Create version → Submit for review
2. After approval, publish the app
3. Add users who need push notifications to the **Availability Scope**

### 6. Configure in ClawBench

1. Open the ClawBench settings panel
2. Select **Feishu Push** as the push mode
3. Enter the App ID and App Secret
4. Click **Test Connection** to verify the configuration
5. Save to apply (takes effect immediately)

---

## Usage

### Subscribe to Notifications

Send any message to the bot in Feishu to auto-subscribe. You will receive a reply confirming subscription.

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

You can view and manage Feishu subscribers in the ClawBench settings panel, including manual add and delete.

---

## FAQ

**Q: No notifications after saving configuration?**

A: Check the following:
1. Has the app been approved and published?
2. Has `im.message.receive_v1` been added in event subscriptions?
3. Is the event receiving method set to Long Connection (WebSocket)?
4. Is the user within the app's availability scope?
5. Has the robot enabled receiving P2P messages?

**Q: Test connection succeeds but sending messages gets no response?**

A: Confirm that the robot has P2P message receiving enabled, and that the event subscription uses long connection mode.

**Q: How to switch push mode?**

A: Select a different push mode in the settings panel. DingTalk and Feishu are mutually exclusive — switching does not delete subscriber data; you can switch back anytime.
