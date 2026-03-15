package database

import (
	"context"
	"time"

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
	// Soft-delete old tokens for this user (keep only the new one)
	db.DB.NewDelete().Model((*models.Token)(nil)).
		Where("user_id = ?", token.UserID).
		Exec(context.Background())

	_, err := db.DB.NewInsert().Model(token).Exec(context.Background())
	return token, err
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

func (db *Database) GetUserByStripeCustomerID(customerID string) (*models.User, error) {
	var user models.User
	err := db.DB.NewSelect().Model(&user).Where("customer_id = ?", customerID).Scan(context.Background())

	return &user, err
}

func (db *Database) UpdateUserGitHubFields(user *models.User) error {
	user.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(user).
		Column("git_hub_access_token", "git_hub_username", "updated_at").
		Where("id = ?", user.ID).
		Exec(context.Background())
	return err
}
