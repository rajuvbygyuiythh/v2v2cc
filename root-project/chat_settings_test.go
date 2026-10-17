package main

import (
	"chitchatbot/internal/browser"
	"chitchatbot/internal/model"
	"strings"
	"testing"
	"time"
)

func appSharedFixture() model.ChatSettings {
	g := model.DefaultGeneralSettings()
	g.ReplyDelayMinSeconds = 3
	g.ReplyDelayMaxSeconds = 6
	g.FirstMessage = model.DefaultGeneralSettings().FirstMessage
	return model.ChatSettings{General: g, ShowAutoSkip: true}
}
func TestSharedSettingsRejectEitherRunningModeAndApplyAfterStop(t *testing.T) {
	a := browserTestApp(t)
	want := appSharedFixture()
	a.mu.Lock()
	a.running = true
	a.mu.Unlock()
	if err := a.SaveChatSettings(want); err == nil || !strings.Contains(err.Error(), "Standard") {
		t.Fatal("saved during Standard", err)
	}
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	s := readyShow(t, a, fake, "synthetic-account")
	startShow(t, a, s.view.ID)
	if a.SaveChatSettings(want) == nil {
		t.Fatal("saved during Show")
	}
	if err := a.StopShowChat(s.view.ID); err != nil {
		t.Fatal(err)
	}
	revision, _ := a.GetShowControlState()
	if err := a.SaveChatSettings(want); err != nil {
		t.Fatal(err)
	}
	after, _ := a.GetShowControlState()
	if after.Revision <= revision.Revision || after.Settings.ReplyDelayMinSeconds != 3 {
		t.Fatal("revision/view not updated")
	}
	if err := a.StartShowChat(s.view.ID, revision.Revision); err == nil {
		t.Fatal("stale revision started")
	}
	startShow(t, a, s.view.ID)
	if err := a.StopShowChat(s.view.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := a.GetChatSettings()
	if !got.Unified || got.General != want.General {
		t.Fatal("shared values not loaded")
	}
}
func TestSharedLegacyAppSavesAlsoRespectBothModes(t *testing.T) {
	a := browserTestApp(t)
	want := appSharedFixture()
	if err := a.SaveChatSettings(want); err != nil {
		t.Fatal(err)
	}
	show, _ := a.uiStore.LoadShowModeSettings()
	a.mu.Lock()
	a.running = true
	a.mu.Unlock()
	if a.SaveGeneralSettings(want.General) == nil || a.SaveShowModeSettings(show) == nil {
		t.Fatal("legacy save bypassed running guard")
	}
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	s := readyShow(t, a, fake, "synthetic-account")
	startShow(t, a, s.view.ID)
	if a.SaveGeneralSettings(want.General) == nil || a.SaveShowModeSettings(show) == nil {
		t.Fatal("legacy save bypassed Show guard")
	}
	_ = a.StopShowChat(s.view.ID)
}

func TestSharedSettingsWaitsForCancelledWorkerOwnershipToDrain(t *testing.T) {
	a := browserTestApp(t)
	a.showMu.Lock()
	a.automationOwners = map[string]string{"synthetic": "standard:1:test"}
	a.showMu.Unlock()
	if a.SaveChatSettings(appSharedFixture()) == nil {
		t.Fatal("saved while worker still draining")
	}
	a.showMu.Lock()
	delete(a.automationOwners, "synthetic")
	a.showMu.Unlock()
	if err := a.SaveChatSettings(appSharedFixture()); err != nil {
		t.Fatal(err)
	}
}

func TestThreeTimingsSavePreservesStandardPolicyAndForcesShowSkip(t *testing.T) {
	a := browserTestApp(t)
	g := model.DefaultGeneralSettings()
	g.FirstMessage = model.FirstMessageSettings{Mode: "user"}
	if err := a.uiStore.SaveGeneralSettings(g); err != nil {
		t.Fatal(err)
	}
	request := appSharedFixture()
	request.General.FirstMessage = model.FirstMessageSettings{Mode: "random", BotFirstPercent: 0}
	request.ShowAutoSkip = false
	if err := a.SaveChatSettings(request); err != nil {
		t.Fatal(err)
	}
	saved, _ := a.uiStore.LoadGeneralSettings()
	show, _ := a.GetShowControlState()
	if saved.FirstMessage != g.FirstMessage || !show.Settings.AutoSkip || show.Settings.FirstMessage.Mode != "bot" || show.Settings.FirstMessage.WaitMinSeconds != saved.MatchReplyTimeoutMinSeconds {
		t.Fatal("removed controls still affect runtime/Standard", saved, show.Settings)
	}
}

func TestContextualSaveChangesStandardPolicyButShowCannotRewriteIt(t *testing.T) {
	a := browserTestApp(t)
	request := appSharedFixture()
	request.EditMode = model.ModeStandard
	request.General.FirstMessage = model.FirstMessageSettings{Mode: "random", BotFirstPercent: 25, WaitTimeout: true, WaitMinSeconds: 7, WaitMaxSeconds: 9}
	request.General.PollMinSeconds = 4
	request.General.PollMaxSeconds = 5
	if err := a.SaveChatSettings(request); err != nil {
		t.Fatal(err)
	}
	showEdit := request
	showEdit.EditMode = model.ModeShow
	showEdit.General.FirstMessage.Mode = "malicious"
	showEdit.General.PollMinSeconds = 0
	showEdit.General.ActionMaxSeconds = -1
	showEdit.General.ReplyDelayMaxSeconds = 8
	if err := a.SaveChatSettings(showEdit); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetChatSettings()
	if err != nil || got.General.FirstMessage != request.General.FirstMessage || got.General.PollMinSeconds != 4 || got.General.ActionMaxSeconds != request.General.ActionMaxSeconds || got.General.ReplyDelayMaxSeconds != 8 {
		t.Fatal("Show rewrote Standard-only behavior", got, err)
	}
	showEdit.EditMode = "rod"
	if a.SaveChatSettings(showEdit) == nil {
		t.Fatal("driver accepted as edit mode")
	}
	request.General.FirstMessage.BotFirstPercent = 101
	if a.SaveChatSettings(request) == nil {
		t.Fatal("invalid Standard policy accepted")
	}
}
func TestExecutionDetailsSeparateDriverAndModeAndAreDetached(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveChatSettings(appSharedFixture()); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveBrowserSettings(model.BrowserSettings{Backend: "chromedp", Headless: true, LiveChat: true}); err != nil {
		t.Fatal(err)
	}
	std, err := a.GetExecutionSettings(model.ModeStandard)
	if err != nil {
		t.Fatal(err)
	}
	show, err := a.GetExecutionSettings(model.ModeShow)
	if err != nil {
		t.Fatal(err)
	}
	login, err := a.GetExecutionSettings(model.ModeLogin)
	if err != nil {
		t.Fatal(err)
	}
	if std.Driver != "chromedp" || std.Standard == nil || std.Show != nil || show.Driver != "rod" || show.Show == nil || show.Standard != nil || login.Driver != "chromedp" || login.Standard != nil || login.Show != nil {
		t.Fatal("mode/driver conflated", std, show, login)
	}
	a.mu.Lock()
	snapshot := std
	a.standardRun = &snapshot
	a.running = true
	a.mu.Unlock()
	view, _ := a.GetExecutionSettings(model.ModeStandard)
	view.Standard.ReplyDelayMaxSeconds = 123
	again, _ := a.GetExecutionSettings(model.ModeStandard)
	if again.Standard.ReplyDelayMaxSeconds == 123 {
		t.Fatal("debug mutated frozen run")
	}
	if _, err = a.GetExecutionSettings("rod"); err == nil {
		t.Fatal("driver inferred as engine")
	}
}

func TestStandardConfigUsesResolvedCommonValuesAndRetainsDriverPolicy(t *testing.T) {
	a := browserTestApp(t)
	request := appSharedFixture()
	request.EditMode = model.ModeStandard
	request.General.FirstMessage = model.FirstMessageSettings{Mode: "user", WaitTimeout: true, WaitMinSeconds: 7, WaitMaxSeconds: 12}
	request.General.MatchCloseWaitMinSeconds = 4
	request.General.MatchCloseWaitMaxSeconds = 8
	request.General.PollMinSeconds = 2
	request.General.PollMaxSeconds = 3
	if err := a.SaveChatSettings(request); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveBrowserSettings(model.BrowserSettings{Backend: "chromedp", Headless: true, Profile: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	// Show's visible Rod preset must not change worker driver/headless preferences.
	if err := a.SaveBrowserSettings(model.BrowserSettings{Backend: "show_mode", Headless: true, Profile: "show"}); err != nil {
		t.Fatal(err)
	}
	configs, err := a.buildBotConfigs()
	if err != nil || len(configs) != 1 {
		t.Fatal("config resolution failed", len(configs), err)
	}
	cfg := configs[0]
	if cfg.Backend != "chromedp" || !cfg.Headless || cfg.FirstMessage != request.General.FirstMessage || cfg.PollMin != 2*time.Second || cfg.ReplyDelayMin != 3*time.Second || cfg.MatchCloseWaitMax != 8*time.Second {
		t.Fatal("wrong Standard config", cfg)
	}
	if err := a.SaveChatSettings(appSharedFixture()); err != nil {
		t.Fatal(err)
	}
	if cfg.FirstMessage != request.General.FirstMessage || cfg.MatchCloseWaitMax != 8*time.Second {
		t.Fatal("saved settings changed an existing worker config")
	}
}

func TestLegacyCommonSaveWithoutEditModeRetainsPreviousAPIContract(t *testing.T) {
	a := browserTestApp(t)
	initial := appSharedFixture()
	initial.EditMode = model.ModeStandard
	initial.General.FirstMessage = model.FirstMessageSettings{Mode: "user"}
	if err := a.SaveChatSettings(initial); err != nil {
		t.Fatal(err)
	}
	legacy := appSharedFixture()
	legacy.General.PollMinSeconds = 4
	legacy.General.PollMaxSeconds = 8
	legacy.General.ActionMinSeconds = 2
	legacy.General.ActionMaxSeconds = 5
	legacy.General.FirstMessage = model.FirstMessageSettings{Mode: "random", BotFirstPercent: 99}
	if err := a.SaveChatSettings(legacy); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetChatSettings()
	if err != nil || got.General.FirstMessage != initial.General.FirstMessage || got.General.PollMinSeconds != 4 || got.General.ActionMaxSeconds != 5 {
		t.Fatal("legacy common API changed contract", got, err)
	}
}
