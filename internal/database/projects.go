package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetProjects(user *models.User) ([]models.Project, error) {
	var projects []models.Project
	err := db.DB.NewSelect().Model(&projects).Where("user_id = ?", user.ID.String()).Scan(context.Background())

	return projects, err
}

func (db *Database) GetProjectById(id string) (*models.Project, error) {
	var project models.Project
	err := db.DB.NewSelect().Model(&project).Where("id = ?", id).Scan(context.Background())

	return &project, err
}

func (db *Database) GetProjectByUserAndURL(userID string, url string) (*models.Project, error) {
	var project models.Project
	err := db.DB.NewSelect().Model(&project).Where("user_id = ? AND url = ?", userID, url).Limit(1).Scan(context.Background())
	return &project, err
}

func (db *Database) CreateProject(project *models.Project) (*models.Project, error) {
	_, err := db.DB.NewInsert().Model(project).Exec(context.Background())

	return project, err
}

func (db *Database) UpdateProject(project *models.Project) (*models.Project, error) {
	_, err := db.DB.NewUpdate().Model(project).Where("id = ?", project.ID).Exec(context.Background())

	return project, err
}
func (db *Database) DeleteProject(project *models.Project) error {
	_, err := db.DB.NewDelete().Model(project).Where("id = ?", project.ID).Exec(context.Background())

	return err
}
