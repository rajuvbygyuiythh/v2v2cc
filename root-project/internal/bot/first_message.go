package bot

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"chitchatbot/internal/api"
	"chitchatbot/internal/model"
)

func workerFirstTurn(cfg Config, state *model.AccountState, conversationID string, rng *rand.Rand) model.FirstTurnState {
	if state.FirstTurns == nil {
		state.FirstTurns = make(map[string]model.FirstTurnState)
	}
	if turn, ok := state.FirstTurns[conversationID]; ok {
		return turn
	}
	policy := cfg.FirstMessage.Effective()
	turn := model.FirstTurnState{BotFirst: policy.ChooseBotFirst(rng)}
	if !turn.BotFirst && policy.WaitTimeout {
		turn.Deadline = time.Now().Add(policy.WaitDuration(rng))
	}
	state.FirstTurns[conversationID] = turn
	return turn
}

func sleepFirstReply(ctx context.Context, cfg Config, rng *rand.Rand) error {
	delay := cfg.ReplyDelayMin
	if cfg.ReplyDelayMax > delay && rng != nil {
		delay += time.Duration(rng.Int63n(int64(cfg.ReplyDelayMax - delay)))
	}
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Wrap only the first turn; existing message engines keep their later loops.
func executeMatchChatSequence(ctx context.Context, client *api.Client, cfg Config, p paths, login model.LoginInfo, currentUserID string, target model.DMTarget, rng *rand.Rand, events <-chan realtimeEvent) (bool, error) {
	state, key, account, err := loadBotAccountState(p.BotState, currentUserID)
	if err != nil {
		return false, err
	}
	if account.SentMessageCounts[target.UserID] == 0 {
		turn := workerFirstTurn(cfg, &account, target.ConversationID, rng)
		if err := saveBotAccountState(p.BotState, state, key, account); err != nil {
			return false, err
		}
		incoming, closed, err := firstMatchInput(ctx, client, cfg, target, currentUserID, account, turn, events)
		if err != nil {
			return true, err
		}
		if closed {
			return true, nil
		}
		cfg.firstIncoming = incoming
		if incoming != nil {
			cfg.firstIncomingID = incoming.ID
		}
	}
	return executeMatchChatSequenceBody(ctx, client, cfg, p, login, currentUserID, target, rng, events)
}

func firstEventMessage(event realtimeEvent, target model.DMTarget) *model.Message {
	if event.Type != "chatMessage" || event.ConversationID != target.ConversationID || event.UserID != target.UserID || event.MessageID == "" || event.Content == "" {
		return nil
	}
	message := &model.Message{ID: event.MessageID, Content: event.Content, CreatedAt: event.CreatedAt}
	message.Author.ID = event.UserID
	return message
}

// Check fresh history before opening and on timeout. No new transport is added.
func firstMatchInput(ctx context.Context, client *api.Client, cfg Config, target model.DMTarget, currentUserID string, account model.AccountState, turn model.FirstTurnState, events <-chan realtimeEvent) (*model.Message, bool, error) {
	checkHistory := func() (*model.Message, error) {
		messages, status, err := fetchConversationMessages(ctx, client, cfg, target.ConversationID)
		if err != nil {
			return nil, err
		}
		if status == 401 {
			return nil, errLoginExpired
		}
		if status >= 400 {
			return nil, fmt.Errorf("first-message history check failed: HTTP %d", status)
		}
		for _, message := range incomingMessages(messages, currentUserID) {
			if message.Author.ID != target.UserID {
				continue
			}
			if _, seen := account.IncomingMessages[message.ID]; !seen {
				copy := message
				return &copy, nil
			}
		}
		return nil, nil
	}
	if incoming, err := checkHistory(); incoming != nil || err != nil {
		return incoming, false, err
	}
	// Drain already queued events before allowing a bot-first opener.
	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case event, ok := <-events:
			if !ok {
				return nil, true, nil
			}
			if event.ConversationID == target.ConversationID && event.Closed {
				return nil, true, nil
			}
			if incoming := firstEventMessage(event, target); incoming != nil {
				return incoming, false, nil
			}
		default:
			goto drained
		}
	}
drained:
	if turn.BotFirst {
		return nil, false, nil
	}
	var timeout <-chan time.Time
	var timer *time.Timer
	if !turn.Deadline.IsZero() {
		timer = time.NewTimer(time.Until(turn.Deadline))
		timeout = timer.C
		defer timer.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timeout:
			// A ready realtime message wins even if select chose the ready timer.
			for {
				select {
				case event, ok := <-events:
					if !ok {
						return nil, true, nil
					}
					if event.ConversationID == target.ConversationID && event.Closed {
						return nil, true, nil
					}
					if message := firstEventMessage(event, target); message != nil {
						return message, false, nil
					}
				default:
					goto timeoutDrained
				}
			}
		timeoutDrained:
			incoming, err := checkHistory()
			return incoming, incoming == nil, err
		case event, ok := <-events:
			if !ok {
				return nil, true, nil
			}
			if event.ConversationID == target.ConversationID && event.Closed {
				return nil, true, nil
			}
			if incoming := firstEventMessage(event, target); incoming != nil {
				return incoming, false, nil
			}
		}
	}
}
