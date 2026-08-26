package storage

import (
	"context"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"testing"
	"time"
)

func TestNotificationCursorAdvancesPastReturnedPage(t *testing.T) {
	st := openTestStore(t)
	u := createUser(t, st, "cursor9@example.test", domain.RoleCollector)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if _, _, e := st.CreateNotification(context.Background(), u.ID, "cursor-"+string(rune('a'+i)), "x", "{}", now); e != nil {
			t.Fatal(e)
		}
	}
	items, next, e := st.ListNotifications(context.Background(), u.ID, 0, 2)
	if e != nil {
		t.Fatal(e)
	}
	if len(items) != 2 || next != items[1].ID {
		t.Fatalf("items=%d next=%d want last id=%d", len(items), next, items[1].ID)
	}
}
