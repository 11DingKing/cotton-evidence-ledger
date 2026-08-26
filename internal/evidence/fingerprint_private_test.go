package evidence

import (
	"github.com/11DingKing/cotton-evidence-ledger/internal/domain"
	"testing"
)

func TestDifferentTitlesProduceDifferentDerivedFingerprints(t *testing.T) {
	a := normalizeFingerprint("", domain.SourcePaper, "", "Title A")
	b := normalizeFingerprint("", domain.SourcePaper, "", "Title B")
	if a == b {
		t.Fatalf("derived fingerprint collision observed for distinct titles: %s", a)
	}
}
