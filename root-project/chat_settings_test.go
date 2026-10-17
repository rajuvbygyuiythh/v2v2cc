package main

import (
	"chitchatbot/internal/browser"
	"chitchatbot/internal/model"
	"strings"
	"testing"
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
