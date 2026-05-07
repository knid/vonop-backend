package controllers

import (
	"errors"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) CancelBuildTask(user *models.User, taskID string) error {
	task, err := c.DB.GetAppBuildTaskById(taskID)
	if err != nil {
		return errors.New("task not found")
	}

	// Scope ownership through AppProject. Build tasks always carry
	// app_project_id directly (the v1 shadow Project FK was removed
	// 2026-05-07).
	if task.AppProjectID == uuid.Nil {
		return errors.New("task not found")
	}
	appProject, err := c.DB.GetAppProjectByID(task.AppProjectID.String())
	if err != nil || appProject == nil || appProject.ID == uuid.Nil {
		return errors.New("task not found")
	}
	if appProject.UserID.String() != user.ID.String() {
		return errors.New("you do not have access to this resource")
	}

	if task.Finished {
		return errors.New("task is already finished")
	}

	return c.DB.CancelAppBuildTask(taskID)
}

// GetAllAppBuildTasks returns build tasks for the authenticated user. When
// projectID is non-empty, the result is scoped to that AppProject (the DB
// layer enforces ownership via the join). When empty, returns the user's
// full build-task history across every AppProject they own.
func (c *Controller) GetAllAppBuildTasks(user *models.User, projectID string) ([]responses.BuildTaskInformationResponse, error) {
	var (
		tasks []models.AppBuildTask
		err   error
	)
	if projectID != "" {
		tasks, err = c.DB.GetAppBuildTasksByUserAndAppProject(user, projectID)
	} else {
		tasks, err = c.DB.GetAppBuildTasksByUser(user)
	}
	if err != nil {
		return nil, err
	}

	var buildTaskInformationResponses []responses.BuildTaskInformationResponse
	for _, task := range tasks {
		// Surface the AppProject FK as project_id in the JSON — the frontend
		// keys the build-card list off this field.
		buildTaskInformationResponse := responses.BuildTaskInformationResponse{
			ID:           task.ID,
			ProjectID:    task.AppProjectID,
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
