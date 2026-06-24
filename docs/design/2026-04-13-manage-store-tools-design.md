# Store Management Agent Tools — Design

**Date:** 2026-04-13
**Status:** Approved — ready for implementation plan

## Goal

Give the main app-builder agent (`internal/agent/`) direct access to ecommerce store data — products, categories, discounts, store settings — via a set of atomic tools registered conditionally for ecommerce projects. A user building a shop in the main chat should be able to say "add 10 sample hats with images" or "rename the 'accessories' category to 'gear'" and have the agent execute it alongside normal code edits, in the same run, through native Anthropic `tool_use` blocks.

This replaces the current situation where store data can only be managed through the separate `StreamChatStoreManagement` controller path (which uses a hand-rolled JSON action protocol). That legacy path is **not** touched by this change — it stays as-is and can be cleaned up in a later task.

## Non-goals

- No changes to `StreamChatStoreManagement` or its wiring in `internal/views/ecommerce_views.go`.
- No changes to existing agent tools (`read_file`, `write_file`, `edit_file`, `delete_file`, `list_files`, `generate_image`, `web_search`, `execute_sql`).
- No changes to agent system prompts beyond each new tool's own description — no upfront store-state dump.
- No new controller CRUD paths for store data. All tools reuse the existing helpers in `ecommerce_controller.go` (`createProductFromChat`, `updateProductFromChat`, …).
- No separate per-tool credit deduction. The existing `TokenTracker`-based deduction at run end continues to cover all tool calls.

## Tool list

Fifteen tools, each an atomic `ToolExecutor` following the exact pattern of `ReadFileTool`, `ListFilesTool`, etc. in `internal/agent/tools_base.go`.

### Products (5)

| Tool | Purpose |
|---|---|
| `list_products` | Return all products for the store as a JSON array. No params. |
| `create_product` | Create one product. Params: `name` (required), `slug?`, `description?`, `price_cents` (required), `compare_at_price_cents?`, `sku?`, `inventory?`, `images?[]`, `tags?[]`, `category_slug?`, `status?`. |
| `update_product` | Update one product. Params: `identifier` (id or slug, required) + any updatable fields from `create_product`. |
| `delete_product` | Delete one product. Params: `identifier` (id or slug). |
| `bulk_create_products` | Create many products in one call. Params: `products: [...]` — same shape as `create_product`. |

### Categories (4)

| Tool | Purpose |
|---|---|
| `list_categories` | Return all categories as a JSON array. No params. |
| `create_category` | Create one category. Params: `name` (required), `slug?`, `description?`, `parent_slug?`. |
| `update_category` | Update one category. Params: `identifier` (id or slug) + updatable fields. |
| `delete_category` | Delete one category. Params: `identifier`. |

### Discounts (4)

| Tool | Purpose |
|---|---|
| `list_discounts` | Return all discounts as a JSON array. No params. *(Added during brainstorm — needed so the agent can look up existing discount codes before update/delete.)* |
| `create_discount` | Params: `code` (required), `type` (`percentage` or `fixed`), `value` (required), `starts_at?`, `ends_at?`, `usage_limit?`. |
| `update_discount` | Params: `identifier` (id or code) + updatable fields. |
| `delete_discount` | Params: `identifier`. |

### Store (2)

| Tool | Purpose |
|---|---|
| `get_store` | Return current store settings. No params. *(Added during brainstorm — needed so the agent can make informed `update_store_settings` calls instead of guessing.)* |
| `update_store_settings` | Params: any subset of `name?`, `currency?`, `locale?`, `tax_rate?`, `shipping_config?`, `payment_config?`, `notify_email?`. |

## Registration

In `internal/agent/registry.go`, `BuildRegistryFor` gets a new conditional block:

```go
if project != nil && project.ProjectType == "ecommerce" {
    r.Register(ListProductsTool{Deps: deps})
    r.Register(CreateProductTool{Deps: deps})
    r.Register(UpdateProductTool{Deps: deps})
    r.Register(DeleteProductTool{Deps: deps})
    r.Register(BulkCreateProductsTool{Deps: deps})
    r.Register(ListCategoriesTool{Deps: deps})
    r.Register(CreateCategoryTool{Deps: deps})
    r.Register(UpdateCategoryTool{Deps: deps})
    r.Register(DeleteCategoryTool{Deps: deps})
    r.Register(ListDiscountsTool{Deps: deps})
    r.Register(CreateDiscountTool{Deps: deps})
    r.Register(UpdateDiscountTool{Deps: deps})
    r.Register(DeleteDiscountTool{Deps: deps})
    r.Register(GetStoreTool{Deps: deps})
    r.Register(UpdateStoreSettingsTool{Deps: deps})
}
```

Condition is `project.ProjectType == "ecommerce"` — the same flag already used in `internal/agent/templates.go:48`, `internal/agent/context.go:36`, `internal/controllers/app_env_var_controller.go:115`, and `internal/controllers/app_database_controller.go:136`. No new flag is introduced.

## Lazy store creation

On the first call to *any* store tool for a project:

1. The tool's `Execute` invokes `Deps.StoreOp(ctx, rc.ProjectID, action, params)`.
2. `Controller.ExecuteStoreOp` (new) calls `DB.GetStoreByAppProjectID(projectID)`.
3. If no store row exists, it creates one using `CreateStore(project, user, project.Name)` — the project's own name becomes the store's default name.
4. Subsequent calls find the existing row.

No separate `init_store` tool. No error surfaced to the agent in the "store doesn't exist yet" case. The lazy path is idempotent.

## Dependency plumbing

A single new field on `internal/agent/dependencies.go`'s `RunDependencies`:

```go
StoreOp func(ctx context.Context, projectID, action string, params map[string]any) (result string, err error)
```

Every store tool's `Execute` method does the same three things:

1. Unmarshal `input json.RawMessage` into a strict per-tool struct (so the LLM gets clear schemas and validation errors).
2. Convert that struct into a `map[string]any` with the keys the existing `*FromChat` helpers already expect.
3. Call `rc.Deps.StoreOp(ctx, rc.ProjectID, "<action>", params)` and return its result.

In `internal/controllers/agent_deps.go`, `AgentDeps()` wires `StoreOp` to a new controller method:

```go
func (c *Controller) ExecuteStoreOp(
    ctx context.Context,
    projectID, action string,
    params map[string]any,
) (string, error)
```

which:

- Looks up / lazy-creates the `Store` row for the project.
- Dispatches on `action` via a `switch` that routes to the existing helpers in `ecommerce_controller.go`:
  - `create_product` → `createProductFromChat(store, params)`
  - `update_product` → `updateProductFromChat(store, params)`
  - `delete_product` → `deleteProductFromChat(store, params)`
  - `bulk_create_products` → `bulkCreateProductsFromChat(store, params)`
  - `list_products` → `DB.GetProductsByStoreID(store.ID.String())` → JSON
  - `create_category` → `createCategoryFromChat(store, params)`
  - … etc. for categories, discounts, store settings
  - `get_store` → marshal the store row to JSON
- Returns a JSON string for list/get actions, a short success string for mutations, or an error.

**No duplication** — every existing `*FromChat` helper is reused. The dispatcher is the one new piece of routing logic.

## Tool results

| Case | Return shape |
|---|---|
| `list_*` / `get_store` | JSON string (serialized array or object). Matches `execute_sql`'s pattern. |
| Create / update / delete (success) | Short human-readable string: `"created product 'Red Hat' (id: ..., slug: red-hat-a1b2c3d4)"`. The returned id/slug lets the agent reference the new entity in follow-up calls. |
| Validation / lookup failures | `isError=true` + descriptive message (e.g., `"product with slug 'red-hat' not found"`, `"category_slug 'gear' does not exist"`). Agent can retry with corrected input. |
| Infrastructure failures (DB down, etc.) | `err != nil`. Propagates as a run-level error; matches how `execute_sql` treats DB outages. |

## `bulk_create_products` semantics

- Accepts an array of product objects, each with the same shape as `create_product`.
- Per item, if `category_slug` is set and the category does not exist, **the whole call fails fast** with an error naming the first bad slug. The agent is expected to call `create_category` (or `list_categories` first) before retrying.
- Rationale: partial success would leave the store in a confusing half-populated state that's hard for the agent to reason about. Fail fast is cleaner and matches how the existing file tools behave on bad paths.
- Items without a `category_slug` are always allowed (products without a category are valid today).
- On success, returns `"created N products"` with a JSON array of `{slug, id}` for each so the agent can reference them.

## Image generation stays decoupled

No image handling is baked into the store tools. The LLM composes existing tools:

1. Call `generate_image` with a prompt describing the product photo.
2. Receive a URL.
3. Pass that URL into `create_product` or `bulk_create_products` via the `images` array.

This keeps each tool focused on one job and matches how the agent already handles image generation for app code.

## Prompt / context changes

**None beyond the tool descriptions themselves.** Specifically:

- No upfront dump of store state into the system prompt (unlike the old `StreamChatStoreManagement` which injected full products/categories/discounts JSON every turn).
- The ecommerce addendum in `internal/agent/prompt_addenda.go` may need a short line pointing to the new tools (e.g., "When the user asks to manage products, categories, discounts, or store settings, use the corresponding store tools rather than editing code."). This is the only prompt-level change and it's one sentence.

Context discovery is on-demand: the agent calls `list_products` / `list_categories` / `list_discounts` / `get_store` only when it actually needs to know current state. This avoids the stale-context problem the old chat had.

## File layout

```
internal/agent/
  tools_store.go    (new — all 15 store tool struct definitions)
  registry.go       (modified — conditional block in BuildRegistryFor)
  dependencies.go   (modified — add StoreOp field)

internal/controllers/
  agent_deps.go             (modified — wire StoreOp callback)
  ecommerce_controller.go   (modified — add ExecuteStoreOp dispatcher method; existing *FromChat helpers unchanged)
```

Following the pattern of `tools_db.go` (single file for the DB tool) and `tools_mobile.go` (single file for the mobile tool), all fifteen store tools go in one new `tools_store.go`. Each struct is small (Name, Description, Schema, Execute) and sharing a file keeps related code co-located.

## Testing

Per `CLAUDE.md`, this project has no Go test suite and adding `_test.go` files is out of scope. Verification during implementation:

- `go build ./...` and `go vet ./...` after each phase.
- Manual verification against a staging ecommerce project: create a product, list products, update the product, delete it, create a category, bulk-create 5 products with a valid category_slug, bulk-create with a bad category_slug (should fail with a clear error), update store settings, verify lazy store creation on a fresh project.

## Out of scope (future cleanup candidates)

- Retiring `StreamChatStoreManagement` and the `/ecommerce/.../chat` view route. Once the agent can do everything it does (through the main builder chat), the separate endpoint becomes dead code.
- Per-tool credit surcharges. If store operations prove to be expensive in ways token tracking doesn't capture, they can be added later.
- Inventory-level tools (stock adjustments, low-stock alerts). Not in the original scope.
