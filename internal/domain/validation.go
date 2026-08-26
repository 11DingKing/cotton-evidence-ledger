package domain

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
)

func ValidateSource(source Source) error {
	var problems []error
	if !source.Kind.Valid() {
		problems = append(problems, apperr.New("invalid_source_kind", "资料类型无效", apperr.ErrInvalid))
	}
	if len(strings.TrimSpace(source.ExternalID)) < 3 {
		problems = append(problems, apperr.New("invalid_external_id", "来源编号至少需要 3 个字符", apperr.ErrInvalid))
	}
	if utf8.RuneCountInString(strings.TrimSpace(source.Title)) < 4 {
		problems = append(problems, apperr.New("invalid_title", "资料标题至少需要 4 个字符", apperr.ErrInvalid))
	}
	if len(strings.TrimSpace(source.Origin)) < 3 {
		problems = append(problems, apperr.New("invalid_origin", "来源说明至少需要 3 个字符", apperr.ErrInvalid))
	}
	if strings.TrimSpace(source.Fingerprint) == "" {
		problems = append(problems, apperr.New("invalid_fingerprint", "来源指纹不能为空", apperr.ErrInvalid))
	}
	return errors.Join(problems...)
}

func ValidateVersion(version EvidenceVersion) error {
	var problems []error
	if utf8.RuneCountInString(strings.TrimSpace(version.Title)) < 4 {
		problems = append(problems, apperr.New("invalid_version_title", "证据版本标题至少需要 4 个字符", apperr.ErrInvalid))
	}
	if utf8.RuneCountInString(strings.TrimSpace(version.Abstract)) < 20 {
		problems = append(problems, apperr.New("invalid_abstract", "证据摘要至少需要 20 个字符", apperr.ErrInvalid))
	}
	if strings.TrimSpace(version.ContentHash) == "" {
		problems = append(problems, apperr.New("invalid_content_hash", "内容哈希不能为空", apperr.ErrInvalid))
	}
	return errors.Join(problems...)
}

func ValidateClaim(claim Claim) error {
	if utf8.RuneCountInString(strings.TrimSpace(claim.Statement)) < 10 {
		return apperr.New("invalid_claim", "论断内容至少需要 10 个字符", apperr.ErrInvalid)
	}
	if strings.TrimSpace(claim.Locator) == "" {
		return apperr.New("invalid_locator", "论断必须包含来源定位", apperr.ErrInvalid)
	}
	if claim.Confidence < 0 || claim.Confidence > 1 {
		return apperr.New("invalid_confidence", "置信度必须在 0 到 1 之间", apperr.ErrInvalid)
	}
	return nil
}

func ValidateReview(decision ReviewDecision, opinion string) error {
	if !decision.Valid() {
		return apperr.New("invalid_review_decision", "审校结论无效", apperr.ErrInvalid)
	}
	if utf8.RuneCountInString(strings.TrimSpace(opinion)) < 8 {
		return apperr.New("invalid_review_opinion", "审校意见至少需要 8 个字符", apperr.ErrInvalid)
	}
	return nil
}
