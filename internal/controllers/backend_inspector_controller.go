package controllers

import (
	"regexp"
	"sort"
	"strings"
)

// RouteEntry is one method-handler exported from a Next.js route.ts file.
type RouteEntry struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	File   string `json:"file"`
}

var routeFilePattern = regexp.MustCompile(`^backend/app/api/(?:.*/)?route\.ts$`)
var handlerExportPattern = regexp.MustCompile(`export\s+(?:async\s+)?function\s+(GET|POST|PUT|DELETE|PATCH|OPTIONS|HEAD)\b`)

// ParseRoutes scans a map of repo-relative file paths to file contents and
// returns the list of API route handlers exported from
// backend/app/api/**/route.ts. Pure; does no IO.
//
// Limitation: scans raw text, so a single-line-commented handler signature
// (`// export async function GET() {}`) is incorrectly counted. Acceptable
// for the studio's "what handlers exist?" view; not suitable as a lint.
func ParseRoutes(files map[string]string) []RouteEntry {
	var out []RouteEntry
	for path, content := range files {
		if !routeFilePattern.MatchString(path) {
			continue
		}
		urlPath := nextRouteURL(path)
		for _, m := range handlerExportPattern.FindAllStringSubmatch(content, -1) {
			out = append(out, RouteEntry{
				Method: m[1],
				Path:   urlPath,
				File:   path,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// nextRouteURL converts a Next.js route file path to its URL path.
// Examples:
//
//	backend/app/api/todos/route.ts                  → /api/todos
//	backend/app/api/users/[id]/route.ts             → /api/users/:id
//	backend/app/api/files/[...slug]/route.ts        → /api/files/*slug
//	backend/app/api/users/[id]/posts/[pid]/route.ts → /api/users/:id/posts/:pid
func nextRouteURL(filePath string) string {
	trimmed := strings.TrimPrefix(filePath, "backend/app")
	trimmed = strings.TrimSuffix(trimmed, "/route.ts")
	if trimmed == "" {
		return "/"
	}
	parts := strings.Split(strings.TrimPrefix(trimmed, "/"), "/")
	for i, part := range parts {
		switch {
		case strings.HasPrefix(part, "[...") && strings.HasSuffix(part, "]"):
			parts[i] = "*" + part[4:len(part)-1]
		case strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]"):
			parts[i] = ":" + part[1:len(part)-1]
		}
	}
	return "/" + strings.Join(parts, "/")
}
