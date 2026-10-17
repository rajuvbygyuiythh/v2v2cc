package main

// Display-only structured diagnostics. No SMS bodies, credentials, raw errors,
// transport payloads or persistence. Event append never waits for the UI reader.
import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"chitchatbot/internal/model"
)

const diagnosticRuns = 12
const diagnosticEvents = 500

type HomeDiagnosticEvent struct {
	DetectedAt   string             `json:"detected_at,omitempty"`
	Sequence     uint64             `json:"sequence"`
	Time         string             `json:"time"`
	Code         string             `json:"code"`
	Reason       string             `json:"reason,omitempty"`
	Conversation string             `json:"conversation,omitempty"`
	Reply        uint64             `json:"reply,omitempty"`
	Part         int                `json:"part,omitempty"`
	WaitKind     string             `json:"wait_kind,omitempty"`
	Metrics      map[string]float64 `json:"metrics,omitempty"`
}
type HomeDiagnosticRun struct {
	ID          string                  `json:"id"`
	Browser     string                  `json:"browser"`
	Profile     string                  `json:"profile"`
	Account     string                  `json:"account"`
	Scope       string                  `json:"scope"`
	Mode        string                  `json:"mode"`
	MessageMode string                  `json:"message_mode"`
	Revision    uint64                  `json:"revision"`
	Started     string                  `json:"started"`
	Ended       string                  `json:"ended,omitempty"`
	Settings    *model.ShowModeSettings `json:"settings,omitempty"`
	Dropped     int                     `json:"dropped"`
	Cleared     int                     `json:"cleared"`
}
type HomeDiagnosticsSnapshot struct {
	Version  uint64                `json:"version"`
	Selected string                `json:"selected"`
	Runs     []HomeDiagnosticRun   `json:"runs"`
	Events   []HomeDiagnosticEvent `json:"events"`
	Evicted  int                   `json:"evicted"`
	Missed   uint64                `json:"missed"`
}
type diagnosticRunData struct {
	info   HomeDiagnosticRun
	events []HomeDiagnosticEvent
}
type homeDiagnosticsFeed struct {
	mu                        sync.Mutex
	missed                    atomic.Uint64
	serial, sequence, version uint64
	order                     []string
	runs                      map[string]*diagnosticRunData
	evicted                   int
}

func diagnosticCode(code string) bool {
	switch code {
	case "LOGIN_PENDING", "READY", "BROWSER_CLOSED", "STARTED", "START_FAILED", "STOP_REQUESTED", "STOPPED", "ACCOUNT_CONFLICT", "CHAT_STARTED", "CONVERSATION_CHANGED", "HISTORY_IGNORED", "FIRST_TURN", "FIRST_TURN_OVERRIDE", "FIRST_WAIT_ARMED", "FIRST_TIMEOUT", "INCOMING_DETECTED", "REPLY_WAIT", "WAIT_FINISHED", "REPLY_EMPTY", "SEND_STARTED", "SEND_CONFIRMED", "SEND_UNCONFIRMED", "CANCELLED", "NO_REPLY_ARMED", "NO_REPLY_TIMEOUT", "NEXT_WAIT_ARMED", "SKIP_CLICKED", "START_CLICKED", "BUTTON_UNCONFIRMED", "SAFETY_NOTICE":
		return true
	}
	return false
}
func diagnosticReason(reason string) string {
	switch reason {
	case "bot_first", "user_first", "history_ignored", "incoming_already_present", "conversation_changed", "stop_or_cancel", "page_unavailable", "message_engine", "baseline_unavailable", "account_busy", "not_ready", "settings_changed", "settings_invalid", "inspect_browser", "confirmed", "unconfirmed", "flow_no_output", "skip", "confirm", "start":
		return reason
	}
	return ""
}
func cloneDiagnosticEvent(e HomeDiagnosticEvent) HomeDiagnosticEvent {
	result := e
	result.DetectedAt = ""
	if e.Code == "INCOMING_DETECTED" {
		if stamp, err := time.Parse(time.RFC3339Nano, e.DetectedAt); err == nil {
			result.DetectedAt = stamp.UTC().Format(time.RFC3339Nano)
		}
	}
	result.Metrics = nil
	for _, key := range []string{"selected_ms", "configured_min_ms", "configured_max_ms", "actual_wait_ms", "timer_elapsed_ms", "queue_ms", "detected_to_send_ms", "detected_to_confirmed_ms", "send_ms", "pending", "count"} {
		if value, ok := e.Metrics[key]; ok && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			if result.Metrics == nil {
				result.Metrics = map[string]float64{}
			}
			result.Metrics[key] = value
		}
	}
	return result
}
func cloneDiagnosticRun(info HomeDiagnosticRun) HomeDiagnosticRun {
	if info.Settings != nil {
		copy := *info.Settings
		info.Settings = &copy
	}
	return info
}
func (f *homeDiagnosticsFeed) start(browser, profile, account, scope, messageMode string, revision uint64, settings *model.ShowModeSettings) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runs == nil {
		f.runs = make(map[string]*diagnosticRunData)
	}
	f.serial++
	id := fmt.Sprintf("diagnostic-%06d", f.serial)
	info := HomeDiagnosticRun{ID: id, Browser: browser, Profile: profile, Account: account, Scope: scope, Mode: "Show Mode — Rod (visible)", MessageMode: messageMode, Revision: revision, Started: time.Now().UTC().Format(time.RFC3339Nano), Settings: settings}
	f.runs[id] = &diagnosticRunData{info: cloneDiagnosticRun(info), events: []HomeDiagnosticEvent{}}
	f.order = append(f.order, id)
	for len(f.order) > diagnosticRuns {
		delete(f.runs, f.order[0])
		f.order = f.order[1:]
		f.evicted++
	}
	f.version++
	return id
}
func (f *homeDiagnosticsFeed) account(id, account string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if run := f.runs[id]; run != nil {
		run.info.Account = account
		f.version++
	}
}
func (f *homeDiagnosticsFeed) end(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if run := f.runs[id]; run != nil && run.info.Ended == "" {
		run.info.Ended = time.Now().UTC().Format(time.RFC3339Nano)
		f.version++
	}
}
func (f *homeDiagnosticsFeed) record(id string, event HomeDiagnosticEvent) {
	if !diagnosticCode(event.Code) {
		return
	}
	if !f.mu.TryLock() {
		f.missed.Add(1)
		return
	}
	defer f.mu.Unlock()
	run := f.runs[id]
	if run == nil {
		return
	} // Never resurrect evicted display history.
	event = cloneDiagnosticEvent(event)
	event.Reason = diagnosticReason(event.Reason)
	switch event.WaitKind {
	case "reply", "follow_up", "first_message", "no_reply", "next_chat":
	default:
		event.WaitKind = ""
	}
	if len(event.Conversation) > 256 {
		event.Conversation = ""
	}
	f.sequence++
	event.Sequence = f.sequence
	event.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if len(run.events) == diagnosticEvents {
		copy(run.events, run.events[1:])
		run.events[len(run.events)-1] = event
		run.info.Dropped++
	} else {
		run.events = append(run.events, event)
	}
	f.version++
}
func (f *homeDiagnosticsFeed) snapshot(selected, conversation string) HomeDiagnosticsSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := HomeDiagnosticsSnapshot{Version: f.version, Selected: selected, Runs: []HomeDiagnosticRun{}, Events: []HomeDiagnosticEvent{}, Evicted: f.evicted, Missed: f.missed.Load()}
	for _, id := range f.order {
		out.Runs = append(out.Runs, cloneDiagnosticRun(f.runs[id].info))
	}
	run := f.runs[selected]
	if selected == "" && len(f.order) > 0 {
		out.Selected = f.order[len(f.order)-1]
		run = f.runs[out.Selected]
	}
	// An evicted explicit selection stays empty rather than silently mixing runs.
	if run != nil {
		for _, event := range run.events {
			if conversation == "" || event.Conversation == "" || event.Conversation == conversation {
				out.Events = append(out.Events, cloneDiagnosticEvent(event))
			}
		}
	}
	return out
}
func (f *homeDiagnosticsFeed) clear(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	run := f.runs[id]
	if run == nil {
		return fmt.Errorf("diagnostic run is no longer retained")
	}
	run.info.Cleared += len(run.events)
	run.events = []HomeDiagnosticEvent{}
	f.version++
	return nil
}
func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
