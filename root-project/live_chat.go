package main

// Show Mode live chat, DOM-only edition. After the user logs in manually in
// the visible Rod browser, chatting continues inside that same browser using
// only what a human sees: message rows are read from the DOM, replies are
// typed into the message box, and Skip/START are real button clicks. No
// WebSocket frames and no API calls are used (explicit user requirement).
// Normal Rod/ChromeDP login capture and the Start-Bot worker are untouched;
// this runs only when BrowserSettings.LiveChat is set (Show Mode).

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"chitchatbot/internal/bot"
	"chitchatbot/internal/browser"
	"chitchatbot/internal/model"
)

// ---------------------------------------------------------------------------
// Reply responders (unchanged semantics). EVA mode reuses the exported
// EVAChat engine; Folder mode picks ONE sequence per conversation, like the
// worker bot; Single mode walks its lines.
// ---------------------------------------------------------------------------

type liveResponder interface {
	Start() []string
	Reply(incoming string) (lines []string, end bool)
}

type detectOnlyResponder struct{}

func (detectOnlyResponder) Start() []string { return nil }
func (detectOnlyResponder) Reply(string) ([]string, bool) {
	return nil, false
}

type sequenceLiveResponder struct {
	sequences [][]string
	lines     []string
	next      int
	rng       *rand.Rand
}

func (r *sequenceLiveResponder) Start() []string {
	usable := r.sequences[:0]
	for _, seq := range r.sequences {
		if len(seq) > 0 {
			usable = append(usable, seq)
		}
	}
	if len(usable) == 0 {
		return nil
	}
	pick := 0
	if len(usable) > 1 && r.rng != nil {
		pick = r.rng.Intn(len(usable))
	}
	r.lines = usable[pick]
	r.next = 1
	return []string{r.lines[0]}
}

func (r *sequenceLiveResponder) Reply(string) ([]string, bool) {
	if r.lines == nil {
		return r.Start(), false
	}
	if r.next < len(r.lines) {
		line := r.lines[r.next]
		r.next++
		return []string{line}, false
	}
	return nil, true
}

type evaLiveResponder struct {
	engine   *bot.EVAChatEngine
	state    *model.EVAChatSession
	rng      *rand.Rand
	username string
}

func evaLines(result bot.EVAReplyResult) []string {
	lines := []string{}
	if strings.TrimSpace(result.Text) != "" {
		lines = append(lines, result.Text)
	}
	for _, extra := range result.Extra {
		if strings.TrimSpace(extra) != "" {
			lines = append(lines, extra)
		}
	}
	return lines
}

func (r *evaLiveResponder) Start() []string {
	return r.render(r.engine.Start(r.state, r.rng))
}

func (r *evaLiveResponder) Reply(incoming string) ([]string, bool) {
	result := r.engine.ReplyWithOptions(incoming, r.state, r.rng, bot.EVAReplyOptions{HasUsernames: r.username != ""})
	return r.render(result), result.Ended
}

func (r *evaLiveResponder) render(result bot.EVAReplyResult) []string {
	lines := evaLines(result)
	for i, line := range lines {
		lines[i] = strings.NewReplacer("%username%", r.username, "{snap}", r.username).Replace(line)
	}
	return lines
}

func liveSequences(settings model.MessageSettings) [][]string {
	if settings.EffectiveChatMode() == model.ChatModeFolder && len(settings.LoadedSequences) > 0 {
		sequences := make([][]string, 0, len(settings.LoadedSequences))
		for _, seq := range settings.LoadedSequences {
			sequences = append(sequences, model.MessageSequenceLines(seq))
		}
		return sequences
	}
	return [][]string{settings.SequenceLines()}
}

func (a *App) newLiveResponder() (liveResponder, string, error) {
	settings, err := a.uiStore.LoadMessageSettings()
	if err != nil {
		return nil, "", err
	}
	return a.newLiveResponderWithSettings(settings)
}

func (a *App) newLiveResponderWithSettings(settings model.MessageSettings) (liveResponder, string, error) {
	mode := settings.EffectiveChatMode()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	username := ""
	if handles := settings.UsernameLines(); len(handles) > 0 {
		username = handles[rng.Intn(len(handles))]
	}
	if mode == model.ChatModeEVAChat {
		dataRoot, err := bot.EnsureEVAChatData(a.dataDir)
		if err != nil {
			return nil, "", err
		}
		engine, err := bot.LoadEVAChatEngine(dataRoot)
		if err != nil {
			return nil, "", err
		}
		session := bot.NewEVAChatSession()
		return &evaLiveResponder{
			engine:   engine,
			state:    &session,
			rng:      rng,
			username: username,
		}, "EVAChat Flow", nil
	}
	sequences := liveSequences(settings)
	for i, seq := range sequences {
		for j, line := range seq {
			if username == "" && (strings.Contains(line, "%username%") || strings.Contains(line, "{snap}")) {
				return nil, "", fmt.Errorf("Show Mode sequence needs a Message → Usernames entry")
			}
			sequences[i][j] = strings.NewReplacer("%username%", username, "{snap}", username).Replace(line)
		}
	}
	return &sequenceLiveResponder{sequences: sequences, rng: rng}, mode, nil
}

// DOM runner: single-consumer queue over the observer's retained rows. Unknown
// authors are not guessed; uncertain sends pause until a different conversation.
type liveIncomingTrace struct {
	id       uint64
	detected time.Time
}

type liveRunner struct {
	lastCheckReason                                       string
	onDiagnostic                                          func(HomeDiagnosticEvent)
	firstWaitStarted, noReplyWaitStarted, nextWaitStarted time.Time
	incomingTrace                                         map[string]liveIncomingTrace
	traceSerial                                           uint64
	activeTrace                                           liveIncomingTrace
	activeQueueMS                                         float64
	activeWaitMS                                          float64
	initialBaseline                                       *browser.DOMSnapshot
	onProgress                                            func(string, string)
	firstMessage                                          model.FirstMessageSettings
	firstWaiting                                          bool
	firstDeadline                                         time.Time
	dom                                                   browser.DOMChatBackend
	prefix                                                string
	logf                                                  func(string, string)
	newResponder                                          func() (liveResponder, string, error)
	replyDelayMin                                         time.Duration
	replyDelayMax                                         time.Duration
	replyDelayRNG                                         *rand.Rand
	pollInterval                                          time.Duration
	conversation                                          string
	controls                                              browser.DOMControls
	responder                                             liveResponder
	seenIDs                                               map[string]bool
	queuedIDs                                             map[string]bool
	queue                                                 []browser.DOMMessage
	paused                                                bool
	rotating                                              bool
	lastStartAt                                           time.Time
	lastRotateAt                                          time.Time
	lastStepError                                         string
	timeoutRotating                                       bool
	autoSkip                                              bool
	noReplyMin, noReplyMax                                time.Duration
	nextChatMin, nextChatMax                              time.Duration
	replyDeadline                                         time.Time
	nextChatAt                                            time.Time
	previousPhase                                         string
	probeLogged                                           bool
}

func showModeReplyDelayRange(settings model.ShowModeSettings) (time.Duration, time.Duration) {
	minSeconds := settings.ReplyDelayMinSeconds
	maxSeconds := settings.ReplyDelayMaxSeconds
	if minSeconds < 0 {
		minSeconds = 0
	}
	if maxSeconds < 0 {
		maxSeconds = 0
	}
	if maxSeconds < minSeconds {
		maxSeconds = minSeconds
	}
	return time.Duration(minSeconds) * time.Second, time.Duration(maxSeconds) * time.Second
}

func chooseShowModeReplyDelay(rng *rand.Rand, minDelay, maxDelay time.Duration) time.Duration {
	if minDelay < 0 {
		minDelay = 0
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	if rng == nil || maxDelay == minDelay {
		return minDelay
	}
	return minDelay + time.Duration(rng.Int63n(int64(maxDelay-minDelay)))
}

func (r *liveRunner) run(ctx context.Context) {
	defer func() {
		if ctx.Err() != nil {
			r.diagnostic(HomeDiagnosticEvent{Code: "CANCELLED", Reason: "stop_or_cancel"})
		}
	}()
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil || r.step(ctx) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *liveRunner) reset(c browser.DOMControls) {
	if r.conversation != "" && r.conversation != c.Conversation {
		r.diagnostic(HomeDiagnosticEvent{Code: "CONVERSATION_CHANGED", Reason: "conversation_changed", Metrics: map[string]float64{"pending": float64(len(r.queue))}})
	}
	r.activeTrace = liveIncomingTrace{}
	r.firstWaitStarted = time.Time{}
	r.noReplyWaitStarted = time.Time{}
	r.incomingTrace = make(map[string]liveIncomingTrace)
	r.firstWaiting = false
	r.firstDeadline = time.Time{}
	r.timeoutRotating = false
	r.replyDeadline = time.Time{}
	r.conversation = c.Conversation
	r.controls = c
	r.responder = nil
	r.seenIDs = make(map[string]bool)
	r.queuedIDs = make(map[string]bool)
	r.queue = nil
	r.paused = false
	r.rotating = false
	r.probeLogged = false
	r.lastStepError = ""
	r.lastRotateAt = time.Time{}
	r.lastStartAt = time.Time{}
}

func (r *liveRunner) step(ctx context.Context) bool {
	snapshot, err := r.dom.ReadDOMSnapshot(ctx)
	if err != nil {
		if r.pageGone(err) {
			return true
		}
		r.logStepError("DOM READ FAILED | " + err.Error())
		return false
	}
	detectedAt := time.Now()
	c := snapshot.Controls
	// Remember the chat-to-lobby transition even when the DOM generation changes.
	if c.Phase != "chatting" && r.previousPhase == "chatting" {
		armed := time.Now()
		selected := chooseShowModeReplyDelay(r.replyDelayRNG, r.nextChatMin, r.nextChatMax)
		r.nextWaitStarted = armed
		r.nextChatAt = armed.Add(selected)
		r.diagnostic(HomeDiagnosticEvent{Code: "NEXT_WAIT_ARMED", WaitKind: "next_chat", Metrics: map[string]float64{"selected_ms": milliseconds(selected), "configured_min_ms": milliseconds(r.nextChatMin), "configured_max_ms": milliseconds(r.nextChatMax)}})
	}
	r.previousPhase = c.Phase
	if c.Conversation == "" {
		r.logStepError("DOM NOT READY | missing conversation identity")
		return false
	}
	if c.Conversation != r.conversation {
		r.reset(c)
	}
	r.controls = c
	if c.Overflow {
		r.paused = true
		r.logStepError("QUEUE FULL | paused safely; start a new chat")
		return false
	}
	if c.Phase != "chatting" {
		r.responder = nil
		r.queue = nil
		if time.Now().Before(r.nextChatAt) {
			r.progress("Waiting before next chat until "+r.nextChatAt.Format("15:04:05"), "")
		}
		if !time.Now().Before(r.nextChatAt) && hasDOMButton(c.Buttons, "START") && time.Since(r.lastStartAt) > 6*time.Second {
			r.lastStartAt = time.Now()
			clicked, err := r.dom.ClickDOMButton(ctx, []string{"START"})
			if err != nil {
				if r.pageGone(err) {
					return true
				}
				r.logStepError("START FAILED | " + err.Error())
			} else if clicked != "" {
				r.diagnostic(HomeDiagnosticEvent{Code: "START_CLICKED", Reason: "start"})
				if !r.nextWaitStarted.IsZero() {
					r.diagnostic(HomeDiagnosticEvent{Code: "WAIT_FINISHED", WaitKind: "next_chat", Metrics: waitElapsed(r.nextWaitStarted)})
					r.nextWaitStarted = time.Time{}
				}
				r.logf("activity", r.prefix+" START clicked | waiting for a new conversation")
			}
		} else if !r.probeLogged {
			r.probeLogged = true
			if probe, err := r.dom.DOMProbe(ctx); err == nil {
				r.logf("activity", r.prefix+" PAGE PROBE | "+probe)
			}
		}
		return false
	}
	if r.rotating {
		return r.rotate(ctx)
	}
	if r.paused {
		return false
	}
	if !c.CanSend || c.Partner == "" || c.OwnName == "" || strings.EqualFold(c.Partner, c.OwnName) {
		r.logStepError("DOM WAIT | composer or own/partner identity not ready; no guessed replies")
		return false
	}
	if time.Now().Before(r.nextChatAt) {
		return false
	}
	if r.responder == nil {
		suppressOpener := false
		if r.initialBaseline != nil {
			count := 0
			if r.initialBaseline.Controls.Conversation == c.Conversation {
				for _, row := range r.initialBaseline.Messages {
					if row.ID != "" {
						r.seenIDs[row.ID] = true
						count++
						suppressOpener = true
					}
				}
			}
			if count > 0 {
				r.diagnostic(HomeDiagnosticEvent{Code: "HISTORY_IGNORED", Reason: "history_ignored", Metrics: map[string]float64{"count": float64(count)}})
			}
			r.initialBaseline = nil
		}
		// Seed own/unknown history only. An already-present partner message
		// wins over an unsolicited opener and is processed through Reply.
		for _, row := range snapshot.Messages {
			if row.ID != "" && !strings.EqualFold(row.Author, c.Partner) {
				r.seenIDs[row.ID] = true
			}
		}
		responder, mode, err := r.newResponder()
		if err != nil {
			r.paused = true
			r.logStepError("REPLY ENGINE FAILED | " + err.Error())
			return false
		}
		r.responder = responder
		r.diagnostic(HomeDiagnosticEvent{Code: "CHAT_STARTED"})
		r.logf("activity", fmt.Sprintf("%s CHAT STARTED | mode=%s history=%d observer=%t", r.prefix, mode, len(r.seenIDs), c.ObserverReady))
		hasIncoming := false
		for _, row := range snapshot.Messages {
			if row.ID != "" && !r.seenIDs[row.ID] && strings.EqualFold(row.Author, c.Partner) && strings.TrimSpace(row.Text) != "" {
				hasIncoming = true
				break
			}
		}
		botFirst := r.firstMessage.ChooseBotFirst(r.replyDelayRNG) && !suppressOpener
		decision := "User first"
		if botFirst {
			decision = "Bot first"
		}
		if suppressOpener {
			decision = "Existing chat — waiting for a new user message (history ignored)"
		}
		reason := "user_first"
		if botFirst {
			reason = "bot_first"
		}
		if suppressOpener {
			reason = "history_ignored"
		}
		r.diagnostic(HomeDiagnosticEvent{Code: "FIRST_TURN", Reason: reason})
		r.progress("Waiting for first turn", decision)
		if botFirst && !hasIncoming {
			// Recheck just before opening: incoming may have arrived while loading
			// the responder. There is no atomic browser/server ordering guarantee.
			fresh, err := r.dom.ReadDOMSnapshot(ctx)
			if err != nil {
				r.paused = true
				r.logStepError("FIRST TURN RECHECK FAILED | " + err.Error())
				return r.pageGone(err)
			}
			if fresh.Controls.Conversation != r.conversation {
				return false
			}
			detectedAt = time.Now()
			snapshot.Messages = fresh.Messages
			for _, row := range snapshot.Messages {
				if row.ID != "" && !r.seenIDs[row.ID] && strings.EqualFold(row.Author, c.Partner) && strings.TrimSpace(row.Text) != "" {
					hasIncoming = true
					break
				}
			}
			if !hasIncoming {
				return r.sendLines(ctx, responder.Start())
			}
		}
		r.firstWaiting = true
		policy := r.firstMessage.Effective()
		if policy.WaitTimeout {
			armed := time.Now()
			selected := policy.WaitDuration(r.replyDelayRNG)
			r.firstWaitStarted = armed
			r.firstDeadline = armed.Add(selected)
			r.diagnostic(HomeDiagnosticEvent{Code: "FIRST_WAIT_ARMED", WaitKind: "first_message", Metrics: map[string]float64{"selected_ms": milliseconds(selected), "configured_min_ms": float64(policy.WaitMinSeconds) * 1000, "configured_max_ms": float64(policy.WaitMaxSeconds) * 1000}})
		}
		if hasIncoming {
			r.diagnostic(HomeDiagnosticEvent{Code: "FIRST_TURN_OVERRIDE", Reason: "incoming_already_present"})
			r.progress("New user message detected — preparing reply", "User already messaged — reply first")
		}
		// Fall through: a partner already present must receive a reply, not a second opener.
	}
	for _, row := range snapshot.Messages {
		if row.ID == "" || r.seenIDs[row.ID] || r.queuedIDs[row.ID] || strings.TrimSpace(row.Text) == "" {
			continue
		}
		if strings.EqualFold(row.Author, c.OwnName) {
			r.seenIDs[row.ID] = true
			continue
		}
		if !strings.EqualFold(row.Author, c.Partner) {
			r.logStepError("AUTHOR UNKNOWN | unread row retained; no text-only echo guessing")
			continue
		}
		if !r.firstWaitStarted.IsZero() {
			r.diagnostic(HomeDiagnosticEvent{Code: "WAIT_FINISHED", WaitKind: "first_message", Metrics: waitElapsed(r.firstWaitStarted)})
			r.firstWaitStarted = time.Time{}
		}
		if !r.noReplyWaitStarted.IsZero() {
			r.diagnostic(HomeDiagnosticEvent{Code: "WAIT_FINISHED", WaitKind: "no_reply", Metrics: waitElapsed(r.noReplyWaitStarted)})
			r.noReplyWaitStarted = time.Time{}
		}
		r.firstWaiting = false
		r.firstDeadline = time.Time{}
		r.replyDeadline = time.Time{}
		r.queue = append(r.queue, row)
		r.queuedIDs[row.ID] = true
		if r.incomingTrace == nil {
			r.incomingTrace = make(map[string]liveIncomingTrace)
		}
		r.traceSerial++
		trace := liveIncomingTrace{id: r.traceSerial, detected: detectedAt}
		r.incomingTrace[row.ID] = trace
		r.diagnostic(HomeDiagnosticEvent{Code: "INCOMING_DETECTED", DetectedAt: detectedAt.UTC().Format(time.RFC3339Nano), Reply: trace.id, Metrics: map[string]float64{"pending": float64(len(r.queue))}})
	}
	if len(r.queue) == 0 {
		if r.firstWaiting && !r.firstDeadline.IsZero() && !time.Now().Before(r.firstDeadline) {
			r.diagnostic(HomeDiagnosticEvent{Code: "FIRST_TIMEOUT", WaitKind: "first_message", Metrics: waitElapsed(r.firstWaitStarted)})
			r.progress("First-message timeout — skipping idle conversation", "")
			r.timeoutRotating = true
			r.rotating = true
			return r.rotate(ctx)
		}
		if r.autoSkip && !r.replyDeadline.IsZero() && !time.Now().Before(r.replyDeadline) {
			r.diagnostic(HomeDiagnosticEvent{Code: "NO_REPLY_TIMEOUT", WaitKind: "no_reply", Metrics: waitElapsed(r.noReplyWaitStarted)})
			r.progress("No-reply timeout — skipping idle conversation", "")
			r.replyDeadline = time.Time{}
			r.timeoutRotating = true
			r.rotating = true
			return r.rotate(ctx)
		}
		return false
	}
	m := r.queue[0]
	trace := r.incomingTrace[m.ID]
	queueMS := 0.0
	if !trace.detected.IsZero() {
		queueMS = milliseconds(time.Since(trace.detected))
	}
	conversation := r.conversation
	r.logf("activity", fmt.Sprintf("%s INCOMING | partner row accepted; pending=%d", r.prefix, len(r.queue)))
	replyDelay := chooseShowModeReplyDelay(r.replyDelayRNG, r.replyDelayMin, r.replyDelayMax)
	r.logf("activity", fmt.Sprintf("%s REPLY DELAY | selected=%s setting=%s-%s", r.prefix, replyDelay, r.replyDelayMin, r.replyDelayMax))
	r.progress(fmt.Sprintf("Reply pending — waiting %s (configured %s–%s)", replyDelay, r.replyDelayMin, r.replyDelayMax), "")
	r.diagnostic(HomeDiagnosticEvent{Code: "REPLY_WAIT", Reply: trace.id, WaitKind: "reply", Metrics: map[string]float64{"selected_ms": milliseconds(replyDelay), "configured_min_ms": milliseconds(r.replyDelayMin), "configured_max_ms": milliseconds(r.replyDelayMax), "queue_ms": queueMS}})
	waitStarted := time.Now()
	if sleepLive(ctx, replyDelay) {
		r.diagnostic(HomeDiagnosticEvent{Code: "CANCELLED", Conversation: conversation, Reply: trace.id, Reason: "stop_or_cancel", WaitKind: "reply", Metrics: map[string]float64{"actual_wait_ms": milliseconds(time.Since(waitStarted))}})
		return true
	}
	actualWaitMS := milliseconds(time.Since(waitStarted))
	r.diagnostic(HomeDiagnosticEvent{Code: "WAIT_FINISHED", Reply: trace.id, WaitKind: "reply", Metrics: map[string]float64{"actual_wait_ms": actualWaitMS}})
	// Do not let a slow reply or an old batch leak into the next partner's chat.
	if !r.stillCurrent(ctx) {
		reason := r.lastCheckReason
		if ctx.Err() != nil {
			reason = "stop_or_cancel"
		}
		r.diagnostic(HomeDiagnosticEvent{Code: "CANCELLED", Conversation: conversation, Reply: trace.id, Reason: reason})
		return ctx.Err() != nil
	}
	r.activeTrace = trace
	r.activeQueueMS = queueMS
	r.activeWaitMS = actualWaitMS
	generation := r.conversation
	lines, end := r.responder.Reply(m.Text)
	r.seenIDs[m.ID] = true
	delete(r.queuedIDs, m.ID)
	r.queue = r.queue[1:]
	delete(r.incomingTrace, m.ID)
	if len(lines) == 0 {
		r.diagnostic(HomeDiagnosticEvent{Code: "REPLY_EMPTY", Reply: trace.id, Reason: "flow_no_output"})
	}
	if r.sendLines(ctx, lines) {
		return true
	}
	if r.conversation != generation || r.responder == nil {
		return false
	}
	if end && !r.paused {
		r.queue = nil
		r.rotating = true
		return r.rotate(ctx)
	}
	return false
}

func (r *liveRunner) stillCurrent(ctx context.Context) bool {
	r.lastCheckReason = "inspect_browser"
	s, err := r.dom.ReadDOMSnapshot(ctx)
	if err != nil {
		r.paused = true
		r.logStepError("DOM RECHECK FAILED | " + err.Error())
		return false
	}
	if s.Controls.Conversation != r.conversation || s.Controls.Phase != "chatting" {
		r.lastCheckReason = "conversation_changed"
		r.reset(s.Controls)
		return false
	}
	if !s.Controls.CanSend || s.Controls.Overflow {
		r.paused = true
		r.logStepError("SEND PAUSED | composer is not ready; inspect the page")
		return false
	}
	return true
}

func (r *liveRunner) sendLines(ctx context.Context, lines []string) bool {
	trace := r.activeTrace
	conversation := r.conversation
	defer func() { r.activeTrace = liveIncomingTrace{}; r.activeQueueMS = 0; r.activeWaitMS = 0 }()
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if ctx.Err() != nil {
			r.diagnostic(HomeDiagnosticEvent{Code: "CANCELLED", Conversation: conversation, Reply: trace.id, Part: i + 1, Reason: "stop_or_cancel"})
			return true
		}
		if i > 0 {
			delay := chooseShowModeReplyDelay(r.replyDelayRNG, r.replyDelayMin, r.replyDelayMax)
			r.progress(fmt.Sprintf("Follow-up pending — waiting %s", delay), "")
			r.diagnostic(HomeDiagnosticEvent{Code: "REPLY_WAIT", Reply: trace.id, Part: i + 1, WaitKind: "follow_up", Metrics: map[string]float64{"selected_ms": milliseconds(delay), "configured_min_ms": milliseconds(r.replyDelayMin), "configured_max_ms": milliseconds(r.replyDelayMax)}})
			waitStarted := time.Now()
			if sleepLive(ctx, delay) {
				r.diagnostic(HomeDiagnosticEvent{Code: "CANCELLED", Conversation: conversation, Reply: trace.id, Part: i + 1, Reason: "stop_or_cancel", WaitKind: "follow_up", Metrics: map[string]float64{"actual_wait_ms": milliseconds(time.Since(waitStarted))}})
				return true
			}
			r.diagnostic(HomeDiagnosticEvent{Code: "WAIT_FINISHED", Reply: trace.id, Part: i + 1, WaitKind: "follow_up", Metrics: map[string]float64{"actual_wait_ms": milliseconds(time.Since(waitStarted))}})
		}
		if !r.stillCurrent(ctx) {
			reason := r.lastCheckReason
			if ctx.Err() != nil {
				reason = "stop_or_cancel"
			}
			r.diagnostic(HomeDiagnosticEvent{Code: "CANCELLED", Conversation: conversation, Reply: trace.id, Part: i + 1, Reason: reason})
			return ctx.Err() != nil
		}
		if ctx.Err() != nil {
			r.diagnostic(HomeDiagnosticEvent{Code: "CANCELLED", Conversation: conversation, Reply: trace.id, Part: i + 1, Reason: "stop_or_cancel"})
			return true
		}
		metrics := map[string]float64{}
		if !trace.detected.IsZero() {
			metrics["detected_to_send_ms"] = milliseconds(time.Since(trace.detected))
			metrics["queue_ms"] = r.activeQueueMS
			if i == 0 {
				metrics["actual_wait_ms"] = r.activeWaitMS
			}
		}
		r.diagnostic(HomeDiagnosticEvent{Code: "SEND_STARTED", Reply: trace.id, Part: i + 1, Metrics: metrics})
		sendStarted := time.Now()
		sent, err := r.dom.SendDOMMessage(ctx, line)
		metrics = map[string]float64{"send_ms": milliseconds(time.Since(sendStarted))}
		if err == nil && sent && !trace.detected.IsZero() {
			metrics["detected_to_confirmed_ms"] = milliseconds(time.Since(trace.detected))
		}
		code := "SEND_UNCONFIRMED"
		reason := "unconfirmed"
		if err == nil && sent {
			code = "SEND_CONFIRMED"
			reason = "confirmed"
		}
		r.diagnostic(HomeDiagnosticEvent{Code: code, Conversation: conversation, Reply: trace.id, Part: i + 1, Reason: reason, Metrics: metrics})
		if err != nil && r.pageGone(err) {
			return true
		}
		if err != nil || !sent {
			r.paused = true
			r.logStepError("SEND UNCONFIRMED | automation paused; inspect the page before starting a new chat (no automatic resend)")
			return false
		}
		if r.autoSkip {
			armed := time.Now()
			selected := chooseShowModeReplyDelay(r.replyDelayRNG, r.noReplyMin, r.noReplyMax)
			r.noReplyWaitStarted = armed
			r.replyDeadline = armed.Add(selected)
			r.diagnostic(HomeDiagnosticEvent{Code: "NO_REPLY_ARMED", WaitKind: "no_reply", Metrics: map[string]float64{"selected_ms": milliseconds(selected), "configured_min_ms": milliseconds(r.noReplyMin), "configured_max_ms": milliseconds(r.noReplyMax)}})
		}
		r.progress("Send confirmed — waiting for partner", "")
		r.logf("activity", r.prefix+" SEND CONFIRMED | new own message row observed")
	}
	return false
}

func (r *liveRunner) rotate(ctx context.Context) bool {
	if time.Since(r.lastRotateAt) < 2*time.Second {
		return false
	}
	r.lastRotateAt = time.Now()
	s, err := r.dom.ReadDOMSnapshot(ctx)
	if err != nil {
		if r.pageGone(err) {
			return true
		}
		r.logStepError("SKIP CHECK FAILED | " + err.Error())
		return false
	}
	if s.Controls.Conversation != r.conversation {
		r.reset(s.Controls)
		return false
	}
	// Recheck fresh rows before a timeout skip; a late incoming message wins.
	if r.timeoutRotating {
		for _, row := range s.Messages {
			if row.ID != "" && !r.seenIDs[row.ID] && strings.EqualFold(row.Author, s.Controls.Partner) && strings.TrimSpace(row.Text) != "" {
				r.firstWaiting = false
				r.firstDeadline = time.Time{}
				r.rotating = false
				r.timeoutRotating = false
				return false
			}
		}
	}
	if hasDOMButton(s.Controls.Buttons, "START") && time.Now().Before(r.nextChatAt) {
		return false
	}
	// Exact active aria-label only; hidden START/STOP labels never participate.
	clicked, err := r.dom.ClickDOMButton(ctx, []string{"SKIP", "CONFIRM?", "START"})
	if err != nil {
		if r.pageGone(err) {
			return true
		}
		r.logStepError("SKIP FAILED | " + err.Error())
	} else if clicked == "" {
		r.logStepError("SKIP WAIT | active cycle button unavailable; use the page manually")
	} else {
		if r.nextChatAt.IsZero() || time.Now().After(r.nextChatAt) {
			armed := time.Now()
			selected := chooseShowModeReplyDelay(r.replyDelayRNG, r.nextChatMin, r.nextChatMax)
			r.nextWaitStarted = armed
			r.nextChatAt = armed.Add(selected)
			r.diagnostic(HomeDiagnosticEvent{Code: "NEXT_WAIT_ARMED", WaitKind: "next_chat", Metrics: map[string]float64{"selected_ms": milliseconds(selected), "configured_min_ms": milliseconds(r.nextChatMin), "configured_max_ms": milliseconds(r.nextChatMax)}})
		}
		reason := "skip"
		if clicked == "START" {
			reason = "start"
		}
		if clicked == "CONFIRM?" {
			reason = "confirm"
		}
		r.diagnostic(HomeDiagnosticEvent{Code: "SKIP_CLICKED", Reason: reason})
		r.logf("activity", r.prefix+" SKIP | clicked active "+clicked+"; awaiting conversation change")
		r.lastStartAt = time.Time{}
	}
	// Do not greet the old page again just because click() returned successfully.
	return false
}

func (r *liveRunner) pageGone(err error) bool {
	gone := errors.Is(err, browser.ErrPageUnavailable) || errors.Is(err, context.Canceled)
	if gone {
		reason := "page_unavailable"
		if errors.Is(err, context.Canceled) {
			reason = "stop_or_cancel"
		}
		r.diagnostic(HomeDiagnosticEvent{Code: "CANCELLED", Reason: reason})
	}
	return gone
}
func (r *liveRunner) progress(detail, decision string) {
	if r.onProgress != nil {
		r.onProgress(detail, decision)
	}
}
func (r *liveRunner) logStepError(message string) {
	r.progress(message, "")
	if message == r.lastStepError {
		return
	}
	r.lastStepError = message
	r.diagnostic(HomeDiagnosticEvent{Code: "SAFETY_NOTICE", Reason: "inspect_browser"})
	r.logf("activity", r.prefix+" "+message)
}
func hasDOMButton(buttons []string, label string) bool {
	for _, b := range buttons {
		if strings.EqualFold(strings.TrimSpace(b), label) {
			return true
		}
	}
	return false
}
func sleepLive(ctx context.Context, d time.Duration) bool {
	if ctx.Err() != nil {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}

// A diagnostic subscriber must never change chat control flow, even if faulty.
func (r *liveRunner) diagnostic(event HomeDiagnosticEvent) {
	if r.onDiagnostic == nil {
		return
	}
	defer func() { _ = recover() }()
	if event.Conversation == "" {
		event.Conversation = r.conversation
	}
	r.onDiagnostic(event)
}

func waitElapsed(start time.Time) map[string]float64 {
	if start.IsZero() {
		return nil
	}
	return map[string]float64{"timer_elapsed_ms": milliseconds(time.Since(start))}
}
