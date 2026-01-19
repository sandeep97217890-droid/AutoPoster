# AutoPoster

A Telegram autoposter with user account authentication, conversation-based setup, YAML configuration, and developer access control.

## Features

- 🤖 **User Account Backend**: Authenticates with Telegram user account (not bot token)
- 💬 **Interactive Setup**: Conversation-based interface to collect chat IDs, intervals, and messages
- ⚙️ **YAML Configuration**: All credentials and settings in `config.yaml`
- 🔐 **Access Control**: Restricted to authorized developer user IDs only
- ⏰ **Flexible Intervals**: 2h, 4h, or 6h posting schedules
- 📧 **Error Notifications**: Sends error reports to job owners
- 🛡️ **Smart Ban Detection**: Automatically removes banned/kicked chats
- 🔒 **Thread-Safe**: Concurrent posting with mutex protection

## Installation

```bash
git clone https://github.com/sandeep97217890-droid/AutoPoster.git
cd AutoPoster
go build -o autoposter
```

## Configuration

Edit `config.yaml`:

```yaml
telegram:
  app_id: 6
  app_hash: "YOUR_APP_HASH"
  session_file: "autoposter.session"

authorized_users:
  - 123456789

logging:
  level: "info"
```

**Get API Credentials:**
1. Visit https://my.telegram.org
2. Go to "API development tools"
3. Create application and copy `app_id` and `app_hash`

**Get Your User ID:**
- Send a message to @userinfobot on Telegram

## Usage

```bash
./autoposter
```

On first run, enter your phone number and verification code.

**Commands** (send to yourself in Telegram):
- `/start` - Welcome message
- `/setup` - Configure autoposting (chat IDs, interval, message)
- `/status` - View current jobs

**Setup Flow:**
1. `/setup`
2. Enter chat IDs (one per message, type 'done' when finished)
3. Select interval (2, 4, or 6 hours)
4. Enter message to post
5. Done! Autoposting starts immediately

## Features

**Authorization:**
- Only users in `authorized_users` list can use the bot
- Unauthorized users see "Access Denied" message

**Error Handling:**
- Detects ban/kick/forbidden errors
- Auto-removes problematic chats
- Sends error notifications to job owner
- Includes success/failure counts

**Posting:**
- 2-second delay between messages (rate limiting)
- Thread-safe concurrent goroutines
- Tracks job ownership for notifications

## Example

```
You: /setup

Bot: 🚀 Starting AutoPoster Setup

You: -1001234567890
Bot: ✅ Added chat ID: -1001234567890 (1 total)

You: done

Bot: Step 2: Posting Interval
     Select: 2, 4, or 6

You: 2

Bot: Step 3: Message
     What message to post?

You: Hello from AutoPoster!

Bot: ✅ Setup Complete!
     Interval: Every 2 hours
     🚀 Autoposting is now active!
```

## Running as Service

```bash
sudo nano /etc/systemd/system/autoposter.service
```

```ini
[Unit]
Description=AutoPoster
After=network.target

[Service]
Type=simple
User=youruser
WorkingDirectory=/path/to/AutoPoster
ExecStart=/path/to/AutoPoster/autoposter
Restart=always

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable autoposter
sudo systemctl start autoposter
```

## Security

⚠️ **Important:**
- Never share `autoposter.session` file
- Keep `config.yaml` private
- Don't commit credentials to Git
- Add to `.gitignore`:
  ```
  config.yaml
  *.session
  autoposter
  ```

## Code Structure

- `main.go` - Application logic
- `config.yaml` - Configuration
- `autoposter.session` - Session data (auto-created)

**Key Functions:**
- `loadConfig()` - Loads YAML configuration
- `authMiddleware()` - Checks user authorization
- `setupHandler()` - Interactive conversation setup
- `sendLoop()` - Main posting goroutine
- `isBannedError()` - Detects ban/kick errors
- `removeChat()` - Removes and notifies on ban

## License

MIT License

## Disclaimer

For educational purposes. Comply with Telegram's ToS. Excessive posting may result in restrictions.