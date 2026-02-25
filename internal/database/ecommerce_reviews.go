package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateReview(review *models.ProductReview) (*models.ProductReview, error) {
	_, err := db.DB.NewInsert().Model(review).Exec(context.Background())
	return review, err
}

func (db *Database) GetReviewsByProductID(productID string) ([]models.ProductReview, error) {
	var reviews []models.ProductReview
	err := db.DB.NewSelect().
		Model(&reviews).
		Where("product_id = ? AND status = 'approved'", productID).
		Order("created_at DESC").
		Scan(context.Background())
	return reviews, err
}

func (db *Database) GetAverageRating(productID string) (float64, error) {
	var avg float64
	err := db.DB.NewSelect().
		TableExpr("product_reviews").
		ColumnExpr("COALESCE(AVG(rating), 0)").
		Where("product_id = ? AND status = 'approved'", productID).
		Scan(context.Background(), &avg)
	return avg, err
}

func (db *Database) GetReviewCount(productID string) (int, error) {
	count, err := db.DB.NewSelect().
		Model((*models.ProductReview)(nil)).
		Where("product_id = ? AND status = 'approved'", productID).
		Count(context.Background())
	return count, err
}
