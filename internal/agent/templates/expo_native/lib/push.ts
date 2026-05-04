import {
  LogLevel,
  OneSignal,
  type NotificationWillDisplayEvent,
} from "react-native-onesignal";

const ONESIGNAL_APP_ID = process.env.EXPO_PUBLIC_ONESIGNAL_APP_ID ?? "";

export function initPush() {
  if (!ONESIGNAL_APP_ID) {
    if (__DEV__) console.warn("EXPO_PUBLIC_ONESIGNAL_APP_ID is not set; push disabled");
    return;
  }
  OneSignal.Debug.setLogLevel(__DEV__ ? LogLevel.Verbose : LogLevel.None);
  OneSignal.initialize(ONESIGNAL_APP_ID);
}

export async function requestPushPermission(): Promise<boolean> {
  const granted = await OneSignal.Notifications.requestPermission(true);
  return granted ?? false;
}

export function loginUserToPush(externalUserId: string) {
  if (!ONESIGNAL_APP_ID) return;
  OneSignal.login(externalUserId);
}

export function logoutUserFromPush() {
  if (!ONESIGNAL_APP_ID) return;
  OneSignal.logout();
}

/**
 * Returns the current push subscription state. Safe to call at any time —
 * if push is disabled at the EXPO_PUBLIC_ONESIGNAL_APP_ID level, returns
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
  if (!ONESIGNAL_APP_ID) {
    return { subscribed: false, optedIn: false, externalUserId: null };
  }
  try {
    const [id, optedIn, externalUserId] = await Promise.all([
      OneSignal.User.pushSubscription.getIdAsync(),
      OneSignal.User.pushSubscription.getOptedInAsync(),
      OneSignal.User.getExternalId(),
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
  if (!ONESIGNAL_APP_ID) return () => {};
  OneSignal.Notifications.addEventListener("foregroundWillDisplay", handler);
  return () => {
    OneSignal.Notifications.removeEventListener("foregroundWillDisplay", handler);
  };
}
