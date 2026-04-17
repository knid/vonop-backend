package controllers

import (
	"errors"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) CancelBuildTask(user *models.User, taskID string) error {
	task, err := c.DB.GetAppBuildTaskById(taskID)
	if err != nil {
		return errors.New("task not found")
	}

	project, err := c.DB.GetProjectById(task.ProjectID.String())
	if err != nil {
		return errors.New("project not found")
	}

	if project.UserID.String() != user.ID.String() {
		return errors.New("you do not have access to this resource")
	}

	if task.Finished {
		return errors.New("task is already finished")
	}

	return c.DB.CancelAppBuildTask(taskID)
}

func (c *Controller) GetAllAppBuildTasks(user *models.User) ([]responses.BuildTaskInformationResponse, error) {
	tasks, err := c.DB.GetAppBuildTasksByUser(user)
	if err != nil {
		return nil, err
	}

	var buildTaskInformationResponses []responses.BuildTaskInformationResponse
	for _, task := range tasks {
		buildTaskInformationResponse := responses.BuildTaskInformationResponse{
			ID:           task.ID,
			ProjectID:    task.ProjectID,
			BuildID:      task.BuildID,
			Finished:     task.Finished,
			Platform:     task.Platform,
			AutoDeploy:   task.AutoDeploy,
			Status:       task.StatusText,
			ErrorMessage: task.ErrorMessage,
			CreatedAt:    task.CreatedAt,
		}
		buildTaskInformationResponses = append(buildTaskInformationResponses, buildTaskInformationResponse)
	}

	return buildTaskInformationResponses, nil
}
