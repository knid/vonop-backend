package models

import (
	"time"

	"github.com/google/uuid"
)

type AppDashboard struct {
	ID           uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID      `bun:",notnull,type:uuid"`
	AppProject   *AppProject    `bun:"rel:belongs-to,join:app_project_id=id"`
	UserID       uuid.UUID      `bun:",notnull,type:uuid"`
	User         *User          `bun:"rel:belongs-to,join:user_id=id"`
	Name         string         `bun:",notnull"`
	Description  string         `bun:",type:text"`
	Schema       []byte         `bun:",type:jsonb,default:'[]'"`
	Layout       []byte         `bun:",type:jsonb,default:'[]'"`
	Settings     map[string]any `bun:",type:jsonb,default:'{}'"`
	Provider     string         `bun:",notnull"` // "supabase" or "neon"
	Status       string         `bun:",notnull,default:'generating'"` // generating, ready, error
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time      `bun:",soft_delete,nullzero"`
}

type AppDashboardPanel struct {
	ID          uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	DashboardID uuid.UUID      `bun:",notnull,type:uuid"`
	Dashboard   *AppDashboard  `bun:"rel:belongs-to,join:dashboard_id=id"`
	Title       string         `bun:",notnull"`
	PanelType   string         `bun:",notnull"` // stat_card, bar_chart, line_chart, pie_chart, table, area_chart
	Query       []byte         `bun:",type:jsonb,default:'{}'"`
	Config      map[string]any `bun:",type:jsonb,default:'{}'"`
	SortOrder   int            `bun:",notnull,default:0"`
	GridWidth   int            `bun:",notnull,default:6"` // 1-12
	CreatedAt   time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time      `bun:",soft_delete,nullzero"`
}

// DashboardQuery represents the query configuration for a panel.
// This is stored as JSONB in AppDashboardPanel.Query.
type DashboardQuery struct {
	// Supabase REST
	Table   string        `json:"table,omitempty"`
	Select  string        `json:"select,omitempty"`
	Filters []QueryFilter `json:"filters,omitempty"`
	Order   string        `json:"order,omitempty"`
	Limit   int           `json:"limit,omitempty"`
	// Neon SQL
	SQL string `json:"sql,omitempty"`
	// Shared
	Transform string `json:"transform,omitempty"` // count, sum, avg, group_by, time_series
	GroupBy   string `json:"group_by,omitempty"`
	AggField  string `json:"agg_field,omitempty"`
	TimeField string `json:"time_field,omitempty"`
	TimeRange string `json:"time_range,omitempty"` // 7d, 30d, 90d, 1y
}

type QueryFilter struct {
	Column   string `json:"column"`
	Operator string `json:"operator"` // eq, neq, gt, gte, lt, lte, like, ilike, in, is
	Value    string `json:"value"`
}
