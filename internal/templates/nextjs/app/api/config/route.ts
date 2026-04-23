import { NextResponse } from "next/server";
import { getStoreConfig } from "@/lib/wemob-store-server";

// GET /api/config — returns the StoreConfig the client SDK (useStoreConfig)
// uses for name/currency/locale/payment_providers. Backed by the tenant DB
// via wemob-store-server; there is no legacy /v2/storefront/* call anymore.
export const dynamic = "force-dynamic";

export async function GET() {
  try {
    const config = await getStoreConfig();
    return NextResponse.json(config);
  } catch (err) {
    // Surface as 500 with the real message so the client SDK's Error() carries
    // something diagnosable in dev. Don't leak it to production UIs directly.
    return NextResponse.json(
      { error: (err as Error).message || "config_unavailable" },
      { status: 500 },
    );
  }
}
