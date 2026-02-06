package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetSEOByWebsiteId(websiteId string) (*models.WebsiteSEO, error) {
	var seo models.WebsiteSEO
	err := db.DB.NewSelect().Model(&seo).Where("website_id = ?", websiteId).Scan(context.Background())
	return &seo, err
}

func (db *Database) CreateSEO(s *models.WebsiteSEO) (*models.WebsiteSEO, error) {
	_, err := db.DB.NewInsert().Model(s).Exec(context.Background())
	return s, err
}

func (db *Database) UpdateSEO(s *models.WebsiteSEO) (*models.WebsiteSEO, error) {
	_, err := db.DB.NewUpdate().Model(s).Where("id = ?", s.ID).Exec(context.Background())
	return s, err
}
