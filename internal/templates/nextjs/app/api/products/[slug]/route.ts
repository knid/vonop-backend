import { NextRequest, NextResponse } from "next/server";
import { getProduct } from "@/lib/wemob-store-server";

// GET /api/products/[slug] — single product lookup by slug. Returns 404 when
// the product doesn't exist so the client SDK's useProduct hook can branch
// cleanly (`product === null`) instead of surfacing a generic API error.
export const dynamic = "force-dynamic";

export async function GET(
  _req: NextRequest,
  { params }: { params: Promise<{ slug: string }> },
) {
  const { slug } = await params;
  if (!slug) {
    return NextResponse.json({ error: "missing_slug" }, { status: 400 });
  }

  try {
    const product = await getProduct(slug);
    if (!product) {
      return NextResponse.json({ error: "not_found" }, { status: 404 });
    }
    return NextResponse.json(product);
  } catch (err) {
    return NextResponse.json(
      { error: (err as Error).message || "product_unavailable" },
      { status: 500 },
    );
  }
}
