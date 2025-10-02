package controllers

import "github.com/knid/wemob/internal/models"

func (c *Controller) GetAllAppBuildTasks(user *models.User) ([]models.AppBuildTask, error) {
	tasks, err := c.DB.GetAppBuildTasksByUser(user)
	if err != nil {
		return nil, err
	}

	return tasks, nil
}
