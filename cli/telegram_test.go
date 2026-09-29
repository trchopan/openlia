package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestValidateTelegramToken(t *testing.T) {
	valid := []string{
		"123456789:ABCdefGHIjklMNOpqrsTUVwxyz_123",
		"9876543210:AA-bb_ccDDeeFFggHHiiJJkkLLmmNN",
		"1:a",
	}
	for _, tok := range valid {
		if err := validateTelegramToken(tok); err != nil {
			t.Errorf("expected valid token for %q, got error: %v", tok, err)
		}
	}

	invalid := []string{
		"",
		"123456789",
		":ABCdef",
		"abc:123",
		"123:abc def",
		"123:abc$def",
		"123:abc\ndef",
	}
	for _, tok := range invalid {
		if err := validateTelegramToken(tok); err == nil {
			t.Errorf("expected error for invalid token %q, got nil", tok)
		}
	}
}

func TestResolveTelegramToken(t *testing.T) {
	tok1, err := resolveTelegramToken("123:flagToken", true)
	if err != nil || tok1 != "123:flagToken" {
		t.Fatalf("expected flag token, got %q, %v", tok1, err)
	}

	t.Setenv("TELEGRAM_BOT_TOKEN", "456:envToken")
	tok2, err := resolveTelegramToken("", true)
	if err != nil || tok2 != "456:envToken" {
		t.Fatalf("expected env token, got %q, %v", tok2, err)
	}

	// Flag takes precedence over env
	tok3, err := resolveTelegramToken("789:flagToken", true)
	if err != nil || tok3 != "789:flagToken" {
		t.Fatalf("expected flag token override, got %q, %v", tok3, err)
	}

	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	_, err = resolveTelegramToken("", true)
	if err == nil {
		t.Fatal("expected error when resolving empty token in non-interactive mode")
	}
}

func TestTelegramIDSuccess(t *testing.T) {
	testToken := "123456789:TestTokenSecret123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot" + testToken + "/getMe":
			resp := tgAPIResponse[tgUser]{
				OK: true,
				Result: tgUser{
					ID:        123456789,
					IsBot:     true,
					FirstName: "OpenLiaBot",
					Username:  "OpenLiaTestBot",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/bot" + testToken + "/getUpdates":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "allowed_updates") {
				t.Errorf("expected allowed_updates in request body: %s", string(body))
			}
			resp := tgAPIResponse[[]tgUpdate]{
				OK: true,
				Result: []tgUpdate{
					{
						UpdateID: 101,
						Message: &tgMessage{
							MessageID: 1,
							From: &tgUser{
								ID:        987654321,
								FirstName: "Alice",
								LastName:  "Smith",
								Username:  "alicesmith",
							},
							Chat: tgChat{
								ID:        987654321,
								Type:      "private",
								FirstName: "Alice",
								LastName:  "Smith",
								Username:  "alicesmith",
							},
							Date: 1727618000,
							Text: "/start",
						},
					},
					{
						UpdateID: 102,
						Message: &tgMessage{
							MessageID: 2,
							From: &tgUser{
								ID:        555444333,
								FirstName: "Bob",
								Username:  "bobbuilder",
							},
							Chat: tgChat{
								ID:    -1001234567890,
								Type:  "supergroup",
								Title: "OpenLia Developers",
							},
							Date: 1727618060,
							Text: "Hello team!",
						},
					},
					{
						UpdateID: 103,
						MyChatMember: &tgChatMemberUpdated{
							Chat: tgChat{
								ID:    -1009876543210,
								Type:  "supergroup",
								Title: "Operations Room",
							},
							From: tgUser{
								ID:        555444333,
								FirstName: "Bob",
							},
							Date: 1727618100,
							NewChatMember: struct {
								Status string `json:"status"`
							}{Status: "administrator"},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldBase := telegramAPIBase
	telegramAPIBase = server.URL
	t.Cleanup(func() {
		telegramAPIBase = oldBase
	})

	// Capture output
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	code := Run([]string{"telegram", "id", "--token", testToken}, nil)

	w.Close()
	os.Stdout = oldStdout

	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d", code)
	}

	outBytes, _ := io.ReadAll(r)
	out := string(outBytes)

	if !strings.Contains(out, "@OpenLiaTestBot") {
		t.Errorf("expected bot username in output: %s", out)
	}
	if !strings.Contains(out, "987654321") || !strings.Contains(out, "Alice Smith") || !strings.Contains(out, `"/start"`) {
		t.Errorf("expected Alice Smith message and user ID in output: %s", out)
	}
	if !strings.Contains(out, "-1001234567890") || !strings.Contains(out, `"OpenLia Developers"`) {
		t.Errorf("expected group ID and title in output: %s", out)
	}
	if !strings.Contains(out, "TELEGRAM_ALLOWED_USERS=") {
		t.Errorf("expected TELEGRAM_ALLOWED_USERS in output: %s", out)
	}
}

func TestTelegramIDJSONOutput(t *testing.T) {
	testToken := "123456789:TestTokenSecret123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot" + testToken + "/getMe":
			resp := tgAPIResponse[tgUser]{
				OK: true,
				Result: tgUser{
					ID:        123456789,
					IsBot:     true,
					FirstName: "OpenLiaBot",
					Username:  "OpenLiaTestBot",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/bot" + testToken + "/getUpdates":
			resp := tgAPIResponse[[]tgUpdate]{
				OK: true,
				Result: []tgUpdate{
					{
						UpdateID: 101,
						Message: &tgMessage{
							MessageID: 1,
							From: &tgUser{
								ID:        987654321,
								FirstName: "Alice",
								Username:  "alice",
							},
							Chat: tgChat{
								ID:   987654321,
								Type: "private",
							},
							Date: 1727618000,
							Text: "/start",
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldBase := telegramAPIBase
	telegramAPIBase = server.URL
	t.Cleanup(func() {
		telegramAPIBase = oldBase
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	code := Run([]string{"--json", "telegram", "lookup", "--token", testToken}, nil)

	w.Close()
	os.Stdout = oldStdout

	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d", code)
	}

	outBytes, _ := io.ReadAll(r)
	var res TelegramLookupResult
	if err := json.Unmarshal(outBytes, &res); err != nil {
		t.Fatalf("failed to decode JSON output: %v, raw: %s", err, string(outBytes))
	}

	if !res.OK || res.Bot.Username != "OpenLiaTestBot" {
		t.Fatalf("unexpected bot result: %#v", res.Bot)
	}
	if len(res.Events) != 1 || res.Events[0].Message != "/start" {
		t.Fatalf("unexpected events: %#v", res.Events)
	}
	if len(res.Users) != 1 || res.Users[0].ID != 987654321 {
		t.Fatalf("unexpected users: %#v", res.Users)
	}
	if res.ConfigSample["TELEGRAM_ALLOWED_USERS"] != "987654321" {
		t.Fatalf("unexpected config sample: %#v", res.ConfigSample)
	}
}

func TestTelegramIDEmptyUpdates(t *testing.T) {
	testToken := "123456789:TestTokenSecret123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot" + testToken + "/getMe":
			resp := tgAPIResponse[tgUser]{
				OK: true,
				Result: tgUser{
					ID:        123456789,
					IsBot:     true,
					FirstName: "OpenLiaBot",
					Username:  "OpenLiaTestBot",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/bot" + testToken + "/getUpdates":
			resp := tgAPIResponse[[]tgUpdate]{
				OK:     true,
				Result: []tgUpdate{},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldBase := telegramAPIBase
	telegramAPIBase = server.URL
	t.Cleanup(func() {
		telegramAPIBase = oldBase
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	code := Run([]string{"telegram", "id", "--token", testToken}, nil)

	w.Close()
	os.Stdout = oldStdout

	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d", code)
	}

	outBytes, _ := io.ReadAll(r)
	out := string(outBytes)

	if !strings.Contains(out, "No recent messages or events found") {
		t.Errorf("expected empty updates notice in output: %s", out)
	}
	if !strings.Contains(out, "search for @OpenLiaTestBot") || !strings.Contains(out, "/start") {
		t.Errorf("expected step-by-step guidance in output: %s", out)
	}
}

func TestTelegramIDUnauthorized(t *testing.T) {
	testToken := "123456789:BadToken"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tgAPIResponse[any]{
			OK:          false,
			ErrorCode:   401,
			Description: "Unauthorized",
		})
	}))
	defer server.Close()

	oldBase := telegramAPIBase
	telegramAPIBase = server.URL
	t.Cleanup(func() {
		telegramAPIBase = oldBase
	})

	code := Run([]string{"telegram", "id", "--token", testToken}, nil)
	if code != ExitFailure {
		t.Fatalf("expected ExitFailure for unauthorized token, got %d", code)
	}
}

func TestTelegramCommandUsageAndValidation(t *testing.T) {
	if code := Run([]string{"telegram"}, nil); code != ExitUsage {
		t.Errorf("telegram without subcommand exit code = %d, want %d", code, ExitUsage)
	}
	if code := Run([]string{"telegram", "unknown"}, nil); code != ExitUsage {
		t.Errorf("telegram unknown subcommand exit code = %d, want %d", code, ExitUsage)
	}
	if code := Run([]string{"telegram", "id", "--token", "invalid-token"}, nil); code != ExitUsage {
		t.Errorf("telegram id with invalid token exit code = %d, want %d", code, ExitUsage)
	}
	if code := Run([]string{"--non-interactive", "telegram", "id"}, nil); code != ExitUsage {
		t.Errorf("telegram id without token non-interactive exit code = %d, want %d", code, ExitUsage)
	}
}
