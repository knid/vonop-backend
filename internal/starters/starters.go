// Package starters holds canonical starter schemas seeded into tenant databases
// at project provisioning time.
package starters

import _ "embed"

//go:embed ecommerce_schema.sql
var EcommerceSchemaSQL string
