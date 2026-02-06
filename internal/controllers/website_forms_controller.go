package controllers

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// CreateForm creates a new form for a website.
func (c *Controller) CreateForm(user *models.User, websiteId string, req requests.CreateFormRequest) (*responses.FormResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.CreateFormRequest](req, validate); err != nil {
		return nil, err
	}

	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	websiteUUID, err := uuid.Parse(websiteId)
	if err != nil {
		return nil, errors.New("invalid website id")
	}

	fields := make([]models.FormField, len(req.Fields))
	for i, f := range req.Fields {
		fields[i] = models.FormField{
			Name:     f.Name,
			Type:     f.Type,
			Label:    f.Label,
			Required: f.Required,
			Options:  f.Options,
		}
	}

	form := models.WebsiteForm{
		WebsiteID:         websiteUUID,
		FormName:          req.FormName,
		Fields:            fields,
		RedirectURL:       req.RedirectURL,
		EmailNotification: req.EmailNotification,
		NotifyEmail:       req.NotifyEmail,
	}

	created, err := c.DB.CreateForm(&form)
	if err != nil {
		return nil, fmt.Errorf("failed to create form: %v", err)
	}

	return formToResponse(created), nil
}

// GetForms returns all forms for a website.
func (c *Controller) GetForms(user *models.User, websiteId string) ([]responses.FormResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	forms, err := c.DB.GetFormsByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	result := make([]responses.FormResponse, len(forms))
	for i, f := range forms {
		result[i] = *formToResponse(&f)
	}
	return result, nil
}

// DeleteForm soft-deletes a form.
func (c *Controller) DeleteForm(user *models.User, websiteId string, formId string) error {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return err
	}

	form, err := c.DB.GetFormById(formId)
	if err != nil {
		return errors.New("form not found")
	}

	if form.WebsiteID.String() != websiteId {
		return errors.New("form does not belong to this website")
	}

	return c.DB.DeleteForm(form)
}

// GetFormSubmissions returns all submissions for a form.
func (c *Controller) GetFormSubmissions(user *models.User, websiteId string, formId string) ([]responses.FormSubmissionResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	form, err := c.DB.GetFormById(formId)
	if err != nil {
		return nil, errors.New("form not found")
	}

	if form.WebsiteID.String() != websiteId {
		return nil, errors.New("form does not belong to this website")
	}

	submissions, err := c.DB.GetSubmissionsByFormId(formId)
	if err != nil {
		return nil, err
	}

	result := make([]responses.FormSubmissionResponse, len(submissions))
	for i, s := range submissions {
		result[i] = responses.FormSubmissionResponse{
			ID:        s.ID,
			FormID:    s.FormID,
			Data:      s.Data,
			IPAddress: s.IPAddress,
			CreatedAt: s.CreatedAt,
		}
	}
	return result, nil
}

// SubmitForm handles public form submissions (no auth required).
func (c *Controller) SubmitForm(formId string, req requests.SubmitFormRequest, ipAddress string, userAgent string) (*responses.SuccessResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.SubmitFormRequest](req, validate); err != nil {
		return nil, err
	}

	form, err := c.DB.GetFormById(formId)
	if err != nil {
		return nil, errors.New("form not found")
	}

	// Validate required fields
	for _, field := range form.Fields {
		if field.Required {
			if val, ok := req.Data[field.Name]; !ok || val == "" || val == nil {
				return nil, fmt.Errorf("field '%s' is required", field.Label)
			}
		}
	}

	formUUID, _ := uuid.Parse(formId)
	submission := models.WebsiteFormSubmission{
		FormID:    formUUID,
		Data:      req.Data,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}

	_, err = c.DB.CreateFormSubmission(&submission)
	if err != nil {
		return nil, fmt.Errorf("failed to save submission")
	}

	// Log notification if enabled (email notification to be implemented via Brevo template)
	if form.EmailNotification && form.NotifyEmail != "" {
		slog.Info("form submission notification",
			"form", form.FormName,
			"notify_email", form.NotifyEmail,
			"submission_time", time.Now().Format(time.RFC3339),
		)
	}

	return &responses.SuccessResponse{Success: true}, nil
}

func formToResponse(f *models.WebsiteForm) *responses.FormResponse {
	fields := make([]responses.FormFieldResponse, len(f.Fields))
	for i, field := range f.Fields {
		fields[i] = responses.FormFieldResponse{
			Name:     field.Name,
			Type:     field.Type,
			Label:    field.Label,
			Required: field.Required,
			Options:  field.Options,
		}
	}

	return &responses.FormResponse{
		ID:                f.ID,
		WebsiteID:         f.WebsiteID,
		FormName:          f.FormName,
		Fields:            fields,
		RedirectURL:       f.RedirectURL,
		EmailNotification: f.EmailNotification,
		NotifyEmail:       f.NotifyEmail,
		CreatedAt:         f.CreatedAt,
		UpdatedAt:         f.UpdatedAt,
	}
}
