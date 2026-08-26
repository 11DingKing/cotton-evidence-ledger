package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
	"github.com/11DingKing/cotton-evidence-ledger/internal/audit"
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
)

type PublishParams struct {
	Actor            domain.Actor
	EvidenceID       int64
	VersionID        int64
	ExpectedRevision int64
	CitationTargets  []int64
	RequestID        string
	Now              time.Time
}

func (s *Store) Publish(ctx context.Context, params PublishParams) (domain.EvidenceVersion, error) {
	var published domain.EvidenceVersion
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, params.EvidenceID)
		if err != nil {
			return err
		}
		version, err := versionByID(ctx, tx, params.VersionID)
		if err != nil {
			return err
		}
		if evidence.Revision != params.ExpectedRevision {
			return apperr.ErrVersion
		}
		if version.EvidenceID != evidence.ID || version.State != domain.VersionApproved {
			return apperr.ErrInvalidState
		}
		for _, targetID := range params.CitationTargets {
			if err := ctx.Err(); err != nil {
				return err
			}
			if targetID == version.ID {
				return apperr.New("citation_cycle", "证据版本不能引用自身", apperr.ErrInvalid)
			}
			target, err := versionByID(ctx, tx, targetID)
			if err != nil {
				return err
			}
			if target.State != domain.VersionPublished {
				return apperr.New("citation_target_unpublished", "引用目标尚未发布", apperr.ErrInvalidState)
			}
			var reverse int
			if err := tx.QueryRowContext(ctx, `
                SELECT COUNT(*) FROM citations WHERE from_version_id = ? AND to_version_id = ?`, targetID, version.ID).Scan(&reverse); err != nil {
				return normalizeError("check reverse citation", err)
			}
			if reverse > 0 {
				return apperr.New("citation_cycle", "引用关系会形成直接循环", apperr.ErrConflict)
			}
		}
		if err := version.State.Transition(domain.VersionPublished); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
            UPDATE evidence_versions SET state = ?, revision = revision + 1, published_at = ?
            WHERE id = ? AND revision = ?`, domain.VersionPublished, formatTime(params.Now), version.ID, version.Revision)
		if err != nil {
			return normalizeError("publish evidence version", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		result, err = tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, current_version_id = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND revision = ?`, domain.EvidencePublished, version.ID,
			formatTime(params.Now), evidence.ID, evidence.Revision)
		if err != nil {
			return normalizeError("publish evidence", err)
		}
		changed, err = result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		for _, targetID := range params.CitationTargets {
			if _, err := tx.ExecContext(ctx, `
                INSERT INTO citations(from_version_id, to_version_id, relation, created_by, created_at)
                VALUES(?,?,'supports',?,?)`, version.ID, targetID, params.Actor.UserID, formatTime(params.Now)); err != nil {
				return normalizeError("create publication citation", err)
			}
		}
		actorID := params.Actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{
			ActorID: &actorID, Action: "version.published", ObjectType: "version",
			ObjectID: strconv.FormatInt(version.ID, 10), Result: "success", RequestID: params.RequestID,
			Before:    map[string]any{"version_state": version.State, "evidence_state": evidence.State},
			After:     map[string]any{"version_state": domain.VersionPublished, "citation_targets": params.CitationTargets},
			CreatedAt: params.Now,
		})
		if err != nil {
			return err
		}
		published, err = versionByID(ctx, tx, version.ID)
		return err
	})
	return published, err
}

type CorrectionParams struct {
	Actor              domain.Actor
	EvidenceID         int64
	PublishedVersionID int64
	ExpectedRevision   int64
	Title              string
	Abstract           string
	ContentHash        string
	RequestID          string
	Now                time.Time
}

func (s *Store) StartCorrection(ctx context.Context, params CorrectionParams) (domain.EvidenceVersion, error) {
	var created domain.EvidenceVersion
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, params.EvidenceID)
		if err != nil {
			return err
		}
		current, err := versionByID(ctx, tx, params.PublishedVersionID)
		if err != nil {
			return err
		}
		if evidence.Revision != params.ExpectedRevision {
			return apperr.ErrVersion
		}
		if current.EvidenceID != evidence.ID || current.State != domain.VersionPublished ||
			evidence.CurrentVersionID == nil || *evidence.CurrentVersionID != current.ID {
			return apperr.ErrInvalidState
		}
		var nextNumber int64
		if err := tx.QueryRowContext(ctx, `
            SELECT COALESCE(MAX(number),0)+1 FROM evidence_versions WHERE evidence_id = ?`, evidence.ID).Scan(&nextNumber); err != nil {
			return normalizeError("allocate correction version number", err)
		}
		result, err := tx.ExecContext(ctx, `
            INSERT INTO evidence_versions(evidence_id, number, state, title, abstract, content_hash,
                created_by, revision, supersedes_id, created_at)
            VALUES(?,?,?,?,?,?,?,1,?,?)`, evidence.ID, nextNumber, domain.VersionDraft,
			params.Title, params.Abstract, params.ContentHash, params.Actor.UserID, current.ID, formatTime(params.Now))
		if err != nil {
			return normalizeError("create correction version", err)
		}
		newID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read correction version id: %w", err)
		}
		result, err = tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND revision = ?`, domain.EvidenceCorrecting, formatTime(params.Now), evidence.ID, evidence.Revision)
		if err != nil {
			return normalizeError("start evidence correction", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		actorID := params.Actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{
			ActorID: &actorID, Action: "version.correction_started", ObjectType: "evidence",
			ObjectID: strconv.FormatInt(evidence.ID, 10), Result: "success", RequestID: params.RequestID,
			Before: map[string]any{"state": evidence.State, "current_version_id": current.ID},
			After:  map[string]any{"state": domain.EvidenceCorrecting, "draft_version_id": newID}, CreatedAt: params.Now,
		})
		if err != nil {
			return err
		}
		created, err = versionByID(ctx, tx, newID)
		return err
	})
	return created, err
}

func (s *Store) ReplacePublishedVersion(ctx context.Context, actor domain.Actor, evidenceID, oldVersionID, newVersionID, expectedRevision int64, requestID string, now time.Time) (domain.EvidenceVersion, error) {
	var replacement domain.EvidenceVersion
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, evidenceID)
		if err != nil {
			return err
		}
		oldVersion, err := versionByID(ctx, tx, oldVersionID)
		if err != nil {
			return err
		}
		newVersion, err := versionByID(ctx, tx, newVersionID)
		if err != nil {
			return err
		}
		if evidence.Revision != expectedRevision || oldVersion.State != domain.VersionPublished ||
			newVersion.State != domain.VersionApproved || newVersion.EvidenceID != evidence.ID ||
			newVersion.SupersedesID == nil || *newVersion.SupersedesID != oldVersion.ID {
			return apperr.ErrInvalidState
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE evidence_versions SET state = ?, revision = revision + 1 WHERE id = ?`,
			domain.VersionSuperseded, oldVersion.ID); err != nil {
			return normalizeError("supersede published version", err)
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE evidence_versions SET state = ?, revision = revision + 1, published_at = ? WHERE id = ?`,
			domain.VersionPublished, formatTime(now), newVersion.ID); err != nil {
			return normalizeError("publish replacement version", err)
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE citations SET to_version_id = ?
            WHERE to_version_id = ? AND from_version_id <> ?`, newVersion.ID, oldVersion.ID, newVersion.ID); err != nil {
			return normalizeError("relink inbound citations", err)
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, current_version_id = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND revision = ?`, domain.EvidencePublished, newVersion.ID, formatTime(now), evidence.ID, expectedRevision); err != nil {
			return normalizeError("activate replacement version", err)
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{
			ActorID: &actorID, Action: "version.replaced", ObjectType: "evidence",
			ObjectID: strconv.FormatInt(evidence.ID, 10), Result: "success", RequestID: requestID,
			Before: map[string]any{"current_version_id": oldVersion.ID, "state": evidence.State},
			After:  map[string]any{"current_version_id": newVersion.ID, "state": domain.EvidencePublished}, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		replacement, err = versionByID(ctx, tx, newVersion.ID)
		return err
	})
	return replacement, err
}

func (s *Store) WithdrawEvidence(ctx context.Context, actor domain.Actor, evidenceID, expectedRevision int64, reason, requestID string, now time.Time) error {
	return s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, evidenceID)
		if err != nil {
			return err
		}
		if evidence.Revision != expectedRevision || evidence.State != domain.EvidencePublished || evidence.CurrentVersionID == nil {
			return apperr.ErrInvalidState
		}
		var inbound int
		if err := tx.QueryRowContext(ctx, `
            SELECT COUNT(*) FROM citations c JOIN evidence_versions v ON v.id = c.from_version_id
            WHERE c.to_version_id = ? AND v.state = ?`, *evidence.CurrentVersionID, domain.VersionPublished).Scan(&inbound); err != nil {
			return normalizeError("count active inbound citations", err)
		}
		if inbound > 0 {
			return apperr.New("active_citations", "仍有已发布证据引用该版本，不能直接撤回", apperr.ErrConflict)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE evidence_versions SET state = ?, revision = revision + 1 WHERE id = ?`,
			domain.VersionWithdrawn, *evidence.CurrentVersionID); err != nil {
			return normalizeError("withdraw current version", err)
		}
		result, err := tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND revision = ?`, domain.EvidenceWithdrawn, formatTime(now), evidence.ID, expectedRevision)
		if err != nil {
			return normalizeError("withdraw evidence", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{ActorID: &actorID, Action: "evidence.withdrawn",
			ObjectType: "evidence", ObjectID: strconv.FormatInt(evidence.ID, 10), Result: "success", RequestID: requestID,
			Before: evidence, After: map[string]any{"state": domain.EvidenceWithdrawn, "reason": reason}, CreatedAt: now})
		return err
	})
}

func (s *Store) ArchiveEvidence(ctx context.Context, actor domain.Actor, evidenceID, expectedRevision int64, reason, requestID string, now time.Time) error {
	return s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, evidenceID)
		if err != nil {
			return err
		}
		if evidence.Revision != expectedRevision {
			return apperr.ErrVersion
		}
		if evidence.State != domain.EvidenceWithdrawn {
			return apperr.ErrInvalidState
		}
		if err := evidence.State.Transition(domain.EvidenceArchived); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND revision = ?`, domain.EvidenceArchived, formatTime(now), evidence.ID, expectedRevision)
		if err != nil {
			return normalizeError("archive evidence", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{ActorID: &actorID, Action: "evidence.archived",
			ObjectType: "evidence", ObjectID: strconv.FormatInt(evidence.ID, 10), Result: "success", RequestID: requestID,
			Before: map[string]any{"state": evidence.State, "revision": evidence.Revision},
			After:  map[string]any{"state": domain.EvidenceArchived, "reason": reason}, CreatedAt: now})
		return err
	})
}

func (s *Store) RestoreEvidence(ctx context.Context, actor domain.Actor, evidenceID, expectedRevision int64, reason, requestID string, now time.Time) error {
	return s.InTx(ctx, func(tx *sql.Tx) error {
		evidence, err := evidenceByID(ctx, tx, evidenceID)
		if err != nil {
			return err
		}
		if evidence.Revision != expectedRevision {
			return apperr.ErrVersion
		}
		if evidence.State != domain.EvidenceWithdrawn && evidence.State != domain.EvidenceArchived {
			return apperr.ErrInvalidState
		}
		if evidence.CurrentVersionID == nil {
			return apperr.New("restore_version_missing", "恢复证据缺少当前版本", apperr.ErrInvalidState)
		}
		version, err := versionByID(ctx, tx, *evidence.CurrentVersionID)
		if err != nil {
			return err
		}
		if version.State != domain.VersionWithdrawn {
			return apperr.ErrInvalidState
		}
		if err := version.State.Transition(domain.VersionUnderReview); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
            UPDATE evidence_versions SET state = ?, revision = revision + 1 WHERE id = ? AND revision = ?`,
			domain.VersionUnderReview, version.ID, version.Revision); err != nil {
			return normalizeError("restore evidence version for review", err)
		}
		result, err := tx.ExecContext(ctx, `
            UPDATE evidence_units SET state = ?, revision = revision + 1, updated_at = ?
            WHERE id = ? AND revision = ?`, domain.EvidenceReviewing, formatTime(now), evidence.ID, expectedRevision)
		if err != nil {
			return normalizeError("restore evidence", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return apperr.ErrVersion
		}
		if _, err := tx.ExecContext(ctx, `
            INSERT INTO jobs(kind, object_type, object_id, payload, status, attempts, max_attempts,
                available_at, lease_owner, last_error, created_at, updated_at)
            VALUES('integrity_check','evidence',?,?,'pending',0,5,?,'','',?,?)`,
			strconv.FormatInt(evidence.ID, 10), fmt.Sprintf(`{"reason":%q}`, reason), formatTime(now),
			formatTime(now), formatTime(now)); err != nil {
			return normalizeError("enqueue restored evidence integrity check", err)
		}
		actorID := actor.UserID
		_, err = AppendAudit(ctx, tx, audit.Payload{ActorID: &actorID, Action: "evidence.restored",
			ObjectType: "evidence", ObjectID: strconv.FormatInt(evidence.ID, 10), Result: "success", RequestID: requestID,
			Before: map[string]any{"state": evidence.State, "version_state": version.State},
			After: map[string]any{"state": domain.EvidenceReviewing, "version_state": domain.VersionUnderReview,
				"integrity_check": "queued", "reason": reason}, CreatedAt: now})
		return err
	})
}
