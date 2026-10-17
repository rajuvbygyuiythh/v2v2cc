package main

// Show Mode lifecycle only. Browser login/drivers and the standard chat engine
// remain separate; both desktop paths share an account ownership guard.
import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"chitchatbot/internal/bot"
	"chitchatbot/internal/browser"
	"chitchatbot/internal/model"
	"chitchatbot/internal/store"
	"chitchatbot/internal/uistore"
)

type ShowBrowserView struct {
	BlockedBy   string                  `json:"blocked_by,omitempty"`
	ID          string                  `json:"id"`
	Profile     string                  `json:"profile"`
	Account     string                  `json:"account"`
	Status      string                  `json:"status"`
	Detail      string                  `json:"detail"`
	Decision    string                  `json:"decision"`
	MessageMode string                  `json:"message_mode"`
	Settings    *model.ShowModeSettings `json:"settings,omitempty"`
}
type ShowControlState struct {
	Revision uint64                 `json:"revision"`
	Settings model.ShowModeSettings `json:"settings"`
	Active   bool                   `json:"active"`
	Browsers []ShowBrowserView      `json:"browsers"`
}
type showBrowserSession struct {
	view       ShowBrowserView
	ctx        context.Context
	cancel     context.CancelFunc
	backend    browser.Backend
	dom        browser.DOMChatBackend
	chatCancel context.CancelFunc
	chatDone   chan struct{}
}

func (a *App) showControlState() (ShowControlState, error) {
	a.showMu.Lock()
	defer a.showMu.Unlock()
	settings, err := a.uiStore.LoadShowModeSettings()
	if err != nil {
		return ShowControlState{}, err
	}
	if a.showRevision == 0 {
		a.showRevision = 1
	}
	result := ShowControlState{Revision: a.showRevision, Settings: settings, Browsers: []ShowBrowserView{}}
	for _, s := range a.showSessions {
		view := s.view
		if owner := a.automationOwners[view.Account]; owner != "" && owner != view.ID {
			view.BlockedBy = owner
		}
		if view.Settings != nil {
			copy := *view.Settings
			view.Settings = &copy
		}
		result.Browsers = append(result.Browsers, view)
		result.Active = result.Active || s.chatDone != nil
	}
	sort.Slice(result.Browsers, func(i, j int) bool { return result.Browsers[i].ID < result.Browsers[j].ID })
	return result, nil
}
func (a *App) saveShowSettings(settings model.ShowModeSettings) error {
	a.showMu.Lock()
	defer a.showMu.Unlock()
	for _, s := range a.showSessions {
		if s.chatDone != nil {
			return fmt.Errorf("stop all Show chats before changing Show Mode settings")
		}
	}
	if err := a.uiStore.SaveShowModeSettings(settings); err != nil {
		return err
	}
	if a.showRevision == 0 {
		a.showRevision = 1
	}
	a.showRevision++
	return nil
}
func (a *App) registerShowBrowser(ctx context.Context, backend browser.Backend, profile string, index, total int) *showBrowserSession {
	ctx, cancel := context.WithCancel(ctx)
	a.showMu.Lock()
	defer a.showMu.Unlock()
	if a.showSessions == nil {
		a.showSessions = make(map[string]*showBrowserSession)
	}
	// Keep only a bounded display history; never evict an open browser.
	if len(a.showSessions) >= 12 {
		for id, s := range a.showSessions {
			if s.view.Status == "closed" {
				delete(a.showSessions, id)
			}
		}
	}
	a.showSerial++
	id := fmt.Sprintf("show-%06d", a.showSerial)
	dom, _ := backend.(browser.DOMChatBackend)
	s := &showBrowserSession{ctx: ctx, cancel: cancel, backend: backend, dom: dom, view: ShowBrowserView{ID: id, Profile: fmt.Sprintf("%s (%d/%d)", profile, index, total), Status: "login_pending", Detail: "Login required — automation is OFF"}}
	a.showSessions[id] = s
	return s
}
func (a *App) showLoginReady(s *showBrowserSession, login model.LoginInfo) {
	a.showMu.Lock()
	defer a.showMu.Unlock()
	s.view.Account = login.CurrentUserID()
	s.view.Status = "ready"
	s.view.Detail = "Logged in — chat stopped. Press Start Show Chat."
	if s.dom == nil || s.view.Account == "" {
		s.view.Status = "error"
		s.view.Detail = "DOM chat or account identity unavailable; no automation started"
	}
}
func (a *App) detachShowBrowser(s *showBrowserSession) {
	s.cancel()
	_ = a.stopShowChat(s.view.ID)
	a.showMu.Lock()
	defer a.showMu.Unlock()
	s.view.Status = "closed"
	s.view.Detail = "Browser closed — chat stopped"
}
func (a *App) waitShowBrowser(s *showBrowserSession) {
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
		if s.dom == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 750*time.Millisecond)
		_, err := s.dom.ReadDOMSnapshot(ctx)
		cancel()
		if (&liveRunner{}).pageGone(err) {
			s.cancel()
			return
		}
	}
}

// Never release an account until the owning goroutine has completely drained.
func (a *App) acquireAutomation(account, owner string) (func(), error) {
	a.showMu.Lock()
	defer a.showMu.Unlock()
	return a.acquireAutomationLocked(account, owner)
}
func (a *App) acquireAutomationLocked(account, owner string) (func(), error) {
	if account == "" {
		return nil, fmt.Errorf("account identity is missing; cannot safely start automation")
	}
	if a.automationOwners == nil {
		a.automationOwners = make(map[string]string)
	}
	if other := a.automationOwners[account]; other != "" {
		return nil, fmt.Errorf("account already controlled by %s; stop it before starting another mode", other)
	}
	a.automationOwners[account] = owner
	var once sync.Once
	return func() {
		once.Do(func() {
			a.showMu.Lock()
			defer a.showMu.Unlock()
			if a.automationOwners[account] == owner {
				delete(a.automationOwners, account)
			}
		})
	}, nil
}
func (a *App) checkStandardConflicts(configs []bot.Config) error {
	for _, cfg := range configs {
		path := cfg.LoginInfoPath
		if path == "" {
			path = filepath.Join(cfg.DataDir, "profiles", safeProfileName(cfg.Profile), "login_info.json")
		}
		var login model.LoginInfo
		ok, err := store.LoadJSON(path, &login)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		account := login.CurrentUserID()
		a.showMu.Lock()
		owner := a.automationOwners[account]
		a.showMu.Unlock()
		if owner != "" {
			return fmt.Errorf("cannot start Standard Bot: account already controlled by %s", owner)
		}
	}
	return nil
}
func (a *App) startShowChat(id string, revision uint64) error {
	a.showMu.Lock()
	s := a.showSessions[id]
	if s == nil || s.ctx.Err() != nil || s.view.Status != "ready" || s.chatDone != nil {
		a.showMu.Unlock()
		return fmt.Errorf("browser must be logged in and chat stopped before Start")
	}
	if a.showRevision == 0 {
		a.showRevision = 1
	}
	if revision != a.showRevision {
		a.showMu.Unlock()
		return fmt.Errorf("saved settings changed; refresh and review before starting")
	}
	settings, err := a.uiStore.LoadShowModeSettings()
	if err == nil {
		err = uistore.ValidateShowModeSettings(settings)
	}
	var messages model.MessageSettings
	if err == nil {
		messages, err = a.uiStore.LoadMessageSettings()
	}
	if err != nil {
		a.showMu.Unlock()
		return err
	}
	release, err := a.acquireAutomationLocked(s.view.Account, s.view.ID)
	if err != nil {
		a.showMu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.chatCancel = cancel
	s.chatDone = done
	s.view.Status = "starting"
	s.view.Detail = "Preparing saved settings and baseline; no automatic opener yet"
	settings.FirstMessage = settings.FirstMessage.Effective()
	copy := settings
	s.view.Settings = &copy
	s.view.MessageMode = messages.EffectiveChatMode()
	s.view.Decision = ""
	a.showMu.Unlock()
	go a.executeShowChat(s, ctx, done, settings, messages, release)
	return nil
}
func (a *App) executeShowChat(s *showBrowserSession, ctx context.Context, done chan struct{}, settings model.ShowModeSettings, messages model.MessageSettings, release func()) {
	detail := "Chat stopped — browser remains open. Start again to use saved settings."
	defer func() {
		release()
		a.showMu.Lock()
		if s.chatDone == done {
			s.chatCancel()
			s.chatCancel = nil
			s.chatDone = nil
			s.view.Settings = nil
			s.view.Decision = ""
			s.view.Status = "ready"
			s.view.Detail = detail
		}
		close(done)
		a.showMu.Unlock()
	}()
	// Validate the message engine before any side effect. Freeze message settings
	// too: later Message-page saves cannot change this run halfway through a chat.
	if _, _, err := a.newLiveResponderWithSettings(messages); err != nil {
		detail = "Cannot start: " + err.Error()
		return
	}
	baselineCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	baseline, err := s.dom.ReadDOMSnapshot(baselineCtx)
	cancel()
	if err != nil {
		detail = "Cannot start: DOM baseline unavailable; " + err.Error()
		return
	}
	if ctx.Err() != nil {
		return
	}
	observed, stopTranscript := a.startHomeChatObserver(ctx, s.dom, s.view.Profile, 1, 1)
	defer stopTranscript()
	min, max := showModeReplyDelayRange(settings)
	r := &liveRunner{dom: observed, prefix: "[" + s.view.ID + "]", logf: a.log, newResponder: func() (liveResponder, string, error) { return a.newLiveResponderWithSettings(messages) }, firstMessage: settings.FirstMessage, replyDelayMin: min, replyDelayMax: max, replyDelayRNG: rand.New(rand.NewSource(time.Now().UnixNano())), autoSkip: settings.AutoSkip, noReplyMin: time.Duration(settings.NoReplyMinSeconds) * time.Second, noReplyMax: time.Duration(settings.NoReplyMaxSeconds) * time.Second, nextChatMin: time.Duration(settings.NextChatMinSeconds) * time.Second, nextChatMax: time.Duration(settings.NextChatMaxSeconds) * time.Second, pollInterval: 350 * time.Millisecond, initialBaseline: &baseline}
	r.onProgress = func(detail, decision string) {
		a.showMu.Lock()
		defer a.showMu.Unlock()
		if s.chatDone == done && ctx.Err() == nil {
			s.view.Status = "running"
			s.view.Detail = detail
			if decision != "" {
				s.view.Decision = decision
			}
		}
	}
	r.onProgress("Chat running — saved settings active", "")
	r.run(ctx)
	if ctx.Err() == nil {
		s.cancel()
	} // A runner ending without Stop means its page became unavailable.
}
func (a *App) stopShowChat(id string) error {
	a.showMu.Lock()
	s := a.showSessions[id]
	if s == nil {
		a.showMu.Unlock()
		return fmt.Errorf("unknown Show browser")
	}
	done := s.chatDone
	if done != nil {
		s.view.Status = "stopping"
		s.view.Detail = "Stopping — waiting for pending automation to drain"
		s.chatCancel()
	}
	a.showMu.Unlock()
	if done != nil {
		<-done
	}
	return nil
}
func (a *App) stopAllShowBrowsers() {
	a.showMu.Lock()
	sessions := make([]*showBrowserSession, 0, len(a.showSessions))
	for _, s := range a.showSessions {
		sessions = append(sessions, s)
		s.cancel()
	}
	a.showMu.Unlock()
	for _, s := range sessions {
		_ = a.stopShowChat(s.view.ID)
	}
}
