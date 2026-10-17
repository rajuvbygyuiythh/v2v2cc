package model

import (
	"math/rand"
	"testing"
	"time"
)

func TestFirstMessageDefaultsAndProbability(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	if !(FirstMessageSettings{}).ChooseBotFirst(rng) {
		t.Fatal("legacy default changed")
	}
	for i := 0; i < 500; i++ {
		if (FirstMessageSettings{Mode: "user"}).ChooseBotFirst(rng) || (FirstMessageSettings{Mode: "random", BotFirstPercent: 0}).ChooseBotFirst(rng) {
			t.Fatal("user-first sent opener")
		}
		if !(FirstMessageSettings{Mode: "random", BotFirstPercent: 100}).ChooseBotFirst(rng) {
			t.Fatal("100% did not open")
		}
	}
	s := FirstMessageSettings{Mode: "random", BotFirstPercent: 70, WaitMinSeconds: 2, WaitMaxSeconds: 5}
	count := 0
	for i := 0; i < 10000; i++ {
		if s.ChooseBotFirst(rng) {
			count++
		}
		d := s.WaitDuration(rng)
		if d < 2*time.Second || d >= 5*time.Second {
			t.Fatal(d)
		}
	}
	if count < 6500 || count > 7500 {
		t.Fatal("unexpected seeded probability", count)
	}
}
func TestFirstMessageValidation(t *testing.T) {
	for _, s := range []FirstMessageSettings{{Mode: "oops"}, {Mode: "random", BotFirstPercent: 101}, {Mode: "bot", BotFirstPercent: -1}, {Mode: "user", WaitTimeout: true}, {Mode: "user", WaitMinSeconds: 3, WaitMaxSeconds: 1}, {Mode: "user", WaitMaxSeconds: 86401}} {
		if s.Validate() == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
	if err := DefaultFirstMessageSettings().Validate(); err != nil {
		t.Fatal(err)
	}
}
