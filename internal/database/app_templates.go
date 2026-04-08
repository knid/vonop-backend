package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) CreateAppTemplate(t *models.AppTemplate) (*models.AppTemplate, error) {
	_, err := db.DB.NewInsert().Model(t).Exec(context.Background())
	return t, err
}

func (db *Database) GetAppTemplateByID(id string) (*models.AppTemplate, error) {
	var t models.AppTemplate
	err := db.DB.NewSelect().Model(&t).Where("id = ?", id).Scan(context.Background())
	return &t, err
}

func (db *Database) GetAppTemplateBySlug(slug string) (*models.AppTemplate, error) {
	var t models.AppTemplate
	err := db.DB.NewSelect().Model(&t).Where("slug = ?", slug).Scan(context.Background())
	return &t, err
}

func (db *Database) UpdateAppTemplate(t *models.AppTemplate) (*models.AppTemplate, error) {
	_, err := db.DB.NewUpdate().Model(t).Where("id = ?", t.ID).Exec(context.Background())
	return t, err
}

func (db *Database) DeleteAppTemplate(t *models.AppTemplate) error {
	_, err := db.DB.NewDelete().Model(t).Where("id = ?", t.ID).Exec(context.Background())
	return err
}

// ListPublishedTemplates returns published templates with optional tag and type filtering.
// Results are ordered by sort_order ASC, then created_at DESC.
func (db *Database) ListPublishedTemplates(tag, templateType string) ([]models.AppTemplate, error) {
	var templates []models.AppTemplate
	q := db.DB.NewSelect().Model(&templates).
		Where("published = ?", true)
	if tag != "" {
		q = q.Where("tags @> ?", bun.Safe("'[\""+tag+"\"]'"))
	}
	if templateType != "" {
		q = q.Where("template_type = ?", templateType)
	}
	err := q.Order("sort_order ASC", "created_at DESC").Scan(context.Background())
	return templates, err
}

// ListAllTemplates returns all templates (including unpublished) for admin use.
func (db *Database) ListAllTemplates() ([]models.AppTemplate, error) {
	var templates []models.AppTemplate
	err := db.DB.NewSelect().Model(&templates).
		Order("sort_order ASC", "created_at DESC").
		Scan(context.Background())
	return templates, err
}
