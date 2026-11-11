package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tiofani03/artracker/backend/internal/alerts"
	"github.com/tiofani03/artracker/backend/internal/config"
)

const (
	testMasterKey = "0123456789abcdef0123456789abcdef" // 32 bytes
	testToken     = "987654321:AAFakeTokenForTestsOnly_abcdefghijk-XYZ"
	badFormatTok  = "not-a-token"
)

type wiringEnv struct {
	buf *bytes.Buffer
	log *slog.Logger
	wg  *sync.WaitGroup
}

func newWiringEnv() wiringEnv {
	buf := &bytes.Buffer{}
	return wiringEnv{buf: buf, log: slog.New(slog.NewTextHandler(buf, nil)), wg: &sync.WaitGroup{}}
}

func (e wiringEnv) build(t *testing.T, ctx context.Context, cfg config.Config) (*alerts.Service, alerts.UpdateSource, error) {
	t.Helper()
	return buildAlerts(ctx, ctx, e.wg, cfg, nil, e.log)
}

func TestBuildAlertsTenantWithKey(t *testing.T) {
	e := newWiringEnv()
	svc, src, err := e.build(t, context.Background(), config.Config{AlertsBotMode: "tenant", AlertsMasterKey: []byte(testMasterKey), AlertsMasterKeyID: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if src != nil {
		t.Fatal("src must be a nil interface in tenant mode")
	}
	if svc.Mode() != alerts.ModeTenant || svc.TelegramConfigured() {
		t.Fatalf("mode=%s configured=%v", svc.Mode(), svc.TelegramConfigured())
	}
	if _, err := svc.SetBot(context.Background(), 1, 1, badFormatTok, ""); !errors.Is(err, alerts.ErrInvalidToken) {
		t.Fatalf("SetBot err = %v, want ErrInvalidToken", err)
	}
	if strings.Contains(e.buf.String(), "master key not configured") {
		t.Fatalf("unexpected log: %s", e.buf.String())
	}
}

func TestBuildAlertsTenantWithoutKey(t *testing.T) {
	e := newWiringEnv()
	svc, src, err := e.build(t, context.Background(), config.Config{AlertsBotMode: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if src != nil {
		t.Fatal("src must be nil")
	}
	if _, err := svc.SetBot(context.Background(), 1, 1, badFormatTok, ""); !errors.Is(err, alerts.ErrNoSealer) {
		t.Fatalf("SetBot err = %v, want ErrNoSealer", err)
	}
	if n := strings.Count(e.buf.String(), "alerts master key not configured: organisation bot API disabled"); n != 1 {
		t.Fatalf("info line count = %d, log: %s", n, e.buf.String())
	}
	if !strings.Contains(e.buf.String(), "level=INFO") {
		t.Fatalf("not INFO: %s", e.buf.String())
	}
}

func TestBuildAlertsTenantIgnoresTelegramToken(t *testing.T) {
	e := newWiringEnv()
	svc, src, err := e.build(t, context.Background(), config.Config{AlertsBotMode: "tenant", TelegramBotToken: testToken, AlertsMasterKey: []byte(testMasterKey), AlertsMasterKeyID: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if src != nil || svc.TelegramConfigured() {
		t.Fatal("tenant mode must not build a global client")
	}
	e.wg.Wait()
}

func TestBuildAlertsSharedWithoutToken(t *testing.T) {
	e := newWiringEnv()
	svc, src, err := e.build(t, context.Background(), config.Config{AlertsBotMode: "shared", AlertsMasterKey: []byte(testMasterKey), AlertsMasterKeyID: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if src != nil || svc.Mode() != alerts.ModeShared || svc.TelegramConfigured() {
		t.Fatalf("src=%v mode=%s", src, svc.Mode())
	}
	if _, err := svc.SetBot(context.Background(), 1, 1, badFormatTok, ""); !errors.Is(err, alerts.ErrWrongMode) {
		t.Fatalf("shared mode must ignore the master key, SetBot err = %v", err)
	}
}

func TestBuildAlertsSharedWithTokenBuildsClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"username":"TestBot"}}`))
	}))
	defer srv.Close()
	e := newWiringEnv()
	svc, src, err := e.build(t, context.Background(), config.Config{AlertsBotMode: "shared", TelegramBotToken: testToken, TelegramAPIBase: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if src == nil || svc.BotUsername() != "TestBot" || !svc.TelegramConfigured() {
		t.Fatalf("src=%v name=%q", src, svc.BotUsername())
	}
	e.wg.Wait()
}

func TestBuildAlertsSharedGetMeFailureStillSetsNotifier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	e := newWiringEnv()
	ctx, cancel := context.WithCancel(context.Background())
	svc, src, err := e.build(t, ctx, config.Config{AlertsBotMode: "shared", TelegramBotToken: testToken, TelegramAPIBase: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if src == nil {
		t.Fatal("src must be set when a token exists")
	}
	if svc.BotUsername() != "" {
		t.Fatalf("username = %q", svc.BotUsername())
	}
	if !strings.Contains(e.buf.String(), "telegram getMe failed") {
		t.Fatalf("missing warn: %s", e.buf.String())
	}
	cancel()
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("retry goroutine did not stop after cancel")
	}
}

func TestBuildAlertsNeverLogsSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	key := "SECRETKEYSECRETKEYSECRETKEY!!!!!"
	for _, cfg := range []config.Config{
		{AlertsBotMode: "tenant", AlertsMasterKey: []byte(key), AlertsMasterKeyID: "v1", TelegramBotToken: testToken},
		{AlertsBotMode: "tenant", TelegramBotToken: testToken},
		{AlertsBotMode: "shared", AlertsMasterKey: []byte(key), AlertsMasterKeyID: "v1", TelegramBotToken: testToken, TelegramAPIBase: srv.URL},
	} {
		e := newWiringEnv()
		ctx, cancel := context.WithCancel(context.Background())
		if _, _, err := e.build(t, ctx, cfg); err != nil {
			t.Fatal(err)
		}
		cancel()
		e.wg.Wait()
		out := e.buf.String()
		if strings.Contains(out, testToken) || strings.Contains(out, "AAFakeToken") || strings.Contains(out, key) {
			t.Fatalf("secret leaked in log: %s", out)
		}
	}
}

func TestBuildAlertsBadKeyIsError(t *testing.T) {
	e := newWiringEnv()
	_, _, err := e.build(t, context.Background(), config.Config{AlertsBotMode: "tenant", AlertsMasterKey: []byte(testMasterKey[:31]), AlertsMasterKeyID: "v1"})
	if err == nil {
		t.Fatal("expected an error for a 31-byte key")
	}
}
