package main

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"chitchatbot/internal/browser"
	"chitchatbot/internal/model"
)

// fakeDOMBackend implements browser.Backend + browser.DOMChatBackend with a
// scripted page. No browser is launched: rows and controls are faked, typing
// is recorded, and sends echo back as own rows just like the real page.
type fakeDOMBackend struct {
	mu          sync.Mutex
	controls    browser.DOMControls
	rows        []browser.DOMMessage
	sent        []string
	clicks      []string
	serial      int
	generation  int
	lastPhase   string
	lastPartner string
	failSend    bool
	sendHook    func()
}

func (f *fakeDOMBackend) Name() string { return "rod" }
func (f *fakeDOMBackend) Start(context.Context, browser.Options) error {
	return nil
}
func (f *fakeDOMBackend) CaptureSession(context.Context) (model.LoginInfo, error) {
	return model.LoginInfo{}, nil
}
func (f *fakeDOMBackend) Fetch(context.Context, browser.FetchRequest) (browser.FetchResponse, error) {
	return browser.FetchResponse{}, nil
}
func (f *fakeDOMBackend) Close() error { return nil }

func (f *fakeDOMBackend) ReadDOMSnapshot(context.Context) (browser.DOMSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.controls
	if c.Phase == "" {
		if c.HasInput && !c.ChatEnded {
			c.Phase = "chatting"
		} else {
			c.Phase = "lobby"
		}
	}
	if c.Partner == "" {
		c.Partner = "partner"
	}
	if c.OwnName == "" {
		c.OwnName = "me"
	}
	if c.Phase != f.lastPhase || c.Partner != f.lastPartner {
		f.generation++
		f.lastPhase = c.Phase
		f.lastPartner = c.Partner
	}
	if c.Conversation == "" {
		c.Conversation = fmt.Sprintf("fake-%d", f.generation)
	}
	c.CanSend = c.Phase == "chatting"
	c.ObserverReady = true
	return browser.DOMSnapshot{Controls: c, Messages: append([]browser.DOMMessage(nil), f.rows...)}, nil
}

func (f *fakeDOMBackend) SendDOMMessage(_ context.Context, text string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	if f.sendHook != nil {
		f.sendHook()
	}
	if f.failSend {
		return false, nil
	}
	f.serial++
	f.rows = append(f.rows, browser.DOMMessage{ID: fmt.Sprintf("own-%d", f.serial), Author: "me", Text: text})
	return true, nil
}

func (f *fakeDOMBackend) DOMProbe(context.Context) (string, error) {
	return "fake probe", nil
}

func (f *fakeDOMBackend) ClickDOMButton(_ context.Context, labels []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, label := range labels {
		for _, b := range f.controls.Buttons {
			if !strings.EqualFold(b, label) {
				continue
			}
			f.clicks = append(f.clicks, label)
			switch strings.ToUpper(label) {
			case "START":
				f.controls.HasInput = true
				f.controls.ChatEnded = false
				f.controls.Buttons = []string{"Skip"}
				f.rows = nil
			case "SKIP":
				f.controls.HasInput = false
				f.controls.Buttons = []string{"START"}
				f.rows = nil
			}
			return label, nil
		}
	}
	return "", nil
}

func (f *fakeDOMBackend) addRow(author, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serial++
	f.rows = append(f.rows, browser.DOMMessage{ID: fmt.Sprintf("row-%d", f.serial), Author: author, Text: text})
}

func (f *fakeDOMBackend) sentCount(text string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.sent {
		if s == text {
			n++
		}
	}
	return n
}

func (f *fakeDOMBackend) totalSent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeDOMBackend) clickCount(label string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.clicks {
		if strings.EqualFold(c, label) {
			n++
		}
	}
	return n
}

func waitForLive(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func testLiveRunner(a *App, fake *fakeDOMBackend) *liveRunner {
	return &liveRunner{
		dom:           fake,
		prefix:        "[test show-live]",
		logf:          func(string, string) {},
		replyDelayMin: time.Millisecond,
		replyDelayMax: time.Millisecond,
		replyDelayRNG: rand.New(rand.NewSource(1)),
		pollInterval:  20 * time.Millisecond,
		newResponder:  a.newLiveResponder,
	}
}

func TestShowModeReplyDelayUsesIndependentSettings(t *testing.T) {
	a := browserTestApp(t)
	settings := model.ShowModeSettings{}
	settings.ReplyDelayMinSeconds = 2
	settings.ReplyDelayMaxSeconds = 5
	if err := a.uiStore.SaveShowModeSettings(settings); err != nil {
		t.Fatal(err)
	}
	savedSettings, err := a.uiStore.LoadShowModeSettings()
	if err != nil {
		t.Fatal(err)
	}
	minDelay, maxDelay := showModeReplyDelayRange(savedSettings)
	if minDelay != 2*time.Second || maxDelay != 5*time.Second {
		t.Fatalf("reply delay range=%s-%s, want 2s-5s", minDelay, maxDelay)
	}

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 100; i++ {
		delay := chooseShowModeReplyDelay(rng, minDelay, maxDelay)
		if delay < minDelay || delay >= maxDelay {
			t.Fatalf("selected delay %s outside configured range [%s, %s)", delay, minDelay, maxDelay)
		}
	}

	fixedMin, fixedMax := showModeReplyDelayRange(model.ShowModeSettings{ReplyDelayMinSeconds: 4, ReplyDelayMaxSeconds: 4})
	if got := chooseShowModeReplyDelay(rng, fixedMin, fixedMax); got != 4*time.Second {
		t.Fatalf("fixed delay=%s, want 4s", got)
	}

	normalizedMin, normalizedMax := showModeReplyDelayRange(model.ShowModeSettings{ReplyDelayMinSeconds: -2, ReplyDelayMaxSeconds: -1})
	if normalizedMin != 0 || normalizedMax != 0 {
		t.Fatalf("negative delays were not normalized: %s-%s", normalizedMin, normalizedMax)
	}
}

func TestLiveRunnerWaitsForConfiguredReplyDelay(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("hello first\nreply line", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	delay := 80 * time.Millisecond
	r.replyDelayMin = delay
	r.replyDelayMax = delay

	ctx := context.Background()
	r.step(ctx) // Initial opener is sent without the incoming-reply delay.
	fake.addRow("partner", "hi")
	started := time.Now()
	r.step(ctx)
	elapsed := time.Since(started)
	if elapsed < delay-10*time.Millisecond {
		t.Fatalf("reply was sent after %s; configured delay is %s", elapsed, delay)
	}
	if fake.sentCount("reply line") != 1 {
		t.Fatal("reply was not sent after the configured delay")
	}
}

func TestLiveChatDOMFlow(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("hello first\nreply line", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: false, Buttons: []string{"START"}}}
	r := testLiveRunner(a, fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.run(ctx)

	// Lobby: START is clicked automatically, greeting goes out.
	waitForLive(t, "greeting after START", func() bool { return fake.sentCount("hello first") == 1 })

	// Partner message is answered with the next sequence line.
	fake.addRow("partner", "hi")
	waitForLive(t, "reply", func() bool { return fake.sentCount("reply line") == 1 })

	// Our own echoed rows never trigger extra replies.
	time.Sleep(200 * time.Millisecond)
	if n := fake.totalSent(); n != 2 {
		t.Fatalf("own echo caused extra sends (total %d)", n)
	}

	// Sequence exhausted -> Skip is clicked, then a fresh START + greeting.
	fake.addRow("partner", "more")
	waitForLive(t, "skip click", func() bool { return fake.clickCount("Skip") >= 1 })
	waitForLive(t, "second greeting", func() bool { return fake.sentCount("hello first") >= 2 })

	// Partner leaves: bot returns to lobby and matches again.
	fake.mu.Lock()
	fake.controls.ChatEnded = true
	fake.controls.HasInput = false
	fake.controls.Buttons = []string{"START"}
	fake.rows = nil
	fake.mu.Unlock()
	waitForLive(t, "re-match after partner left", func() bool { return fake.clickCount("START") >= 3 })
}

func TestLiveFolderResponderPicksOneSequencePerConversation(t *testing.T) {
	a := browserTestApp(t)
	settings := model.NewMessageSettings("", "")
	settings.ChatMode = model.ChatModeFolder
	settings.LoadedSequences = []string{"greet one\nreply one", "greet two\nreply two"}
	settings.UseLoadedSequences = true
	if err := a.SaveMessageSettings(settings); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		responder, mode, err := a.newLiveResponder()
		if err != nil {
			t.Fatal(err)
		}
		if mode != model.ChatModeFolder {
			t.Fatalf("mode = %q", mode)
		}
		start := responder.Start()
		reply, end := responder.Reply("hi")
		if len(start) != 1 || len(reply) != 1 || end {
			t.Fatalf("bad flow: start=%v reply=%v end=%v", start, reply, end)
		}
		if (start[0] == "greet one") != (reply[0] == "reply one") {
			t.Fatalf("mixed sequences: %q then %q", start[0], reply[0])
		}
	}
}

func TestLiveChatEVAResponderStarts(t *testing.T) {
	a := browserTestApp(t)
	settings := model.NewMessageSettings("", "")
	settings.ChatMode = model.ChatModeEVAChat
	if err := a.SaveMessageSettings(settings); err != nil {
		t.Fatal(err)
	}
	responder, mode, err := a.newLiveResponder()
	if err != nil {
		t.Fatalf("EVA responder: %v", err)
	}
	if mode != "EVAChat Flow" {
		t.Fatalf("unexpected mode %q", mode)
	}
	if lines := responder.Start(); len(lines) == 0 {
		t.Fatal("EVA Start should produce a greeting line from the starter pack")
	}
}

func TestShowModeRepeatedTextOwnEchoAndUnknownAuthor(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("opener\nreply1\nreply2\nreply3", "")); err != nil {
		t.Fatal(err)
	}
	f := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, f)
	ctx := context.Background()
	r.step(ctx)
	f.addRow("partner", "opener") // identical to our greeting, but NOT our echo
	f.addRow("partner", "opener") // identical text, different stable IDs
	f.addRow("", "unclassified")
	r.step(ctx)
	r.step(ctx)
	r.step(ctx)
	if f.sentCount("reply1") != 1 || f.sentCount("reply2") != 1 || f.totalSent() != 3 {
		t.Fatalf("bad queue/echo handling: %v", f.sent)
	}
	// A late author resolution must not lose that unread message.
	f.mu.Lock()
	for i := range f.rows {
		if f.rows[i].Author == "" {
			f.rows[i].Author = "partner"
		}
	}
	f.mu.Unlock()
	r.step(ctx)
	if f.sentCount("reply3") != 1 {
		t.Fatal("late identity message was lost")
	}
}

func TestShowModeUnconfirmedSendPausesWithoutResendOrSkip(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("opener\nreply", ""))
	f := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, f)
	ctx := context.Background()
	r.step(ctx)
	f.failSend = true
	f.addRow("partner", "hi")
	r.step(ctx)
	for i := 0; i < 4; i++ {
		r.step(ctx)
	}
	if !r.paused || f.sentCount("reply") != 1 || f.clickCount("SKIP") != 0 {
		t.Fatal("uncertain send was retried/skipped")
	}
}

func TestShowModePartnerChangeDuringDelayDiscardsOldQueue(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("opener\nreply", ""))
	f := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, f)
	r.replyDelayMin = 40 * time.Millisecond
	r.replyDelayMax = 40 * time.Millisecond
	ctx := context.Background()
	r.step(ctx)
	f.addRow("partner", "hi")
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(5 * time.Millisecond)
		f.mu.Lock()
		f.controls.Partner = "new partner"
		f.rows = nil
		f.mu.Unlock()
	}()
	r.step(ctx)
	<-done
	if f.sentCount("reply") != 0 {
		t.Fatal("old reply leaked to new partner")
	}
	r.step(ctx)
	if f.sentCount("opener") != 2 {
		t.Fatal("new conversation did not open")
	}
}

func TestShowModeUnchangedPageAfterSkipDoesNotRegreet(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("opener", ""))
	f := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"NO ACTION"}}}
	r := testLiveRunner(a, f)
	ctx := context.Background()
	r.step(ctx)
	f.addRow("partner", "next")
	r.step(ctx)
	for i := 0; i < 4; i++ {
		r.step(ctx)
	}
	if f.sentCount("opener") != 1 || !r.rotating {
		t.Fatal("old page was greeted again after failed skip")
	}
}

func TestShowModeResetDuringFinalSendNeverSkipsNewConversation(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("opener\nreply", ""))
	f := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, f)
	ctx := context.Background()
	r.step(ctx)
	// Custom responder ends with a two-line reply; simulate a rematch after line 1.
	r.responder = endingLiveTestResponder{}
	f.sendHook = func() { f.controls.Partner = "new partner"; f.rows = nil }
	f.addRow("partner", "go")
	r.step(ctx)
	if r.rotating || f.clickCount("SKIP") != 0 || f.sentCount("second") != 0 {
		t.Fatal("stale reply/skip crossed conversation boundary")
	}
}

type endingLiveTestResponder struct{}

func (endingLiveTestResponder) Start() []string { return nil }
func (endingLiveTestResponder) Reply(string) ([]string, bool) {
	return []string{"first", "second"}, true
}

func TestShowModeLiveUsernameRenderingAndNoUsernameGate(t *testing.T) {
	a := browserTestApp(t)
	settings := model.NewMessageSettings("hello\nadd %username%", "test-handle")
	_ = a.SaveMessageSettings(settings)
	responder, _, err := a.newLiveResponder()
	if err != nil {
		t.Fatal(err)
	}
	responder.Start()
	lines, _ := responder.Reply("hi")
	if len(lines) != 1 || lines[0] != "add test-handle" {
		t.Fatalf("unrendered handle: %v", lines)
	}
	settings.Usernames = ""
	_ = a.SaveMessageSettings(settings)
	if _, _, err := a.newLiveResponder(); err == nil {
		t.Fatal("sequence without required username accepted")
	}
	settings.ChatMode = model.ChatModeEVAChat
	_ = a.SaveMessageSettings(settings)
	responder, _, err = a.newLiveResponder()
	if err != nil {
		t.Fatal(err)
	}
	responder.Start()
	lines, end := responder.Reply("whats ur snap")
	if !end || strings.Contains(strings.Join(lines, " "), "%username%") {
		t.Fatal("EVA bypassed no-username gate")
	}
}

func TestShowModeAutoSkipAndNextChatWait(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("hello\nreply", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	r.autoSkip = true
	r.noReplyMin, r.noReplyMax = time.Hour, time.Hour
	r.nextChatMin, r.nextChatMax = time.Hour, time.Hour
	ctx := context.Background()
	r.step(ctx)
	if r.replyDeadline.IsZero() {
		t.Fatal("confirmed opener did not arm timeout")
	}
	r.replyDeadline = time.Now().Add(-time.Second)
	r.step(ctx)
	if fake.clickCount("SKIP") != 1 {
		t.Fatal("expired timeout did not skip")
	}
	r.step(ctx)
	if fake.clickCount("START") != 0 {
		t.Fatal("next-chat wait was bypassed")
	}
	r.nextChatAt = time.Now().Add(-time.Second)
	r.step(ctx)
	if fake.clickCount("START") != 1 {
		t.Fatal("next chat did not start after wait")
	}
}

func TestShowModeIncomingWinsExpiredTimeout(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("hello\nreply", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	r.autoSkip = true
	r.noReplyMin, r.noReplyMax = time.Hour, time.Hour
	ctx := context.Background()
	r.step(ctx)
	r.replyDeadline = time.Now().Add(-time.Second)
	fake.addRow("partner", "hi")
	r.step(ctx)
	if fake.clickCount("SKIP") != 0 || fake.sentCount("reply") != 1 {
		t.Fatal("incoming was skipped instead of answered")
	}
}

func TestShowModeDisabledTimeoutAndUnconfirmedSend(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("hello\nreply", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	ctx := context.Background()
	r.step(ctx)
	r.replyDeadline = time.Now().Add(-time.Second)
	r.step(ctx)
	if fake.clickCount("SKIP") != 0 {
		t.Fatal("disabled auto skip fired")
	}
	fake.failSend = true
	r.autoSkip = true
	fake.addRow("partner", "hi")
	r.step(ctx)
	r.step(ctx)
	if !r.paused || fake.clickCount("SKIP") != 0 {
		t.Fatal("uncertain send did not retain safe pause")
	}
}

func TestAppTimingProfilesSaveIndependently(t *testing.T) {
	a := browserTestApp(t)
	standard := model.DefaultGeneralSettings()
	standard.ReplyDelayMinSeconds, standard.ReplyDelayMaxSeconds = 2, 5
	if err := a.uiStore.SaveGeneralSettings(standard); err != nil {
		t.Fatal(err)
	}
	// Saving standard before the first snapshot must freeze the original migration.
	standard.ReplyDelayMinSeconds, standard.ReplyDelayMaxSeconds = 10, 12
	if err := a.SaveGeneralSettings(standard); err != nil {
		t.Fatal(err)
	}
	show, err := a.uiStore.LoadShowModeSettings()
	if err != nil || show.ReplyDelayMinSeconds != 2 {
		t.Fatalf("migration leaked: %+v %v", show, err)
	}
	show.ReplyDelayMinSeconds, show.ReplyDelayMaxSeconds = 20, 25
	if err := a.SaveShowModeSettings(show); err != nil {
		t.Fatal(err)
	}
	saved, err := a.uiStore.LoadGeneralSettings()
	if err != nil || saved.ReplyDelayMinSeconds != 10 || saved.ReplyDelayMaxSeconds != 12 {
		t.Fatalf("show save changed worker: %+v %v", saved, err)
	}
}

func TestShowModeUserFirstKeepsLineOne(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("first line\nsecond line", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	r.firstMessage = model.FirstMessageSettings{Mode: "user"}
	ctx := context.Background()
	r.step(ctx)
	r.step(ctx)
	if fake.totalSent() != 0 || !r.firstWaiting {
		t.Fatal("user-first sent unsolicited opener")
	}
	fake.addRow("partner", "hello")
	r.step(ctx)
	if fake.sentCount("first line") != 1 || fake.sentCount("second line") != 0 {
		t.Fatal("first incoming skipped line 1")
	}
	fake.addRow("partner", "more")
	r.step(ctx)
	if fake.sentCount("second line") != 1 {
		t.Fatal("second reply incorrect")
	}
}
func TestShowModeFirstWaitTimeoutIndependent(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("hi", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	r.firstMessage = model.FirstMessageSettings{Mode: "user", WaitTimeout: true, WaitMinSeconds: 30, WaitMaxSeconds: 30}
	ctx := context.Background()
	r.step(ctx)
	deadline := r.firstDeadline
	if deadline.IsZero() {
		t.Fatal("no first-message deadline")
	}
	r.step(ctx)
	if !deadline.Equal(r.firstDeadline) {
		t.Fatal("poll reset timer")
	}
	r.firstDeadline = time.Now().Add(-time.Second)
	r.step(ctx)
	if fake.clickCount("SKIP") != 1 || fake.totalSent() != 0 {
		t.Fatal("first timeout failed with no-reply skip disabled")
	}
}
func TestShowModeExistingIncomingOverridesBotFirst(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("hi\nnext", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	fake.addRow("partner", "already here")
	r := testLiveRunner(a, fake)
	r.replyDelayMin, r.replyDelayMax = 30*time.Millisecond, 30*time.Millisecond
	before := time.Now()
	r.step(context.Background())
	if fake.sentCount("hi") != 1 || time.Since(before) < 25*time.Millisecond {
		t.Fatal("incoming lost or sent as undelayed opener")
	}
}
func TestShowModeFirstDecisionNotRerolled(t *testing.T) {
	a := browserTestApp(t)
	if err := a.SaveMessageSettings(model.NewMessageSettings("hi", "")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	r.firstMessage = model.FirstMessageSettings{Mode: "random", BotFirstPercent: 0}
	r.step(context.Background())
	r.firstMessage.BotFirstPercent = 100
	r.step(context.Background())
	if fake.totalSent() != 0 {
		t.Fatal("same conversation rerolled")
	}
}
