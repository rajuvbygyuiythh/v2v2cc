package main

import (
	"chitchatbot/internal/browser"
	"chitchatbot/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticsBoundedFrozenFilteredAndClear(t *testing.T) {
	f := &homeDiagnosticsFeed{}
	settings := model.ShowModeSettings{ReplyDelayMinSeconds: 3, ReplyDelayMaxSeconds: 6}
	id := f.start("browser", "profile", "account", "Chat run", "single", 7, &settings)
	settings.ReplyDelayMinSeconds = 99
	f.record(id, HomeDiagnosticEvent{Code: "READY"})
	for i := 0; i < 505; i++ {
		f.record(id, HomeDiagnosticEvent{Code: "INCOMING_DETECTED", Conversation: fmt.Sprint(i % 2)})
	}
	s := f.snapshot(id, "")
	if len(s.Events) != 500 || s.Runs[0].Dropped != 6 || s.Runs[0].Settings.ReplyDelayMinSeconds != 3 {
		t.Fatal("bound/frozen settings", s.Runs)
	}
	s.Runs[0].Settings.ReplyDelayMinSeconds = 88
	for _, e := range f.snapshot(id, "1").Events {
		if e.Conversation != "1" && e.Conversation != "" {
			t.Fatal("conversation mixed")
		}
	}
	if f.snapshot(id, "").Runs[0].Settings.ReplyDelayMinSeconds != 3 {
		t.Fatal("mutable snapshot")
	}
	f.end(id)
	if err := f.clear(id); err != nil {
		t.Fatal(err)
	}
	s = f.snapshot(id, "")
	if len(s.Events) != 0 || s.Runs[0].Cleared != 500 || s.Runs[0].Ended == "" || s.Runs[0].Revision != 7 {
		t.Fatal("clear removed metadata")
	}
	for i := 0; i < 12; i++ {
		f.start("b", "p", "a", "Chat run", "single", 1, nil)
	}
	f.record(id, HomeDiagnosticEvent{Code: "READY"})
	s = f.snapshot(id, "")
	if len(s.Runs) != 12 || len(s.Events) != 0 || s.Selected != id || s.Evicted != 1 {
		t.Fatal("eviction switched/resurrected run")
	}
	if f.clear(id) == nil {
		t.Fatal("cleared missing run")
	}
}
func TestDiagnosticsAllowlistDeepCopyNonblocking(t *testing.T) {
	f := &homeDiagnosticsFeed{}
	id := f.start("b", "p", "a", "Chat run", "single", 1, nil)
	metrics := map[string]float64{"selected_ms": 12, "api_key": 9, "send_ms": math.Inf(1), "queue_ms": -1}
	f.record(id, HomeDiagnosticEvent{Code: "SMS_BODY"})
	f.record(id, HomeDiagnosticEvent{Code: "READY", Reason: "private raw error", Metrics: metrics, WaitKind: "secret"})
	metrics["selected_ms"] = 999
	s := f.snapshot(id, "")
	if len(s.Events) != 1 || s.Events[0].Reason != "" || s.Events[0].WaitKind != "" || len(s.Events[0].Metrics) != 1 || s.Events[0].Metrics["selected_ms"] != 12 {
		t.Fatal("allowlist/copy")
	}
	s.Events[0].Metrics["selected_ms"] = 100
	if f.snapshot(id, "").Events[0].Metrics["selected_ms"] != 12 {
		t.Fatal("snapshot alias")
	}
	f.mu.Lock()
	done := make(chan struct{})
	go func() { f.record(id, HomeDiagnosticEvent{Code: "READY"}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		f.mu.Unlock()
		t.Fatal("diagnostic blocked runner")
	}
	f.mu.Unlock()
	if f.snapshot(id, "").Missed != 1 {
		t.Fatal("missing omission notice")
	}
}
func TestDiagnosticsConcurrentReadersAndWriters(t *testing.T) {
	f := &homeDiagnosticsFeed{}
	id := f.start("b", "p", "a", "Chat run", "single", 1, nil)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				f.record(id, HomeDiagnosticEvent{Code: "READY"})
				f.snapshot(id, "")
			}
		}()
	}
	wg.Wait()
	if len(f.snapshot(id, "").Events) > 500 {
		t.Fatal("unbounded feed")
	}
}
func TestDiagnosticsTraceTimingNoTextAndNoPollSpam(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("opener\nreply", ""))
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	r.firstMessage = model.FirstMessageSettings{Mode: "user"}
	r.replyDelayMin = 15 * time.Millisecond
	r.replyDelayMax = r.replyDelayMin
	var events []HomeDiagnosticEvent
	r.onDiagnostic = func(e HomeDiagnosticEvent) { events = append(events, e) }
	r.step(context.Background())
	before := len(events)
	r.step(context.Background())
	if len(events) != before {
		t.Fatal("per-poll spam")
	}
	fake.addRow("partner", "PRIVATE_SMS_SENTINEL")
	r.step(context.Background())
	found := map[string]HomeDiagnosticEvent{}
	for _, e := range events {
		found[e.Code] = e
	}
	for _, code := range []string{"FIRST_TURN", "INCOMING_DETECTED", "REPLY_WAIT", "WAIT_FINISHED", "SEND_STARTED", "SEND_CONFIRMED"} {
		if _, ok := found[code]; !ok {
			t.Fatal("missing", code)
		}
	}
	e := found["SEND_CONFIRMED"]
	if e.Reply == 0 || e.Reply != found["INCOMING_DETECTED"].Reply || e.Metrics["detected_to_confirmed_ms"] < 14 || found["WAIT_FINISHED"].Metrics["actual_wait_ms"] < 14 {
		t.Fatal("timing/trace", events)
	}
	payload, _ := json.Marshal(events)
	if strings.Contains(string(payload), "PRIVATE_SMS") {
		t.Fatal("SMS leaked")
	}
}
func TestDiagnosticsUnconfirmedSendAndFaultySubscriber(t *testing.T) {
	a := browserTestApp(t)
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}, failSend: true}
	r := testLiveRunner(a, fake)
	snapshot, _ := fake.ReadDOMSnapshot(context.Background())
	r.reset(snapshot.Controls)
	r.activeTrace = liveIncomingTrace{id: 3, detected: time.Now()}
	var events []HomeDiagnosticEvent
	r.onDiagnostic = func(e HomeDiagnosticEvent) { events = append(events, e) }
	r.sendLines(context.Background(), []string{"private reply"})
	for _, e := range events {
		if e.Code == "SEND_CONFIRMED" {
			t.Fatal("false confirmation")
		}
		if _, ok := e.Metrics["detected_to_confirmed_ms"]; ok {
			t.Fatal("false confirmed duration")
		}
	}
	if !r.paused {
		t.Fatal("send safety changed")
	}
	r.onDiagnostic = func(HomeDiagnosticEvent) { panic("subscriber") }
	r.diagnostic(HomeDiagnosticEvent{Code: "READY"})
}
func TestDiagnosticsDoesNotDrawExtraRandomNumbers(t *testing.T) {
	a := browserTestApp(t)
	run := func(enabled bool) (string, int64) {
		fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
		r := testLiveRunner(a, fake)
		r.replyDelayRNG = rand.New(rand.NewSource(42))
		r.firstMessage = model.FirstMessageSettings{Mode: "random", BotFirstPercent: 48, WaitTimeout: true, WaitMinSeconds: 2, WaitMaxSeconds: 5}
		reason := ""
		if enabled {
			r.onDiagnostic = func(e HomeDiagnosticEvent) {
				if e.Code == "FIRST_TURN" {
					reason = e.Reason
				}
			}
		}
		r.step(context.Background())
		return reason, r.replyDelayRNG.Int63()
	}
	_, plain := run(false)
	_, observed := run(true)
	if plain != observed {
		t.Fatal("observation changed random choices")
	}
}
func TestDiagnosticsTranscriptRunCorrelation(t *testing.T) {
	f := &homeChatFeed{}
	a := f.startWithDiagnostics("browser A", "run-A")
	b := f.startWithDiagnostics("browser B", "run-B")
	f.observe(a, transcriptSnapshot("same-dom-id", transcriptRow("a", "partner", "one", 1)), false)
	f.observe(b, transcriptSnapshot("same-dom-id", transcriptRow("b", "partner", "two", 1)), false)
	s := f.snapshot("")
	if len(s.Sessions) != 2 || s.Sessions[0].DiagnosticRun != "run-A" || s.Sessions[1].DiagnosticRun != "run-B" || s.Sessions[0].Conversation != "same-dom-id" {
		t.Fatal("run correlation", s.Sessions)
	}
}

func TestDiagnosticsShowLifecycleFrozenAndDistinctRestart(t *testing.T) {
	a := browserTestApp(t)
	_ = a.SaveMessageSettings(model.NewMessageSettings("hello\nreply", ""))
	settings := model.ShowModeSettings{FirstMessage: model.FirstMessageSettings{Mode: "user"}, ReplyDelayMinSeconds: 3, ReplyDelayMaxSeconds: 6}
	if err := a.SaveShowModeSettings(settings); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	s := readyShow(t, a, fake, "synthetic-account")
	lifecycle := s.lifecycleDiagnostic
	before := a.GetHomeDiagnostics(lifecycle, "")
	if len(before.Events) != 2 || before.Events[1].Code != "READY" || before.Runs[0].Settings != nil || fake.totalSent() != 0 {
		t.Fatal("false login automation", before)
	}
	startShow(t, a, s.view.ID)
	first := s.chatDiagnostic
	waitForLive(t, "first decision", func() bool {
		for _, e := range a.GetHomeDiagnostics(first, "").Events {
			if e.Code == "FIRST_TURN" {
				return true
			}
		}
		return false
	})
	if err := a.StopShowChat(s.view.ID); err != nil {
		t.Fatal(err)
	}
	old := a.GetHomeDiagnostics(first, "")
	foundStop := false
	for _, e := range old.Events {
		if e.Code == "STOP_REQUESTED" {
			foundStop = true
		}
	}
	if !foundStop {
		t.Fatal("stop request missing")
	}
	var info HomeDiagnosticRun
	for _, run := range old.Runs {
		if run.ID == first {
			info = run
		}
	}
	if info.Ended == "" || info.Settings == nil || info.Settings.ReplyDelayMinSeconds != 3 {
		t.Fatal("stopped snapshot lost", info)
	}
	settings.ReplyDelayMinSeconds = 0
	settings.ReplyDelayMaxSeconds = 0
	if err := a.SaveShowModeSettings(settings); err != nil {
		t.Fatal(err)
	}
	startShow(t, a, s.view.ID)
	second := s.chatDiagnostic
	if second == first || second == lifecycle {
		t.Fatal("run IDs reused")
	}
	if err := a.StopShowChat(s.view.ID); err != nil {
		t.Fatal(err)
	}
	for _, run := range a.GetHomeDiagnostics(first, "").Runs {
		if run.ID == first && run.Settings.ReplyDelayMinSeconds != 3 {
			t.Fatal("old run settings changed")
		}
	}
}
func TestDiagnosticsInvalidLoginNeverReady(t *testing.T) {
	a := browserTestApp(t)
	s := readyShow(t, a, &fakeDOMBackend{}, "")
	for _, e := range a.GetHomeDiagnostics(s.lifecycleDiagnostic, "").Events {
		if e.Code == "READY" {
			t.Fatal("invalid login advertised ready")
		}
	}
}

func TestDiagnosticsCancellationNeverSendsAndRetainsReplyCorrelation(t *testing.T) {
	a := browserTestApp(t)
	fake := &fakeDOMBackend{controls: browser.DOMControls{HasInput: true, Buttons: []string{"SKIP"}}}
	r := testLiveRunner(a, fake)
	r.firstMessage = model.FirstMessageSettings{Mode: "user"}
	r.replyDelayMin = time.Second
	r.replyDelayMax = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var events []HomeDiagnosticEvent
	r.onDiagnostic = func(e HomeDiagnosticEvent) {
		events = append(events, e)
		if e.Code == "REPLY_WAIT" {
			cancel()
		}
	}
	r.step(ctx)
	fake.addRow("partner", "private incoming")
	if !r.step(ctx) {
		t.Fatal("cancel not propagated")
	}
	found := false
	for _, e := range events {
		if e.Code == "CANCELLED" && e.Reply > 0 && e.Conversation != "" && e.Reason == "stop_or_cancel" {
			found = true
		}
	}
	if !found || fake.totalSent() != 0 {
		t.Fatal("cancellation diagnostics changed send behavior", events)
	}
}
