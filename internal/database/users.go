package database

import (
	"context"

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

func (db *Database) GetToken(user *models.User) (*models.Token, error) {
	var token models.Token
	err := db.DB.NewSelect().Model(&token).Where("user_id = ?", user.ID.String()).Scan(context.Background())

	return &token, err
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

func (db *Database) DeleteToken(token *models.Token) error {
	_, err := db.DB.NewDelete().Model(token).Where("id = ?", token.ID).Exec(context.Background())

	return err
}

func (db *Database) GetUserByStripeCustomerID(customerID string) (*models.User, error) {
	var user models.User
	err := db.DB.NewSelect().Model(&user).Where("customer_id = ?", customerID).Scan(context.Background())

	return &user, err
}
