package models

import (
	"time"

	"github.com/google/uuid"
)

type StandartUserSubscription struct {
	ID           uuid.UUID          `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Name         string             `bun:",notnull"`
	Description  string             `bun:",notnull"`
	LimitsID     uuid.UUID          `bun:",notnull,unique,type:uuid"`
	Limits       StandartUserLimits `bun:"rel:belongs-to,join:limits_id=id"`
	MonthlyPrice int                `bun:",nullzero,notnull"`
	CreatedAt    time.Time          `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time          `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time          `bun:",soft_delete,nullzero"`
}
