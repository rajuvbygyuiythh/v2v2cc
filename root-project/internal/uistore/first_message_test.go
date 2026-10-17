package uistore

import (
	"chitchatbot/internal/model"
	"testing"
)

func TestFirstMessageProfilesPersistIndependently(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	standard := model.DefaultGeneralSettings()
	standard.FirstMessage = model.FirstMessageSettings{Mode: "user", BotFirstPercent: 25, WaitTimeout: true, WaitMinSeconds: 10, WaitMaxSeconds: 20}
	if err := s.SaveGeneralSettings(standard); err != nil {
		t.Fatal(err)
	}
	show, err := s.LoadShowModeSettings()
	if err != nil {
		t.Fatal(err)
	}
	if show.FirstMessage.Effective().Mode != "bot" {
		t.Fatal("worker policy leaked into new Show Mode profile")
	}
	show.FirstMessage = model.FirstMessageSettings{Mode: "random", BotFirstPercent: 80, WaitMinSeconds: 30, WaitMaxSeconds: 40}
	if err := s.SaveShowModeSettings(show); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	gotStandard, err := s.LoadGeneralSettings()
	if err != nil || gotStandard.FirstMessage != standard.FirstMessage {
		t.Fatal("worker policy did not persist independently", err)
	}
	gotShow, err := s.LoadShowModeSettings()
	if err != nil || gotShow.FirstMessage != show.FirstMessage {
		t.Fatal("Show Mode policy did not persist independently", err)
	}
	standard.FirstMessage.BotFirstPercent = 101
	if err := s.SaveGeneralSettings(standard); err == nil {
		t.Fatal("invalid worker chance accepted")
	}
	show.FirstMessage.Mode = "unknown"
	if err := s.SaveShowModeSettings(show); err == nil {
		t.Fatal("invalid Show Mode policy accepted")
	}
}
