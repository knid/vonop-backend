package controllers

import (
	"errors"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// CreateSnapshotBeforeEdit creates an automatic snapshot of a website's current files.
func (c *Controller) CreateSnapshotBeforeEdit(websiteId uuid.UUID, userId uuid.UUID, description string) error {
	files, err := c.DB.GetFilesByWebsiteId(websiteId.String())
	if err != nil {
		return err
	}

	snapshotFiles := make([]models.SnapshotFile, len(files))
	for i, f := range files {
		snapshotFiles[i] = models.SnapshotFile{
			FilePath: f.FilePath,
			Content:  f.Content,
			FileType: f.FileType,
			Layer:    f.Layer,
		}
	}

	snapshot := models.WebsiteSnapshot{
		WebsiteID:   websiteId,
		Files:       snapshotFiles,
		Description: description,
		Source:      "auto",
		CreatedBy:   userId,
	}

	_, err = c.DB.CreateSnapshot(&snapshot)
	return err
}

// CreateManualSnapshot creates a user-triggered snapshot.
func (c *Controller) CreateManualSnapshot(user *models.User, websiteId string, req requests.CreateSnapshotRequest) (*responses.SnapshotResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.CreateSnapshotRequest](req, validate); err != nil {
		return nil, err
	}

	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	files, err := c.DB.GetFilesByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	snapshotFiles := make([]models.SnapshotFile, len(files))
	for i, f := range files {
		snapshotFiles[i] = models.SnapshotFile{
			FilePath: f.FilePath,
			Content:  f.Content,
			FileType: f.FileType,
			Layer:    f.Layer,
		}
	}

	snapshot := models.WebsiteSnapshot{
		WebsiteID:   website.ID,
		Files:       snapshotFiles,
		Description: req.Description,
		Source:      "manual",
		CreatedBy:   user.ID,
	}

	created, err := c.DB.CreateSnapshot(&snapshot)
	if err != nil {
		return nil, err
	}

	return &responses.SnapshotResponse{
		ID:          created.ID,
		WebsiteID:   created.WebsiteID,
		Description: created.Description,
		Source:      created.Source,
		CreatedBy:   created.CreatedBy,
		CreatedAt:   created.CreatedAt,
	}, nil
}

// GetSnapshots lists all snapshots for a website.
func (c *Controller) GetSnapshots(user *models.User, websiteId string) ([]responses.SnapshotResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	snapshots, err := c.DB.GetSnapshotsByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	result := make([]responses.SnapshotResponse, len(snapshots))
	for i, s := range snapshots {
		result[i] = responses.SnapshotResponse{
			ID:          s.ID,
			WebsiteID:   s.WebsiteID,
			Description: s.Description,
			Source:      s.Source,
			CreatedBy:   s.CreatedBy,
			CreatedAt:   s.CreatedAt,
		}
	}
	return result, nil
}

// RestoreSnapshot restores a website to a snapshot state.
func (c *Controller) RestoreSnapshot(user *models.User, websiteId string, snapshotId string) (*responses.SuccessResponse, error) {
	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	snapshot, err := c.DB.GetSnapshotById(snapshotId)
	if err != nil {
		return nil, errors.New("snapshot not found")
	}

	if snapshot.WebsiteID.String() != websiteId {
		return nil, errors.New("snapshot does not belong to this website")
	}

	// Create safety snapshot before restoring
	c.CreateSnapshotBeforeEdit(website.ID, user.ID, "Auto-save before restore")

	// Delete all current files
	err = c.DB.DeleteFilesByWebsiteId(websiteId)
	if err != nil {
		return nil, errors.New("failed to clear current files")
	}

	// Recreate files from snapshot
	for _, sf := range snapshot.Files {
		file := models.WebsiteFile{
			WebsiteID: website.ID,
			FilePath:  sf.FilePath,
			Content:   sf.Content,
			FileType:  sf.FileType,
			Layer:     sf.Layer,
		}
		c.DB.CreateFile(&file)
	}

	website.UpdatedAt = time.Now()
	c.DB.UpdateWebsite(website)

	return &responses.SuccessResponse{Success: true}, nil
}

// UndoLastChange restores the website to the state before the last change.
func (c *Controller) UndoLastChange(user *models.User, websiteId string) (*responses.SuccessResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	snapshots, err := c.DB.GetSnapshotsByWebsiteId(websiteId)
	if err != nil || len(snapshots) < 2 {
		return nil, errors.New("no previous state to undo to")
	}

	// snapshots[0] is the most recent (current save), snapshots[1] is the "before" state
	return c.RestoreSnapshot(user, websiteId, snapshots[1].ID.String())
}
