package storage

import (
	"context"
	"errors"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"testing"
	"time"
)

func TestCanceledDecisionDoesNotCompleteSlot(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	author := createUser(t, st, "author6@example.test", domain.RoleResearcher)
	reviewer := createUser(t, st, "reviewer6@example.test", domain.RoleReviewer)
	ev, v := prepareSubmittedVersion(t, st, author, "cancel-review")
	slot, e := st.ClaimReviewSlot(ctx, actorFor(reviewer), v.ID, now.Add(time.Hour), "req", now)
	if e != nil {
		t.Fatal(e)
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, e = st.DecideReview(c, actorFor(reviewer), slot.ID, domain.ReviewApprove, "ok", "req", now)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("error=%v want canceled", e)
	}
	_ = ev
}
