package database

import (
	"github.com/uptrace/bun"
)

type Database struct {
	DB *bun.DB
}

func NewDatabase(db *bun.DB) *Database {
	return &Database{
		DB: db,
	}
}
