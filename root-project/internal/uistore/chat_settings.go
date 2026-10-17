package uistore

import (
	"chitchatbot/internal/model"
	"encoding/json"
	"errors"
	"fmt"
	badger "github.com/dgraph-io/badger/v4"
)

const keyChatUnified = "settings/chat_shared/v1"
const keyChatCanonical = "settings/chat_canonical/v2"

var errLegacyConflict = errors.New("legacy timing records conflict; review Settings and save the chosen values before starting")

func sharedShow(g model.GeneralSettings, auto bool) model.ShowModeSettings {
	return model.ShowModeSettings{FirstMessage: g.FirstMessage, ReplyDelayMinSeconds: g.ReplyDelayMinSeconds, ReplyDelayMaxSeconds: g.ReplyDelayMaxSeconds, AutoSkip: auto, NoReplyMinSeconds: g.MatchReplyTimeoutMinSeconds, NoReplyMaxSeconds: g.MatchReplyTimeoutMaxSeconds, NextChatMinSeconds: g.MatchCloseWaitMinSeconds, NextChatMaxSeconds: g.MatchCloseWaitMaxSeconds}.ThreeTimings()
}
func commonFromShow(g model.GeneralSettings, s model.ShowModeSettings) model.GeneralSettings {
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
	var canonical model.GeneralSettings
	if ok, err := s.loadJSON(keyChatCanonical, &canonical); err != nil || ok {
		return ok, err
	}
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
	migrationErr := s.migrateCanonicalUnlocked()
	if migrationErr != nil && !errors.Is(migrationErr, errLegacyConflict) {
		return model.ChatSettings{}, migrationErr
	}
	g, err := s.LoadGeneralSettings()
	if err != nil {
		return model.ChatSettings{}, err
	}
	show, err := s.loadShowModeUnlocked()
	if err != nil {
		return model.ChatSettings{}, err
	}
	unified, err := s.chatUnifiedUnlocked()
	source := "legacy_review"
	var canonical model.GeneralSettings
	if ok, e := s.loadJSON(keyChatCanonical, &canonical); e != nil {
		return model.ChatSettings{}, e
	} else if ok {
		source = "canonical_v2"
	}
	if errors.Is(migrationErr, errLegacyConflict) {
		unified = false
		source = "legacy_conflict"
	}
	return model.ChatSettings{General: g, ShowAutoSkip: true, Unified: unified, LegacyShow: show, Source: source}, err
}
func (s *Store) SaveChatSettings(settings model.ChatSettings) error {
	s.showModeMu.Lock()
	defer s.showModeMu.Unlock()
	return s.saveChatPairUnlocked(settings.General, settings.ShowAutoSkip)
}

// A single authoritative record is written. The original records are retained
// as migration backups, never re-read once canonical_v2 exists.
func (s *Store) saveChatPairUnlocked(g model.GeneralSettings, _ bool) error {
	if err := ValidateChatSettings(g); err != nil {
		return err
	}
	data, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set([]byte(keyChatCanonical), data); err != nil {
			return err
		}
		return txn.Set([]byte(keyChatUnified), []byte("true"))
	})
}
func (s *Store) migrateCanonicalUnlocked() error {
	var canonical model.GeneralSettings
	if ok, err := s.loadJSON(keyChatCanonical, &canonical); err != nil {
		return err
	} else if ok {
		return ValidateChatSettings(canonical)
	}
	var unified bool
	if _, err := s.loadJSON(keyChatUnified, &unified); err != nil || !unified {
		return err
	}
	g, err := s.LoadGeneralSettings()
	if err != nil {
		return err
	}
	show, err := s.loadShowModeUnlocked()
	if err != nil {
		return err
	}
	if commonFromShow(g, show) != g {
		return errLegacyConflict
	}
	return s.saveChatPairUnlocked(g, true)
}

// ResolveChatSettings is shared by both Start paths. Conflicting v1 records
// require explicit review instead of silently picking a mode-dependent value.
func (s *Store) ResolveChatSettings(mode string) (model.ExecutionSettings, error) {
	if mode != model.ModeStandard && mode != model.ModeShow {
		return model.ExecutionSettings{}, fmt.Errorf("unsupported execution mode")
	}
	s.showModeMu.Lock()
	defer s.showModeMu.Unlock()
	if err := s.migrateCanonicalUnlocked(); err != nil {
		return model.ExecutionSettings{}, err
	}
	g, err := s.LoadGeneralSettings()
	if err != nil {
		return model.ExecutionSettings{}, err
	}
	result := model.ExecutionSettings{Mode: mode, Source: "legacy_general"}
	var canonical model.GeneralSettings
	if ok, err := s.loadJSON(keyChatCanonical, &canonical); err != nil {
		return result, err
	} else if ok {
		result.Source = "canonical_v2"
	}
	if mode == model.ModeStandard {
		g.FirstMessage = g.FirstMessage.Effective()
		if err := ValidateChatSettings(g); err != nil {
			return result, err
		}
		result.Standard = &g
	} else {
		show, err := s.loadShowModeUnlocked()
		if err != nil {
			return result, err
		}
		show = show.ThreeTimings()
		if err := ValidateShowModeSettings(show); err != nil {
			return result, err
		}
		result.Show = &show
		if result.Source != "canonical_v2" {
			result.Source = "legacy_show"
		}
	}
	return result, nil
}
