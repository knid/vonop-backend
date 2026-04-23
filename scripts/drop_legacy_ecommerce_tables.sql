-- Drop legacy main-DB ecommerce tables.
--
-- Run ONCE against the wemob main Postgres after PR 5 of the per-tenant-Neon
-- migration merges and deploys. These tables are no longer written to or read
-- from by any Go code (all ecommerce data now lives on per-tenant Neon DBs).
--
-- This is a manual step because main-DB migrations are not automated in this
-- project — see CLAUDE.md ("No formal migration tool. Schema auto-created via
-- createSchema() in main.go using IfNotExists()").
--
-- Order matches foreign-key dependency (children first, then parents), though
-- CASCADE makes order flexible.
--
-- Safety: before running in production, confirm that no `AppIntegration` rows
-- or other main-DB data reference these tables. (As of PR 5, ecommerce data
-- was already decoupled; this is a paranoia check.)
--
-- Usage:
--   psql "$WEMOB_DATABASE_URL" -f scripts/drop_legacy_ecommerce_tables.sql

BEGIN;

DROP TABLE IF EXISTS wishlists           CASCADE;
DROP TABLE IF EXISTS product_reviews     CASCADE;
DROP TABLE IF EXISTS order_items         CASCADE;
DROP TABLE IF EXISTS orders              CASCADE;
DROP TABLE IF EXISTS cart_items          CASCADE;
DROP TABLE IF EXISTS carts               CASCADE;
DROP TABLE IF EXISTS discounts           CASCADE;
DROP TABLE IF EXISTS products            CASCADE;
DROP TABLE IF EXISTS product_categories  CASCADE;
DROP TABLE IF EXISTS stores              CASCADE;

COMMIT;
