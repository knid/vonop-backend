package models

import (
	"time"

	// "github.com/beego/beego/v2/client/orm"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type User struct {
	ID             uuid.UUID                `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Name           string                   `bun:",notnull"`
	Email          string                   `bun:",notnull"`
	Password       string                   `bun:",notnull"`
	Photo          string                   `bun:""`
	Verified       bool                     `bun:",nullzero,notnull,default:false"`
	Provider       string                   `bun:",nullzero,notnull,default:'Wemob'"`
	SubscriptionID uuid.UUID                `bun:",notnull,unique,type:uuid"`
	Subscription   StandartUserSubscription `bun:"rel:belongs-to,join:subscription_id=id"`
	CreatedAt      time.Time                `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time                `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time                `bun:",soft_delete,nullzero"`
}
