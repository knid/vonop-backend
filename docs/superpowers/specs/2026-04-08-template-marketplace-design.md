# Template Marketplace Design

## Overview

A template marketplace where users can browse ready-made web and mobile apps, preview them live, and copy them as a starting point for their own project. Templates are fully editable after copying — users can change everything.

**Phase 1:** Wemob team creates and curates templates via a dedicated admin interface.
**Phase 2 (future):** Open template publishing to users.

## Requirements

- Templates are **free** — no paid templates
- Public marketplace page (SEO/marketing) + in-app browsing
- **Live preview** on template detail page
- Copying a template **counts toward the user's app limit**
- Templates organized by **tags** (flexible labels, no fixed categories)
- Templates support web, mobile, or both app types

## Data Model

### AppTemplate

| Field           | Type           | Description                                       |
|-----------------|----------------|---------------------------------------------------|
| ID              | uuid (pk)      | Primary key                                       |
| Name            | string         | Template display name                             |
| Slug            | string (unique)| URL-friendly identifier for public URLs           |
| Description     | text           | Template description                              |
| Tags            | jsonb ([]string)| Flexible tags for filtering (e.g. "restaurant")  |
| TemplateType    | string         | "web", "mobile", or "both"                       |
| PreviewImageKey | string         | S3 key for screenshot/thumbnail                   |
| PreviewURL      | string         | Live preview URL of the deployed template         |
| Theme           | jsonb          | AppTheme structure (same as AppProject)           |
| DataModels      | jsonb          | Data model definitions (same as AppProject)       |
| Settings        | jsonb          | App settings (same as AppProject)                 |
| ProjectType     | string         | "app", "ecommerce", or "mobile_app"              |
| Prompt          | text           | Original prompt used to generate the template     |
| SourceRepoID    | string         | Git repo ID holding the template source files     |
| Published       | bool           | Only visible in marketplace when true             |
| CreatedByID     | uuid (fk)      | Admin user who created the template               |
| SortOrder       | int            | Manual ordering in marketplace                    |
| CreatedAt       | timestamp      | Creation timestamp                                |
| UpdatedAt       | timestamp      | Last update timestamp                             |
| DeletedAt       | timestamp      | Soft delete                                       |

### AppProject Addition

Add `TemplateID` (uuid, nullable, fk → AppTemplate) to `AppProject` to track which template a project was copied from (useful for analytics).

## API Endpoints

### Public (no auth)

| Method | Path                    | Description                              |
|--------|-------------------------|------------------------------------------|
| GET    | /v2/templates           | List published templates (tag filtering) |
| GET    | /v2/templates/{slug}    | Get template detail + preview URL        |

**Query parameters for listing:**
- `tag` — filter by tag (e.g. `?tag=restaurant`)
- `type` — filter by template type (e.g. `?type=web`)

### Authenticated User

| Method | Path                        | Description                        |
|--------|-----------------------------|------------------------------------|
| POST   | /v2/templates/{slug}/copy   | Copy template into user's apps     |

### Admin

| Method | Path                                | Description              |
|--------|-------------------------------------|--------------------------|
| POST   | /v2/admin/templates                 | Create template          |
| GET    | /v2/admin/templates                 | List all (incl. unpublished) |
| GET    | /v2/admin/templates/{id}            | Get template detail      |
| PUT    | /v2/admin/templates/{id}            | Update template metadata |
| DELETE | /v2/admin/templates/{id}            | Delete template          |
| PUT    | /v2/admin/templates/{id}/publish    | Publish/unpublish        |
| PUT    | /v2/admin/templates/{id}/sort       | Update sort order        |

## Admin Template Creation Flow

When an admin creates a template:

1. Admin calls `POST /v2/admin/templates` with metadata (name, description, tags, etc.)
2. Backend creates the `AppTemplate` record and initializes a new git repo via `GitRepoService.Init()` — the repo ID is stored as `SourceRepoID`
3. Admin builds the template app using the normal app builder tools (chat, file CRUD) against the template's repo
4. When ready, admin publishes via `PUT /v2/admin/templates/{id}/publish`

This keeps template creation consistent with normal app building — the admin just targets a template repo instead of an app project repo.

## Copy Flow

When a user copies a template:

1. Validate user authentication
2. Check user's app limit (same logic as `CreateAppProject`) — reject if at limit
3. Create new `AppProject` with fields cloned from template:
   - Name, Prompt, ProjectType, Theme, DataModels, Settings
   - Generate new unique slug
   - Set `TemplateID` to the source template's ID
   - Set Status to "ready"
4. Clone the template's git repo into the new project's repo via `GitRepoService.Clone()`
5. Return the new app project to the user

This reuses the proven cloning pattern from `CloneAppProjectAsMobile`.

## File Structure

```
internal/
├── models/
│   └── app_template.go              — AppTemplate model + TemplateID on AppProject
├── requests/
│   └── app_template_requests.go     — CreateTemplate, UpdateTemplate request structs
├── responses/
│   └── app_template_responses.go    — TemplateList, TemplateDetail response structs
├── controllers/
│   └── app_template_controller.go   — CRUD, copy, publish, tag filtering logic
├── views/
│   └── app_template_views.go        — HTTP handlers for all template endpoints
```

Database queries are added to the existing database layer. Routes registered in `cmd/main.go` following existing patterns.

## Auth & Access

- **Public endpoints**: No auth middleware — query only published templates
- **Copy endpoint**: Standard user auth via `RequireAuth` + `RequireSubscription` middleware
- **Admin endpoints**: Existing admin auth check from `admin_view.go`

## Tag Filtering

Tags stored as a jsonb string array. Filtered using PostgreSQL jsonb operators via Bun ORM:

```sql
SELECT * FROM app_templates WHERE tags @> '["restaurant"]' AND published = true
```

## Future Considerations (Not in Scope)

- User-published templates (Phase 2)
- Template ratings/reviews
- Template usage analytics dashboard
- Paid/premium templates
