import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { and, eq } from "drizzle-orm";
import { db, schema } from "@/lib/db";

const CART_SESSION_COOKIE = "wemob_cart_session";

// DELETE /api/wishlist/[productId] — remove a product from the cookie
// session's wishlist. Treats a missing session as a no-op so the client-side
// optimistic remove stays idempotent.
export async function DELETE(
  _req: NextRequest,
  { params }: { params: Promise<{ productId: string }> },
) {
  const { productId } = await params;

  const jar = await cookies();
  const sid = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sid) return NextResponse.json({ ok: true });

  await db
    .delete(schema.wishlists)
    .where(
      and(
        eq(schema.wishlists.sessionId, sid),
        eq(schema.wishlists.productId, productId),
      ),
    );

  return NextResponse.json({ ok: true });
}
