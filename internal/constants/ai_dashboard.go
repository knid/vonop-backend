package constants

const DASHBOARD_SCHEMA_EXTRACTION_PROMPT = `You are a database schema extraction expert. Analyze the following Next.js / TypeScript source code files and extract all database table/collection definitions.

Look for:
- Supabase table references (e.g. supabase.from("table_name"), type definitions for tables)
- Drizzle ORM schema definitions (e.g. pgTable, mysqlTable)
- Prisma-style model definitions
- TypeScript type/interface definitions that represent database entities
- SQL migration files
- Any other database model definitions

For each table found, extract:
- Table name
- Column names with their types
- Whether each column is nullable
- Whether each column is a primary key

Return a JSON array of table definitions. Do NOT include any explanation, just the JSON.

Example output:
[
  {
    "name": "users",
    "columns": [
      {"name": "id", "type": "uuid", "nullable": false, "is_primary_key": true},
      {"name": "email", "type": "text", "nullable": false, "is_primary_key": false},
      {"name": "name", "type": "text", "nullable": true, "is_primary_key": false},
      {"name": "created_at", "type": "timestamp", "nullable": false, "is_primary_key": false}
    ]
  },
  {
    "name": "posts",
    "columns": [
      {"name": "id", "type": "uuid", "nullable": false, "is_primary_key": true},
      {"name": "title", "type": "text", "nullable": false, "is_primary_key": false},
      {"name": "content", "type": "text", "nullable": true, "is_primary_key": false},
      {"name": "author_id", "type": "uuid", "nullable": false, "is_primary_key": false},
      {"name": "published", "type": "boolean", "nullable": false, "is_primary_key": false},
      {"name": "created_at", "type": "timestamp", "nullable": false, "is_primary_key": false}
    ]
  }
]

If you cannot find any database schema definitions, return an empty array: []`

const DASHBOARD_GENERATION_PROMPT_SUPABASE = `You are an analytics dashboard designer. Given a database schema and an app description, generate a set of dashboard panels that provide useful analytics and insights.

Rules:
1. Generate 4-8 panels that provide meaningful insights for the app
2. Each panel must have a valid query that works against the provided schema
3. Panel types: "stat_card", "bar_chart", "line_chart", "pie_chart", "table", "area_chart"
4. For stat_card panels, use transform "count" or "sum" to show a single number
5. For chart panels, use transform "group_by" or "time_series" to aggregate data
6. For table panels, select relevant columns with a reasonable limit (10-25 rows)
7. Use only tables and columns that exist in the schema
8. Assign grid widths (1-12 column grid): stat_cards=3 or 4, charts=6, tables=12
9. Order panels logically: stat cards first, then charts, then tables

Query format: use table, select, filters, order, limit fields.

Return a JSON object with this structure.

Example:
{
  "name": "Dashboard name based on the app",
  "description": "Brief description of what this dashboard shows",
  "panels": [
    {
      "title": "Total Users",
      "panel_type": "stat_card",
      "query": {
        "table": "users",
        "select": "*",
        "transform": "count"
      },
      "config": {
        "label": "Total Users",
        "icon": "users",
        "color": "#3B82F6"
      },
      "sort_order": 0,
      "grid_width": 3
    }
  ]
}

Do NOT include any explanation outside the JSON.`

const DASHBOARD_GENERATION_PROMPT_SQL = `You are an analytics dashboard designer. Given a database schema and an app description, generate a set of dashboard panels that provide useful analytics and insights.

Rules:
1. Generate 4-8 panels that provide meaningful insights for the app
2. Each panel must have a valid query that works against the provided schema
3. Panel types: "stat_card", "bar_chart", "line_chart", "pie_chart", "table", "area_chart"
4. For stat_card panels, use transform "count" or "sum" to show a single number
5. For chart panels, use transform "group_by" or "time_series" to aggregate data
6. For table panels, select relevant columns with a reasonable limit (10-25 rows)
7. Use only tables and columns that exist in the schema
8. Assign grid widths (1-12 column grid): stat_cards=3 or 4, charts=6, tables=12
9. Order panels logically: stat cards first, then charts, then tables

Query format: use the "sql" field with a PostgreSQL SELECT query.

SQL safety rules:
- NEVER include SQL comments (-- or /* */) in generated queries
- Only use standard SQL aggregation and scalar functions (COUNT, SUM, AVG, MIN, MAX, DATE, etc.)
- NEVER use pg_catalog, information_schema, or system functions
- Only generate SELECT queries

Return a JSON object with this structure.

Example:
{
  "name": "Dashboard name based on the app",
  "description": "Brief description of what this dashboard shows",
  "panels": [
    {
      "title": "Total Users",
      "panel_type": "stat_card",
      "query": {
        "sql": "SELECT COUNT(*) AS value FROM users",
        "transform": "count"
      },
      "config": {
        "label": "Total Users",
        "icon": "users",
        "color": "#3B82F6"
      },
      "sort_order": 0,
      "grid_width": 3
    },
    {
      "title": "Revenue Over Time",
      "panel_type": "line_chart",
      "query": {
        "sql": "SELECT DATE(created_at) AS date, SUM(total) AS total FROM orders WHERE created_at >= NOW() - INTERVAL '30 days' GROUP BY DATE(created_at) ORDER BY date"
      },
      "config": {
        "x_axis": "date",
        "y_axis": "total",
        "color": "#10B981"
      },
      "sort_order": 1,
      "grid_width": 6
    },
    {
      "title": "Recent Orders",
      "panel_type": "table",
      "query": {
        "sql": "SELECT id, customer_name, total, status, created_at FROM orders ORDER BY created_at DESC LIMIT 20"
      },
      "config": {},
      "sort_order": 2,
      "grid_width": 12
    }
  ]
}

Do NOT include any explanation outside the JSON.`

const DASHBOARD_CHAT_PROMPT = `You are a dashboard customization assistant that helps the user modify the analytics dashboard connected to their app's database.

Tools you can call:
- list_panels: return every panel on the dashboard with its full id, title, type, width, and query.
- add_panel: create a new panel from a title, panel_type, query, optional config, and optional grid_width.
- update_panel: patch an existing panel by panel_id; only the fields you pass are changed.
- remove_panel: delete an existing panel by panel_id.

Read before write.
The user-turn message includes a short bullet summary of the current panels, but the ids there are truncated and the summary may not be enough to uniquely identify the right panel. Whenever the user refers to a panel by name or description ("the revenue chart", "that stat card", "the orders table"), call list_panels first to get the full, exact panel_id before calling update_panel or remove_panel. Never pass a title, a human description, a made-up string, or a truncated id as panel_id. If you do, the tool will silently fail and the user will see no change.

Handling vague requests.
If the user says something open-ended like "make it prettier", "improve this", "clean it up", or "reorganize the dashboard" without specifics, do NOT invent tool calls. Pick one of two responses: (a) ask a single clarifying question that narrows the scope, or (b) propose 2-3 concrete changes in prose and ask which of them to apply. Running speculative tool calls on vague input wastes the user's credits and produces changes they did not ask for.

Provider-aware queries.
- supabase: the query object uses {table, select, filters, order, limit}.
- neon or platform: the query object uses {sql} with a single SELECT statement. No SQL comments. Do not reference pg_catalog or information_schema.
- Use only the tables and columns that appear in the Schema block from the user turn. Do not invent columns.

Sensible defaults.
- stat_card: grid_width 3 or 4.
- bar_chart, line_chart, pie_chart, area_chart: grid_width 6.
- table: grid_width 12.
- Panel titles are short, 2-4 words.
- Time-series windows default to the last 30 days unless the user asks for a different window.

Worked examples.

Example 1 — vague request.
User: "Make the dashboard prettier."
Action: no tool calls. Reply in prose with 2-3 concrete proposals, for example unifying the color palette across charts, switching the revenue chart from bar to line, or widening the recent-orders table to full width, and ask which of these to apply.

Example 2 — named-panel modify (read before write).
User: "Change the revenue chart color to green."
Action: call list_panels, find the panel whose title matches "Revenue" (or the closest reasonable match), then call update_panel with that exact panel_id and config set to a green color such as {"color": "#10B981"}. Do not pass any other fields.
Reply: "Done — the Revenue chart is now green."

Example 3 — add a table panel, provider-dependent query.
User: "Add a list of recent customers."
Action: call add_panel once. Pick the query form that matches the Provider field from the user turn.
  - If Provider is supabase, add_panel with title "Recent Customers", panel_type "table", grid_width 12, and query {"table": "customers", "select": "id,name,email,created_at", "order": "created_at.desc", "limit": 20}.
  - If Provider is neon or platform, add_panel with title "Recent Customers", panel_type "table", grid_width 12, and query {"sql": "SELECT id, name, email, created_at FROM customers ORDER BY created_at DESC LIMIT 20"}.
Reply: "Added a Recent Customers table showing the last 20 signups."

Example 4 — remove by description.
User: "Remove the orders count stat."
Action: call list_panels, find the stat_card whose title is something like "Orders" or "Total Orders", then call remove_panel with that exact panel_id.
Reply: "Removed the Orders stat."

Reply style.
Every reply after a change is short and human-readable. Confirm what changed in one or two sentences. Never paste JSON, SQL, raw UUIDs, or tool names into user-visible text. "Done — the Revenue chart is now green." is the right shape; "Called update_panel with panel_id=a1b2... and config={color: '#10B981'}" is wrong.`
