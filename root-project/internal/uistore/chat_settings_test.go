package uistore

import (
	"chitchatbot/internal/model"
	"sync"
	"testing"
)

func sharedFixture() model.ChatSettings {
	g := model.DefaultGeneralSettings()
	g.FirstMessage = model.FirstMessageSettings{Mode: "random", BotFirstPercent: 40, WaitTimeout: true, WaitMinSeconds: 5, WaitMaxSeconds: 8}
	g.ReplyDelayMinSeconds = 3
	g.ReplyDelayMaxSeconds = 6
	g.MatchReplyTimeoutMinSeconds = 20
	g.MatchReplyTimeoutMaxSeconds = 30
	g.MatchCloseWaitMinSeconds = 1
	g.MatchCloseWaitMaxSeconds = 4
	return model.ChatSettings{General: g, ShowAutoSkip: true}
}
func TestSharedSettingsAtomicRoundtripAndReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := sharedFixture()
	if err = s.SaveChatSettings(want); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g, err := s.LoadGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	show, err := s.LoadShowModeSettings()
	if err != nil {
		t.Fatal(err)
	}
	if g != want.General || show != sharedShow(want.General, true) {
		t.Fatal("engine views disagree", g, show)
	}
	got, err := s.LoadChatSettings()
	if err != nil || !got.Unified || got.General != want.General || !got.ShowAutoSkip {
		t.Fatal(got, err)
	}
}
func TestSharedMigrationReadNeverOverwritesLegacyCandidates(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g := model.DefaultGeneralSettings()
	g.ReplyDelayMinSeconds = 3
	g.ReplyDelayMaxSeconds = 6
	_ = s.SaveGeneralSettings(g)
	show, _ := s.LoadShowModeSettings()
	show.FirstMessage = model.FirstMessageSettings{Mode: "user"}
	show.ReplyDelayMinSeconds = 7
	show.ReplyDelayMaxSeconds = 9
	_ = s.SaveShowModeSettings(show)
	for i := 0; i < 3; i++ {
		got, err := s.LoadChatSettings()
		if err != nil || got.Unified || got.General != g || got.LegacyShow != show {
			t.Fatal("migration silently changed source", got, err)
		}
	}
}
func TestSharedInvalidSaveRollsBackAllViewsAndMarker(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before, err := s.LoadChatSettings()
	if err != nil {
		t.Fatal(err)
	}
	bad := sharedFixture()
	bad.General.MatchReplyTimeoutMaxSeconds = 1
	if s.SaveChatSettings(bad) == nil {
		t.Fatal("invalid accepted")
	}
	after, _ := s.LoadChatSettings()
	if before != after {
		t.Fatal("partial migration/save")
	}
	good := sharedFixture()
	_ = s.SaveChatSettings(good)
	before, _ = s.LoadChatSettings()
	bad = good
	bad.General.FirstMessage.BotFirstPercent = 101
	if s.SaveChatSettings(bad) == nil {
		t.Fatal("bad probability accepted")
	}
	after, _ = s.LoadChatSettings()
	if after != before {
		t.Fatal("invalid update changed saved state")
	}
}
func TestSharedLegacySettersCannotReintroduceDuplicateValues(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := sharedFixture()
	_ = s.SaveChatSettings(want)
	g := want.General
	g.ReplyDelayMinSeconds = 5
	g.ReplyDelayMaxSeconds = 8
	if err := s.SaveGeneralSettings(g); err != nil {
		t.Fatal(err)
	}
	show, _ := s.LoadShowModeSettings()
	if show != sharedShow(g, true) {
		t.Fatal("general save diverged", show)
	}
	show.FirstMessage = model.FirstMessageSettings{Mode: "user"}
	show.NextChatMinSeconds = 10
	show.NextChatMaxSeconds = 20
	show.AutoSkip = false
	if err := s.SaveShowModeSettings(show); err != nil {
		t.Fatal(err)
	}
	g2, _ := s.LoadGeneralSettings()
	if sharedShow(g2, false) != show || g2.PollMinSeconds != g.PollMinSeconds || g2.ActionMaxSeconds != g.ActionMaxSeconds {
		t.Fatal("show save diverged/lost standard-only values")
	}
}
func TestSharedConcurrentSavesKeepSnapshotConsistent(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.SaveChatSettings(sharedFixture())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				want := sharedFixture()
				want.General.ReplyDelayMinSeconds = i
				want.General.ReplyDelayMaxSeconds = i + 3
				if err := s.SaveChatSettings(want); err != nil {
					t.Error(err)
				}
				got, err := s.LoadChatSettings()
				if err != nil || got.LegacyShow != sharedShow(got.General, got.ShowAutoSkip) {
					t.Error("torn snapshot", err)
				}
			}
		}(i)
	}
	wg.Wait()
}
