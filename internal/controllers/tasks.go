package controllers

import (
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetAllAppBuildTasks(user *models.User) ([]responses.BuildTaskInformationResponse, error) {
	tasks, err := c.DB.GetAppBuildTasksByUser(user)
	if err != nil {
		return nil, err
	}

	var buildTaskInformationResponses []responses.BuildTaskInformationResponse
	for _, task := range tasks {
		buildTaskInformationResponse := responses.BuildTaskInformationResponse{
			ID:         task.ID,
			ProjectID:  task.ProjectID,
			BuildID:    task.BuildID,
			Finished:   task.Finished,
			Platform:   task.Platform,
			AutoDeploy: task.AutoDeploy,
			Status:     task.StatusText,
			CreatedAt:  task.CreatedAt,
		}
		buildTaskInformationResponses = append(buildTaskInformationResponses, buildTaskInformationResponse)
	}

	return buildTaskInformationResponses, nil
}
