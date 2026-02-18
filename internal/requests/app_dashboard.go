package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type CreateDashboardRequest struct {
	Prompt string `json:"prompt" validate:"omitempty,max=2000" errormgs:"must be at most 2000 characters"`
}

func (r *CreateDashboardRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateDashboardRequest](*r, validate)
}

type UpdateDashboardRequest struct {
	Name        string         `json:"name" validate:"omitempty,min=2,max=200" errormgs:"must be between 2 and 200 characters"`
	Description string         `json:"description" validate:"omitempty,max=1000" errormgs:"must be at most 1000 characters"`
	Settings    map[string]any `json:"settings,omitempty"`
}

func (r *UpdateDashboardRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateDashboardRequest](*r, validate)
}

type UpdateDashboardPanelRequest struct {
	Title     string         `json:"title" validate:"omitempty,min=1,max=200" errormgs:"must be between 1 and 200 characters"`
	PanelType string         `json:"panel_type" validate:"omitempty,oneof=stat_card bar_chart line_chart pie_chart table area_chart" errormgs:"must be a valid panel type"`
	Config    map[string]any `json:"config,omitempty"`
	SortOrder *int           `json:"sort_order,omitempty"`
	GridWidth *int           `json:"grid_width" validate:"omitempty,min=1,max=12" errormgs:"must be between 1 and 12"`
}

func (r *UpdateDashboardPanelRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateDashboardPanelRequest](*r, validate)
}

type DashboardChatRequest struct {
	Message string `json:"message" validate:"required,min=2" errormgs:"must be at least 2 characters"`
}

func (r *DashboardChatRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[DashboardChatRequest](*r, validate)
}
