package models

import (
	"time"

	"github.com/google/uuid"
)

type StandartUserLimits struct {
	ID                      uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	MaximumProjectCount     int       `bun:",notnull"`
	MontlyNotificationCount int       `bun:",notnull"`
	MontlyBuildCount        int       `bun:",notnull"`
	CanDeployToAndroid      bool      `bun:",notnull"`
	CanDeployToIOS          bool      `bun:",notnull"`
	CanUseAnalytics         bool      `bun:",notnull"`
	CanRemoveBrandLogo      bool      `bun:",notnull"`
	CreatedAt               time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt               time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt               time.Time `bun:",soft_delete,nullzero"`
}
