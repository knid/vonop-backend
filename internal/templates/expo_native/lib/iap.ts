import { Platform } from "react-native";
import Constants from "expo-constants";
import type {
  default as PurchasesType,
  LOG_LEVEL as LOG_LEVEL_TYPE,
  CustomerInfo,
  PurchasesPackage,
} from "react-native-purchases";

export type { CustomerInfo, PurchasesPackage };

const RC_KEY_IOS = process.env.EXPO_PUBLIC_REVENUECAT_KEY_IOS ?? "";
const RC_KEY_ANDROID = process.env.EXPO_PUBLIC_REVENUECAT_KEY_ANDROID ?? "";

// Expo Go can't load `react-native-purchases` — it bridges to native StoreKit /
// Play Billing via TurboModule, which doesn't exist in Expo Go's runtime.
// We detect Expo Go and run a sandbox stub: getOfferings() returns a fake
// monthly package, purchasePackage() simulates a successful purchase after a
// short delay, getActiveEntitlements() returns ["pro"] post-purchase. This
// matches what Rork does — gives the user a feedback loop on the paywall UI
// in Expo Go without exercising real StoreKit. Real purchases only happen on
// dev-client / TestFlight / store builds, where the import branch below runs.
const isExpoGo = Constants.appOwnership === "expo";

let Purchases: typeof PurchasesType | null = null;
let LOG_LEVEL: typeof LOG_LEVEL_TYPE | null = null;
if (!isExpoGo) {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const mod = require("react-native-purchases");
  Purchases = mod.default;
  LOG_LEVEL = mod.LOG_LEVEL;
}

// Tracks whether the Expo Go sandbox stub has "purchased" the fake offering
// in the current session. Resets on app reload — same lifetime as a real
// CustomerInfo cache across a single dev session.
let expoGoFakeEntitled = false;

function isIAPAvailable(): boolean {
  if (isExpoGo) return true; // sandbox stub is always "available"
  return Platform.OS === "ios" ? !!RC_KEY_IOS : !!RC_KEY_ANDROID;
}

export function initIAP() {
  if (isExpoGo) {
    if (__DEV__) console.warn("Expo Go: IAP running in RevenueCat sandbox stub (purchases are simulated)");
    return;
  }
  const apiKey = Platform.OS === "ios" ? RC_KEY_IOS : RC_KEY_ANDROID;
  if (!apiKey) {
    if (__DEV__) {
      console.warn(
        `EXPO_PUBLIC_REVENUECAT_KEY_${Platform.OS.toUpperCase()} is not set; IAP disabled`
      );
    }
    return;
  }
  Purchases!.setLogLevel(__DEV__ ? LOG_LEVEL!.DEBUG : LOG_LEVEL!.WARN);
  Purchases!.configure({ apiKey });
}

export async function loginIAP(externalUserId: string) {
  if (isExpoGo) return;
  if (!isIAPAvailable()) return;
  await Purchases!.logIn(externalUserId);
}

export async function logoutIAP() {
  if (isExpoGo) {
    expoGoFakeEntitled = false;
    return;
  }
  if (!isIAPAvailable()) return;
  await Purchases!.logOut();
}

export async function getOfferings() {
  if (isExpoGo) {
    // Minimal shape that matches RC's PurchasesOfferings just enough to drive
    // a paywall UI. Real RevenueCat returns a much richer object — extend
    // here only as the template's screens demand.
    return {
      current: {
        identifier: "default",
        serverDescription: "Sandbox offering (Expo Go)",
        availablePackages: [
          {
            identifier: "$rc_monthly",
            packageType: "MONTHLY",
            product: {
              identifier: "wemob_sandbox_monthly",
              title: "Premium",
              description: "Sandbox subscription (Expo Go)",
              priceString: "$9.99",
              price: 9.99,
              currencyCode: "USD",
            },
          },
        ],
      },
      all: {},
    } as unknown as Awaited<ReturnType<NonNullable<typeof Purchases>["getOfferings"]>>;
  }
  if (!isIAPAvailable()) return null;
  return Purchases!.getOfferings();
}

export async function purchasePackage(pkg: PurchasesPackage) {
  if (isExpoGo) {
    // Simulate StoreKit's brief native dialog with a 500ms delay, then flip
    // the fake entitlement on. Mirrors the shape Purchases.purchasePackage
    // resolves to — { customerInfo, productIdentifier }.
    await new Promise((r) => setTimeout(r, 500));
    expoGoFakeEntitled = true;
    return {
      customerInfo: {
        entitlements: { active: { pro: { identifier: "pro" } } },
      },
      productIdentifier: "wemob_sandbox_monthly",
    } as unknown as Awaited<ReturnType<NonNullable<typeof Purchases>["purchasePackage"]>>;
  }
  return Purchases!.purchasePackage(pkg);
}

export async function restorePurchases() {
  if (isExpoGo) {
    // No purchase history to restore in the sandbox — return the current state.
    return {
      entitlements: {
        active: expoGoFakeEntitled ? { pro: { identifier: "pro" } } : {},
      },
    } as unknown as CustomerInfo;
  }
  if (!isIAPAvailable()) return null;
  return Purchases!.restorePurchases();
}

/**
 * Returns the list of currently-active entitlement IDs for the signed-in user
 * (or the anonymous device if not yet signed in via loginIAP).
 *
 * Use to gate features behind a subscription:
 *
 *   const entitlements = await getActiveEntitlements();
 *   const isPro = entitlements.includes("pro");
 *
 * Empty array on:
 *   - IAP not available (key not set / unsupported platform)
 *   - No active entitlements (free user)
 *   - Network error fetching CustomerInfo (RevenueCat returns the cached info
 *     when offline; this catch is a defense against rare hard failures)
 *
 * In Expo Go, returns ["pro"] once the user has tapped the paywall's Subscribe
 * button (sandbox stub). Reset by logoutIAP() or app reload.
 *
 * Entitlement IDs are user-defined in the RevenueCat dashboard. Common
 * choices: "pro", "premium", "unlimited".
 */
export async function getActiveEntitlements(): Promise<string[]> {
  if (isExpoGo) {
    return expoGoFakeEntitled ? ["pro"] : [];
  }
  if (!isIAPAvailable()) return [];
  try {
    const info: CustomerInfo = await Purchases!.getCustomerInfo();
    return Object.keys(info.entitlements.active);
  } catch {
    return [];
  }
}
