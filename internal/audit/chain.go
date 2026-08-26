package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

type Payload struct {
	ActorID    *int64
	Action     string
	ObjectType string
	ObjectID   string
	Result     string
	RequestID  string
	Before     any
	After      any
	CreatedAt  time.Time
}

func Build(previousHash string, payload Payload) (domain.AuditEvent, error) {
	if previousHash == "" {
		previousHash = GenesisHash
	}
	beforeJSON, err := canonical(payload.Before)
	if err != nil {
		return domain.AuditEvent{}, fmt.Errorf("encode audit before image: %w", err)
	}
	afterJSON, err := canonical(payload.After)
	if err != nil {
		return domain.AuditEvent{}, fmt.Errorf("encode audit after image: %w", err)
	}
	event := domain.AuditEvent{
		ActorID:      payload.ActorID,
		Action:       payload.Action,
		ObjectType:   payload.ObjectType,
		ObjectID:     payload.ObjectID,
		Result:       payload.Result,
		RequestID:    payload.RequestID,
		BeforeJSON:   beforeJSON,
		AfterJSON:    afterJSON,
		PreviousHash: previousHash,
		CreatedAt:    payload.CreatedAt.UTC(),
	}
	event.EventHash = Hash(event)
	return event, nil
}

func Hash(event domain.AuditEvent) string {
	hasher := sha256.New()
	fields := []string{
		event.PreviousHash,
		actorString(event.ActorID),
		event.Action,
		event.ObjectType,
		event.ObjectID,
		event.Result,
		event.RequestID,
		event.BeforeJSON,
		event.AfterJSON,
		event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	for _, field := range fields {
		hasher.Write([]byte(strconv.Itoa(len(field))))
		hasher.Write([]byte{':'})
		hasher.Write([]byte(field))
		hasher.Write([]byte{'|'})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func Verify(events []domain.AuditEvent) error {
	previous := GenesisHash
	for index, event := range events {
		if event.PreviousHash != previous {
			return fmt.Errorf("audit event %d links to %s, expected %s", index, event.PreviousHash, previous)
		}
		if calculated := Hash(event); calculated != event.EventHash {
			return fmt.Errorf("audit event %d hash mismatch: got %s, want %s", index, event.EventHash, calculated)
		}
		previous = event.EventHash
	}
	return nil
}

func canonical(value any) (string, error) {
	if value == nil {
		return "null", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func actorString(actorID *int64) string {
	if actorID == nil {
		return "system"
	}
	return strconv.FormatInt(*actorID, 10)
}
