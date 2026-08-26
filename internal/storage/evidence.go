package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

type RegisterEvidenceParams struct {
	Source         domain.Source
	Version        domain.EvidenceVersion
	OwnerID        int64
	RequestID      string
	IdempotencyKey string
	RequestHash    string
	Now            time.Time
}

type registrationReplay struct {
	EvidenceID int64 `json:"evidence_id"`
	VersionID  int64 `json:"version_id"`
}

func (s *Store) RegisterEvidence(ctx context.Context, params RegisterEvidenceParams) (domain.Evidence, domain.EvidenceVersion, error) {
	var evidence domain.Evidence
	var version domain.EvidenceVersion
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		if params.IdempotencyKey != "" {
			record, inserted, err := ReserveIdempotency(ctx, tx, "evidence.register", params.IdempotencyKey,
				params.RequestHash, params.Now)
			if err != nil {
				return err
			}
			if !inserted {
				if !record.Committed {
					return apperr.New("idempotency_in_progress", "相同幂等请求仍在处理中", apperr.ErrConflict)
				}
				var replay registrationReplay
				if err := json.Unmarshal([]byte(record.ResponseBody), &replay); err != nil {
					return fmt.Errorf("decode idempotent registration response: %w", err)
				}
				evidence, err = evidenceByID(ctx, tx, replay.EvidenceID)
				if err != nil {
					return err
				}
				version, err = versionByID(ctx, tx, replay.VersionID)
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `
            INSERT INTO sources(kind, external_id, title, origin, fingerprint, submitter_id, created_at)
            VALUES(?,?,?,?,?,?,?)`, params.Source.Kind, params.Source.ExternalID, params.Source.Title,
			params.Source.Origin, params.Source.Fingerprint, params.Source.SubmitterID, formatTime(params.Now))
		if err != nil {
			return normalizeError("insert source", err)
		}
		sourceID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read source id: %w", err)
		}
		result, err = tx.ExecContext(ctx, `
            INSERT INTO evidence_units(source_id, owner_id, state, revision, created_at, updated_at)
            VALUES(?,?,?,1,?,?)`, sourceID, params.OwnerID, domain.EvidenceRegistered,
			formatTime(params.Now), formatTime(params.Now))
		if err != nil {
			return normalizeError("insert evidence", err)
		}
		evidenceID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read evidence id: %w", err)
		}
		result, err = tx.ExecContext(ctx, `
            INSERT INTO evidence_versions(evidence_id, number, state, title, abstract, content_hash,
                created_by, revision, created_at)
            VALUES(?,1,?,?,?,?,?,1,?)`, evidenceID, domain.VersionDraft, params.Version.Title,
			params.Version.Abstract, params.Version.ContentHash, params.Version.CreatedBy, formatTime(params.Now))
		if err != nil {
			return normalizeError("insert evidence version", err)
		}
		versionID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read evidence version id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE evidence_units SET current_version_id = ? WHERE id = ?`, versionID, evidenceID); err != nil {
			return normalizeError("bind current evidence version", err)
		}
		actorID := params.Source.SubmitterID
		_, err = AppendAudit(ctx, tx, audit.Payload{
			ActorID: &actorID, Action: "evidence.registered", ObjectType: "evidence",
			ObjectID: strconv.FormatInt(evidenceID, 10), Result: "success", RequestID: params.RequestID,
			After:     map[string]any{"source_id": sourceID, "version_id": versionID, "state": domain.EvidenceRegistered},
			CreatedAt: params.Now,
		})
		if err != nil {
			return err
		}
		evidence, err = evidenceByID(ctx, tx, evidenceID)
		if err != nil {
			return err
		}
		version, err = versionByID(ctx, tx, versionID)
		if err != nil {
			return err
		}
		if params.IdempotencyKey != "" {
			encoded, err := json.Marshal(registrationReplay{EvidenceID: evidenceID, VersionID: versionID})
			if err != nil {
				return fmt.Errorf("encode idempotent registration response: %w", err)
			}
			if err := CommitIdempotency(ctx, tx, "evidence.register", params.IdempotencyKey, 201, string(encoded), params.Now); err != nil {
				return err
			}
		}
		return nil
	})
	return evidence, version, err
}

func (s *Store) EvidenceByID(ctx context.Context, id int64) (domain.Evidence, error) {
	return evidenceByID(ctx, s.db, id)
}

func evidenceByID(ctx context.Context, db DBTX, id int64) (domain.Evidence, error) {
	row := db.QueryRowContext(ctx, `
        SELECT id, source_id, owner_id, state, revision, current_version_id, created_at, updated_at
        FROM evidence_units WHERE id = ?`, id)
	var evidence domain.Evidence
	var state string
	var currentVersion sql.NullInt64
	var created, updated string
	if err := row.Scan(&evidence.ID, &evidence.SourceID, &evidence.OwnerID, &state, &evidence.Revision,
		&currentVersion, &created, &updated); err != nil {
		return domain.Evidence{}, normalizeError("find evidence", err)
	}
	evidence.State = domain.EvidenceState(state)
	if currentVersion.Valid {
		evidence.CurrentVersionID = &currentVersion.Int64
	}
	var err error
	evidence.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.Evidence{}, err
	}
	evidence.UpdatedAt, err = parseTime(updated)
	if err != nil {
		return domain.Evidence{}, err
	}
	return evidence, nil
}

func (s *Store) VersionByID(ctx context.Context, id int64) (domain.EvidenceVersion, error) {
	return versionByID(ctx, s.db, id)
}

func versionByID(ctx context.Context, db DBTX, id int64) (domain.EvidenceVersion, error) {
	row := db.QueryRowContext(ctx, `
        SELECT id, evidence_id, number, state, title, abstract, content_hash, created_by,
               revision, supersedes_id, published_at, created_at
        FROM evidence_versions WHERE id = ?`, id)
	var version domain.EvidenceVersion
	var state string
	var supersedes sql.NullInt64
	var published sql.NullString
	var created string
	if err := row.Scan(&version.ID, &version.EvidenceID, &version.Number, &state, &version.Title,
		&version.Abstract, &version.ContentHash, &version.CreatedBy, &version.Revision,
		&supersedes, &published, &created); err != nil {
		return domain.EvidenceVersion{}, normalizeError("find evidence version", err)
	}
	version.State = domain.VersionState(state)
	if supersedes.Valid {
		version.SupersedesID = &supersedes.Int64
	}
	var err error
	version.PublishedAt, err = nullableTime(published)
	if err != nil {
		return domain.EvidenceVersion{}, err
	}
	version.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.EvidenceVersion{}, err
	}
	return version, nil
}

func (s *Store) SourceByFingerprint(ctx context.Context, fingerprint string) (domain.Source, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, kind, external_id, title, origin, fingerprint, submitter_id, created_at
        FROM sources WHERE fingerprint = ?`, fingerprint)
	var source domain.Source
	var kind, created string
	if err := row.Scan(&source.ID, &kind, &source.ExternalID, &source.Title, &source.Origin,
		&source.Fingerprint, &source.SubmitterID, &created); err != nil {
		return domain.Source{}, normalizeError("find source fingerprint", err)
	}
	source.Kind = domain.SourceType(kind)
	var err error
	source.CreatedAt, err = parseTime(created)
	return source, err
}

func (s *Store) AddClaim(ctx context.Context, actor domain.Actor, evidenceID, versionID int64, claim domain.Claim, expectedRevision int64, requestID string, now time.Time) (domain.Claim, error) {
	var saved domain.Claim
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, evidenceID)
		if err != nil {
			return err
		}
		version, err := versionByID(ctx, tx, versionID)
		if err != nil {
			return err
		}
		if version.EvidenceID != evidence.ID || version.State != domain.VersionDraft {
			return fmt.Errorf("claim target is not current draft: %w", apperr.ErrInvalidState)
		}
		if evidence.Revision != expectedRevision {
			return fmt.Errorf("evidence revision is %d, expected %d: %w", evidence.Revision, expectedRevision, apperr.ErrVersion)
		}
		result, err := tx.ExecContext(ctx, `
            INSERT INTO claims(version_id, statement, locator, confidence, created_by, created_at)
            VALUES(?,?,?,?,?,?)`, versionID, claim.Statement, claim.Locator, claim.Confidence, actor.UserID, formatTime(now))
		if err != nil {
			return normalizeError("insert claim", err)
		}
		claimID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read claim id: %w", err)
		}
		result, err = tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND revision = ?`, domain.EvidenceExtracting, formatTime(now), evidenceID, expectedRevision)
		if err != nil {
			return normalizeError("advance evidence after claim", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read evidence update count: %w", err)
		}
		if changed != 1 {
			return apperr.ErrVersion
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{
			ActorID: &actorID, Action: "claim.extracted", ObjectType: "version",
			ObjectID: strconv.FormatInt(versionID, 10), Result: "success", RequestID: requestID,
			After: map[string]any{"claim_id": claimID, "evidence_revision": expectedRevision + 1}, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		saved = claim
		saved.ID = claimID
		saved.VersionID = versionID
		saved.CreatedBy = actor.UserID
		saved.CreatedAt = now.UTC()
		return nil
	})
	return saved, err
}

func (s *Store) ListEvidence(ctx context.Context, state domain.EvidenceState, ownerID, afterID int64, limit int) (domain.Page[domain.Evidence], error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	conditions := []string{"id > ?"}
	args := []any{afterID}
	if state != "" {
		conditions = append(conditions, "state = ?")
		args = append(args, state)
	}
	if ownerID > 0 {
		conditions = append(conditions, "owner_id = ?")
		args = append(args, ownerID)
	}
	args = append(args, limit+1)
	query := `SELECT id, source_id, owner_id, state, revision, current_version_id, created_at, updated_at
        FROM evidence_units WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY id LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return domain.Page[domain.Evidence]{}, normalizeError("list evidence", err)
	}
	defer rows.Close()
	page := domain.Page[domain.Evidence]{Items: make([]domain.Evidence, 0, limit)}
	for rows.Next() {
		var item domain.Evidence
		var stateValue string
		var current sql.NullInt64
		var created, updated string
		if err := rows.Scan(&item.ID, &item.SourceID, &item.OwnerID, &stateValue, &item.Revision,
			&current, &created, &updated); err != nil {
			return domain.Page[domain.Evidence]{}, normalizeError("scan evidence list", err)
		}
		item.State = domain.EvidenceState(stateValue)
		if current.Valid {
			item.CurrentVersionID = &current.Int64
		}
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return domain.Page[domain.Evidence]{}, err
		}
		item.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return domain.Page[domain.Evidence]{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = page.Items[len(page.Items)-1].ID
			break
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.Page[domain.Evidence]{}, fmt.Errorf("iterate evidence list: %w", err)
	}
	return page, nil
}

func (s *Store) ClaimsByVersion(ctx context.Context, versionID int64) ([]domain.Claim, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, version_id, statement, locator, confidence, created_by, created_at
        FROM claims WHERE version_id = ? ORDER BY id`, versionID)
	if err != nil {
		return nil, normalizeError("list version claims", err)
	}
	defer rows.Close()
	claims := make([]domain.Claim, 0)
	for rows.Next() {
		var claim domain.Claim
		var created string
		if err := rows.Scan(&claim.ID, &claim.VersionID, &claim.Statement, &claim.Locator,
			&claim.Confidence, &claim.CreatedBy, &created); err != nil {
			return nil, normalizeError("scan version claim", err)
		}
		claim.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}
