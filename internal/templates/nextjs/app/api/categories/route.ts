import { NextResponse } from "next/server";
import { getCategories } from "@/lib/wemob-store-server";

// GET /api/categories — ordered list of non-deleted categories. Client SDK
// hooks (useCategories) consume this; server components import from
// @/lib/wemob-store-server directly instead of round-tripping through fetch.
export const dynamic = "force-dynamic";

export async function GET() {
  try {
    const categories = await getCategories();
    return NextResponse.json(categories);
  } catch (err) {
    return NextResponse.json(
      { error: (err as Error).message || "categories_unavailable" },
      { status: 500 },
    );
  }
}
