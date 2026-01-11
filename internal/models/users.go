package models

import (
	"time"

	// "github.com/beego/beego/v2/client/orm"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type User struct {
	ID                  uuid.UUID                 `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Name                string                    `bun:",notnull"`
	Email               string                    `bun:",unique,notnull"`
	Password            string                    `bun:",notnull"`
	Photo               string                    `bun:""`
	CompanyLogo         string                    `bun:""`
	Verified            bool                      `bun:",notnull,default:false"`
	Type                string                    `bun:",notnull,default:'standart'"`
	Company             string                    `bun:",nullzero"`
	Provider            string                    `bun:",nullzero,notnull,default:'Wemob'"`
	SubscriptionID      uuid.UUID                 `bun:",type:uuid"`
	Subscription        *StandartUserSubscription `bun:"rel:belongs-to,join:subscription_id=id"`
	SubscriptionStatus  string                    `bun:",notnull,default:'inactive'"`
	SubscriptionStartAt time.Time                 `bun:""`
	TrialStartAt        time.Time                 `bun:""`
	CustomerID          string                    `bun:",nullzero"`
	TrialUsed           bool                      `bun:",notnull,default:false"`
	SetupFinished       bool                      `bun:",notnull,default:false"`
	CreatedAt           time.Time                 `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt           time.Time                 `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt           time.Time                 `bun:",soft_delete,nullzero"`
}
