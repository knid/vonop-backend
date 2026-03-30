package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	fixTable(db, "app_projects", "id", "theme")
	fixTable(db, "app_snapshots", "id", "theme")
}

func fixTable(db *sql.DB, table, idCol, themeCol string) {
	query := fmt.Sprintf("SELECT %s, %s::text FROM %s WHERE octet_length(%s::text) > 500", idCol, themeCol, table, themeCol)
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		log.Fatalf("query %s failed: %v", table, err)
	}
	defer rows.Close()

	fixed := 0
	skipped := 0
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}

		decoded := decodeRecursive(raw)
		if decoded == raw {
			skipped++
			continue
		}

		// Strip removed fields
		cleaned := stripDeadFields(decoded)
		if cleaned == "" {
			log.Printf("  [%s] %s: failed to clean, skipping", table, id)
			skipped++
			continue
		}

		_, err := db.ExecContext(context.Background(),
			fmt.Sprintf("UPDATE %s SET %s = $1::jsonb WHERE %s = $2", table, themeCol, idCol),
			cleaned, id,
		)
		if err != nil {
			log.Printf("  [%s] %s: update failed: %v", table, id, err)
			skipped++
			continue
		}

		oldSize := len(raw)
		newSize := len(cleaned)
		log.Printf("  [%s] %s: %d -> %d bytes (%.0f%% reduction)", table, id, oldSize, newSize, float64(oldSize-newSize)/float64(oldSize)*100)
		fixed++
	}

	log.Printf("[%s] Done: %d fixed, %d skipped", table, fixed, skipped)
}

// decodeRecursive peels off base64 and JSON string layers until clean JSON object is reached.
func decodeRecursive(s string) string {
	s = strings.TrimSpace(s)

	// If it's already a JSON object, we're done
	if len(s) > 0 && s[0] == '{' {
		var obj map[string]any
		if json.Unmarshal([]byte(s), &obj) == nil {
			return s
		}
	}

	// Try unwrapping a JSON string (quoted value)
	if len(s) > 1 && s[0] == '"' {
		var unquoted string
		if json.Unmarshal([]byte(s), &unquoted) == nil {
			return decodeRecursive(unquoted)
		}
	}

	// Try base64 decoding
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// Try URL-safe base64
		decoded, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			return s // can't decode further
		}
	}
	return decodeRecursive(string(decoded))
}

// stripDeadFields removes dark_color_scheme, spacing, border_radius from the theme JSON.
func stripDeadFields(s string) string {
	var theme map[string]any
	if err := json.Unmarshal([]byte(s), &theme); err != nil {
		return ""
	}
	delete(theme, "dark_color_scheme")
	delete(theme, "spacing")
	delete(theme, "border_radius")
	out, err := json.Marshal(theme)
	if err != nil {
		return ""
	}
	return string(out)
}
