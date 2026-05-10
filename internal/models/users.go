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
	Type                string                    `bun:",notnull,default:'standard'"`
	Company             string                    `bun:",nullzero"`
	Provider            string                    `bun:",nullzero,notnull,default:'Wemob'"`
	SubscriptionID      uuid.UUID                 `bun:",type:uuid,nullzero"`
	Subscription        *StandardUserSubscription `bun:"rel:belongs-to,join:subscription_id=id"`
	SubscriptionStatus  string                    `bun:",notnull,default:'inactive'"`
	SubscriptionStartAt time.Time                 `bun:""`
	TrialStartAt        time.Time                 `bun:""`
	CustomerID          string                    `bun:",nullzero"`
	TrialUsed           bool                      `bun:",notnull,default:false"`
	SetupFinished       bool                      `bun:",notnull,default:false"`
	GitHubAccessToken string `bun:",nullzero" json:"-"`
	GitHubUsername    string `bun:",nullzero" json:"github_username"`
	// Meta CAPI ad attribution. Captured from request body (fbp/fbc/url) and
	// headers (ip/ua) on register and checkout, then read by the async Stripe
	// webhook to keep Subscribe/Purchase match quality high. Never returned
	// in JSON responses.
	MetaFbp            string `bun:",nullzero" json:"-"`
	MetaFbc            string `bun:",nullzero" json:"-"`
	MetaClientIP       string `bun:",nullzero" json:"-"`
	MetaClientUA       string `bun:",nullzero" json:"-"`
	MetaEventSourceURL string `bun:",nullzero" json:"-"`
	Language            string                    `bun:",notnull,default:'en'" json:"language"`
	CreatedAt           time.Time                 `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt           time.Time                 `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt           time.Time                 `bun:",soft_delete,nullzero"`
}
