package database

import (
	"context"
	"regexp"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// MatchBuildAnnotation walks the enabled annotations and returns the
// first one whose LogRegex matches errorMessage, or (uuid.Nil, nil) if
// none match. Regex compilation is done per-call for simplicity — the
// registry is small (tens of rows) and failures are infrequent, so this
// isn't hot-path. Revisit if the registry grows past ~200.
func MatchBuildAnnotation(ctx context.Context, db *bun.DB, errorMessage string) (uuid.UUID, error) {
	var annotations []models.BuildAnnotation
	err := db.NewSelect().
		Model(&annotations).
		Where("enabled = ?", true).
		Scan(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	for _, a := range annotations {
		re, err := regexp.Compile(a.LogRegex)
		if err != nil {
			// Bad regex in the registry — skip it rather than fail the
			// whole match; operator will see the bad one via metrics.
			continue
		}
		if re.MatchString(errorMessage) {
			return a.ID, nil
		}
	}
	return uuid.Nil, nil
}
