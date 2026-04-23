import { NextRequest, NextResponse } from "next/server";
import { getProducts } from "@/lib/wemob-store-server";

// GET /api/products?category=<slug>&q=<search>&limit=&offset= — paginated
// product listing. Matches the StorefrontProduct[] shape the client SDK
// (useProducts) consumes and keeps the browse logic close to the SSR
// path (both funnel through wemob-store-server).
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  const sp = new URL(req.url).searchParams;
  const category = sp.get("category") ?? undefined;
  const q = sp.get("q") ?? undefined;
  const limit = clampInt(sp.get("limit"), 1, 100);
  const offset = clampInt(sp.get("offset"), 0, 10000);

  try {
    const products = await getProducts({ category, q, limit, offset });
    return NextResponse.json(products);
  } catch (err) {
    return NextResponse.json(
      { error: (err as Error).message || "products_unavailable" },
      { status: 500 },
    );
  }
}

function clampInt(raw: string | null, min: number, max: number): number | undefined {
  if (!raw) return undefined;
  const n = Number.parseInt(raw, 10);
  if (!Number.isFinite(n)) return undefined;
  return Math.min(max, Math.max(min, n));
}
