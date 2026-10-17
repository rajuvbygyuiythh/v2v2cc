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
	if sharedShow(g2, false) != show.ThreeTimings() || g2.FirstMessage != g.FirstMessage || g2.PollMinSeconds != g.PollMinSeconds || g2.ActionMaxSeconds != g.ActionMaxSeconds {
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

func TestCanonicalRecordIgnoresLegacyBackupsAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := sharedFixture()
	if err = s.SaveChatSettings(want); err != nil {
		t.Fatal(err)
	}
	var raw model.GeneralSettings
	if ok, err := s.loadJSON(keyGeneral, &raw); err != nil || ok {
		t.Fatal("canonical save wrote legacy general", ok, err)
	}
	// Simulate stale legacy records without going through compatibility setters.
	stale := want.General
	stale.ReplyDelayMaxSeconds = 999
	if err = s.saveJSON(keyGeneral, stale); err != nil {
		t.Fatal(err)
	}
	oldShow := sharedShow(stale, true)
	if err = s.saveJSON(keyShowMode, oldShow); err != nil {
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
	got, err := s.LoadChatSettings()
	if err != nil || got.Source != "canonical_v2" || got.General != want.General || got.LegacyShow != sharedShow(want.General, true) {
		t.Fatal("stale backup won", got, err)
	}
	next := want.General
	next.PollMinSeconds = 7
	next.PollMaxSeconds = 8
	if err = s.SaveGeneralSettings(next); err != nil {
		t.Fatal(err)
	}
	_, err = s.loadJSON(keyGeneral, &raw)
	if err != nil || raw != stale {
		t.Fatal("legacy backup was rewritten", raw, err)
	}
	show, _ := s.LoadShowModeSettings()
	if show != sharedShow(next, true) {
		t.Fatal("canonical projection drifted", show)
	}
}
func TestV1MigrationKeepsPolicyAndRejectsDivergentCommonValues(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "consistent", true: "conflict"}[conflict], func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			g := sharedFixture().General
			show := sharedShow(g, true)
			if conflict {
				show.ReplyDelayMaxSeconds++
			}
			for key, value := range map[string]interface{}{keyGeneral: g, keyShowMode: show, keyChatUnified: true} {
				if err = s.saveJSON(key, value); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.LoadChatSettings()
			if err != nil {
				t.Fatal(err)
			}
			if !conflict {
				if !got.Unified || got.Source != "canonical_v2" || got.General != g {
					t.Fatal("migration lost Standard policy", got)
				}
				return
			}
			if got.Unified || got.Source != "legacy_conflict" || got.General != g || got.LegacyShow != show {
				t.Fatal("silent conflict resolution", got)
			}
			for _, mode := range []string{model.ModeStandard, model.ModeShow} {
				if _, err = s.ResolveChatSettings(mode); err == nil {
					t.Fatal("conflict reached engine", mode)
				}
			}
			if s.SaveGeneralSettings(g) == nil || s.SaveShowModeSettings(show) == nil {
				t.Fatal("legacy setter silently resolved conflict")
			}
			var canonical model.GeneralSettings
			if ok, _ := s.loadJSON(keyChatCanonical, &canonical); ok {
				t.Fatal("conflict wrote canonical")
			}
			// An explicit common Save is the only authorized conflict resolution.
			if err = s.SaveChatSettings(model.ChatSettings{General: commonFromShow(g, show)}); err != nil {
				t.Fatal(err)
			}
			got, err = s.LoadChatSettings()
			if err != nil || !got.Unified || got.Source != "canonical_v2" || got.General.FirstMessage != g.FirstMessage {
				t.Fatal(got, err)
			}
		})
	}
}
func TestResolverModeViewsAreValidatedAndDetached(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := sharedFixture()
	if err = s.SaveChatSettings(want); err != nil {
		t.Fatal(err)
	}
	std, err := s.ResolveChatSettings(model.ModeStandard)
	if err != nil {
		t.Fatal(err)
	}
	show, err := s.ResolveChatSettings(model.ModeShow)
	if err != nil {
		t.Fatal(err)
	}
	if std.Standard == nil || std.Show != nil || show.Standard != nil || show.Show == nil || !show.Show.AutoSkip || show.Show.FirstMessage.Mode != "bot" || std.Standard.FirstMessage != want.General.FirstMessage {
		t.Fatal("wrong engine projection", std, show)
	}
	std.Standard.PollMinSeconds = 333
	show.Show.NoReplyMinSeconds = 444
	again, _ := s.LoadChatSettings()
	if again.General != want.General {
		t.Fatal("mutated store via returned pointer")
	}
	for _, mode := range []string{"", "rod", "chromedp", "show_mode", model.ModeLogin} {
		if _, err = s.ResolveChatSettings(mode); err == nil {
			t.Fatal("driver/unknown mode treated as chat engine", mode)
		}
	}
	// Corrupt canonical values must not fall back to valid legacy records.
	bad := want.General
	bad.MatchReplyTimeoutMinSeconds = 0
	if err = s.saveJSON(keyChatCanonical, bad); err != nil {
		t.Fatal(err)
	}
	if _, err = s.LoadGeneralSettings(); err == nil {
		t.Fatal("invalid canonical accepted")
	}
	if _, err = s.LoadShowModeSettings(); err == nil {
		t.Fatal("invalid canonical masked by legacy Show")
	}
	if _, err = s.ResolveChatSettings(model.ModeShow); err == nil {
		t.Fatal("invalid canonical reached Show")
	}
}
