import "server-only";
// lib/discounts.ts — Single source of truth for discount validity and
// amount math. Imported by every route that applies a discount (cart view,
// apply endpoint, checkout) so one change in the rules propagates everywhere.
// This file is template-managed. Do not modify.

import { and, eq, isNull } from "drizzle-orm";

import { db, schema } from "./db";
import type { Discount } from "./schema";

export type DiscountResolution =
  | { kind: "applied"; discount: Discount; cents: number }
  | { kind: "invalid"; reason: DiscountInvalidReason };

export type DiscountInvalidReason =
  | "not_found"
  | "not_yet_valid"
  | "expired"
  | "exhausted"
  | "below_minimum";

/**
 * Load a discount by id and compute the cents it would remove from a given
 * subtotal. Centralises every validity gate:
 *   - row exists + isActive + !deletedAt
 *   - startsAt <= now < endsAt
 *   - used_count < max_uses (0 means unlimited)
 *   - subtotal >= min_order_cents
 * The returned `cents` is already clamped to [0, subtotal].
 */
export async function resolveDiscountById(
  discountId: string,
  subtotal: number,
  now: Date = new Date(),
): Promise<DiscountResolution> {
  const [row] = await db
    .select()
    .from(schema.discounts)
    .where(
      and(
        eq(schema.discounts.id, discountId),
        eq(schema.discounts.isActive, true),
        isNull(schema.discounts.deletedAt),
      ),
    );
  if (!row) return { kind: "invalid", reason: "not_found" };
  return evaluateDiscount(row, subtotal, now);
}

/**
 * Load a discount by code (the customer-facing identifier) and evaluate it
 * against a subtotal. Used by the apply endpoint before the code is attached
 * to the cart row.
 */
export async function resolveDiscountByCode(
  code: string,
  subtotal: number,
  now: Date = new Date(),
): Promise<DiscountResolution> {
  const [row] = await db
    .select()
    .from(schema.discounts)
    .where(
      and(
        eq(schema.discounts.code, code),
        eq(schema.discounts.isActive, true),
        isNull(schema.discounts.deletedAt),
      ),
    );
  if (!row) return { kind: "invalid", reason: "not_found" };
  return evaluateDiscount(row, subtotal, now);
}

function evaluateDiscount(
  row: Discount,
  subtotal: number,
  now: Date,
): DiscountResolution {
  if (row.startsAt && row.startsAt > now) {
    return { kind: "invalid", reason: "not_yet_valid" };
  }
  if (row.endsAt && row.endsAt < now) {
    return { kind: "invalid", reason: "expired" };
  }
  if (row.maxUses > 0 && row.usedCount >= row.maxUses) {
    return { kind: "invalid", reason: "exhausted" };
  }
  if (row.minOrderCents > 0 && subtotal < row.minOrderCents) {
    return { kind: "invalid", reason: "below_minimum" };
  }

  let cents = 0;
  if (row.type === "percentage") {
    cents = Math.floor((subtotal * Number(row.value)) / 100);
  } else if (row.type === "fixed") {
    cents = Number(row.value);
  }
  cents = Math.max(0, Math.min(cents, subtotal));
  return { kind: "applied", discount: row, cents };
}
