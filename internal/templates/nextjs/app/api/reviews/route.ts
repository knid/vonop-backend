import { NextRequest, NextResponse } from "next/server";
import { and, desc, eq, gte, isNull, sql } from "drizzle-orm";
import { db, schema } from "@/lib/db";

export const dynamic = "force-dynamic";

type ReviewBody = {
  slug?: string;
  customer_name?: string;
  customer_email?: string;
  rating?: number;
  title?: string;
  body?: string;
};

const MAX_BODY_LEN = 5000;
const MAX_TITLE_LEN = 200;
const MAX_NAME_LEN = 100;
const REVIEWS_PER_EMAIL_PER_DAY = 5;

// Fold plus-addressing and subdomain noise so `buyer@gmail.com`, `Buyer@gmail.com`,
// and `buyer+r@gmail.com` all count as the same identity for verified-purchase
// and rate-limit purposes. Leaves unrecognized formats alone.
function normalizeEmail(raw: string): string {
  const email = raw.trim().toLowerCase();
  const at = email.lastIndexOf("@");
  if (at < 1) return email;
  const local = email.slice(0, at);
  const domain = email.slice(at + 1);
  const plus = local.indexOf("+");
  const baseLocal = plus >= 0 ? local.slice(0, plus) : local;
  return `${baseLocal}@${domain}`;
}

// POST /api/reviews — submit a review for a product identified by slug.
// Accepts the snake_case shape the existing client sends.
//
// Hardened version:
//   - Reviews default to status="pending"; merchant moderates before publish.
//   - is_verified is derived server-side from the customer's paid order
//     history for this product; the client cannot forge it.
//   - Per-email rate limit + one-review-per-email-per-product uniqueness
//     keep the endpoint from being a spam / competitor-sabotage vector.
export async function POST(req: NextRequest) {
  const body = (await req.json()) as ReviewBody;

  const slug = body.slug?.trim();
  const customerName = body.customer_name?.trim();
  const rawEmail = body.customer_email?.trim().toLowerCase();
  const rating = body.rating;

  if (!slug || !customerName || !rawEmail || typeof rating !== "number") {
    return NextResponse.json({ error: "missing_fields" }, { status: 400 });
  }
  if (!Number.isInteger(rating) || rating < 1 || rating > 5) {
    return NextResponse.json({ error: "invalid_rating" }, { status: 400 });
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(rawEmail)) {
    return NextResponse.json({ error: "invalid_email" }, { status: 400 });
  }
  if (
    customerName.length > MAX_NAME_LEN ||
    (body.title && body.title.length > MAX_TITLE_LEN) ||
    (body.body && body.body.length > MAX_BODY_LEN)
  ) {
    return NextResponse.json({ error: "text_too_long" }, { status: 400 });
  }

  // Normalized form is what we store + compare against so plus-addressing
  // tricks (`buyer+r1@...`, `buyer+r2@...`) can't dodge the rate limit or
  // the already-reviewed gate.
  const customerEmail = normalizeEmail(rawEmail);

  // Rate limit first so repeat hammering on already-reviewed products can't
  // spin up unlimited DB work before the 409 short-circuit below.
  const oneDayAgo = new Date(Date.now() - 24 * 60 * 60 * 1000);
  const [{ count: recentCount }] = await db
    .select({ count: sql<number>`count(*)::int` })
    .from(schema.productReviews)
    .where(
      and(
        eq(schema.productReviews.customerEmail, customerEmail),
        gte(schema.productReviews.createdAt, oneDayAgo),
      ),
    );
  if (Number(recentCount) >= REVIEWS_PER_EMAIL_PER_DAY) {
    return NextResponse.json({ error: "rate_limited" }, { status: 429 });
  }

  const [product] = await db
    .select({ id: schema.products.id })
    .from(schema.products)
    .where(
      and(
        eq(schema.products.slug, slug),
        isNull(schema.products.deletedAt),
      ),
    );
  if (!product) {
    return NextResponse.json({ error: "product_not_found" }, { status: 404 });
  }

  // Uniqueness: one review per (email, product). A repeat submission is
  // silently treated as an update — we return 409 and let the client
  // surface an appropriate message rather than silently dropping the post.
  const [existing] = await db
    .select({ id: schema.productReviews.id })
    .from(schema.productReviews)
    .where(
      and(
        eq(schema.productReviews.productId, product.id),
        eq(schema.productReviews.customerEmail, customerEmail),
      ),
    );
  if (existing) {
    return NextResponse.json({ error: "already_reviewed" }, { status: 409 });
  }

  // Verified purchase: does this email have a paid order containing this
  // product? Only join order_items once through orders so we don't need
  // additional indexes beyond what the seeded schema already provides.
  const [verifiedRow] = await db
    .select({ id: schema.orderItems.id })
    .from(schema.orderItems)
    .innerJoin(schema.orders, eq(schema.orderItems.orderId, schema.orders.id))
    .where(
      and(
        eq(schema.orderItems.productId, product.id),
        eq(schema.orders.customerEmail, customerEmail),
        eq(schema.orders.paymentStatus, "paid"),
      ),
    )
    .limit(1);
  const isVerified = !!verifiedRow;

  const [inserted] = await db
    .insert(schema.productReviews)
    .values({
      productId: product.id,
      customerName,
      customerEmail,
      rating,
      title: body.title,
      body: body.body,
      isVerified,
      // Override the schema's "approved" default — merchant moderates first.
      status: "pending",
    })
    .returning({
      id: schema.productReviews.id,
      status: schema.productReviews.status,
    });

  return NextResponse.json({
    id: inserted.id,
    status: inserted.status,
    is_verified: isVerified,
    message: "Review submitted — awaiting moderation.",
  });
}

// GET /api/reviews?slug=<slug> — list approved reviews for a product.
// Returns the `ReviewsResult` shape the existing useReviews hook expects
// (snake_case `average_rating` / `review_count`) so client consumers can
// migrate off the legacy /v2/storefront/* path without a shape change.
export async function GET(req: NextRequest) {
  const slug = new URL(req.url).searchParams.get("slug");
  const empty = { reviews: [], average_rating: 0, review_count: 0 };
  if (!slug) return NextResponse.json(empty);

  const [product] = await db
    .select({ id: schema.products.id })
    .from(schema.products)
    .where(
      and(
        eq(schema.products.slug, slug),
        isNull(schema.products.deletedAt),
      ),
    );
  if (!product) return NextResponse.json(empty);

  const rows = await db
    .select()
    .from(schema.productReviews)
    .where(
      and(
        eq(schema.productReviews.productId, product.id),
        eq(schema.productReviews.status, "approved"),
      ),
    )
    .orderBy(desc(schema.productReviews.createdAt));

  const reviews = rows.map((r) => ({
    id: r.id,
    customer_name: r.customerName,
    rating: r.rating,
    title: r.title ?? "",
    body: r.body ?? "",
    is_verified: r.isVerified,
    created_at: r.createdAt.toISOString(),
  }));

  const count = reviews.length;
  const average =
    count === 0 ? 0 : reviews.reduce((sum, r) => sum + r.rating, 0) / count;

  return NextResponse.json({
    reviews,
    average_rating: average,
    review_count: count,
  });
}
