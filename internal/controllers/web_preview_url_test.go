package controllers

import (
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

func TestWebPreviewURL_NilProject(t *testing.T) {
	c := &Controller{}
	if got := c.WebPreviewURL(nil); got != "" {
		t.Errorf("nil project: got %q want empty", got)
	}
}

func TestWebPreviewURL_NoDBNoRuntimeApp_FallsBackToCanonical(t *testing.T) {
	c := &Controller{} // DB nil
	project := &models.AppProject{ID: uuid.New(), Slug: "acme"}
	got := c.WebPreviewURL(project)
	want := "https://acme.wemob.app/_web/"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
