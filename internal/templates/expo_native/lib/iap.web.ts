// Web stub for lib/iap. react-native-purchases bridges to native StoreKit
// (iOS) / Play Billing (Android) via TurboModule and crashes on web with
// "TurboModuleRegistry.getEnforcing is undefined". Metro auto-picks this
// `.web.ts` file over `iap.ts` when bundling for web.
//
// Every export is a no-op that returns a sensible empty value, so screens
// that gate features on getActiveEntitlements() / getOfferings() still
// compile + render in the web preview (showing the free tier).
//
// On a real device the native iap.ts runs unchanged. Production purchases
// happen via App Store / Play Store IAP, which has no web equivalent — a
// dedicated web-billing flow (Stripe etc.) would be a separate concern.

// Minimal local types so consumers can still `import type { CustomerInfo }`
// from "@/lib/iap" without depending on react-native-purchases on web.
export type CustomerInfo = {
  entitlements: { active: Record<string, unknown> }
}
export type PurchasesPackage = unknown

export function initIAP(): void {
  // no-op on web
}

export async function loginIAP(_externalUserId: string): Promise<void> {
  // no-op on web
}

export async function logoutIAP(): Promise<void> {
  // no-op on web
}

export async function getOfferings(): Promise<null> {
  return null
}

export async function purchasePackage(_pkg: PurchasesPackage): Promise<null> {
  return null
}

export async function restorePurchases(): Promise<null> {
  return null
}

export async function getActiveEntitlements(): Promise<string[]> {
  return []
}
