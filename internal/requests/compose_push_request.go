package requests

import "github.com/go-playground/validator/v10"

// ComposePushRequest is the dashboard's "send a push" payload for a mobile_app_native project.
//
// AudienceType is one of "segments" or "external_ids". Only one set of audience
// IDs is read per request:
//   - "segments": audience_segments populated; defaults to ["Subscribed Users"] if empty.
//   - "external_ids": audience_external_user_ids populated (Better-Auth user IDs from P2.4 wiring).
//
// Title and Body are localised maps keyed by IETF tag. The dashboard ships
// at minimum {en: "..."}; agents that translate user content can add more.
// Validators: title ≤ 65 chars (OneSignal display cap on iOS lock screen),
// body ≤ 240 chars (Apple's lock-screen line-wrap cap).
type ComposePushRequest struct {
	AudienceType            string            `json:"audience_type" validate:"required,oneof=segments external_ids" errormgs:"audience_type must be segments or external_ids"`
	AudienceSegments        []string          `json:"audience_segments,omitempty" validate:"omitempty,max=10,dive,min=1,max=64"`
	AudienceExternalUserIDs []string          `json:"audience_external_user_ids,omitempty" validate:"omitempty,max=2000,dive,uuid4"`
	Title                   map[string]string `json:"title" validate:"required,min=1,dive,keys,min=2,max=8,endkeys,min=1,max=65"`
	Body                    map[string]string `json:"body" validate:"required,min=1,dive,keys,min=2,max=8,endkeys,min=1,max=240"`
}

// Validate runs the struct validator over r.
func (r *ComposePushRequest) Validate(v *validator.Validate) error {
	return v.Struct(r)
}
