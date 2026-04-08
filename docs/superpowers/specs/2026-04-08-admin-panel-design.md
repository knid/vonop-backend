# Wemob Admin Panel — Design Spec

## Overview

Full-featured admin panel for managing the Wemob platform. Two-part system: new admin API endpoints in the existing Go backend, and a separate Next.js frontend at `~/Projects/wemob-admin-panel`.

**Goal:** Give the platform admin (you) complete visibility and control over all platform data — users, apps, billing, AI credits, tickets, deployments, and more.

## Architecture

### Backend (wemob-backend)

New admin API routes under `/v1/admin/*`, protected by existing `RequireAuth` + `RequireAdmin` middleware chain. Follows the same layer pattern: views → controllers → database.

- All endpoints check `user.Type == "admin"`
- Reuses existing `Database`, `Controller`, `HttpViews` structs
- New files: `internal/views/admin_*.go`, `internal/controllers/admin_*.go`, `internal/database/admin_*.go`, `internal/requests/admin_*.go`, `internal/responses/admin_*.go`

### Frontend (wemob-admin-panel)

Separate Next.js 15 App Router project.

- **Stack:** Next.js 15, shadcn/ui, Tailwind CSS, TanStack Table, TanStack Query, Recharts, next-themes
- **Theme:** Dark, persistent sidebar layout
- **Auth:** Login via `POST /v1/users/login`, token stored in localStorage, sent as `Authorization: Bearer <token>` on all API calls

### Auth Flow

1. Admin navigates to admin panel URL
2. If no token, shown login page
3. Login calls `POST /v1/users/login` on the backend
4. Token stored client-side
5. All subsequent API calls include the token
6. Backend middleware validates token + verifies `user.Type == "admin"`

## Backend API Endpoints

All routes: `/v1/admin/*` with `RequireAuth(controller)` + `RequireAdmin()` middleware.

### Dashboard

| Method | Path | Description |
|--------|------|-------------|
| GET | `/dashboard` | Expanded aggregate stats (users, revenue, AI, apps, deployments) |

### Users

| Method | Path | Description |
|--------|------|-------------|
| GET | `/users` | Paginated list, search by name/email, filter by subscription status, date range |
| GET | `/users/{userId}` | Full user detail: subscription, credits balance, apps, orgs, recent activity |
| PUT | `/users/{userId}` | Edit user fields (name, email, type, subscription status) |
| DELETE | `/users/{userId}` | Soft delete user |
| POST | `/users/{userId}/impersonate` | Generate short-lived token to act as this user |
| POST | `/users/{userId}/reset-password` | Force reset password, return temporary password |
| POST | `/users/{userId}/credits` | Grant or deduct AI credits with reason |
| POST | `/users/{userId}/subscription` | Force assign a subscription plan and status |

### App Projects (V2)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/apps` | All apps across all users, search/filter by name, status, user |
| GET | `/apps/{appId}` | Full app detail (files count, deployments, conversations, settings) |
| PUT | `/apps/{appId}` | Edit status, settings |
| DELETE | `/apps/{appId}` | Soft delete |

### Projects (V1)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/projects` | All legacy mobile projects |
| GET | `/projects/{projectId}` | Detail with builds, deployments |
| DELETE | `/projects/{projectId}` | Soft delete |

### Subscriptions & Billing

| Method | Path | Description |
|--------|------|-------------|
| GET | `/subscriptions` | List all plans with user counts per plan |
| PUT | `/subscriptions/{subId}` | Edit plan details and limits |

### AI Credits & Token Usage

| Method | Path | Description |
|--------|------|-------------|
| GET | `/credits/transactions` | All transactions across users, filter by user/action/date |
| GET | `/credits/usage` | Token usage aggregates by model, action, time period |
| GET | `/credits/balances` | All user balances sorted by usage |

### Templates

Reuse existing admin CRUD endpoints in the template controller (already implemented).

### Organizations

| Method | Path | Description |
|--------|------|-------------|
| GET | `/organizations` | List all orgs |
| GET | `/organizations/{orgId}` | Detail with members |

### Tickets

| Method | Path | Description |
|--------|------|-------------|
| GET | `/tickets` | All tickets, filter by status/priority |
| GET | `/tickets/{ticketId}` | Detail |
| PUT | `/tickets/{ticketId}` | Update status, add response (sends email notification) |

### Generation Jobs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/jobs` | All jobs across users, filter by status/type |
| GET | `/jobs/{jobId}` | Detail with events |

### Deployments

| Method | Path | Description |
|--------|------|-------------|
| GET | `/deployments` | All web deployments across users |
| DELETE | `/deployments/{deploymentId}` | Force undeploy (Coolify teardown) |

### Analytics (Platform-wide)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/analytics/overview` | Signups over time, active users trend, revenue trend |

## Frontend Pages

### Layout

Persistent dark sidebar with navigation sections:

- **Dashboard**
- **Users**
- **Apps** (V2)
- **Projects** (V1)
- **Billing** (subscriptions + plans)
- **AI Credits**
- **Templates**
- **Organizations**
- **Tickets**
- **Jobs**
- **Deployments**
- **Analytics**

### Shared Patterns

All list pages share:
- Data table (TanStack Table) with column sorting, pagination, search
- Filter dropdowns (status, date range, type)
- Click row to navigate to detail view
- Bulk actions where relevant (delete, export)

### Dashboard Page

- KPI cards: total users, active subscriptions, MRR estimate, AI credits consumed, active apps
- Signups chart (last 30 days, Recharts line/bar chart)
- Recent users table (last 10 signups)
- Open tickets list
- Active/failed generation jobs
- Recent deployments

### Detail Pages (example: User Detail)

- Header: avatar, name, email, subscription badge, created date
- Action buttons: Impersonate, Reset Password, Grant Credits, Change Subscription, Delete
- Tabbed content: Overview, Apps, Credits & Usage, Billing, Activity

### Login Page

- Minimal centered card with email + password fields
- Calls existing login endpoint, redirects to dashboard on success

## God Mode Features

### Impersonate User

- Button on user detail page
- Backend generates a short-lived token for the target user
- Frontend opens the main Wemob dashboard URL in a new tab with that token
- Admin panel shows a banner: "Impersonating: user@email.com"

### Grant/Deduct Credits

- Modal: amount input (positive = grant, negative = deduct), reason text field
- Creates `AICreditTransaction` record, updates `AICreditsBalance`
- Visible in user's transaction history with admin action label

### Force Subscription Change

- Dropdown to select any plan + status override (active/inactive/free)
- Direct DB update, bypasses Stripe
- Logged as admin action

### Reset Password

- Generates temporary password, updates user record, hashes and saves it
- Shows password once in a modal with copy-to-clipboard button
- No email sent — admin copies and shares manually

### Force Delete / Undeploy

- Soft delete any entity with confirmation modal ("Type the name to confirm")
- Undeploy web apps triggers Coolify teardown via existing service

### Ticket Response

- Inline text editor on ticket detail page
- Updates ticket status + response field
- Sends email notification to the ticket creator via Brevo

## Tech Stack Summary

### Backend (additions to wemob-backend)
- New view handlers, controller methods, DB queries
- Follows existing patterns exactly
- No new dependencies

### Frontend (new project: wemob-admin-panel)
- Next.js 15 (App Router)
- TypeScript
- shadcn/ui (dark theme)
- Tailwind CSS
- TanStack Table (data tables)
- TanStack Query (data fetching, caching, mutations)
- Recharts (dashboard charts)
- next-themes (locked to dark)
