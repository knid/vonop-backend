package database

import (
	"sync"
	"time"

	"github.com/knid/wemob/internal/models"
)

type projectCacheEntry struct {
	project *models.AppProject
	expiry  time.Time
}

var projectCache sync.Map

const projectCacheTTL = 5 * time.Second

func getProjectFromCache(id string) (*models.AppProject, bool) {
	val, ok := projectCache.Load(id)
	if !ok {
		return nil, false
	}
	entry := val.(*projectCacheEntry)
	if time.Now().After(entry.expiry) {
		projectCache.Delete(id)
		return nil, false
	}
	return entry.project, true
}

func setProjectInCache(id string, project *models.AppProject) {
	projectCache.Store(id, &projectCacheEntry{
		project: project,
		expiry:  time.Now().Add(projectCacheTTL),
	})
}

// InvalidateProjectCache removes a project from the in-memory cache.
// Call this whenever a project is updated or deleted.
func (db *Database) InvalidateProjectCache(id string) {
	projectCache.Delete(id)
}
