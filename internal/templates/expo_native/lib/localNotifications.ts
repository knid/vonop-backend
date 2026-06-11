// Wrapper over expo-notifications for LOCAL (in-app) notifications.
//
// Push notifications are handled separately by lib/push.ts (OneSignal).
// This module is only for client-scheduled notifications — reminders,
// alarms, "your timer ran out" — that don't need to come from a server.
//
// Permissions: expo-notifications uses its own permission API rather
// than relying on OneSignal's. We expose a thin requestPermissions()
// helper for screens that want to show a custom rationale before the
// system prompt; without it the first scheduleNotification() call will
// silently noop on iOS (Apple requires explicit permission for any
// notification, foreground or background).
//
// The init() call MUST be invoked once at app start (via _layout.tsx)
// so the global notification handler is set. Without it, foreground
// notifications won't display while the app is open. We mirror the
// pattern of initPush().
//
// Web is a no-op via lib/localNotifications.web.ts.
import * as Notifications from "expo-notifications";

let initialised = false;

export function init(): void {
  if (initialised) return;
  initialised = true;
  // Default behaviour: show banner + play sound + update badge for
  // notifications received while the app is foregrounded. Override
  // per-notification by reading content.data inside the handler if
  // you want to suppress some kinds.
  Notifications.setNotificationHandler({
    handleNotification: async () => ({
      shouldShowBanner: true,
      shouldShowList: true,
      shouldPlaySound: true,
      shouldSetBadge: true,
    }),
  });
}

export type ScheduledNotification = {
  id: string;
  title: string;
  body?: string;
  data?: Record<string, unknown>;
  trigger: Notifications.NotificationTriggerInput;
};

export type ScheduleInput = {
  title: string;
  body?: string;
  data?: Record<string, unknown>;
  // Pass either a Date, a number of seconds from now, or any
  // expo-notifications trigger object (e.g. calendar / daily / weekly).
  trigger: Date | number | Notifications.NotificationTriggerInput;
};

export async function available(): Promise<boolean> {
  try {
    const settings = await Notifications.getPermissionsAsync();
    return settings.granted;
  } catch {
    return false;
  }
}

export async function requestPermissions(): Promise<boolean> {
  try {
    const res = await Notifications.requestPermissionsAsync();
    return res.granted || res.ios?.status === Notifications.IosAuthorizationStatus.PROVISIONAL;
  } catch {
    return false;
  }
}

function toTrigger(t: ScheduleInput["trigger"]): Notifications.NotificationTriggerInput {
  if (t instanceof Date) {
    return { type: Notifications.SchedulableTriggerInputTypes.DATE, date: t };
  }
  if (typeof t === "number") {
    return {
      type: Notifications.SchedulableTriggerInputTypes.TIME_INTERVAL,
      seconds: t,
      repeats: false,
    };
  }
  return t;
}

export async function scheduleNotification(input: ScheduleInput): Promise<string | null> {
  try {
    return await Notifications.scheduleNotificationAsync({
      content: {
        title: input.title,
        body: input.body,
        data: input.data,
      },
      trigger: toTrigger(input.trigger),
    });
  } catch {
    return null;
  }
}

export async function cancel(id: string): Promise<void> {
  try {
    await Notifications.cancelScheduledNotificationAsync(id);
  } catch {
    // ignore
  }
}

export async function cancelAll(): Promise<void> {
  try {
    await Notifications.cancelAllScheduledNotificationsAsync();
  } catch {
    // ignore
  }
}

export async function getScheduled(): Promise<ScheduledNotification[]> {
  try {
    const list = await Notifications.getAllScheduledNotificationsAsync();
    return list.map((n) => ({
      id: n.identifier,
      title: n.content.title ?? "",
      body: n.content.body ?? undefined,
      data: (n.content.data ?? {}) as Record<string, unknown>,
      trigger: n.trigger as Notifications.NotificationTriggerInput,
    }));
  } catch {
    return [];
  }
}
