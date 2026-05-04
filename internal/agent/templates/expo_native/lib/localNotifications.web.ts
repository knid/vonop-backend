// Web stub for lib/localNotifications. Browsers expose the Notification
// API (window.Notification), but it doesn't support scheduling — only
// "show now". For studio-preview parity we just no-op everything; if
// you need browser notifications, wire them screen-side in your own
// hook with `new Notification(title, opts)`.

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyTrigger = any;

export type ScheduledNotification = {
  id: string;
  title: string;
  body?: string;
  data?: Record<string, unknown>;
  trigger: AnyTrigger;
};

export type ScheduleInput = {
  title: string;
  body?: string;
  data?: Record<string, unknown>;
  trigger: Date | number | AnyTrigger;
};

export function init(): void {
  // no-op
}

export async function available(): Promise<boolean> {
  return false;
}

export async function requestPermissions(): Promise<boolean> {
  return false;
}

export async function scheduleNotification(_input: ScheduleInput): Promise<string | null> {
  return null;
}

export async function cancel(_id: string): Promise<void> {
  // no-op
}

export async function cancelAll(): Promise<void> {
  // no-op
}

export async function getScheduled(): Promise<ScheduledNotification[]> {
  return [];
}
