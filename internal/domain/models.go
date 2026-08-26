package domain

import "time"

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type Actor struct {
	UserID    int64
	SessionID int64
	Email     string
	Role      Role
}

type SourceType string

const (
	SourcePaper    SourceType = "paper"
	SourcePatent   SourceType = "patent"
	SourceStandard SourceType = "standard"
	SourceBook     SourceType = "book"
	SourceVariety  SourceType = "variety"
)

func (s SourceType) Valid() bool {
	switch s {
	case SourcePaper, SourcePatent, SourceStandard, SourceBook, SourceVariety:
		return true
	default:
		return false
	}
}

type Source struct {
	ID          int64      `json:"id"`
	Kind        SourceType `json:"kind"`
	ExternalID  string     `json:"external_id"`
	Title       string     `json:"title"`
	Origin      string     `json:"origin"`
	Fingerprint string     `json:"fingerprint"`
	SubmitterID int64      `json:"submitter_id"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Evidence struct {
	ID               int64         `json:"id"`
	SourceID         int64         `json:"source_id"`
	OwnerID          int64         `json:"owner_id"`
	State            EvidenceState `json:"state"`
	Revision         int64         `json:"revision"`
	CurrentVersionID *int64        `json:"current_version_id,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

type EvidenceVersion struct {
	ID           int64        `json:"id"`
	EvidenceID   int64        `json:"evidence_id"`
	Number       int64        `json:"number"`
	State        VersionState `json:"state"`
	Title        string       `json:"title"`
	Abstract     string       `json:"abstract"`
	ContentHash  string       `json:"content_hash"`
	CreatedBy    int64        `json:"created_by"`
	Revision     int64        `json:"revision"`
	SupersedesID *int64       `json:"supersedes_id,omitempty"`
	PublishedAt  *time.Time   `json:"published_at,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
}

type Claim struct {
	ID         int64     `json:"id"`
	VersionID  int64     `json:"version_id"`
	Statement  string    `json:"statement"`
	Locator    string    `json:"locator"`
	Confidence float64   `json:"confidence"`
	CreatedBy  int64     `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

type ReviewSlot struct {
	ID         int64      `json:"id"`
	VersionID  int64      `json:"version_id"`
	ReviewerID int64      `json:"reviewer_id"`
	Status     string     `json:"status"`
	DueAt      time.Time  `json:"due_at"`
	ClaimedAt  time.Time  `json:"claimed_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
}

type Review struct {
	ID         int64          `json:"id"`
	SlotID     int64          `json:"slot_id"`
	VersionID  int64          `json:"version_id"`
	ReviewerID int64          `json:"reviewer_id"`
	Decision   ReviewDecision `json:"decision"`
	Opinion    string         `json:"opinion"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Citation struct {
	ID            int64     `json:"id"`
	FromVersionID int64     `json:"from_version_id"`
	ToVersionID   int64     `json:"to_version_id"`
	Relation      string    `json:"relation"`
	CreatedBy     int64     `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}

type AuditEvent struct {
	ID           int64     `json:"id"`
	ActorID      *int64    `json:"actor_id,omitempty"`
	Action       string    `json:"action"`
	ObjectType   string    `json:"object_type"`
	ObjectID     string    `json:"object_id"`
	Result       string    `json:"result"`
	RequestID    string    `json:"request_id"`
	BeforeJSON   string    `json:"before_json"`
	AfterJSON    string    `json:"after_json"`
	PreviousHash string    `json:"previous_hash"`
	EventHash    string    `json:"event_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type Job struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	ObjectType  string     `json:"object_type"`
	ObjectID    string     `json:"object_id"`
	Payload     string     `json:"payload"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	AvailableAt time.Time  `json:"available_at"`
	LeaseOwner  string     `json:"lease_owner"`
	LeaseUntil  *time.Time `json:"lease_until,omitempty"`
	LastError   string     `json:"last_error"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Page[T any] struct {
	Items      []T   `json:"items"`
	NextCursor int64 `json:"next_cursor,omitempty"`
}
