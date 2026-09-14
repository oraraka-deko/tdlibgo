package memory

import (
	"context"
	"sync"
	"time"

	"tdlibgo/internal/domain"
)

// AdminStore is an in-memory implementation of CommandRepository and RestrictionStore.
type AdminStore struct {
	mu       sync.RWMutex
	commands map[string]domain.AdminCommand
	freezes  map[int64]domain.AccountFreeze
}

// NewAdminStore creates a new in-memory AdminStore.
func NewAdminStore() *AdminStore {
	return &AdminStore{
		commands: make(map[string]domain.AdminCommand),
		freezes:  make(map[int64]domain.AccountFreeze),
	}
}

func (s *AdminStore) BeginCommand(ctx context.Context, cmd domain.AdminCommand) (domain.AdminCommand, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.commands[cmd.CommandID]; ok {
		return existing, false, nil
	}
	if cmd.CreatedAt.IsZero() {
		cmd.CreatedAt = time.Now()
	}
	s.commands[cmd.CommandID] = cmd
	return cmd, true, nil
}

func (s *AdminStore) FinishCommand(ctx context.Context, commandID string, status domain.AdminCommandStatus, resultJSON []byte, errorText string) (domain.AdminCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd, ok := s.commands[commandID]
	if !ok {
		cmd = domain.AdminCommand{CommandID: commandID}
	}
	cmd.Status = status
	cmd.ResultJSON = resultJSON
	cmd.Error = errorText
	now := time.Now()
	cmd.CompletedAt = &now
	s.commands[commandID] = cmd
	return cmd, nil
}

func (s *AdminStore) GetAccountFreeze(ctx context.Context, userID int64) (domain.AccountFreeze, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	freeze, ok := s.freezes[userID]
	return freeze, ok, nil
}

func (s *AdminStore) SetAccountFreeze(ctx context.Context, freeze domain.AccountFreeze) (domain.AccountFreeze, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if freeze.Frozen {
		s.freezes[freeze.UserID] = freeze
	} else {
		delete(s.freezes, freeze.UserID)
	}
	return freeze, nil
}

func (s *AdminStore) GetAccountFreezes(ctx context.Context, userIDs []int64) (map[int64]domain.AccountFreeze, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make(map[int64]domain.AccountFreeze, len(userIDs))
	for _, id := range userIDs {
		if f, ok := s.freezes[id]; ok {
			res[id] = f
		}
	}
	return res, nil
}
