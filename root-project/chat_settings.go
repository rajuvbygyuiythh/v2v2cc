package main

import (
	"chitchatbot/internal/model"
	"fmt"
)

func (a *App) getChatSettings() (model.ChatSettings, error) {
	settings, err := a.uiStore.LoadChatSettings()
	settings.ShowAutoSkip = true
	return settings, err
}
func (a *App) chatSettingsIdleLocked() error {
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		return fmt.Errorf("stop Standard Bot before saving shared settings")
	}
	for _, s := range a.showSessions {
		if s.chatDone != nil {
			return fmt.Errorf("stop all Show chats before saving shared settings")
		}
	}
	if len(a.automationOwners) > 0 {
		return fmt.Errorf("wait for automation workers to finish stopping before saving")
	}
	return nil
}
func (a *App) saveChatSettings(settings model.ChatSettings) error {
	a.showMu.Lock()
	defer a.showMu.Unlock()
	if err := a.chatSettingsIdleLocked(); err != nil {
		return err
	}
	// The removed Show first-turn controls must not silently rewrite Standard's
	// existing policy when saving only the three timing ranges.
	saved, err := a.uiStore.LoadGeneralSettings()
	if err != nil {
		return err
	}
	switch settings.EditMode {
	case "": // Preserve the pre-edit_mode API: timings/poll/action, never first policy.
		settings.General.FirstMessage = saved.FirstMessage
	case model.ModeShow: // Explicit Show edits can only change the three shared timings.
		settings.General.FirstMessage = saved.FirstMessage
		settings.General.PollMinSeconds = saved.PollMinSeconds
		settings.General.PollMaxSeconds = saved.PollMaxSeconds
		settings.General.ActionMinSeconds = saved.ActionMinSeconds
		settings.General.ActionMaxSeconds = saved.ActionMaxSeconds
	case model.ModeStandard:
	default:
		return fmt.Errorf("unsupported settings edit mode")
	}
	settings.ShowAutoSkip = true
	if err := a.uiStore.SaveChatSettings(settings); err != nil {
		return err
	}
	if a.showRevision == 0 {
		a.showRevision = 1
	}
	a.showRevision++
	return nil
}

// Caller holds showMu and selects mode explicitly; never infer Show from Rod.
func (a *App) resolveExecutionSettings(mode string) (model.ExecutionSettings, error) {
	if mode == model.ModeLogin {
		browser, err := a.uiStore.LoadBrowserSettings()
		return model.ExecutionSettings{Mode: mode, Driver: browser.Backend, Purpose: "login capture only; no chat automation", Source: "standard_browser_preferences"}, err
	}
	result, err := a.uiStore.ResolveChatSettings(mode)
	if err != nil {
		return result, err
	}
	if a.showRevision == 0 {
		a.showRevision = 1
	}
	result.Revision = a.showRevision
	if mode == model.ModeShow {
		result.Driver = "rod"
		result.Purpose = "visible DOM chat; next Start preview"
	} else {
		browser, err := a.uiStore.LoadBrowserSettings()
		if err != nil {
			return result, err
		}
		result.Driver = browser.Backend
		result.Purpose = "Standard workers; next Start preview"
	}
	return result, nil
}
