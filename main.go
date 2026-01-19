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
	ID       int
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
	jobID  = 1
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

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
	log.Printf("BOT LOGGED IN → %s (@%s)", botinfo.FirstName, botinfo.Username)

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
	log.Printf("USER LOGGED IN → %s (@%s)", me.FirstName, me.Username)

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
	msg := `🤖 <b>AutoPoster</b>

Commands:
/setup - Create job
/status - Show jobs
/cancel - Stop job`

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

	text := "📊 <b>Running Jobs</b>\n\n"

	for _, job := range jobs {
		text += fmt.Sprintf(
			"🆔 #%d\nChats: %d\nInterval: %v\n\n",
			job.ID,
			len(job.Chats),
			job.Interval,
		)
	}

	m.Reply(text, &telegram.SendOptions{ParseMode: "html"})
	return nil
}

func cancelHandler(m *telegram.NewMessage) error {
	mu.Lock()

	if len(jobs) == 0 {
		mu.Unlock()
		m.Reply("❌ No jobs running", &telegram.SendOptions{})
		return nil
	}

	list := "🛑 <b>Send Job ID To Cancel</b>\n\n"
	for _, job := range jobs {
		list += fmt.Sprintf("ID %d → %d chats\n", job.ID, len(job.Chats))
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

	id, _ := strconv.Atoi(strings.TrimSpace(resp.Text()))

	mu.Lock()
	defer mu.Unlock()

	for i, job := range jobs {
		if job.ID == id {
			close(job.StopChan)
			jobs = append(jobs[:i], jobs[i+1:]...)
			log.Printf("JOB STOPPED → ID %d", id)
			conv.Respond("✅ Job cancelled", &telegram.SendOptions{})
			return nil
		}
	}

	conv.Respond("Job not found", &telegram.SendOptions{})
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

	conv.Respond("🚀 Setup started", &telegram.SendOptions{})

	var chatIDs []int64

	for {
		resp, err := conv.Ask("Send chat IDs (comma separated)\nType done when finished", &telegram.SendOptions{})
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

		for _, p := range strings.Split(txt, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
			if err == nil {
				chatIDs = append(chatIDs, id)
			}
		}

		conv.Respond(fmt.Sprintf("Total chats: %d", len(chatIDs)), &telegram.SendOptions{})
	}

	resp, err := conv.Ask("Interval in minutes", &telegram.SendOptions{})
	if err != nil {
		return err
	}

	mins, _ := strconv.Atoi(strings.TrimSpace(resp.Text()))
	interval := time.Duration(mins) * time.Minute

	msgResp, err := conv.Ask("Send message", &telegram.SendOptions{})
	if err != nil {
		return err
	}

	job := &Job{
		ID:       jobID,
		Chats:    chatIDs,
		Message:  msgResp,
		Interval: interval,
		OwnerID:  m.SenderID(),
		StopChan: make(chan struct{}),
	}

	jobID++

	mu.Lock()
	jobs = append(jobs, job)
	mu.Unlock()

	log.Printf("JOB CREATED → ID %d | Chats %d | Interval %v", job.ID, len(chatIDs), interval)

	go sendLoop(job)

	conv.Respond("✅ Job created & started", &telegram.SendOptions{})
	return nil
}

func isBannedError(err error) bool {
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
			log.Printf("CHAT REMOVED → Job %d | Chat %d", job.ID, chatID)
			return
		}
	}
}

func sendLoop(job *Job) {
	log.Printf("JOB LOOP STARTED → ID %d", job.ID)

	sendNow(job)

	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-job.StopChan:
			log.Printf("JOB LOOP STOPPED → ID %d", job.ID)
			return

		case <-ticker.C:
			sendNow(job)
		}
	}
}

func sendNow(job *Job) {
	mu.Lock()
	chats := append([]int64{}, job.Chats...)
	msg := job.Message
	mu.Unlock()

	success := 0

	for _, chat := range chats {
		_, err := client.SendMessage(chat, msg, &telegram.SendOptions{})

		if err != nil {
			log.Printf("SEND FAILED → Job %d | Chat %d | %v", job.ID, chat, err)

			if isBannedError(err) {
				removeChat(job, chat)
			}
		} else {
			success++
			log.Printf("SENT OK → Job %d | Chat %d", job.ID, chat)
		}

		time.Sleep(2 * time.Second)
	}

	log.Printf("JOB ROUND DONE → ID %d | Success %d | Total %d", job.ID, success, len(chats))
}
