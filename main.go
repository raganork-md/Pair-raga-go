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
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

const jwtSecret = "myKeyAan"

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

type PairingResponse struct {
	Code string `json:"code"`
}

// JWT Token Generator
func generateToken() (string, error) {
	claims := jwt.MapClaims{
		"exp": time.Now().Add(time.Hour * 1).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}

// Web Server for Render / Koyeb Health Check
func startWebServer() {
	port := getEnv("PORT", "8080")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "🤖 Bot is running smoothly!")
	})

	log.Printf("🌐 Web server active on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server Error: %v", err)
	}
}

// WebSocket Listener for Session
func listenWebSocket(bot *tgbotapi.BotAPI, chatID int64) {
	// Updated active Socket domain
	wsURL := "wss://session.rgnk.site/socket.io/?EIO=4&transport=websocket"

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

		if strings.Contains(msgStr, "session-received") {
			sessionData := extractSession(msgStr)

			var text string
			if sessionData != "" {
				text = fmt.Sprintf("✅ *Session Received*\n\n🔐 `%s`\n\n📋 Copy this", sessionData)
			} else {
				text = "✅ *Session Received*\n\n📋 Session fetched successfully!"
			}

			msg := tgbotapi.NewMessage(chatID, text)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			break
		}
	}
}

// Helper to extract session string from raw payload
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
			token, err := generateToken()
			if err != nil {
				bot.Send(tgbotapi.NewMessage(cID, "❌ Error generating authorization token"))
				return
			}

			// Background Socket Listener
			go listenWebSocket(bot, cID)

			cleanPhone := strings.TrimPrefix(p, "+")
			reqBody, _ := json.Marshal(map[string]string{
				"phoneNumber": cleanPhone,
			})

			req, err := http.NewRequest("POST", "https://session.rgnk.site/api/get-pairingcode", bytes.NewBuffer(reqBody))
			if err != nil {
				bot.Send(tgbotapi.NewMessage(cID, "❌ Error creating pairing request"))
				return
			}

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 15 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(cID, "❌ Pairing API request failed"))
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
