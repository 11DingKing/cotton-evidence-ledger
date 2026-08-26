package middleware

import (
	"context"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

type actorKey struct{}

func WithActor(ctx context.Context, actor domain.Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func Actor(ctx context.Context) (domain.Actor, bool) {
	actor, ok := ctx.Value(actorKey{}).(domain.Actor)
	return actor, ok
}
