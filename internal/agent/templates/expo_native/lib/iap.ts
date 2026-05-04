import { Platform } from "react-native";
import Purchases, {
  LOG_LEVEL,
  type CustomerInfo,
  type PurchasesPackage,
} from "react-native-purchases";

const RC_KEY_IOS = process.env.EXPO_PUBLIC_REVENUECAT_KEY_IOS ?? "";
const RC_KEY_ANDROID = process.env.EXPO_PUBLIC_REVENUECAT_KEY_ANDROID ?? "";

function isIAPAvailable(): boolean {
  return Platform.OS === "ios" ? !!RC_KEY_IOS : !!RC_KEY_ANDROID;
}

export function initIAP() {
  const apiKey = Platform.OS === "ios" ? RC_KEY_IOS : RC_KEY_ANDROID;
  if (!apiKey) {
    if (__DEV__) {
      console.warn(
        `EXPO_PUBLIC_REVENUECAT_KEY_${Platform.OS.toUpperCase()} is not set; IAP disabled`
      );
    }
    return;
  }
  Purchases.setLogLevel(__DEV__ ? LOG_LEVEL.DEBUG : LOG_LEVEL.WARN);
  Purchases.configure({ apiKey });
}

export async function loginIAP(externalUserId: string) {
  if (!isIAPAvailable()) return;
  await Purchases.logIn(externalUserId);
}

export async function logoutIAP() {
  if (!isIAPAvailable()) return;
  await Purchases.logOut();
}

export async function getOfferings() {
  if (!isIAPAvailable()) return null;
  return Purchases.getOfferings();
}

export async function purchasePackage(pkg: PurchasesPackage) {
  return Purchases.purchasePackage(pkg);
}

export async function restorePurchases() {
  if (!isIAPAvailable()) return null;
  return Purchases.restorePurchases();
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
 * Entitlement IDs are user-defined in the RevenueCat dashboard. Common
 * choices: "pro", "premium", "unlimited".
 */
export async function getActiveEntitlements(): Promise<string[]> {
  if (!isIAPAvailable()) return [];
  try {
    const info: CustomerInfo = await Purchases.getCustomerInfo();
    return Object.keys(info.entitlements.active);
  } catch {
    return [];
  }
}
