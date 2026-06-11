// Web stub for lib/calendar. There's no universal browser API for
// reading/writing the user's calendar; the closest equivalent is
// generating an .ics file. We just no-op so screens fall back to
// "open calendar app" affordances on web.

export type CalendarSummary = {
  id: string;
  title: string;
  source: string;
  allowsModifications: boolean;
  isPrimary: boolean;
};

export type EventInput = {
  title: string;
  startDate: Date;
  endDate: Date;
  notes?: string;
  location?: string;
  allDay?: boolean;
  calendarId?: string;
  timeZone?: string;
};

export type PermissionResult =
  | { granted: true }
  | { granted: false; canAskAgain: boolean };

export function init(): void {
  // no-op
}

export async function available(): Promise<boolean> {
  return false;
}

export async function requestPermissions(): Promise<PermissionResult> {
  return { granted: false, canAskAgain: false };
}

export async function getCalendars(): Promise<CalendarSummary[]> {
  return [];
}

export async function createEvent(_input: EventInput): Promise<string | null> {
  return null;
}
