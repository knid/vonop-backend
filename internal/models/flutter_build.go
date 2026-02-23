package models

import (
	"time"

	"github.com/google/uuid"
)

type FlutterBuild struct {
	ID           uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID      `bun:",notnull,type:uuid"`
	AppProject   *AppProject    `bun:"rel:belongs-to,join:app_project_id=id"`
	Platform     string         `bun:",notnull"` // "android", "ios"
	Status       string         `bun:",notnull,default:'pending'"` // pending, building, success, failed
	TaskID       string         `bun:",nullzero"` // Asynq task ID
	ArtifactPath string         `bun:",nullzero"` // S3 path to built artifact
	BuildLog     string         `bun:",nullzero,type:text"`
	Version      int            `bun:",notnull,default:1"`
	Config       map[string]any `bun:",type:jsonb,default:'{}'"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
