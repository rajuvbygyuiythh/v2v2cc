package bot

import (
	"chitchatbot/internal/api"
	"chitchatbot/internal/model"
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFirstTurnDecisionSurvivesReload(t *testing.T) {
	a := model.AccountState{}
	cfg := Config{FirstMessage: model.FirstMessageSettings{Mode: "random", BotFirstPercent: 0, WaitTimeout: true, WaitMinSeconds: 20, WaitMaxSeconds: 20}}
	first := workerFirstTurn(cfg, &a, "one", rand.New(rand.NewSource(1)))
	if first.BotFirst || first.Deadline.IsZero() {
		t.Fatal(first)
	}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded model.AccountState
	if err := json.Unmarshal(b, &reloaded); err != nil {
		t.Fatal(err)
	}
	cfg.FirstMessage.BotFirstPercent = 100
	again := workerFirstTurn(cfg, &reloaded, "one", rand.New(rand.NewSource(5)))
	if again.BotFirst || !again.Deadline.Equal(first.Deadline) {
		t.Fatal("rerolled conversation")
	}
	if !workerFirstTurn(cfg, &reloaded, "two", rand.New(rand.NewSource(1))).BotFirst {
		t.Fatal("new policy not used for new conversation")
	}
}
func firstInputFixture(t *testing.T, response func() string) (*api.Client, Config, model.DMTarget) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response()))
	}))
	t.Cleanup(srv.Close)
	return api.New(model.LoginInfo{Cookies: []model.Cookie{{Name: "token", Value: "synthetic-local-test-only"}}}, srv.URL, nil), Config{APIBaseURL: srv.URL, MessageFetchLimit: 20}, model.DMTarget{ConversationID: "conv", UserID: "partner"}
}
func TestFirstMatchIncomingOverridesBotOpener(t *testing.T) {
	c, cfg, target := firstInputFixture(t, func() string { return `[{"id":"m","content":"hi","author":{"id":"partner"}}]` })
	m, closed, err := firstMatchInput(context.Background(), c, cfg, target, "me", model.AccountState{}, model.FirstTurnState{BotFirst: true}, nil)
	if err != nil || closed || m == nil || m.ID != "m" {
		t.Fatalf("%+v %v %v", m, closed, err)
	}
}
func TestFirstMatchWaitEventsTimeoutAndCancellation(t *testing.T) {
	c, cfg, target := firstInputFixture(t, func() string { return `[]` })
	events := make(chan realtimeEvent, 2)
	events <- realtimeEvent{Type: "chatMessage", ConversationID: "other", UserID: "partner", Content: "ignore"}
	events <- realtimeEvent{Type: "chatMessage", ConversationID: "conv", UserID: "partner", Content: "hi", MessageID: "m"}
	m, closed, err := firstMatchInput(context.Background(), c, cfg, target, "me", model.AccountState{}, model.FirstTurnState{}, events)
	if err != nil || closed || m == nil || m.ID != "m" {
		t.Fatal("incoming lost", err)
	}
	m, closed, err = firstMatchInput(context.Background(), c, cfg, target, "me", model.AccountState{}, model.FirstTurnState{Deadline: time.Now().Add(-time.Second)}, nil)
	if err != nil || !closed || m != nil {
		t.Fatal("expired wait did not skip")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err = firstMatchInput(ctx, c, cfg, target, "me", model.AccountState{}, model.FirstTurnState{}, nil); err == nil {
		t.Fatal("unlimited wait ignored cancellation")
	}
}
func TestFirstMatchLateIncomingWinsTimeout(t *testing.T) {
	calls := 0
	c, cfg, target := firstInputFixture(t, func() string {
		calls++
		if calls == 1 {
			return `[]`
		}
		return `[{"id":"late","content":"hi","author":{"id":"partner"}}]`
	})
	m, closed, err := firstMatchInput(context.Background(), c, cfg, target, "me", model.AccountState{}, model.FirstTurnState{Deadline: time.Now().Add(-time.Second)}, nil)
	if err != nil || closed || m == nil || m.ID != "late" {
		t.Fatal("late incoming lost", err)
	}
}
func TestFirstMatchClosedStopsWait(t *testing.T) {
	c, cfg, target := firstInputFixture(t, func() string { return `[]` })
	for _, closeChannel := range []bool{true, false} {
		events := make(chan realtimeEvent, 1)
		if closeChannel {
			close(events)
		} else {
			events <- realtimeEvent{ConversationID: "conv", Closed: true}
		}
		if _, closed, err := firstMatchInput(context.Background(), c, cfg, target, "me", model.AccountState{}, model.FirstTurnState{}, events); err != nil || !closed {
			t.Fatal("closed did not stop", err)
		}
	}
}

func TestWorkerFirstIncomingUsesLineOneAndDedupesRealtimeEcho(t *testing.T) {
	for _, folder := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "folder"}[folder], func(t *testing.T) {
			dir := t.TempDir()
			posted := make(chan string, 8)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					w.Write([]byte(`[{"id":"first","content":"hello","author":{"id":"partner"}}]`))
					return
				}
				if strings.HasSuffix(r.URL.Path, "/messages") {
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
					}
					posted <- r.FormValue("content")
				}
				w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			login := model.LoginInfo{Cookies: []model.Cookie{{Name: "token", Value: "synthetic-local-test-only"}}}
			client := api.New(login, srv.URL, nil)
			cfg := DefaultConfig(dir, "test", "rod", false)
			cfg.APIBaseURL = srv.URL
			cfg.MessageSequence = []string{"A1", "A2"}
			cfg.AvailableSequences = [][]string{{"A1", "A2"}}
			if folder {
				cfg.ChatMode = model.ChatModeFolder
				cfg.AvailableSequences = [][]string{{"A1", "A2"}, {"B1", "B2"}}
			}
			cfg.FirstMessage = model.FirstMessageSettings{Mode: "user"}
			cfg.ReplyDelayMin, cfg.ReplyDelayMax = 15*time.Millisecond, 15*time.Millisecond
			cfg.MatchReplyTimeoutMin, cfg.MatchReplyTimeoutMax = time.Hour, time.Hour
			events := make(chan realtimeEvent, 1)
			events <- realtimeEvent{Type: "chatMessage", ConversationID: "conv", UserID: "partner", Content: "hello", MessageID: "first"}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			before := time.Now()
			_, err := executeMatchChatSequence(ctx, client, cfg, profilePaths(cfg), login, "me", model.DMTarget{ConversationID: "conv", UserID: "partner"}, rand.New(rand.NewSource(1)), events)
			if err != context.DeadlineExceeded {
				t.Fatalf("unexpected end: %v", err)
			}
			select {
			case text := <-posted:
				if text != "A1" && text != "B1" {
					t.Fatal("skipped first sequence line", text)
				}
			default:
				t.Fatal("no first reply")
			}
			if len(posted) != 0 {
				t.Fatal("duplicate realtime event generated another reply")
			}
			if time.Since(before) < 15*time.Millisecond {
				t.Fatal("first reply delay bypassed")
			}
		})
	}
}

func TestWorkerDMUserFirstSuppressesOpenerThenReplies(t *testing.T) {
	for _, realtime := range []bool{false, true} {
		t.Run(map[bool]string{false: "poll", true: "realtime"}[realtime], func(t *testing.T) {
			var incoming atomic.Bool
			posted := make(chan string, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					if incoming.Load() {
						w.Write([]byte(`[{"id":"first","content":"hello","author":{"id":"partner"}}]`))
					} else {
						w.Write([]byte(`[]`))
					}
					return
				}
				if strings.HasSuffix(r.URL.Path, "/messages") {
					_ = r.ParseMultipartForm(1 << 20)
					posted <- r.FormValue("content")
				}
				w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			cfg := DefaultConfig(t.TempDir(), "test", "rod", false)
			cfg.APIBaseURL = srv.URL
			cfg.MessageSequence = []string{"line one", "line two"}
			cfg.ActionMin, cfg.ActionMax = 0, 0
			cfg.FirstMessage = model.FirstMessageSettings{Mode: "user"}
			login := model.LoginInfo{Cookies: []model.Cookie{{Name: "token", Value: "synthetic-local-test-only"}}}
			client := api.New(login, srv.URL, nil)
			target := model.DMTarget{ConversationID: "conv", UserID: "partner"}
			ctx := context.Background()
			rng := rand.New(rand.NewSource(1))
			run := func(message *model.Message) (bool, error) {
				if realtime {
					return sendFirstMessageIfNeeded(ctx, client, cfg, profilePaths(cfg), login, "me", target, message, rng)
				}
				return processDMAutoResponses(ctx, client, cfg, profilePaths(cfg), login, "me", []model.DMTarget{target}, rng)
			}
			if sent, err := run(nil); err != nil || sent || len(posted) != 0 {
				t.Fatal("idle user-first DM opened", err)
			}
			incoming.Store(true)
			message := &model.Message{ID: "first", Content: "hello"}
			message.Author.ID = "partner"
			if sent, err := run(message); err != nil || !sent {
				t.Fatal("first incoming was not answered", err)
			}
			if text := <-posted; text != "line one" {
				t.Fatal("first DM line skipped", text)
			}
		})
	}
}

func TestWorkerUserFirstEVAKeepsUnderageGuard(t *testing.T) {
	posted := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			w.Write([]byte(`[{"id":"first","content":"m16 whats your snap","author":{"id":"partner"}}]`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") {
			_ = r.ParseMultipartForm(1 << 20)
			posted <- r.FormValue("content")
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := DefaultConfig(t.TempDir(), "test", "rod", false)
	cfg.APIBaseURL = srv.URL
	cfg.ChatMode = model.ChatModeEVAChat
	cfg.EVAChat = &EVAChatConfig{Engine: testEVAEngine(t)}
	cfg.MessageUsernames = []string{"synthetic_handle_only"}
	cfg.FirstMessage = model.FirstMessageSettings{Mode: "user"}
	cfg.MatchReplyTimeoutMin, cfg.MatchReplyTimeoutMax = time.Hour, time.Hour
	login := model.LoginInfo{Cookies: []model.Cookie{{Name: "token", Value: "synthetic-local-test-only"}}}
	client := api.New(login, srv.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, _ = executeMatchChatSequence(ctx, client, cfg, profilePaths(cfg), login, "me", model.DMTarget{ConversationID: "conv", UserID: "partner"}, rand.New(rand.NewSource(1)), nil)
	_, _, account, err := loadBotAccountState(profilePaths(cfg).BotState, "me")
	if err != nil {
		t.Fatal(err)
	}
	session := account.EVAChatSessions["partner"]
	if !session.Started || !session.Blocked || session.UserAge != 16 || account.SnapTgSentCount != 0 {
		t.Fatalf("first input lost or guard bypassed: %+v", session)
	}
	for len(posted) > 0 {
		if strings.Contains(<-posted, "synthetic_handle_only") {
			t.Fatal("underage username share")
		}
	}
}
