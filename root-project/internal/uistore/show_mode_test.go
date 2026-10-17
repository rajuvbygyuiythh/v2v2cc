package uistore

import (
	"testing"

	"chitchatbot/internal/model"
)

func TestShowModeMigrationAndIsolation(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := model.DefaultGeneralSettings()
	old.ReplyDelayMinSeconds, old.ReplyDelayMaxSeconds = 3, 8
	if err := s.SaveGeneralSettings(old); err != nil {
		t.Fatal(err)
	}
	show, err := s.LoadShowModeSettings()
	if err != nil {
		t.Fatal(err)
	}
	if show.ReplyDelayMinSeconds != 3 || show.ReplyDelayMaxSeconds != 8 || show.AutoSkip || show.NextChatMaxSeconds != 0 {
		t.Fatalf("bad migration: %+v", show)
	}
	show.ReplyDelayMinSeconds, show.ReplyDelayMaxSeconds = 12, 15
	if err := s.SaveShowModeSettings(show); err != nil {
		t.Fatal(err)
	}
	old.ReplyDelayMinSeconds, old.ReplyDelayMaxSeconds = 1, 2
	if err := s.SaveGeneralSettings(old); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadShowModeSettings()
	if err != nil || got != show {
		t.Fatalf("migration repeated or standard save leaked: %+v %v", got, err)
	}
	standard, err := s.LoadGeneralSettings()
	if err != nil || standard.ReplyDelayMaxSeconds != 2 {
		t.Fatalf("show save modified standard: %+v %v", standard, err)
	}
}

func TestShowModeRejectsInvalidRanges(t *testing.T) {
	for _, s := range []model.ShowModeSettings{
		{ReplyDelayMinSeconds: -1}, {ReplyDelayMinSeconds: 5, ReplyDelayMaxSeconds: 2},
		{NextChatMaxSeconds: 86401}, {AutoSkip: true}, {NoReplyMinSeconds: 20, NoReplyMaxSeconds: 10},
	} {
		if ValidateShowModeSettings(s) == nil {
			t.Fatalf("accepted invalid settings: %+v", s)
		}
	}
	if err := ValidateShowModeSettings(model.ShowModeSettings{}); err != nil {
		t.Fatal(err)
	}
}
