package controllers

import (
	"encoding/json"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestToTemplateDetailResponseDoesNotExposePrompt(t *testing.T) {
	resp := ToTemplateDetailResponse(&models.AppTemplate{
		Name:         "Starter",
		Slug:         "starter",
		Description:  "A starter template",
		Tags:         []string{"web"},
		TemplateType: "web",
		ProjectType:  models.ProjectTypeApp,
	})

	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := fields["prompt"]; ok {
		t.Fatalf("template detail response exposed prompt: %s", body)
	}
}
