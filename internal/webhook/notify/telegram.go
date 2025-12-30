package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shieldx-bot/shieldx-platform/internal/config/dotenv"
)

func Getenv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func GetTelegramCredentials() (string, int64, error) {
	botToken := Getenv("TELEGRAM_BOT_TOKEN", "8526833134:AAEim6m-p-H5RdcWnWYqcEGSe-eUWdAEQ7I")
	chatIDStr := Getenv("TELEGRAM_CHAT_ID", "-5090601314")
	// botToken := "8526833134:AAEim6m-p-H5RdcWnWYqcEGSe-eUWdAEQ7I"
	// chatIDStr := "-5090601314"
	if botToken == "" || chatIDStr == "" {
		return "", 0, fmt.Errorf("telegram bot token or chat ID is not set in environment variables")
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid telegram chat ID: %v", err)
	}

	return botToken, chatID, nil
}

func SendMessageTelegram(message string) error {
	// Best-effort load of local .env files for developer convenience.
	// In Kubernetes, env vars should be injected by the runtime.
	dotenv.Load()

	// Implementation for sending message to Telegram
	ctx := context.Background()
	BotToken, chatID, err := GetTelegramCredentials()
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", BotToken)
	form := url.Values{}
	form.Set("chat_id", strconv.FormatInt(chatID, 10))
	form.Set("text", message)
	// Intentionally do not set parse_mode here.
	// Telegram's Markdown parser is strict and can reject messages containing characters
	// like []()<>. Plain text is safer for debug output.

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		fmt.Printf("failed to build Telegram request: %v\n", err)
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("failed to send Telegram notification: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		msg := fmt.Sprintf("Telegram API returned non-2xx: status=%s body=%s", resp.Status, string(body))
		fmt.Printf("%s\n", msg)
		return errors.New(msg)
	}

	return nil
}
