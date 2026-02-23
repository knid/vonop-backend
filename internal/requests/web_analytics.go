package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type CollectAnalyticsRequest struct {
	ProjectID  string         `json:"project_id" validate:"required,uuid" errormgs:"must be a valid project UUID"`
	VisitorID  string         `json:"visitor_id" validate:"required,min=1,max=64" errormgs:"is required"`
	SessionID  string         `json:"session_id" validate:"required,min=1,max=64" errormgs:"is required"`
	EventType  string         `json:"event_type" validate:"required,oneof=pageview pageleave event" errormgs:"must be pageview, pageleave, or event"`
	EventName  string         `json:"event_name,omitempty" validate:"omitempty,max=100" errormgs:"must be at most 100 characters"`
	URL        string         `json:"url,omitempty" validate:"omitempty,max=2048" errormgs:"must be at most 2048 characters"`
	Path       string         `json:"path,omitempty" validate:"omitempty,max=2048" errormgs:"must be at most 2048 characters"`
	Referrer   string         `json:"referrer,omitempty" validate:"omitempty,max=2048" errormgs:"must be at most 2048 characters"`
	Title      string         `json:"title,omitempty" validate:"omitempty,max=500" errormgs:"must be at most 500 characters"`
	ScreenSize string         `json:"screen_size,omitempty" validate:"omitempty,max=20" errormgs:"must be at most 20 characters"`
	Language   string         `json:"language,omitempty" validate:"omitempty,max=20" errormgs:"must be at most 20 characters"`
	UTMSource   string        `json:"utm_source,omitempty" validate:"omitempty,max=200" errormgs:"must be at most 200 characters"`
	UTMMedium   string        `json:"utm_medium,omitempty" validate:"omitempty,max=200" errormgs:"must be at most 200 characters"`
	UTMCampaign string        `json:"utm_campaign,omitempty" validate:"omitempty,max=200" errormgs:"must be at most 200 characters"`
	UTMTerm     string        `json:"utm_term,omitempty" validate:"omitempty,max=200" errormgs:"must be at most 200 characters"`
	UTMContent  string        `json:"utm_content,omitempty" validate:"omitempty,max=200" errormgs:"must be at most 200 characters"`
	ScrollDepth int           `json:"scroll_depth,omitempty" validate:"omitempty,min=0,max=100" errormgs:"must be between 0 and 100"`
	Duration    int           `json:"duration,omitempty" validate:"omitempty,min=0" errormgs:"must be non-negative"`
	Properties  map[string]any `json:"properties,omitempty"`
}

func (r *CollectAnalyticsRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CollectAnalyticsRequest](*r, validate)
}

type CollectAnalyticsBatchRequest struct {
	Events []CollectAnalyticsRequest `json:"events" validate:"required,min=1,max=50,dive" errormgs:"must contain 1-50 events"`
}

func (r *CollectAnalyticsBatchRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CollectAnalyticsBatchRequest](*r, validate)
}
