package models

import (
	"time"

	"github.com/google/uuid"
)

type Deployment struct {
	ID                      uuid.UUID              `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID                  uuid.UUID              `bun:",notnull,type:uuid"`
	User                    *User                  `bun:"rel:belongs-to,join:user_id=id"`
	ProjectID               uuid.UUID              `bun:",notnull,type:uuid"`
	Project                 *Project               `bun:"rel:belongs-to,join:project_id=id"`
	BuildID                 uuid.UUID              `bun:",notnull,type:uuid"`
	Build                   *Build                 `bun:"rel:belongs-to,join:build_id=id"`
	Platforms               []string               `bun:",array,notnull"`
	IconPath                string                 `bun:",notnull"`
	Title                   string                 `bun:",notnull"`
	Description             string                 `bun:",notnull"`
	Status                  string                 `bun:",notnull"`
	StatusMessage           string                 `bun:",notnull"`
	ScreenshotPaths         []string               `bun:",array,notnull"`
	Method                  string                 `bun:",notnull"`
	Locale                  string                 `bun:",notnull"`
	AppleConnectionID       uuid.UUID              `bun:",type:uuid"`
	GooglePlayConnectionID  uuid.UUID              `bun:",type:uuid"`
	AppleConnection         *AppleConnection       `bun:"rel:belongs-to,join:apple_connection_id=id"`
	GooglePlayConnection    *GooglePlayConnection  `bun:"rel:belongs-to,join:google_play_connection_id=id"`
	// ReleaseNotes is the "what's new in this version" copy captured at
	// submission time. Populated by QuickShip; empty on Pilot-first
	// submissions (first-time submissions send metadata via deliver/supply
	// configured elsewhere).
	ReleaseNotes string `bun:",nullzero,type:text"`
	// RolloutTrack names the target track selected by the user.
	// Vocabulary: "internal" | "closed" | "open" | "production" (Play Store)
	// or "internal" | "closed" | "production" (App Store TestFlight).
	// Empty on Pilot submissions (which always target Production).
	RolloutTrack string `bun:",nullzero"`
	CreatedAt               time.Time              `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt               time.Time              `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt               time.Time              `bun:",soft_delete,nullzero"`
}
