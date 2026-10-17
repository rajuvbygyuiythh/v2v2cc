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
	settings.General.FirstMessage = saved.FirstMessage
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
