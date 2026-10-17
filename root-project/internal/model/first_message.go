package model

import (
	"fmt"
	"math/rand"
	"time"
)

// FirstMessageSettings retains Standard's saved policy and Show's derived
// opener/idle snapshot. Show no longer exposes editable first-turn controls.
type FirstMessageSettings struct {
	Mode            string `json:"mode"`
	BotFirstPercent int    `json:"bot_first_percent"`
	WaitTimeout     bool   `json:"wait_timeout"`
	WaitMinSeconds  int    `json:"wait_min_seconds"`
	WaitMaxSeconds  int    `json:"wait_max_seconds"`
}

func DefaultFirstMessageSettings() FirstMessageSettings {
	return FirstMessageSettings{Mode: "bot", BotFirstPercent: 70, WaitMinSeconds: 30, WaitMaxSeconds: 60}
}
func (s FirstMessageSettings) Effective() FirstMessageSettings {
	if s.Mode == "" {
		return DefaultFirstMessageSettings()
	}
	return s
}
func (s FirstMessageSettings) Validate() error {
	s = s.Effective()
	if s.Mode != "bot" && s.Mode != "user" && s.Mode != "random" {
		return fmt.Errorf("unknown first-message mode")
	}
	if s.BotFirstPercent < 0 || s.BotFirstPercent > 100 {
		return fmt.Errorf("bot-first chance must be 0–100 percent")
	}
	if s.WaitMinSeconds < 0 || s.WaitMaxSeconds < s.WaitMinSeconds || s.WaitMaxSeconds > 86400 {
		return fmt.Errorf("invalid first-message wait range")
	}
	if s.WaitTimeout && s.WaitMinSeconds < 1 {
		return fmt.Errorf("first-message timeout requires a positive minimum")
	}
	return nil
}
func (s FirstMessageSettings) ChooseBotFirst(rng *rand.Rand) bool {
	s = s.Effective()
	switch s.Mode {
	case "user":
		return false
	case "random":
		if s.BotFirstPercent <= 0 {
			return false
		}
		if s.BotFirstPercent >= 100 {
			return true
		}
		return rng != nil && rng.Intn(100) < s.BotFirstPercent
	default:
		return true
	}
}
func (s FirstMessageSettings) WaitDuration(rng *rand.Rand) time.Duration {
	s = s.Effective()
	seconds := time.Duration(s.WaitMinSeconds) * time.Second
	if s.WaitMaxSeconds > s.WaitMinSeconds && rng != nil {
		seconds += time.Duration(rng.Int63n(int64(time.Duration(s.WaitMaxSeconds-s.WaitMinSeconds) * time.Second)))
	}
	return seconds
}

// Persisted per account + conversation so polling/restarts do not reroll.
type FirstTurnState struct {
	BotFirst bool      `json:"bot_first"`
	Deadline time.Time `json:"deadline,omitempty"`
}
