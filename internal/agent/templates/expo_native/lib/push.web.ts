// Web stub for lib/push. react-native-onesignal is iOS/Android only —
// importing it on web crashes with "TurboModuleRegistry.getEnforcing is
// undefined" because OneSignal's native module bridge doesn't exist in
// react-native-web. Metro auto-picks this `.web.ts` file over `push.ts`
// when bundling for web (Expo CLI's `expo start --web`, the studio
// preview sandbox).
//
// Every export is a no-op that mirrors the native version's signature,
// so screens that import { initPush, getPushSubscriptionState, … }
// still compile + render in the web preview.
//
// On a real device the native push.ts runs unchanged.

export function initPush(): void {
  // no-op on web
}

export async function requestPushPermission(): Promise<boolean> {
  return false
}

export function loginUserToPush(_externalUserId: string): void {
  // no-op on web
}

export function logoutUserFromPush(): void {
  // no-op on web
}

export async function getPushSubscriptionState(): Promise<{
  subscribed: boolean
  optedIn: boolean
  externalUserId: string | null
}> {
  return { subscribed: false, optedIn: false, externalUserId: null }
}

export function addForegroundHandler(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  _handler: (event: any) => void,
): () => void {
  return () => {}
}
