package store

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/delegation"
)

// DelegationStore persists delegated tasks, from creation through result.
type DelegationStore interface {
	CreateDelegation(ctx context.Context, d *delegation.Delegation) error

	GetDelegation(ctx context.Context, id string) (*delegation.Delegation, error)

	ListDelegations(ctx context.Context, f delegation.Filter) ([]*delegation.Delegation, error)

	// StartDelegation claims a pending delegation. False if already claimed.
	StartDelegation(ctx context.Context, id string) (bool, error)

	CompleteDelegation(ctx context.Context, id, result string) error

	FailDelegation(ctx context.Context, id, errMsg string) error

	// MarkNotified records that the requester was told, so it shows once.
	MarkNotified(ctx context.Context, id string) error
}
