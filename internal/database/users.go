package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetUserById(id string) (*models.User, error) {
	var user models.User
	err := db.DB.NewSelect().Model(&user).Where("id = ?", id).Scan(context.Background())

	return &user, err
}

func (db *Database) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	err := db.DB.NewSelect().Model(&user).Where("email = ?", email).Scan(context.Background())

	return &user, err
}

func (db *Database) CreateUser(user *models.User) (*models.User, error) {
	_, err := db.DB.NewInsert().Model(user).Exec(context.Background())

	return user, err
}

func (db *Database) UpdateUser(user *models.User) (*models.User, error) {
	_, err := db.DB.NewUpdate().Model(user).Where("id = ?", user.ID).Exec(context.Background())
	return user, err
}

func (db *Database) DeleteUser(user *models.User) (*models.User, error) {
	_, err := db.DB.NewDelete().Model(user).Where("id = ?", user.ID).Exec(context.Background())
	return user, err
}

func (db *Database) GetTokenByToken(tokenStr string) (*models.Token, error) {
	var token models.Token
	err := db.DB.NewSelect().Model(&token).Where("string = ?", tokenStr).Scan(context.Background())

	return &token, err
}

func (db *Database) CreateToken(token *models.Token) (*models.Token, error) {
	_, err := db.DB.NewInsert().Model(token).Exec(context.Background())
	return token, err
}

func (db *Database) DeleteTokensByUser(ctx context.Context, userID uuid.UUID) error {
	_, err := db.DB.NewDelete().
		Model((*models.Token)(nil)).
		Where("user_id = ?", userID).
		Exec(ctx)
	return err
}

func (db *Database) DeleteExpiredTokens() (int64, error) {
	res, err := db.DB.NewDelete().Model((*models.Token)(nil)).
		Where("expired_at < ?", time.Now()).
		ForceDelete().
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (db *Database) UpdateUserGitHubFields(user *models.User) error {
	user.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(user).
		Column("git_hub_access_token", "git_hub_username", "updated_at").
		Where("id = ?", user.ID).
		Exec(context.Background())
	return err
}

func (db *Database) SetUserTwentyPersonID(userID, twentyPersonID string) error {
	_, err := db.DB.NewUpdate().
		Model((*models.User)(nil)).
		Set("twenty_person_id = ?", twentyPersonID).
		Where("id = ?", userID).
		Exec(context.Background())
	return err
}

// ListUsersWithoutPersonalOrg returns users that do not yet own an
// Organization with is_personal=true. Used by the org-foundation backfill.
func (db *Database) ListUsersWithoutPersonalOrg() ([]models.User, error) {
	var users []models.User
	err := db.DB.NewSelect().
		Model(&users).
		Where(`NOT EXISTS (
			SELECT 1 FROM organizations o
			WHERE o.owner_id = "user".id
			  AND o.is_personal = true
			  AND o.deleted_at IS NULL
		)`).
		Where(`"user".deleted_at IS NULL`).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return users, nil
}
