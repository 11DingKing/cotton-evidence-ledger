package domain

import (
	"errors"
	"testing"

	"github.com/11DingKing/cotton-evidence-ledger/internal/apperr"
)

func TestValidateSource(t *testing.T) {
	t.Parallel()
	valid := Source{Kind: SourcePaper, ExternalID: "DOI-123", Title: "Cotton fiber study",
		Origin: "Agronomy journal", Fingerprint: "sha256:abc"}
	if err := ValidateSource(valid); err != nil {
		t.Fatalf("valid source rejected: %v", err)
	}
	tests := []struct {
		name   string
		change func(*Source)
	}{
		{"unknown kind", func(source *Source) { source.Kind = "dataset" }},
		{"short external id", func(source *Source) { source.ExternalID = "x" }},
		{"blank external id", func(source *Source) { source.ExternalID = "  " }},
		{"short title", func(source *Source) { source.Title = "abc" }},
		{"blank title", func(source *Source) { source.Title = " " }},
		{"short origin", func(source *Source) { source.Origin = "x" }},
		{"blank fingerprint", func(source *Source) { source.Fingerprint = "" }},
		{"space fingerprint", func(source *Source) { source.Fingerprint = "  " }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate := valid
			test.change(&candidate)
			if err := ValidateSource(candidate); !errors.Is(err, apperr.ErrInvalid) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
}

func TestEverySourceTypeIsRecognized(t *testing.T) {
	t.Parallel()
	for _, kind := range []SourceType{SourcePaper, SourcePatent, SourceStandard, SourceBook, SourceVariety} {
		if !kind.Valid() {
			t.Errorf("expected source kind %q to be valid", kind)
		}
	}
	for _, kind := range []SourceType{"", "report", "article", "seed", "PAPER"} {
		if kind.Valid() {
			t.Errorf("expected source kind %q to be invalid", kind)
		}
	}
}

func TestValidateVersion(t *testing.T) {
	t.Parallel()
	valid := EvidenceVersion{Title: "Fiber strength evidence", Abstract: "This abstract has enough detail for cross review.", ContentHash: "abc123"}
	if err := ValidateVersion(valid); err != nil {
		t.Fatalf("valid version rejected: %v", err)
	}
	tests := []struct {
		name  string
		value EvidenceVersion
	}{
		{"title empty", EvidenceVersion{Title: "", Abstract: valid.Abstract, ContentHash: valid.ContentHash}},
		{"title too short", EvidenceVersion{Title: "abc", Abstract: valid.Abstract, ContentHash: valid.ContentHash}},
		{"abstract empty", EvidenceVersion{Title: valid.Title, Abstract: "", ContentHash: valid.ContentHash}},
		{"abstract too short", EvidenceVersion{Title: valid.Title, Abstract: "not enough", ContentHash: valid.ContentHash}},
		{"hash empty", EvidenceVersion{Title: valid.Title, Abstract: valid.Abstract, ContentHash: ""}},
		{"hash spaces", EvidenceVersion{Title: valid.Title, Abstract: valid.Abstract, ContentHash: "   "}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateVersion(test.value); !errors.Is(err, apperr.ErrInvalid) {
				t.Fatalf("expected invalid version, got %v", err)
			}
		})
	}
}

func TestValidateClaim(t *testing.T) {
	t.Parallel()
	valid := Claim{Statement: "The fiber strength remains stable across treatments", Locator: "page 12 table 3", Confidence: 0.92}
	if err := ValidateClaim(valid); err != nil {
		t.Fatalf("valid claim rejected: %v", err)
	}
	tests := []struct {
		name  string
		claim Claim
	}{
		{"short statement", Claim{Statement: "short", Locator: valid.Locator, Confidence: valid.Confidence}},
		{"missing locator", Claim{Statement: valid.Statement, Locator: "", Confidence: valid.Confidence}},
		{"negative confidence", Claim{Statement: valid.Statement, Locator: valid.Locator, Confidence: -0.01}},
		{"confidence over one", Claim{Statement: valid.Statement, Locator: valid.Locator, Confidence: 1.01}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateClaim(test.claim); !errors.Is(err, apperr.ErrInvalid) {
				t.Fatalf("expected invalid claim, got %v", err)
			}
		})
	}
	for _, confidence := range []float64{0, 0.25, 0.5, 0.999, 1} {
		candidate := valid
		candidate.Confidence = confidence
		if err := ValidateClaim(candidate); err != nil {
			t.Errorf("boundary confidence %.3f rejected: %v", confidence, err)
		}
	}
}

func TestValidateReview(t *testing.T) {
	t.Parallel()
	for _, decision := range []ReviewDecision{ReviewApprove, ReviewRequestChanges} {
		if err := ValidateReview(decision, "Evidence is internally consistent."); err != nil {
			t.Errorf("valid review %q rejected: %v", decision, err)
		}
	}
	tests := []struct {
		decision ReviewDecision
		opinion  string
	}{
		{"", "Evidence is internally consistent."},
		{"reject", "Evidence is internally consistent."},
		{ReviewApprove, "short"},
		{ReviewRequestChanges, "   "},
	}
	for _, test := range tests {
		if err := ValidateReview(test.decision, test.opinion); !errors.Is(err, apperr.ErrInvalid) {
			t.Errorf("expected invalid review %#v, got %v", test, err)
		}
	}
}
