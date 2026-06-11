// Lightweight wrapper over expo-haptics. Exposes the most-common haptic
// patterns as one-call helpers so screens don't have to import the enum
// constants. Each call is fire-and-forget — we swallow errors because
// haptics failing is never worth crashing the app over (e.g. on devices
// with the haptics engine disabled in Settings).
//
// Web is a no-op via lib/haptics.web.ts.
import * as Haptics from "expo-haptics";

export function available(): boolean {
  return true;
}

export function init(): void {
  // No initialization required — kept for API parity across helpers.
}

export async function light(): Promise<void> {
  try {
    await Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
  } catch {
    // ignore
  }
}

export async function medium(): Promise<void> {
  try {
    await Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
  } catch {
    // ignore
  }
}

export async function heavy(): Promise<void> {
  try {
    await Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy);
  } catch {
    // ignore
  }
}

export async function selection(): Promise<void> {
  try {
    await Haptics.selectionAsync();
  } catch {
    // ignore
  }
}

export async function success(): Promise<void> {
  try {
    await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
  } catch {
    // ignore
  }
}

export async function warning(): Promise<void> {
  try {
    await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning);
  } catch {
    // ignore
  }
}

export async function error(): Promise<void> {
  try {
    await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
  } catch {
    // ignore
  }
}
