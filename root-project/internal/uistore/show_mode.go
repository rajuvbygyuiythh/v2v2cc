package uistore

import (
	"fmt"

	"chitchatbot/internal/model"
)

const keyShowMode = "settings/show_mode/v1"

func ValidateShowModeSettings(s model.ShowModeSettings) error {
	if err := s.FirstMessage.Validate(); err != nil {
		return err
	}
	for _, pair := range [][2]int{{s.ReplyDelayMinSeconds, s.ReplyDelayMaxSeconds}, {s.NoReplyMinSeconds, s.NoReplyMaxSeconds}, {s.NextChatMinSeconds, s.NextChatMaxSeconds}} {
		if pair[0] < 0 || pair[1] < pair[0] || pair[1] > 86400 {
			return fmt.Errorf("timing ranges must satisfy 0 <= min <= max <= 86400 seconds")
		}
	}
	if s.AutoSkip && s.NoReplyMinSeconds < 1 {
		return fmt.Errorf("auto skip requires a positive minimum timeout")
	}
	return nil
}

// Persist the migration before returning; subsequent legacy saves cannot affect it.
func (s *Store) LoadShowModeSettings() (model.ShowModeSettings, error) {
	s.showModeMu.Lock()
	defer s.showModeMu.Unlock()
	return s.loadShowModeUnlocked()
}
func (s *Store) loadShowModeUnlocked() (model.ShowModeSettings, error) {
	var canonical model.GeneralSettings
	if ok, err := s.loadJSON(keyChatCanonical, &canonical); err != nil {
		return model.ShowModeSettings{}, err
	} else if ok {
		if err := ValidateChatSettings(canonical); err != nil {
			return model.ShowModeSettings{}, err
		}
		return sharedShow(canonical, true), nil
	}
	var result model.ShowModeSettings
	ok, err := s.loadJSON(keyShowMode, &result)
	if err != nil || ok {
		return result, err
	}
	legacy, err := s.LoadGeneralSettings()
	if err != nil {
		return result, err
	}
	result = model.ShowModeSettings{ReplyDelayMinSeconds: legacy.ReplyDelayMinSeconds, ReplyDelayMaxSeconds: legacy.ReplyDelayMaxSeconds, NoReplyMinSeconds: 60, NoReplyMaxSeconds: 60}
	if result.ReplyDelayMinSeconds < 0 {
		result.ReplyDelayMinSeconds = 0
	}
	if result.ReplyDelayMaxSeconds < result.ReplyDelayMinSeconds {
		result.ReplyDelayMaxSeconds = result.ReplyDelayMinSeconds
	}
	if result.ReplyDelayMaxSeconds > 86400 {
		result.ReplyDelayMaxSeconds = 86400
		result.ReplyDelayMinSeconds = 0
	}
	return result, s.saveJSON(keyShowMode, result)
}
func (s *Store) SaveShowModeSettings(settings model.ShowModeSettings) error {
	s.showModeMu.Lock()
	defer s.showModeMu.Unlock()
	if err := ValidateShowModeSettings(settings); err != nil {
		return err
	}
	if err := s.migrateCanonicalUnlocked(); err != nil {
		return err
	}
	unified, err := s.chatUnifiedUnlocked()
	if err != nil {
		return err
	}
	if unified {
		general, err := s.LoadGeneralSettings()
		if err != nil {
			return err
		}
		return s.saveChatPairUnlocked(commonFromShow(general, settings), settings.AutoSkip)
	}
	return s.saveJSON(keyShowMode, settings)
}
