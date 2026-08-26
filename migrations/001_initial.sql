CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('collector','researcher','reviewer','knowledge_owner')),
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1)),
    created_at TEXT NOT NULL
);

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
);
CREATE INDEX sessions_user_active_idx ON sessions(user_id, revoked_at, expires_at);

CREATE TABLE sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL CHECK (kind IN ('paper','patent','standard','book','variety')),
    external_id TEXT NOT NULL,
    title TEXT NOT NULL,
    origin TEXT NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,
    submitter_id INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL,
    UNIQUE(kind, external_id)
);

CREATE TABLE evidence_units (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL UNIQUE REFERENCES sources(id),
    owner_id INTEGER NOT NULL REFERENCES users(id),
    state TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    current_version_id INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX evidence_state_updated_idx ON evidence_units(state, updated_at, id);

CREATE TABLE evidence_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    evidence_id INTEGER NOT NULL REFERENCES evidence_units(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK (number > 0),
    state TEXT NOT NULL,
    title TEXT NOT NULL,
    abstract TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_by INTEGER NOT NULL REFERENCES users(id),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    supersedes_id INTEGER REFERENCES evidence_versions(id),
    published_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE(evidence_id, number),
    UNIQUE(evidence_id, content_hash)
);
CREATE INDEX versions_evidence_state_idx ON evidence_versions(evidence_id, state, number);

CREATE TABLE claims (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id INTEGER NOT NULL REFERENCES evidence_versions(id) ON DELETE CASCADE,
    statement TEXT NOT NULL,
    locator TEXT NOT NULL,
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL,
    UNIQUE(version_id, statement, locator)
);

CREATE TABLE review_slots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id INTEGER NOT NULL REFERENCES evidence_versions(id) ON DELETE CASCADE,
    reviewer_id INTEGER NOT NULL REFERENCES users(id),
    status TEXT NOT NULL CHECK (status IN ('claimed','completed','released','expired')),
    due_at TEXT NOT NULL,
    claimed_at TEXT NOT NULL,
    released_at TEXT,
    UNIQUE(version_id, reviewer_id)
);
CREATE UNIQUE INDEX one_claimed_slot_per_version ON review_slots(version_id) WHERE status = 'claimed';
CREATE INDEX review_slots_due_idx ON review_slots(status, due_at);

CREATE TABLE reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slot_id INTEGER NOT NULL UNIQUE REFERENCES review_slots(id),
    version_id INTEGER NOT NULL REFERENCES evidence_versions(id),
    reviewer_id INTEGER NOT NULL REFERENCES users(id),
    decision TEXT NOT NULL CHECK (decision IN ('approve','request_changes')),
    opinion TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE citations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    from_version_id INTEGER NOT NULL REFERENCES evidence_versions(id) ON DELETE CASCADE,
    to_version_id INTEGER NOT NULL REFERENCES evidence_versions(id),
    relation TEXT NOT NULL,
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL,
    CHECK (from_version_id <> to_version_id),
    UNIQUE(from_version_id, to_version_id, relation)
);
CREATE INDEX citations_target_idx ON citations(to_version_id, from_version_id);

CREATE TABLE jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL CHECK (kind IN ('integrity_check','overdue_review','expiry_reminder','import_resume')),
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','running','retry','completed','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL CHECK (max_attempts > 0),
    available_at TEXT NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(kind, object_type, object_id, status)
);
CREATE INDEX jobs_claim_idx ON jobs(status, available_at, lease_until, id);

CREATE TABLE idempotency_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope TEXT NOT NULL,
    key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_code INTEGER,
    response_body TEXT,
    committed_at TEXT,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(scope, key)
);

CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id INTEGER REFERENCES users(id),
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    result TEXT NOT NULL,
    request_id TEXT NOT NULL,
    before_json TEXT NOT NULL,
    after_json TEXT NOT NULL,
    previous_hash TEXT NOT NULL,
    event_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);
CREATE INDEX audit_object_idx ON audit_events(object_type, object_id, id);
CREATE INDEX audit_actor_idx ON audit_events(actor_id, id);

CREATE TRIGGER evidence_current_version_fk_insert
BEFORE INSERT ON evidence_units
WHEN NEW.current_version_id IS NOT NULL
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM evidence_versions v WHERE v.id = NEW.current_version_id
    ) THEN RAISE(ABORT, 'current version does not exist') END;
END;
