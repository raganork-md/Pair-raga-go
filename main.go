package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/gorilla/websocket"
)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

type PairingResponse struct {
	Code string `json:"code"`
}

// Socket.io Payload Structure Parser
type SessionPayload struct {
	Session string `json:"session"`
}

// Koyeb Health Check-നു വേണ്ടി റൺ ചെയ്യുന്ന HTTP Server
func startWebServer() {
	port := getEnv("PORT", "8080")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "🤖 Bot is running smoothly on Koyeb!")
	})

	log.Printf("🌐 Web server active on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server Error: %v", err)
	}
}

// WebSocket listener for receiving session
func listenWebSocket(bot *tgbotapi.BotAPI, chatID int64) {
	wsURL := "wss://heroku-session.raganork.site/socket.io/?EIO=4&transport=websocket"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Println("WS Connection Error:", err)
		return
	}
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		msgStr := string(message)

		// Check if payload contains 'session-received' event
		if strings.Contains(msgStr, "session-received") {
			sessionData := extractSession(msgStr)

			var text string
			if sessionData != "" {
				text = fmt.Sprintf("✅ *Session Received*\n\n🔐 `%s`\n\n📋 Copy this", sessionData)
			} else {
				text = "✅ *Session Received*\n\n📋 Session generated successfully! Check your database."
			}

			msg := tgbotapi.NewMessage(chatID, text)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			break
		}
	}
}

// Helper to parse session string from socket.io raw text packet
func extractSession(raw string) string {
	startIdx := strings.Index(raw, "{")
	if startIdx == -1 {
		return ""
	}
	jsonStr := raw[startIdx:]

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err == nil {
		if sessionVal, ok := data["session"].(string); ok {
			return sessionVal
		}
	}
	return ""
}

func main() {
	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		log.Fatal("❌ ERROR: BOT_TOKEN environment variable is not set!")
	}

	go startWebServer()

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic("Bot init error: ", err)
	}

	log.Printf("🤖 Authorized account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	phoneRegex := regexp.MustCompile(`^\+\d{10,15}$`)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID
		text := strings.TrimSpace(update.Message.Text)

		if text == "/start" {
			msg := tgbotapi.NewMessage(chatID, "👋 Welcome!\n\nSend your WhatsApp number with country code 🌍\n\nExample:\n+919876543210")
			bot.Send(msg)
			continue
		}

		if strings.HasPrefix(text, "/") {
			continue
		}

		phone := strings.ReplaceAll(text, " ", "")
		if !strings.HasPrefix(phone, "+") {
			phone = "+" + phone
		}

		if !phoneRegex.MatchString(phone) {
			msg := tgbotapi.NewMessage(chatID, "❌ Send valid number\nExample: +919876543210")
			bot.Send(msg)
			continue
		}

		go func(p string, cID int64) {
			// Start listening for session on socket in background
			go listenWebSocket(bot, cID)

			cleanPhone := strings.TrimPrefix(p, "+")
			reqBody, _ := json.Marshal(map[string]string{
				"phoneNumber": cleanPhone,
			})

			req, err := http.NewRequest("POST", "https://session.rgnk.site/api/get-pairingcode", bytes.NewBuffer(reqBody))
			if err != nil {
				bot.Send(tgbotapi.NewMessage(cID, "❌ Error creating request"))
				return
			}

			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 15 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(cID, "❌ API request failed"))
				return
			}
			defer resp.Body.Close()

			var pairingData PairingResponse
			if err := json.NewDecoder(resp.Body).Decode(&pairingData); err != nil || pairingData.Code == "" {
				bot.Send(tgbotapi.NewMessage(cID, "❌ Invalid response from pairing server"))
				return
			}

			msgText := fmt.Sprintf("🔑 *Pairing Code*\n\n📱 %s\n\n`%s`\n\n➡️ Connect WhatsApp now", p, pairingData.Code)
			msg := tgbotapi.NewMessage(cID, msgText)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
		}(phone, chatID)
	}
}
