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

	// Scope ownership through AppProject (mobile builds carry app_project_id;
	// the legacy v1 project_id path is dropped — those builds had project_id
	// pointing at a shadow row that's no longer being written).
	if task.AppProjectID == nil {
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

// GetAllAppBuildTasks returns build tasks for the authenticated user.
// When projectID is non-empty, the result is scoped to that AppProject
// (still ownership-checked via the join in the DB layer); when empty,
// returns the user's full task history (legacy behavior — kept so any
// existing caller that doesn't yet pass a project_id keeps working, but
// the Publish UI always passes one now).
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
		// keys the build-card list off this field. Older v1 tasks (none in
		// prod) would carry only the v1 project_id; for those we fall through
		// to zero, which the frontend already handles.
		var projectIDOut uuid.UUID
		if task.AppProjectID != nil {
			projectIDOut = *task.AppProjectID
		}
		buildTaskInformationResponse := responses.BuildTaskInformationResponse{
			ID:           task.ID,
			ProjectID:    projectIDOut,
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
