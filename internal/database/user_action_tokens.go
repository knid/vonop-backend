package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

// CreateUserActionToken inserts a new one-time action token.
func (db *Database) CreateUserActionToken(t *models.UserActionToken) (*models.UserActionToken, error) {
	_, err := db.DB.NewInsert().Model(t).Exec(context.Background())
	return t, err
}

// GetUserActionToken looks up a token by its string value and kind.
// Returns the row even if it's expired or already consumed — callers must
// validate status themselves so they can return the right error message.
func (db *Database) GetUserActionToken(tokenStr string, kind models.UserActionTokenKind) (*models.UserActionToken, error) {
	var token models.UserActionToken
	err := db.DB.NewSelect().
		Model(&token).
		Relation("User").
		Where("token = ?", tokenStr).
		Where("kind = ?", string(kind)).
		Scan(context.Background())
	return &token, err
}

// ConsumeUserActionToken marks a token as used. Safe to call once per token;
// callers should check IsConsumed() before calling so the UX error is clear.
func (db *Database) ConsumeUserActionToken(token *models.UserActionToken) error {
	token.ConsumedAt = time.Now()
	_, err := db.DB.NewUpdate().
		Model(token).
		Column("consumed_at").
		Where("id = ?", token.ID).
		Exec(context.Background())
	return err
}

// InvalidatePendingUserActionTokens soft-deletes any outstanding un-consumed
// tokens of a given kind for a user. Call this before issuing a new one so a
// single email link is authoritative at any moment.
func (db *Database) InvalidatePendingUserActionTokens(userID string, kind models.UserActionTokenKind) error {
	_, err := db.DB.NewUpdate().
		Model((*models.UserActionToken)(nil)).
		Set("consumed_at = ?", time.Now()).
		Where("user_id = ?", userID).
		Where("kind = ?", string(kind)).
		Where("consumed_at IS NULL").
		Exec(context.Background())
	return err
}

// DeleteExpiredUserActionTokens hard-deletes fully expired tokens. Cheap
// housekeeping call for a scheduled job; safe to run anytime.
func (db *Database) DeleteExpiredUserActionTokens() (int64, error) {
	res, err := db.DB.NewDelete().
		Model((*models.UserActionToken)(nil)).
		Where("expired_at < ?", time.Now().Add(-24*time.Hour)).
		ForceDelete().
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
