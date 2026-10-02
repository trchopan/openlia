package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

var telegramAPIBase = "https://api.telegram.org"

var telegramTokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)

type tgAPIResponse[T any] struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
	Result      T      `json:"result"`
}

type tgUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

func (u tgUser) displayName() string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name != "" {
		return name
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return fmt.Sprintf("User %d", u.ID)
}

type tgChat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"` // "private", "group", "supergroup", "channel"
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func (c tgChat) displayName() string {
	if c.Title != "" {
		return c.Title
	}
	name := strings.TrimSpace(c.FirstName + " " + c.LastName)
	if name != "" {
		return name
	}
	if c.Username != "" {
		return "@" + c.Username
	}
	return fmt.Sprintf("Chat %d", c.ID)
}

type tgChatMemberUpdated struct {
	Chat          tgChat `json:"chat"`
	From          tgUser `json:"from"`
	Date          int64  `json:"date"`
	NewChatMember struct {
		Status string `json:"status"`
	} `json:"new_chat_member"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Date      int64   `json:"date"`
	Text      string  `json:"text"`
	Caption   string  `json:"caption"`
}

type tgUpdate struct {
	UpdateID     int64                `json:"update_id"`
	Message      *tgMessage           `json:"message"`
	EditedMsg    *tgMessage           `json:"edited_message"`
	ChannelPost  *tgMessage           `json:"channel_post"`
	MyChatMember *tgChatMemberUpdated `json:"my_chat_member"`
}

type TelegramActivityEvent struct {
	UpdateID  int64     `json:"update_id"`
	Date      time.Time `json:"date"`
	ChatID    int64     `json:"chat_id"`
	ChatType  string    `json:"chat_type"`
	ChatTitle string    `json:"chat_title,omitempty"`
	UserID    int64     `json:"user_id,omitempty"`
	UserName  string    `json:"user_name,omitempty"`
	Username  string    `json:"username,omitempty"`
	Message   string    `json:"message"`
}

type TelegramDiscoveredUser struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Username      string    `json:"username,omitempty"`
	LatestMessage string    `json:"latest_message,omitempty"`
	LatestDate    time.Time `json:"latest_date"`
}

type TelegramDiscoveredChat struct {
	ID         int64     `json:"id"`
	Type       string    `json:"type"`
	Title      string    `json:"title"`
	LatestDate time.Time `json:"latest_date"`
}

type TelegramLookupResult struct {
	Schema       int                      `json:"schema"`
	OK           bool                     `json:"ok"`
	Bot          tgUser                   `json:"bot"`
	Events       []TelegramActivityEvent  `json:"events"`
	Users        []TelegramDiscoveredUser `json:"users"`
	Chats        []TelegramDiscoveredChat `json:"chats"`
	ConfigSample map[string]string        `json:"config_sample"`
}

func validateTelegramToken(token string) error {
	if !telegramTokenPattern.MatchString(token) {
		return errors.New("invalid Telegram bot token format; expected '<bot_id>:<secret>' (from BotFather)")
	}
	return nil
}

func resolveTelegramToken(flagToken string, nonInteractive bool) (string, error) {
	if token := strings.TrimSpace(flagToken); token != "" {
		return token, nil
	}
	if envToken := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")); envToken != "" {
		return envToken, nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		if nonInteractive {
			return "", errors.New("telegram bot token is required; specify --token or set TELEGRAM_BOT_TOKEN")
		}
		fmt.Fprint(os.Stderr, "Enter Telegram Bot Token: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return "", errors.New("token cannot be empty")
		}
		return token, nil
	}

	// If stdin is not a terminal, attempt to read a line from piped input
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			token := strings.TrimSpace(scanner.Text())
			if token != "" {
				return token, nil
			}
		}
	}

	return "", errors.New("telegram bot token is required; specify --token or set TELEGRAM_BOT_TOKEN")
}

func fetchTelegramBot(ctx context.Context, client *http.Client, token string) (tgUser, error) {
	url := fmt.Sprintf("%s/bot%s/getMe", telegramAPIBase, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return tgUser{}, err
	}
	req.Header.Set("User-Agent", "openlia/"+defaultVersion)

	resp, err := client.Do(req)
	if err != nil {
		return tgUser{}, fmt.Errorf("connect to Telegram API: %w", err)
	}
	defer resp.Body.Close()

	var apiResp tgAPIResponse[tgUser]
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return tgUser{}, fmt.Errorf("parse Telegram getMe response: %w", err)
	}
	if !apiResp.OK {
		if apiResp.ErrorCode == 401 {
			return tgUser{}, errors.New("telegram bot token is unauthorized or invalid (HTTP 401)")
		}
		return tgUser{}, fmt.Errorf("telegram API error (%d): %s", apiResp.ErrorCode, apiResp.Description)
	}
	return apiResp.Result, nil
}

func fetchTelegramUpdates(ctx context.Context, client *http.Client, token string) ([]tgUpdate, error) {
	url := fmt.Sprintf("%s/bot%s/getUpdates", telegramAPIBase, token)
	bodyPayload := map[string]any{
		"allowed_updates": []string{"message", "edited_message", "channel_post", "my_chat_member"},
	}
	payloadBytes, err := json.Marshal(bodyPayload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "openlia/"+defaultVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query Telegram updates: %w", err)
	}
	defer resp.Body.Close()

	var apiResp tgAPIResponse[[]tgUpdate]
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("parse Telegram getUpdates response: %w", err)
	}
	if !apiResp.OK {
		return nil, fmt.Errorf("telegram getUpdates error (%d): %s", apiResp.ErrorCode, apiResp.Description)
	}
	return apiResp.Result, nil
}

func processTelegramUpdates(updates []tgUpdate) ([]TelegramActivityEvent, []TelegramDiscoveredUser, []TelegramDiscoveredChat) {
	var events []TelegramActivityEvent
	userMap := make(map[int64]*TelegramDiscoveredUser)
	chatMap := make(map[int64]*TelegramDiscoveredChat)

	for _, update := range updates {
		var event TelegramActivityEvent
		event.UpdateID = update.UpdateID

		if update.Message != nil {
			msg := update.Message
			event.Date = time.Unix(msg.Date, 0).UTC()
			event.ChatID = msg.Chat.ID
			event.ChatType = msg.Chat.Type
			event.ChatTitle = msg.Chat.displayName()
			if msg.From != nil {
				event.UserID = msg.From.ID
				event.UserName = msg.From.displayName()
				event.Username = msg.From.Username
			}
			if msg.Text != "" {
				event.Message = msg.Text
			} else if msg.Caption != "" {
				event.Message = msg.Caption
			} else {
				event.Message = "[media or non-text message]"
			}
		} else if update.EditedMsg != nil {
			msg := update.EditedMsg
			event.Date = time.Unix(msg.Date, 0).UTC()
			event.ChatID = msg.Chat.ID
			event.ChatType = msg.Chat.Type
			event.ChatTitle = msg.Chat.displayName()
			if msg.From != nil {
				event.UserID = msg.From.ID
				event.UserName = msg.From.displayName()
				event.Username = msg.From.Username
			}
			event.Message = "[edited] " + msg.Text
		} else if update.ChannelPost != nil {
			post := update.ChannelPost
			event.Date = time.Unix(post.Date, 0).UTC()
			event.ChatID = post.Chat.ID
			event.ChatType = "channel"
			event.ChatTitle = post.Chat.displayName()
			event.Message = post.Text
			if event.Message == "" {
				event.Message = post.Caption
			}
		} else if update.MyChatMember != nil {
			m := update.MyChatMember
			event.Date = time.Unix(m.Date, 0).UTC()
			event.ChatID = m.Chat.ID
			event.ChatType = m.Chat.Type
			event.ChatTitle = m.Chat.displayName()
			event.UserID = m.From.ID
			event.UserName = m.From.displayName()
			event.Username = m.From.Username
			event.Message = fmt.Sprintf("[bot membership updated: %s]", m.NewChatMember.Status)
		} else {
			continue
		}

		events = append(events, event)

		// Aggregate unique users
		if event.UserID != 0 {
			existing, ok := userMap[event.UserID]
			if !ok || event.Date.After(existing.LatestDate) {
				userMap[event.UserID] = &TelegramDiscoveredUser{
					ID:            event.UserID,
					Name:          event.UserName,
					Username:      event.Username,
					LatestMessage: event.Message,
					LatestDate:    event.Date,
				}
			}
		}

		// Aggregate non-private group/channel chats
		if event.ChatType != "private" && event.ChatID != 0 {
			existing, ok := chatMap[event.ChatID]
			if !ok || event.Date.After(existing.LatestDate) {
				chatMap[event.ChatID] = &TelegramDiscoveredChat{
					ID:         event.ChatID,
					Type:       event.ChatType,
					Title:      event.ChatTitle,
					LatestDate: event.Date,
				}
			}
		}
	}

	users := make([]TelegramDiscoveredUser, 0, len(userMap))
	for _, u := range userMap {
		users = append(users, *u)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].LatestDate.After(users[j].LatestDate)
	})

	chats := make([]TelegramDiscoveredChat, 0, len(chatMap))
	for _, c := range chatMap {
		chats = append(chats, *c)
	}
	sort.Slice(chats, func(i, j int) bool {
		return chats[i].LatestDate.After(chats[j].LatestDate)
	})

	return events, users, chats
}

func formatTelegramHumanOutput(bot tgUser, events []TelegramActivityEvent, users []TelegramDiscoveredUser, chats []TelegramDiscoveredChat, token string) string {
	var sb strings.Builder

	botDisplay := "@" + bot.Username
	if bot.Username == "" {
		botDisplay = bot.FirstName
	}
	sb.WriteString(fmt.Sprintf("Bot: %s (ID: %d)\n", botDisplay, bot.ID))

	if len(events) == 0 {
		sb.WriteString("\nNo recent messages or events found for this bot.\n\n")
		sb.WriteString("To link your Telegram account:\n")
		sb.WriteString(fmt.Sprintf("  1. In Telegram, search for %s\n", botDisplay))
		sb.WriteString("  2. Click 'Start' or send a message (e.g. '/start')\n")
		sb.WriteString(fmt.Sprintf("  3. (Optional) Add %s to your group and send a message\n", botDisplay))
		sb.WriteString("  4. Run 'openlia telegram id' again\n")
		sb.WriteString("\nThe bot must receive a message before its chat ID can be discovered.\n")
		return sb.String()
	}

	sb.WriteString("\nRecent Telegram Activity:\n")
	for i, ev := range events {
		sb.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, ev.Date.Format("2006-01-02 15:04:05 UTC")))
		if ev.UserID != 0 {
			userTag := ev.UserName
			if ev.Username != "" {
				userTag += " (@" + ev.Username + ")"
			}
			sb.WriteString(fmt.Sprintf("      From:    %s [User ID: %d]\n", userTag, ev.UserID))
		}
		if ev.ChatType == "private" {
			sb.WriteString(fmt.Sprintf("      Chat:    Private [Chat ID: %d]\n", ev.ChatID))
		} else {
			sb.WriteString(fmt.Sprintf("      Chat:    %s %q [Chat ID: %d]\n", strings.Title(ev.ChatType), ev.ChatTitle, ev.ChatID))
		}
		sb.WriteString(fmt.Sprintf("      Message: %q\n", ev.Message))
	}

	sb.WriteString("\nDiscovered IDs:\n")
	if len(users) > 0 {
		sb.WriteString("  Users (for TELEGRAM_ALLOWED_USERS):\n")
		for _, u := range users {
			tag := u.Name
			if u.Username != "" {
				tag += " (@" + u.Username + ")"
			}
			sb.WriteString(fmt.Sprintf("    • %d (%s) - latest message: %q\n", u.ID, tag, u.LatestMessage))
		}
	}

	if len(chats) > 0 {
		sb.WriteString("  Groups / Channels:\n")
		for _, c := range chats {
			sb.WriteString(fmt.Sprintf("    • %d (%s: %q)\n", c.ID, c.Type, c.Title))
		}
	}

	sb.WriteString("\nSuggested OpenLia configuration (in your protected dotenv source / hermes.env):\n")
	sb.WriteString(fmt.Sprintf("TELEGRAM_BOT_TOKEN=%s\n", token))
	if len(users) > 0 {
		var userIDs []string
		for _, u := range users {
			userIDs = append(userIDs, strconv.FormatInt(u.ID, 10))
		}
		sb.WriteString(fmt.Sprintf("TELEGRAM_ALLOWED_USERS=%s\n", strings.Join(userIDs, ",")))
	}
	sb.WriteString("\nTo make scheduled tasks deliver to one of the chats above, add:\n")
	sb.WriteString("TELEGRAM_HOME_CHANNEL=<copy the selected Chat ID above>\n")
	sb.WriteString("Then run 'openlia auth rotate'; a running Hermes service is recreated automatically.\n")

	return sb.String()
}

func commandTelegram(options Options, args []string) int {
	if len(args) == 0 {
		usageTelegram(os.Stderr)
		return fail(options, ExitUsage, "telegram requires the 'id' subcommand (e.g. openlia telegram id)", nil)
	}

	action := args[0]
	subArgs := args[1:]

	switch action {
	case "id", "ids", "lookup":
		return commandTelegramID(options, subArgs)
	case "help", "--help", "-h":
		usageTelegram(os.Stdout)
		return ExitOK
	default:
		return fail(options, ExitUsage, fmt.Sprintf("unknown telegram subcommand %q", action), map[string]any{"hint": "run openlia telegram --help"})
	}
}

func usageTelegram(out io.Writer) {
	fmt.Fprint(out, `openlia telegram - Telegram integration helpers

Usage:
  openlia telegram id [--token TOKEN]

Retrieve Telegram user IDs, chat IDs, and recent messages using your BotFather bot token.
The discovered user IDs can be used for TELEGRAM_ALLOWED_USERS, and a selected
chat ID can be used for TELEGRAM_HOME_CHANNEL, in your protected dotenv source.
The bot must have received an update from a chat before its ID can be discovered.

Flags:
  --token string   Telegram bot token (format: '<bot_id>:<secret>')
                   If omitted, reads from TELEGRAM_BOT_TOKEN or prompts interactively.
`)
}

func commandTelegramID(options Options, args []string) int {
	set := newFlagSet("telegram id")
	tokenFlag := set.String("token", "", "Telegram bot token")
	if err := set.Parse(args); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}

	token, err := resolveTelegramToken(*tokenFlag, options.NonInteractive)
	if err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}

	if err := validateTelegramToken(token); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 15 * time.Second}

	bot, err := fetchTelegramBot(ctx, client, token)
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}

	updates, err := fetchTelegramUpdates(ctx, client, token)
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}

	events, users, chats := processTelegramUpdates(updates)

	var userIDs []string
	for _, u := range users {
		userIDs = append(userIDs, strconv.FormatInt(u.ID, 10))
	}

	configSample := map[string]string{
		"TELEGRAM_BOT_TOKEN": token,
	}
	if len(userIDs) > 0 {
		configSample["TELEGRAM_ALLOWED_USERS"] = strings.Join(userIDs, ",")
	}

	result := TelegramLookupResult{
		Schema:       1,
		OK:           true,
		Bot:          bot,
		Events:       events,
		Users:        users,
		Chats:        chats,
		ConfigSample: configSample,
	}

	human := formatTelegramHumanOutput(bot, events, users, chats, token)
	return writeResult(options, result, human)
}
