import Constants from "expo-constants";
import type {
  LogLevel as LogLevelType,
  OneSignal as OneSignalType,
  NotificationWillDisplayEvent,
} from "react-native-onesignal";

const ONESIGNAL_APP_ID = process.env.EXPO_PUBLIC_ONESIGNAL_APP_ID ?? "";

// Expo Go has no `react-native-onesignal` native module — importing it
// eagerly would crash with "TurboModuleRegistry.getEnforcing(...): 'RNOneSignal'
// could not be found" the moment the bundle evaluates. The .web.ts shadow
// already covers Metro web. For Expo Go we detect at runtime and skip every
// OneSignal call; the conditional require() below keeps Metro from evaluating
// the native module during Expo Go bundle load.
const isExpoGo = Constants.appOwnership === "expo";

let OneSignal: typeof OneSignalType | null = null;
let LogLevel: typeof LogLevelType | null = null;
if (!isExpoGo) {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const mod = require("react-native-onesignal");
  OneSignal = mod.OneSignal;
  LogLevel = mod.LogLevel;
}

function pushDisabled(): boolean {
  return isExpoGo || !ONESIGNAL_APP_ID || !OneSignal;
}

export function initPush() {
  if (isExpoGo) {
    if (__DEV__) console.warn("Expo Go: push notifications disabled (OneSignal requires a dev/prod build)");
    return;
  }
  if (!ONESIGNAL_APP_ID) {
    if (__DEV__) console.warn("EXPO_PUBLIC_ONESIGNAL_APP_ID is not set; push disabled");
    return;
  }
  OneSignal!.Debug.setLogLevel(__DEV__ ? LogLevel!.Verbose : LogLevel!.None);
  OneSignal!.initialize(ONESIGNAL_APP_ID);
}

export async function requestPushPermission(): Promise<boolean> {
  if (pushDisabled()) return false;
  const granted = await OneSignal!.Notifications.requestPermission(true);
  return granted ?? false;
}

export function loginUserToPush(externalUserId: string) {
  if (pushDisabled()) return;
  OneSignal!.login(externalUserId);
}

export function logoutUserFromPush() {
  if (pushDisabled()) return;
  OneSignal!.logout();
}

/**
 * Returns the current push subscription state. Safe to call at any time —
 * if push is disabled (Expo Go, missing app ID, etc.), returns
 * { subscribed: false, optedIn: false, externalUserId: null }.
 *
 * Use to power per-user opt-out toggles (App Store compliance — users must
 * be able to disable push from inside the app).
 *
 * Async because the OneSignal v5 SDK switched to async getters; previous
 * sync getters are deprecated. Call from useEffect + setState.
 */
export async function getPushSubscriptionState(): Promise<{
  subscribed: boolean;
  optedIn: boolean;
  externalUserId: string | null;
}> {
  if (pushDisabled()) {
    return { subscribed: false, optedIn: false, externalUserId: null };
  }
  try {
    const [id, optedIn, externalUserId] = await Promise.all([
      OneSignal!.User.pushSubscription.getIdAsync(),
      OneSignal!.User.pushSubscription.getOptedInAsync(),
      OneSignal!.User.getExternalId(),
    ]);
    return {
      subscribed: id !== "" && id !== null && id !== undefined,
      optedIn: optedIn ?? true,
      externalUserId: externalUserId ?? null,
    };
  } catch {
    return { subscribed: false, optedIn: false, externalUserId: null };
  }
}

/**
 * Attach a handler for the "foregroundWillDisplay" event. Returns an
 * unsubscribe function. Inside the handler:
 *   - call event.preventDefault() to suppress the system banner
 *   - call event.getNotification().display() to show it after running
 *     your in-app side-effects (banner, badge update, store mutation, etc.)
 *
 * Safe to call when push is disabled — returns a no-op unsubscribe.
 */
export function addForegroundHandler(
  handler: (event: NotificationWillDisplayEvent) => void,
): () => void {
  if (pushDisabled()) return () => {};
  OneSignal!.Notifications.addEventListener("foregroundWillDisplay", handler);
  return () => {
    OneSignal!.Notifications.removeEventListener("foregroundWillDisplay", handler);
  };
}
