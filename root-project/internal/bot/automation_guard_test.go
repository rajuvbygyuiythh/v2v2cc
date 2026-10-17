package bot

import (
	"context"
	"errors"
	"testing"

	"chitchatbot/internal/model"
	"chitchatbot/internal/store"
)

func TestRunAccountGuardRejectsBeforeRealtimeOrAutomation(t *testing.T) {
	cfg := DefaultConfig(t.TempDir(), "guard-test", "rod", false)
	login := model.LoginInfo{UserID: "synthetic-account", Cookies: []model.Cookie{{Name: "token", Value: "synthetic-test-only"}}}
	if err := store.SaveJSON(profilePaths(cfg).LoginInfo, login); err != nil {
		t.Fatal(err)
	}
	busy := errors.New("synthetic account already owned by Show Mode")
	calls := 0
	cfg.AcquireAutomation = func(account string) (func(), error) {
		calls++
		if account != "synthetic-account" {
			t.Fatal(account)
		}
		return nil, busy
	}
	if err := Run(context.Background(), cfg); !errors.Is(err, busy) {
		t.Fatalf("guard bypassed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("guard called %d times", calls)
	}
}
