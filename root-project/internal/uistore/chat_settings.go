package uistore

import (
	"chitchatbot/internal/model"
	"encoding/json"
	"fmt"
	badger "github.com/dgraph-io/badger/v4"
)

const keyChatUnified = "settings/chat_shared/v1"

func sharedShow(g model.GeneralSettings, auto bool) model.ShowModeSettings {
	return model.ShowModeSettings{FirstMessage: g.FirstMessage, ReplyDelayMinSeconds: g.ReplyDelayMinSeconds, ReplyDelayMaxSeconds: g.ReplyDelayMaxSeconds, AutoSkip: auto, NoReplyMinSeconds: g.MatchReplyTimeoutMinSeconds, NoReplyMaxSeconds: g.MatchReplyTimeoutMaxSeconds, NextChatMinSeconds: g.MatchCloseWaitMinSeconds, NextChatMaxSeconds: g.MatchCloseWaitMaxSeconds}
}
func commonFromShow(g model.GeneralSettings, s model.ShowModeSettings) model.GeneralSettings {
	g.FirstMessage = s.FirstMessage
	g.ReplyDelayMinSeconds = s.ReplyDelayMinSeconds
	g.ReplyDelayMaxSeconds = s.ReplyDelayMaxSeconds
	g.MatchReplyTimeoutMinSeconds = s.NoReplyMinSeconds
	g.MatchReplyTimeoutMaxSeconds = s.NoReplyMaxSeconds
	g.MatchCloseWaitMinSeconds = s.NextChatMinSeconds
	g.MatchCloseWaitMaxSeconds = s.NextChatMaxSeconds
	return g
}
func ValidateChatSettings(g model.GeneralSettings) error {
	if err := g.FirstMessage.Validate(); err != nil {
		return err
	}
	for _, pair := range [][2]int{{g.PollMinSeconds, g.PollMaxSeconds}, {g.ActionMinSeconds, g.ActionMaxSeconds}, {g.ReplyDelayMinSeconds, g.ReplyDelayMaxSeconds}, {g.MatchReplyTimeoutMinSeconds, g.MatchReplyTimeoutMaxSeconds}, {g.MatchCloseWaitMinSeconds, g.MatchCloseWaitMaxSeconds}} {
		if pair[0] < 0 || pair[1] < pair[0] || pair[1] > 86400 {
			return fmt.Errorf("use whole seconds: 0 <= min <= max <= 86400")
		}
	}
	if g.PollMinSeconds < 1 || g.ActionMinSeconds < 1 || g.MatchReplyTimeoutMinSeconds < 1 {
		return fmt.Errorf("poll, action and no-reply timeout must be at least 1 second")
	}
	return nil
}
func (s *Store) chatUnifiedUnlocked() (bool, error) {
	var unified bool
	_, err := s.loadJSON(keyChatUnified, &unified)
	return unified, err
}
func (s *Store) ChatSettingsUnified() (bool, error) {
	s.showModeMu.Lock()
	defer s.showModeMu.Unlock()
	return s.chatUnifiedUnlocked()
}
func (s *Store) LoadChatSettings() (model.ChatSettings, error) {
	s.showModeMu.Lock()
	defer s.showModeMu.Unlock()
	g, err := s.LoadGeneralSettings()
	if err != nil {
		return model.ChatSettings{}, err
	}
	show, err := s.loadShowModeUnlocked()
	if err != nil {
		return model.ChatSettings{}, err
	}
	unified, err := s.chatUnifiedUnlocked()
	return model.ChatSettings{General: g, ShowAutoSkip: show.AutoSkip, Unified: unified, LegacyShow: show}, err
}
func (s *Store) SaveChatSettings(settings model.ChatSettings) error {
	s.showModeMu.Lock()
	defer s.showModeMu.Unlock()
	return s.saveChatPairUnlocked(settings.General, settings.ShowAutoSkip)
}

// Both existing engine views and the migration marker commit together. There is
// no half-save. Legacy API setters keep these shared fields synchronized too.
func (s *Store) saveChatPairUnlocked(g model.GeneralSettings, auto bool) error {
	if err := ValidateChatSettings(g); err != nil {
		return err
	}
	show := sharedShow(g, auto)
	if err := ValidateShowModeSettings(show); err != nil {
		return err
	}
	generalJSON, err := json.Marshal(g)
	if err != nil {
		return err
	}
	showJSON, err := json.Marshal(show)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set([]byte(keyGeneral), generalJSON); err != nil {
			return err
		}
		if err := txn.Set([]byte(keyShowMode), showJSON); err != nil {
			return err
		}
		return txn.Set([]byte(keyChatUnified), []byte("true"))
	})
}
