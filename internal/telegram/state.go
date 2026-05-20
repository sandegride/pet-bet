package telegram

import "sync"

type BetState struct {
	MatchID      int64
	SelectedTeam string
}

type StateStore struct {
	mu     sync.Mutex
	states map[int64]BetState
}

func NewStateStore() *StateStore {
	// MVP keeps short-lived Telegram dialogue state in memory.
	// In production this should move to PostgreSQL or Redis to survive restarts.
	return &StateStore{states: make(map[int64]BetState)}
}

func (s *StateStore) SetBetState(telegramID int64, state BetState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[telegramID] = state
}

func (s *StateStore) GetBetState(telegramID int64) (BetState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[telegramID]
	return state, ok
}

func (s *StateStore) Clear(telegramID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.states, telegramID)
}
