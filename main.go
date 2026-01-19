package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram struct {
		AppID       int    `yaml:"app_id"`
		AppHash     string `yaml:"app_hash"`
		SessionFile string `yaml:"session_file"`
	} `yaml:"telegram"`
	AuthorizedUsers []int64 `yaml:"authorized_users"`
	Logging         struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
}

var (
	client *telegram.Client
	mu     sync.Mutex
	config Config

	every2h = []int64{}
	every4h = []int64{}
	every6h = []int64{}

	message2h = ""
	message4h = ""
	message6h = ""

	job2hOwner int64
	job4hOwner int64
	job6hOwner int64
)

func main() {
	if err := loadConfig("config.yaml"); err != nil {
		log.Fatal("Failed to load config:", err)
	}

	var err error

	client, err = telegram.NewClient(telegram.ClientConfig{
		AppID:    int32(config.Telegram.AppID),
		AppHash:  config.Telegram.AppHash,
		LogLevel: getLogLevel(config.Logging.Level),
	})
	if err != nil {
		log.Fatal("Failed to create client:", err)
	}

	if err := client.Connect(); err != nil {
		log.Fatal("Failed to connect:", err)
	}

	authorized, err := client.IsAuthorized()
	if err != nil {
		log.Fatal("Failed to check authorization:", err)
	}

	if !authorized {
		log.Println("User account not authorized. Please login...")
		var phone string
		fmt.Print("Enter phone number (with country code, e.g., +1234567890): ")
		fmt.Scanln(&phone)
		if _, err := client.Login(phone); err != nil {
			log.Fatal("Failed to login:", err)
		}
	}

	me, err := client.GetMe()
	if err != nil {
		log.Fatal("Failed to get user info:", err)
	}
	log.Printf("Logged in as: %s (@%s) [ID: %d]\n", me.FirstName, me.Username, me.ID)

	client.On("command:start", authMiddleware(startHandler))
	client.On("command:setup", authMiddleware(setupHandler))
	client.On("command:status", authMiddleware(statusHandler))

	log.Printf("Bot is running. Authorized users: %v\n", config.AuthorizedUsers)

	client.Idle()
}

func loadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	if config.Telegram.AppID == 0 || config.Telegram.AppHash == "" {
		return fmt.Errorf("telegram credentials not configured in config.yaml")
	}

	if len(config.AuthorizedUsers) == 0 {
		return fmt.Errorf("no authorized users configured in config.yaml")
	}

	return nil
}

func getLogLevel(level string) telegram.LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return telegram.LogDebug
	case "warn", "warning":
		return telegram.LogWarn
	case "error":
		return telegram.LogError
	default:
		return telegram.LogInfo
	}
}

func isAuthorized(userID int64) bool {
	for _, id := range config.AuthorizedUsers {
		if id == userID {
			return true
		}
	}
	return false
}

func authMiddleware(handler func(*telegram.NewMessage) error) func(*telegram.NewMessage) error {
	return func(m *telegram.NewMessage) error {
		senderID := m.SenderID()
		if !isAuthorized(senderID) {
			m.Reply("⛔ <b>Access Denied</b>\n\nYou are not authorized to use this bot.\nThis bot is restricted to authorized developers only.", &telegram.SendOptions{ParseMode: "html"})
			log.Printf("Unauthorized access attempt from user ID: %d\n", senderID)
			return nil
		}
		return handler(m)
	}
}

func startHandler(m *telegram.NewMessage) error {
	welcomeMsg := `🤖 <b>Welcome to AutoPoster!</b>

This bot helps you automatically post messages to multiple Telegram chats at regular intervals.

<b>Available Commands:</b>
/setup - Configure a new autopost job
/status - View current autopost configurations

<b>You are authorized to use this bot.</b>

Use /setup to get started!`

	m.Reply(welcomeMsg, &telegram.SendOptions{ParseMode: "html"})
	return nil
}

func statusHandler(m *telegram.NewMessage) error {
	mu.Lock()
	defer mu.Unlock()

	status := "📊 <b>Current Autoposting Status</b>\n\n"

	if len(every2h) > 0 {
		status += fmt.Sprintf("<b>2-Hour Interval</b> (%d chats):\n", len(every2h))
		for _, id := range every2h {
			status += fmt.Sprintf("  • <code>%d</code>\n", id)
		}
		msg := message2h
		if len(msg) > 50 {
			msg = msg[:50] + "..."
		}
		status += fmt.Sprintf("<b>Message:</b> %s\n", msg)
		status += fmt.Sprintf("<b>Owner:</b> <code>%d</code>\n\n", job2hOwner)
	}

	if len(every4h) > 0 {
		status += fmt.Sprintf("<b>4-Hour Interval</b> (%d chats):\n", len(every4h))
		for _, id := range every4h {
			status += fmt.Sprintf("  • <code>%d</code>\n", id)
		}
		msg := message4h
		if len(msg) > 50 {
			msg = msg[:50] + "..."
		}
		status += fmt.Sprintf("<b>Message:</b> %s\n", msg)
		status += fmt.Sprintf("<b>Owner:</b> <code>%d</code>\n\n", job4hOwner)
	}

	if len(every6h) > 0 {
		status += fmt.Sprintf("<b>6-Hour Interval</b> (%d chats):\n", len(every6h))
		for _, id := range every6h {
			status += fmt.Sprintf("  • <code>%d</code>\n", id)
		}
		msg := message6h
		if len(msg) > 50 {
			msg = msg[:50] + "..."
		}
		status += fmt.Sprintf("<b>Message:</b> %s\n", msg)
		status += fmt.Sprintf("<b>Owner:</b> <code>%d</code>\n\n", job6hOwner)
	}

	if len(every2h) == 0 && len(every4h) == 0 && len(every6h) == 0 {
		status += "❌ No autopost jobs configured. Use /setup to create one."
	}

	m.Reply(status, &telegram.SendOptions{ParseMode: "html"})
	return nil
}

func setupHandler(m *telegram.NewMessage) error {
	conv, err := m.Client.NewConversation(m.ChatID(), &telegram.ConversationOptions{
		Timeout:       300,
		Private:       true,
		AbortKeywords: []string{"cancel", "quit"},
	})
	if err != nil {
		return err
	}
	defer conv.Close()

	m.Reply("🚀 <b>Starting AutoPoster Setup</b>\n\n(Type 'cancel' at any time to abort)", &telegram.SendOptions{ParseMode: "html"})

	conv.Respond("📝 <b>Step 1: Chat IDs</b>\n\nPlease enter the chat IDs you want to post to.\nEnter one chat ID per message.\nType 'done' when finished.\n\n💡 Tip: Use @userinfobot to get chat IDs", &telegram.SendOptions{ParseMode: "html"})

	var chatIDs []int64
	for {
		response, err := conv.Ask("")
		if err != nil {
			return err
		}

		text := strings.TrimSpace(response.Text())

		if strings.ToLower(text) == "done" {
			if len(chatIDs) == 0 {
				conv.Respond("❌ You must add at least one chat ID. Please enter a chat ID:")
				continue
			}
			break
		}

		chatID, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			conv.Respond("❌ Invalid chat ID. Please enter a valid number or 'done' to finish:")
			continue
		}

		chatIDs = append(chatIDs, chatID)
		conv.Respond(fmt.Sprintf("✅ Added chat ID: <code>%d</code> (%d total)\nEnter another or type 'done':", chatID, len(chatIDs)), &telegram.SendOptions{ParseMode: "html"})
	}

	conv.Respond(fmt.Sprintf("📝 <b>Step 2: Posting Interval</b>\n\nYou've added %d chat(s).\n\nPlease select the posting interval:\n• Type <b>2</b> for every 2 hours\n• Type <b>4</b> for every 4 hours\n• Type <b>6</b> for every 6 hours", len(chatIDs)), &telegram.SendOptions{ParseMode: "html"})

	var intervalHours int
	for {
		response, err := conv.Ask("")
		if err != nil {
			return err
		}

		text := strings.TrimSpace(response.Text())
		intervalHours, err = strconv.Atoi(text)
		if err != nil || (intervalHours != 2 && intervalHours != 4 && intervalHours != 6) {
			conv.Respond("❌ Invalid interval. Please enter 2, 4, or 6:")
			continue
		}
		break
	}

	conv.Respond("📝 <b>Step 3: Message</b>\n\nWhat message would you like to post to these chats?\nSend the message now:", &telegram.SendOptions{ParseMode: "html"})

	messageResponse, err := conv.Ask("")
	if err != nil {
		return err
	}

	messageText := messageResponse.Text()
	ownerID := m.SenderID()

	mu.Lock()
	switch intervalHours {
	case 2:
		every2h = append(every2h, chatIDs...)
		message2h = messageText
		job2hOwner = ownerID
		if len(every2h) == len(chatIDs) {
			go sendLoop(&every2h, &message2h, 2*time.Hour, &job2hOwner)
		}
	case 4:
		every4h = append(every4h, chatIDs...)
		message4h = messageText
		job4hOwner = ownerID
		if len(every4h) == len(chatIDs) {
			go sendLoop(&every4h, &message4h, 4*time.Hour, &job4hOwner)
		}
	case 6:
		every6h = append(every6h, chatIDs...)
		message6h = messageText
		job6hOwner = ownerID
		if len(every6h) == len(chatIDs) {
			go sendLoop(&every6h, &message6h, 6*time.Hour, &job6hOwner)
		}
	}
	mu.Unlock()

	summary := fmt.Sprintf(`✅ <b>Setup Complete!</b>

<b>Interval:</b> Every %d hours
<b>Chats added:</b> %d
<b>Message:</b> %s

<b>Chat IDs:</b>
`, intervalHours, len(chatIDs), messageText)

	for _, id := range chatIDs {
		summary += fmt.Sprintf("• <code>%d</code>\n", id)
	}

	summary += "\n🚀 Autoposting is now active!\nUse /status to view current configuration.\n\n📧 You will receive error notifications if any message fails to send."

	conv.Respond(summary, &telegram.SendOptions{ParseMode: "html"})
	return nil
}

func isBannedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "chat_write_forbidden") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "not participant") ||
		strings.Contains(msg, "kicked") ||
		strings.Contains(msg, "banned")
}

func removeChat(chats *[]int64, chatID int64, ownerID int64) {
	mu.Lock()
	defer mu.Unlock()

	list := *chats
	for i, id := range list {
		if id == chatID {
			*chats = append(list[:i], list[i+1:]...)
			if ownerID != 0 {
				client.SendMessage(ownerID, fmt.Sprintf("⚠️ <b>Chat Removed</b>\n\nChat ID <code>%d</code> has been removed from autoposting due to ban/kick.\n\nRemaining chats: %d", chatID, len(*chats)), &telegram.SendOptions{ParseMode: "html"})
			}
			log.Printf("Removed chat %d from posting list (banned/kicked)\n", chatID)
			return
		}
	}
}

func sendLoop(chats *[]int64, message *string, interval time.Duration, ownerID *int64) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		mu.Lock()
		currentChats := append([]int64{}, (*chats)...)
		currentMessage := *message
		currentOwner := *ownerID
		mu.Unlock()

		if len(currentChats) > 0 {
			log.Printf("Starting posting round to %d chats (interval: %v)\n", len(currentChats), interval)

			var failedChats []string
			successCount := 0

			for _, chat := range currentChats {
				_, err := client.SendMessage(chat, currentMessage, nil)

				if err != nil {
					log.Printf("Send failed to chat %d: %v\n", chat, err)
					failedChats = append(failedChats, fmt.Sprintf("• Chat <code>%d</code>: %s", chat, err.Error()))

					if isBannedError(err) {
						removeChat(chats, chat, currentOwner)
					}
				} else {
					successCount++
					log.Printf("✓ Sent to chat %d\n", chat)
				}

				time.Sleep(2 * time.Second)
			}

			if len(failedChats) > 0 && currentOwner != 0 {
				errorMsg := fmt.Sprintf("❌ <b>Posting Errors</b>\n\nInterval: %v\nSuccessful: %d\nFailed: %d\n\n<b>Errors:</b>\n%s",
					interval, successCount, len(failedChats), strings.Join(failedChats, "\n"))
				client.SendMessage(currentOwner, errorMsg, &telegram.SendOptions{ParseMode: "html"})
			} else if successCount > 0 {
				log.Printf("✓ Successfully posted to all %d chats (interval: %v)\n", successCount, interval)
			}
		}

		<-ticker.C
	}
}
