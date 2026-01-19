package main

import (
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram struct {
		AppID         int    `yaml:"app_id"`
		AppHash       string `yaml:"app_hash"`
		StringSession string `yaml:"string_session"`
		BotToken      string `yaml:"bot_token"`
	} `yaml:"telegram"`
	AuthorizedUsers []int64 `yaml:"authorized_users"`
}

type Job struct {
	Chats    []int64
	Message  *telegram.NewMessage
	Interval time.Duration
	OwnerID  int64
	StopChan chan struct{}
}

var (
	config Config
	client *telegram.Client
	mu     sync.Mutex
	jobs   []*Job
)

func main() {
	if err := loadConfig("config.yaml"); err != nil {
		log.Fatal(err)
	}

	bot, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   int32(config.Telegram.AppID),
		AppHash: config.Telegram.AppHash,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := bot.LoginBot(config.Telegram.BotToken); err != nil {
		log.Fatal(err)
	}

	adminFilter := telegram.Filter{
		Func: func(m *telegram.NewMessage) bool {
			return isAuthorized(m.SenderID())
		},
	}

	bot.On("cmd:start", startHandler, adminFilter)
	bot.On("cmd:setup", setupHandler, adminFilter)
	bot.On("cmd:status", statusHandler, adminFilter)
	bot.On("cmd:cancel", cancelHandler, adminFilter)

	botinfo, _ := bot.GetMe()
	log.Printf("Bot logged in as: %s (@%s)", botinfo.FirstName, botinfo.Username)

	client, err = telegram.NewClient(telegram.ClientConfig{
		AppID:         int32(config.Telegram.AppID),
		AppHash:       config.Telegram.AppHash,
		StringSession: config.Telegram.StringSession,
		MemorySession: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := client.Start(); err != nil {
		log.Fatal(err)
	}

	me, _ := client.GetMe()
	log.Printf("User logged in as: %s (@%s)", me.FirstName, me.Username)

	bot.Idle()
}

func loadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return err
	}

	if config.Telegram.AppID == 0 || config.Telegram.AppHash == "" || config.Telegram.BotToken == "" {
		return fmt.Errorf("invalid telegram config")
	}

	if len(config.AuthorizedUsers) == 0 {
		return fmt.Errorf("no authorized users")
	}

	return nil
}

func isAuthorized(userID int64) bool {
	return slices.Contains(config.AuthorizedUsers, userID)
}

func startHandler(m *telegram.NewMessage) error {
	msg := `🤖 <b>Welcome to AutoPoster</b>

Commands:
/setup - Create a new job
/status - View running jobs
/cancel - Stop a job`

	m.Reply(msg, &telegram.SendOptions{ParseMode: "html"})
	return nil
}

func statusHandler(m *telegram.NewMessage) error {
	mu.Lock()
	defer mu.Unlock()

	if len(jobs) == 0 {
		m.Reply("❌ No active jobs", &telegram.SendOptions{})
		return nil
	}

	text := "📊 <b>Active Jobs</b>\n\n"

	for i, job := range jobs {
		preview := job.Message.Text()
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}

		text += fmt.Sprintf(
			"<b>#%d</b>\nChats: %d\nInterval: %v\nOwner: <code>%d</code>\nMessage: %s\n\n",
			i+1,
			len(job.Chats),
			job.Interval,
			job.OwnerID,
			preview,
		)
	}

	m.Reply(text, &telegram.SendOptions{ParseMode: "html"})
	return nil
}

func cancelHandler(m *telegram.NewMessage) error {
	mu.Lock()

	if len(jobs) == 0 {
		mu.Unlock()
		m.Reply("❌ No jobs to cancel", &telegram.SendOptions{})
		return nil
	}

	list := "🛑 <b>Select Job Number To Cancel</b>\n\n"

	for i, job := range jobs {
		list += fmt.Sprintf("#%d → %d chats | %v\n", i+1, len(job.Chats), job.Interval)
	}

	mu.Unlock()

	conv, err := m.Client.NewConversation(m.ChatID(), &telegram.ConversationOptions{
		Timeout:       120,
		Private:       true,
		AbortKeywords: []string{"cancel", "quit"},
	})
	if err != nil {
		return err
	}
	defer conv.Close()

	resp, err := conv.Ask(list, &telegram.SendOptions{ParseMode: "html"})
	if err != nil {
		return err
	}

	index, err := strconv.Atoi(strings.TrimSpace(resp.Text()))
	if err != nil || index <= 0 {
		conv.Respond("Invalid number", &telegram.SendOptions{})
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	if index > len(jobs) {
		conv.Respond("Job not found", &telegram.SendOptions{})
		return nil
	}

	job := jobs[index-1]

	close(job.StopChan)

	jobs = append(jobs[:index-1], jobs[index:]...)

	conv.Respond("✅ Job cancelled successfully", &telegram.SendOptions{})

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

	conv.Respond("🚀 <b>AutoPoster Setup Started</b>", &telegram.SendOptions{ParseMode: "html"})

	var chatIDs []int64

	for {
		resp, err := conv.Ask("Send chat IDs separated by comma\nType <b>done</b> when finished", &telegram.SendOptions{ParseMode: "html"})
		if err != nil {
			return err
		}

		txt := strings.TrimSpace(resp.Text())

		if strings.ToLower(txt) == "done" {
			if len(chatIDs) == 0 {
				conv.Respond("Add at least one chat", &telegram.SendOptions{})
				continue
			}
			break
		}

		parts := strings.Split(txt, ",")

		for _, p := range parts {
			id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
			if err == nil {
				chatIDs = append(chatIDs, id)
			}
		}

		conv.Respond(fmt.Sprintf("Total chats: %d", len(chatIDs)), &telegram.SendOptions{})
	}

	resp, err := conv.Ask("Enter interval in minutes", &telegram.SendOptions{})
	if err != nil {
		return err
	}

	mins, _ := strconv.Atoi(strings.TrimSpace(resp.Text()))
	interval := time.Duration(mins) * time.Minute

	msgResp, err := conv.Ask("Send the message to autopost", &telegram.SendOptions{})
	if err != nil {
		return err
	}

	job := &Job{
		Chats:    chatIDs,
		Message:  msgResp,
		Interval: interval,
		OwnerID:  m.SenderID(),
		StopChan: make(chan struct{}),
	}

	mu.Lock()
	jobs = append(jobs, job)
	mu.Unlock()

	go sendLoop(job)

	conv.Respond("✅ Job created successfully", &telegram.SendOptions{})
	return nil
}

func isBannedError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "not participant") ||
		strings.Contains(msg, "kicked") ||
		strings.Contains(msg, "banned") ||
		strings.Contains(msg, "chat_write_forbidden")
}

func removeChat(job *Job, chatID int64) {
	mu.Lock()
	defer mu.Unlock()

	for i, id := range job.Chats {
		if id == chatID {
			job.Chats = append(job.Chats[:i], job.Chats[i+1:]...)
			return
		}
	}
}

func sendLoop(job *Job) {
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-job.StopChan:
			return

		case <-ticker.C:

			mu.Lock()
			chats := append([]int64{}, job.Chats...)
			msg := job.Message
			mu.Unlock()

			for _, chat := range chats {
				_, err := client.SendMessage(chat, msg, &telegram.SendOptions{})

				if err != nil && isBannedError(err) {
					removeChat(job, chat)
				}

				time.Sleep(2 * time.Second)
			}
		}
	}
}
