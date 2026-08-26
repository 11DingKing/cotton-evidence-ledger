CREATE TABLE responsibility_handoffs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    evidence_id INTEGER NOT NULL REFERENCES evidence_units(id) ON DELETE CASCADE,
    from_user_id INTEGER NOT NULL REFERENCES users(id),
    to_user_id INTEGER NOT NULL REFERENCES users(id),
    expected_revision INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','accepted','cancelled','expired')),
    reason TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    accepted_at TEXT,
    created_at TEXT NOT NULL,
    CHECK (from_user_id <> to_user_id)
);
CREATE UNIQUE INDEX one_pending_handoff_per_evidence
ON responsibility_handoffs(evidence_id) WHERE status = 'pending';

CREATE TABLE notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dedupe_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL,
    read_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE(user_id, dedupe_key)
);
CREATE INDEX notifications_unread_idx ON notifications(user_id, read_at, id);
