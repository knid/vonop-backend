import { NextRequest, NextResponse } from "next/server";
import { and, desc, eq, isNull } from "drizzle-orm";
import { db, schema } from "@/lib/db";

type ReviewBody = {
  slug?: string;
  customer_name?: string;
  customer_email?: string;
  rating?: number;
  title?: string;
  body?: string;
};

// POST /api/reviews — submit a review for a product identified by slug.
// Accepts the snake_case shape the existing client sends.
export async function POST(req: NextRequest) {
  const body = (await req.json()) as ReviewBody;

  const slug = body.slug;
  const customerName = body.customer_name;
  const customerEmail = body.customer_email;
  const rating = body.rating;

  if (!slug || !customerName || !customerEmail || typeof rating !== "number") {
    return NextResponse.json({ error: "missing_fields" }, { status: 400 });
  }
  if (rating < 1 || rating > 5) {
    return NextResponse.json({ error: "invalid_rating" }, { status: 400 });
  }

  const [product] = await db
    .select()
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

  const [inserted] = await db
    .insert(schema.productReviews)
    .values({
      productId: product.id,
      customerName,
      customerEmail,
      rating,
      title: body.title,
      body: body.body,
      // status defaults to 'approved' per SQL schema.
    })
    .returning({ id: schema.productReviews.id, status: schema.productReviews.status });

  return NextResponse.json({
    id: inserted.id,
    status: inserted.status,
    message: "Review submitted",
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
    .select()
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
