package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

// WebsiteFile

func (db *Database) GetFilesByWebsiteId(websiteId string) ([]models.WebsiteFile, error) {
	var files []models.WebsiteFile
	err := db.DB.NewSelect().Model(&files).Where("website_id = ?", websiteId).Order("file_path ASC").Scan(context.Background())
	return files, err
}

func (db *Database) GetFileById(id string) (*models.WebsiteFile, error) {
	var file models.WebsiteFile
	err := db.DB.NewSelect().Model(&file).Where("id = ?", id).Scan(context.Background())
	return &file, err
}

func (db *Database) GetFileByWebsiteIdAndPath(websiteId string, filePath string) (*models.WebsiteFile, error) {
	var file models.WebsiteFile
	err := db.DB.NewSelect().Model(&file).Where("website_id = ? AND file_path = ?", websiteId, filePath).Scan(context.Background())
	return &file, err
}

func (db *Database) CreateFile(f *models.WebsiteFile) (*models.WebsiteFile, error) {
	_, err := db.DB.NewInsert().Model(f).Exec(context.Background())
	return f, err
}

func (db *Database) UpdateFile(f *models.WebsiteFile) (*models.WebsiteFile, error) {
	_, err := db.DB.NewUpdate().Model(f).Where("id = ?", f.ID).Exec(context.Background())
	return f, err
}

func (db *Database) DeleteFile(f *models.WebsiteFile) error {
	_, err := db.DB.NewDelete().Model(f).Where("id = ?", f.ID).Exec(context.Background())
	return err
}

func (db *Database) DeleteFilesByWebsiteId(websiteId string) error {
	_, err := db.DB.NewDelete().Model((*models.WebsiteFile)(nil)).Where("website_id = ?", websiteId).Exec(context.Background())
	return err
}
