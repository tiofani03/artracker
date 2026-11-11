package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "123:SECRET"

func TestSendMessagePostsHTML(t *testing.T) {
	var gotPath, gotMethod string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	tg := NewTelegram(testToken, srv.URL+"/", nil)
	if err := tg.SendMessage(context.Background(), -100123, "<b>hi</b>"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/bot"+testToken+"/sendMessage" {
		t.Fatalf("got %s %s", gotMethod, gotPath)
	}
	if got["chat_id"] != float64(-100123) || got["text"] != "<b>hi</b>" ||
		got["parse_mode"] != "HTML" || got["disable_web_page_preview"] != true {
		t.Fatalf("unexpected body: %v", got)
	}
}

func TestSendMessageErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		check  func(error) bool
	}{
		{"retry_after", 429, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`,
			func(err error) bool {
				var e *RetryAfterError
				return errors.As(err, &e) && e.Seconds == 7
			}},
		{"migrated", 400, `{"ok":false,"error_code":400,"description":"group upgraded","parameters":{"migrate_to_chat_id":-100123}}`,
			func(err error) bool {
				var e *MigratedError
				return errors.As(err, &e) && e.NewChatID == -100123
			}},
		{"forbidden", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked"}`,
			func(err error) bool { return errors.Is(err, ErrChatForbidden) }},
		{"conflict", 409, `{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates"}`,
			func(err error) bool { return errors.Is(err, ErrConflict) }},
		{"api", 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`,
			func(err error) bool {
				var e *APIError
				return errors.As(err, &e) && e.Code == 400 && strings.Contains(e.Description, "can't parse entities")
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			err := NewTelegram(testToken, srv.URL, nil).SendMessage(context.Background(), 1, "x")
			if err == nil || !c.check(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetUpdatesPassesOffsetAndAllowedUpdates(t *testing.T) {
	var q map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":5,"message":{"chat":{"id":77,"type":"private","first_name":"Ann","username":"ann"},"text":"/start abc"}},
			{"update_id":6,"my_chat_member":{"chat":{"id":-9,"type":"group","title":"Ops"},"new_chat_member":{"status":"member"}}}]}`))
	}))
	defer srv.Close()

	ups, err := NewTelegram(testToken, srv.URL, nil).GetUpdates(context.Background(), 42, 30)
	if err != nil {
		t.Fatal(err)
	}
	if q["offset"][0] != "42" || q["timeout"][0] != "30" || q["allowed_updates"][0] != `["message","my_chat_member"]` {
		t.Fatalf("unexpected query: %v", q)
	}
	if len(ups) != 2 || ups[0].UpdateID != 5 || ups[0].Message.Text != "/start abc" ||
		ups[0].Message.Chat.FirstName != "Ann" || ups[0].Message.Chat.Username != "ann" ||
		ups[1].MyChatMember.Chat.Title != "Ops" || ups[1].MyChatMember.NewChatMember.Status != "member" {
		t.Fatalf("unexpected updates: %+v", ups)
	}
}

func TestGetUpdatesRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := NewTelegram(testToken, srv.URL, nil).GetUpdates(ctx, 0, 30)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("expected prompt cancel error, got %v after %v", err, time.Since(start))
	}
}

func TestGetMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot"+testToken+"/getMe" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"username":"artracker_bot"}}`))
	}))
	defer srv.Close()
	name, err := NewTelegram(testToken, srv.URL, nil).GetMe(context.Background())
	if err != nil || name != "artracker_bot" {
		t.Fatalf("got %q, %v", name, err)
	}
}

func TestTransportErrorDoesNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused
	err := NewTelegram(testToken, url, nil).SendMessage(context.Background(), 1, "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), testToken) {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestRetryAfterFallbacks(t *testing.T) {
	cases := []struct {
		name   string
		header string
		body   string
		want   int
	}{
		{"envelope", "", `{"ok":false,"error_code":429,"parameters":{"retry_after":7}}`, 7},
		{"header", "3", `{"ok":false,"error_code":429,"description":"Too Many Requests"}`, 3},
		{"html body", "", `<html>429 Too Many Requests</html>`, 5},
		{"zero retry_after", "", `{"ok":false,"error_code":429,"parameters":{"retry_after":0}}`, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.header != "" {
					w.Header().Set("Retry-After", c.header)
				}
				w.WriteHeader(429)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			err := NewTelegram(testToken, srv.URL, nil).SendMessage(context.Background(), 1, "x")
			var e *RetryAfterError
			if !errors.As(err, &e) || e.Seconds != c.want {
				t.Fatalf("want Seconds %d, got %v", c.want, err)
			}
		})
	}
}
