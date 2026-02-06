package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

// WebsiteForm

func (db *Database) CreateForm(f *models.WebsiteForm) (*models.WebsiteForm, error) {
	_, err := db.DB.NewInsert().Model(f).Exec(context.Background())
	return f, err
}

func (db *Database) GetFormsByWebsiteId(websiteId string) ([]models.WebsiteForm, error) {
	var forms []models.WebsiteForm
	err := db.DB.NewSelect().Model(&forms).Where("website_id = ?", websiteId).Scan(context.Background())
	return forms, err
}

func (db *Database) GetFormById(id string) (*models.WebsiteForm, error) {
	var form models.WebsiteForm
	err := db.DB.NewSelect().Model(&form).Where("id = ?", id).Scan(context.Background())
	return &form, err
}

func (db *Database) UpdateForm(f *models.WebsiteForm) (*models.WebsiteForm, error) {
	_, err := db.DB.NewUpdate().Model(f).Where("id = ?", f.ID).Exec(context.Background())
	return f, err
}

func (db *Database) DeleteForm(f *models.WebsiteForm) error {
	_, err := db.DB.NewDelete().Model(f).Where("id = ?", f.ID).Exec(context.Background())
	return err
}

// WebsiteFormSubmission

func (db *Database) CreateFormSubmission(s *models.WebsiteFormSubmission) (*models.WebsiteFormSubmission, error) {
	_, err := db.DB.NewInsert().Model(s).Exec(context.Background())
	return s, err
}

func (db *Database) GetSubmissionsByFormId(formId string) ([]models.WebsiteFormSubmission, error) {
	var submissions []models.WebsiteFormSubmission
	err := db.DB.NewSelect().Model(&submissions).Where("form_id = ?", formId).Order("created_at DESC").Scan(context.Background())
	return submissions, err
}
