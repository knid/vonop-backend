package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

// Website

func (db *Database) GetWebsitesByUserId(userId string) ([]models.Website, error) {
	var websites []models.Website
	err := db.DB.NewSelect().Model(&websites).Where("user_id = ?", userId).Scan(context.Background())
	return websites, err
}

func (db *Database) GetWebsiteById(id string) (*models.Website, error) {
	var website models.Website
	err := db.DB.NewSelect().Model(&website).Where("id = ?", id).Scan(context.Background())
	return &website, err
}

func (db *Database) GetWebsiteBySlug(slug string) (*models.Website, error) {
	var website models.Website
	err := db.DB.NewSelect().Model(&website).Where("slug = ?", slug).Scan(context.Background())
	return &website, err
}

func (db *Database) CreateWebsite(w *models.Website) (*models.Website, error) {
	_, err := db.DB.NewInsert().Model(w).Exec(context.Background())
	return w, err
}

func (db *Database) UpdateWebsite(w *models.Website) (*models.Website, error) {
	_, err := db.DB.NewUpdate().Model(w).Where("id = ?", w.ID).Exec(context.Background())
	return w, err
}

func (db *Database) DeleteWebsite(w *models.Website) error {
	_, err := db.DB.NewDelete().Model(w).Where("id = ?", w.ID).Exec(context.Background())
	return err
}

// WebsitePage

func (db *Database) GetPagesByWebsiteId(websiteId string) ([]models.WebsitePage, error) {
	var pages []models.WebsitePage
	err := db.DB.NewSelect().Model(&pages).Where("website_id = ?", websiteId).Order("sort_order ASC").Scan(context.Background())
	return pages, err
}

func (db *Database) GetPageById(id string) (*models.WebsitePage, error) {
	var page models.WebsitePage
	err := db.DB.NewSelect().Model(&page).Where("id = ?", id).Scan(context.Background())
	return &page, err
}

func (db *Database) CreatePage(p *models.WebsitePage) (*models.WebsitePage, error) {
	_, err := db.DB.NewInsert().Model(p).Exec(context.Background())
	return p, err
}

func (db *Database) UpdatePage(p *models.WebsitePage) (*models.WebsitePage, error) {
	_, err := db.DB.NewUpdate().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	return p, err
}

func (db *Database) DeletePage(p *models.WebsitePage) error {
	_, err := db.DB.NewDelete().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	return err
}

// WebsiteAsset

func (db *Database) GetAssetsByWebsiteId(websiteId string) ([]models.WebsiteAsset, error) {
	var assets []models.WebsiteAsset
	err := db.DB.NewSelect().Model(&assets).Where("website_id = ?", websiteId).Scan(context.Background())
	return assets, err
}

func (db *Database) CreateAsset(a *models.WebsiteAsset) (*models.WebsiteAsset, error) {
	_, err := db.DB.NewInsert().Model(a).Exec(context.Background())
	return a, err
}

func (db *Database) DeleteAsset(a *models.WebsiteAsset) error {
	_, err := db.DB.NewDelete().Model(a).Where("id = ?", a.ID).Exec(context.Background())
	return err
}

// WebsiteTemplate

func (db *Database) GetActiveTemplates() ([]models.WebsiteTemplate, error) {
	var templates []models.WebsiteTemplate
	err := db.DB.NewSelect().Model(&templates).Where("is_active = ?", true).Scan(context.Background())
	return templates, err
}

func (db *Database) GetTemplateById(id string) (*models.WebsiteTemplate, error) {
	var template models.WebsiteTemplate
	err := db.DB.NewSelect().Model(&template).Where("id = ?", id).Scan(context.Background())
	return &template, err
}

// AIGenerationLog

func (db *Database) CreateAIGenerationLog(log *models.AIGenerationLog) (*models.AIGenerationLog, error) {
	_, err := db.DB.NewInsert().Model(log).Exec(context.Background())
	return log, err
}
