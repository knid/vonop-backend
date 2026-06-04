package agent

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestBuildRegistryFor_ExposesWriteFilesButNotWriteFile(t *testing.T) {
	registry := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp}, nil, "")

	names := map[string]bool{}
	for _, name := range registry.Names() {
		names[name] = true
	}
	if names["write_file"] {
		t.Fatal("write_file must not be exposed; use write_files for all full-file writes")
	}
	if !names["write_files"] {
		t.Fatal("expected write_files to be exposed")
	}
}
