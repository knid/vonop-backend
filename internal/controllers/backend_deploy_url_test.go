package controllers

import (
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

func TestBackendDeployURL_NilProject(t *testing.T) {
	c := &Controller{}
	if got := c.BackendDeployURL(nil); got != "" {
		t.Errorf("nil project: got %q want empty", got)
	}
}

func TestBackendDeployURL_NoDBNoRuntimeApp_FallsBackToCanonical(t *testing.T) {
	c := &Controller{} // DB nil
	project := &models.AppProject{ID: uuid.New(), Slug: "acme"}
	got := c.BackendDeployURL(project)
	want := "https://acme.wemob.app"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
