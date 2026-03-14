package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type LegacyFile struct {
	AppProjectID string `bun:"app_project_id"`
	FilePath     string `bun:"file_path"`
	Content      string `bun:"content"`
	FileType     string `bun:"file_type"`
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL required")
	}
	basePath := os.Getenv("REPO_BASE_PATH")
	if basePath == "" {
		basePath = "/data/repos"
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	defer db.Close()

	svc := gitrepo.NewService(basePath)

	// Get distinct project IDs
	var projectIDs []string
	err := db.NewSelect().
		TableExpr("app_generated_files").
		ColumnExpr("DISTINCT app_project_id::text").
		Scan(context.Background(), &projectIDs)
	if err != nil {
		log.Fatalf("Failed to get project IDs: %v", err)
	}

	log.Printf("Migrating %d projects...", len(projectIDs))

	for i, pid := range projectIDs {
		var files []LegacyFile
		err := db.NewSelect().
			TableExpr("app_generated_files").
			Where("app_project_id = ?", pid).
			Order("file_path ASC").
			Scan(context.Background(), &files)
		if err != nil {
			log.Printf("  [%d/%d] SKIP %s: %v", i+1, len(projectIDs), pid, err)
			continue
		}

		repo, err := svc.Init(pid)
		if err != nil {
			log.Printf("  [%d/%d] SKIP %s: init failed: %v", i+1, len(projectIDs), pid, err)
			continue
		}

		for _, f := range files {
			if err := repo.WriteFile(f.FilePath, []byte(f.Content)); err != nil {
				log.Printf("    WARN: failed to write %s: %v", f.FilePath, err)
			}
		}

		// Also migrate theme from app_projects
		var theme []byte
		err = db.NewSelect().
			TableExpr("app_projects").
			Column("theme").
			Where("id = ?", pid).
			Scan(context.Background(), &theme)
		if err == nil && len(theme) > 2 {
			repo.WriteTheme(theme)
		}

		if _, err := repo.Commit("init: migrated from database"); err != nil {
			log.Printf("  [%d/%d] SKIP %s: commit failed: %v", i+1, len(projectIDs), pid, err)
			continue
		}

		log.Printf("  [%d/%d] OK %s (%d files)", i+1, len(projectIDs), pid, len(files))
	}

	fmt.Println("Migration complete.")
}
