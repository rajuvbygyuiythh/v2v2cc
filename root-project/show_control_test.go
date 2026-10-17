package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"chitchatbot/internal/bot"
	"chitchatbot/internal/browser"
	"chitchatbot/internal/model"
	"chitchatbot/internal/store"
)

func readyShow(t *testing.T, a *App, f browser.Backend, account string) *showBrowserSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := a.registerShowBrowser(ctx, f, "test", 1, 1)
	a.showLoginReady(s, model.LoginInfo{UserID: account})
	t.Cleanup(func() { a.detachShowBrowser(s) })
	return s
}
func showView(t *testing.T, a *App, id string) ShowBrowserView {
	t.Helper()
	snapshot, err := a.GetShowControlState()
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range snapshot.Browsers {
		if view.ID == id {
			return view
		}
	}
	t.Fatal("missing browser")
	return ShowBrowserView{}
}
func startShow(t *testing.T, a *App, id string) {
	t.Helper()
	snapshot, err := a.GetShowControlState()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.StartShowChat(id, snapshot.Revision); err != nil {
		t.Fatal(err)
	}
}
func TestShowReadyDoesNotAutoChatAndExplicitStopKeepsBrowser(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello\nreply", ""))
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	s := readyShow(t, a, fake, "account")
	if showView(t, a, s.view.ID).Status != "ready" || fake.totalSent() != 0 {
		t.Fatal("login auto-started")
	}
	startShow(t, a, s.view.ID)
	waitForLive(t, "explicit opener", func() bool { return fake.sentCount("hello") == 1 })
	if err := a.SaveShowModeSettings(model.ShowModeSettings{}); err == nil {
		t.Fatal("settings changed during active chat")
	}
	if err := a.StopShowChat(s.view.ID); err != nil {
		t.Fatal(err)
	}
	if showView(t, a, s.view.ID).Status != "ready" || s.ctx.Err() != nil {
		t.Fatal("stop closed browser lifecycle")
	}
	before := fake.totalSent()
	fake.addRow("partner", "after stop")
	if fake.totalSent() != before {
		t.Fatal("stopped chat sent")
	}
	if err := a.SaveShowModeSettings(model.ShowModeSettings{}); err != nil {
		t.Fatal(err)
	}
}
func TestShowSavedSettingsUserFirstDelayAndCancellation(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello\nreply", ""))
	settings := model.ShowModeSettings{FirstMessage: model.FirstMessageSettings{Mode: "user"}, ReplyDelayMinSeconds: 3, ReplyDelayMaxSeconds: 6}
	if err := a.SaveShowModeSettings(settings); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	s := readyShow(t, a, fake, "account")
	startShow(t, a, s.view.ID)
	waitForLive(t, "user-first decision", func() bool { return showView(t, a, s.view.ID).Decision == "User first" })
	view := showView(t, a, s.view.ID)
	if view.Settings == nil || view.Settings.ReplyDelayMinSeconds != 3 || fake.totalSent() != 0 {
		t.Fatal("saved settings not used or unsolicited opener")
	}
	fake.addRow("partner", "new message")
	waitForLive(t, "configured delay", func() bool { return strings.Contains(showView(t, a, s.view.ID).Detail, "Reply pending") })
	if fake.totalSent() != 0 {
		t.Fatal("reply delay bypassed")
	}
	before := time.Now()
	_ = a.StopShowChat(s.view.ID)
	if time.Since(before) > time.Second || fake.totalSent() != 0 {
		t.Fatal("stop did not cancel pending reply")
	}
}
func TestShowOldHistoryIgnoredOnStartAndRestart(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello\nreply", ""))
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	fake.addRow("partner", "old history")
	s := readyShow(t, a, fake, "account")
	startShow(t, a, s.view.ID)
	waitForLive(t, "baseline decision", func() bool { return strings.Contains(showView(t, a, s.view.ID).Decision, "history ignored") })
	if fake.totalSent() != 0 {
		t.Fatal("old history was answered")
	}
	fake.addRow("partner", "new message")
	waitForLive(t, "new message reply", func() bool { return fake.sentCount("hello") == 1 })
	_ = a.StopShowChat(s.view.ID)
	startShow(t, a, s.view.ID)
	waitForLive(t, "restart baseline", func() bool { return strings.Contains(showView(t, a, s.view.ID).Decision, "history ignored") })
	if fake.totalSent() != 1 {
		t.Fatal("restart replayed old messages")
	}
}
func TestShowRevisionAndAccountOwnership(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello", ""))
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	s := readyShow(t, a, fake, "account")
	snapshot, _ := a.GetShowControlState()
	_ = a.SaveShowModeSettings(model.ShowModeSettings{})
	if err := a.StartShowChat(s.view.ID, snapshot.Revision); err == nil {
		t.Fatal("stale revision accepted")
	}
	release, err := a.acquireAutomation("account", "standard:test")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ = a.GetShowControlState()
	if err := a.StartShowChat(s.view.ID, snapshot.Revision); err == nil {
		t.Fatal("show/standard conflict accepted")
	}
	if showView(t, a, s.view.ID).BlockedBy != "standard:test" {
		t.Fatal("conflict absent from snapshot")
	}
	release()
	startShow(t, a, s.view.ID)
	if _, err := a.acquireAutomation("account", "standard:test"); err == nil {
		t.Fatal("standard/show conflict accepted")
	}
	otherRelease, err := a.acquireAutomation("other", "standard:other")
	if err != nil {
		t.Fatal("different account blocked", err)
	}
	otherRelease()
	_ = a.StopShowChat(s.view.ID)
	release, err = a.acquireAutomation("account", "standard:test")
	if err != nil {
		t.Fatal("ownership not released", err)
	}
	release()
}

type drainingDOM struct {
	*fakeDOMBackend
	entered chan struct{}
	drain   chan struct{}
}

func (d *drainingDOM) SendDOMMessage(ctx context.Context, _ string) (bool, error) {
	close(d.entered)
	<-ctx.Done()
	<-d.drain
	return false, ctx.Err()
}
func TestShowStopDrainsBeforeReleasingAccountOrAllowingRestart(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello", ""))
	f := &drainingDOM{fakeDOMBackend: &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}, entered: make(chan struct{}), drain: make(chan struct{})}
	defer func() {
		select {
		case <-f.drain:
		default:
			close(f.drain)
		}
	}()
	s := readyShow(t, a, f, "account")
	startShow(t, a, s.view.ID)
	select {
	case <-f.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("send never entered")
	}
	stopped := make(chan struct{})
	go func() { _ = a.StopShowChat(s.view.ID); close(stopped) }()
	waitForLive(t, "stopping", func() bool { return showView(t, a, s.view.ID).Status == "stopping" })
	if _, err := a.acquireAutomation("account", "standard:test"); err == nil {
		t.Fatal("account released before draining")
	}
	snapshot, _ := a.GetShowControlState()
	if err := a.StartShowChat(s.view.ID, snapshot.Revision); err == nil {
		t.Fatal("restart while draining")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned before drain")
	default:
	}
	close(f.drain)
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not finish")
	}
}

type captureShowDOM struct {
	*fakeCaptureBrowser
	dom *fakeDOMBackend
}

func (f *captureShowDOM) ReadDOMSnapshot(c context.Context) (browser.DOMSnapshot, error) {
	return f.dom.ReadDOMSnapshot(c)
}
func (f *captureShowDOM) SendDOMMessage(c context.Context, s string) (bool, error) {
	return f.dom.SendDOMMessage(c, s)
}
func (f *captureShowDOM) ClickDOMButton(c context.Context, l []string) (string, error) {
	return f.dom.ClickDOMButton(c, l)
}
func (f *captureShowDOM) DOMProbe(c context.Context) (string, error) { return f.dom.DOMProbe(c) }
func TestShowLoginPathWaitsWithoutStartingAutomation(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello", ""))
	fake := &captureShowDOM{fakeCaptureBrowser: &fakeCaptureBrowser{login: syntheticLogin(), status: 200}, dom: &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runFreshBrowserWithBackend(ctx, filepath.Join(a.dataDir, "profiles", "test"), "test", model.BrowserSettings{Backend: "rod", LiveChat: true}, nil, 1, 1, func(string) (browser.Backend, error) { return fake, nil })
	}()
	waitForLive(t, "saved login ready", func() bool {
		state, _ := a.GetShowControlState()
		return len(state.Browsers) == 1 && state.Browsers[0].Status == "ready"
	})
	if fake.dom.totalSent() != 0 || fake.dom.clickCount("START") != 0 {
		t.Fatal("login caused automation")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("browser lifecycle did not close")
	}
	if fake.closes != 1 {
		t.Fatal("backend not closed after lifecycle cancellation")
	}
}

func TestManagedShowUsesFixedReplyDelayAndFrozenMessageMode(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("saved line\nsecond line", ""))
	settings := model.ShowModeSettings{FirstMessage: model.FirstMessageSettings{Mode: "user"}, ReplyDelayMinSeconds: 1, ReplyDelayMaxSeconds: 1}
	if err := a.SaveShowModeSettings(settings); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	s := readyShow(t, a, fake, "account")
	startShow(t, a, s.view.ID)
	waitForLive(t, "waiting user", func() bool { return showView(t, a, s.view.ID).Decision == "User first" })
	// Saving the shared Message page must not silently replace a live engine.
	_ = a.SaveMessageSettings(model.NewMessageSettings("changed line", ""))
	before := time.Now()
	fake.addRow("partner", "hi")
	waitForLive(t, "delayed saved line", func() bool { return fake.sentCount("saved line") == 1 })
	if time.Since(before) < time.Second || fake.sentCount("changed line") != 0 {
		t.Fatal("saved delay or message snapshot was not honored")
	}
}
func TestManagedFirstTimeoutAndNextWait(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello", ""))
	settings := model.ShowModeSettings{FirstMessage: model.FirstMessageSettings{Mode: "user", WaitTimeout: true, WaitMinSeconds: 1, WaitMaxSeconds: 1}, NextChatMinSeconds: 1, NextChatMaxSeconds: 1}
	if err := a.SaveShowModeSettings(settings); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	s := readyShow(t, a, fake, "account")
	before := time.Now()
	startShow(t, a, s.view.ID)
	waitForLive(t, "first-message skip", func() bool { return fake.clickCount("SKIP") == 1 })
	if time.Since(before) < time.Second || fake.totalSent() != 0 {
		t.Fatal("first timeout skipped early or sent opener")
	}
	afterSkip := time.Now()
	if fake.clickCount("START") != 0 {
		t.Fatal("next-chat wait bypassed")
	}
	waitForLive(t, "next match after wait", func() bool { return fake.clickCount("START") == 1 })
	// Polling adds overhead; observing SKIP can lag a few milliseconds.
	if time.Since(afterSkip) < 900*time.Millisecond {
		t.Fatal("next-chat wait too short")
	}
}

func TestShowDuplicateAccountAndCleanProfilesGuard(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello", ""))
	first := readyShow(t, a, &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}, "same-account")
	second := readyShow(t, a, &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}, "same-account")
	if err := a.CleanProfiles(); err == nil {
		t.Fatal("clean allowed while Show browser open")
	}
	startShow(t, a, first.view.ID)
	snapshot, _ := a.GetShowControlState()
	if err := a.StartShowChat(second.view.ID, snapshot.Revision); err == nil {
		t.Fatal("second Show browser acquired same account")
	}
	_ = a.StopShowChat(first.view.ID)
	startShow(t, a, second.view.ID)
}
func TestStandardPreflightDetectsSavedShowAccount(t *testing.T) {
	a := browserTestApp(t)
	release, err := a.acquireAutomation("synthetic-account", "show:test")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	path := filepath.Join(t.TempDir(), "login_info.json")
	if err := store.SaveJSON(path, model.LoginInfo{UserID: "synthetic-account"}); err != nil {
		t.Fatal(err)
	}
	if err := a.checkStandardConflicts([]bot.Config{{LoginInfoPath: path}}); err == nil {
		t.Fatal("saved-account preflight ignored active owner")
	}
}
