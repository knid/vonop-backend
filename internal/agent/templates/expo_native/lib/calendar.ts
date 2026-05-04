// Wrapper over expo-calendar. Permission usage description
// (NSCalendarsUsageDescription) is written by Wemob's Codemagic step
// (P0 #6) — you don't add it to app.json.
//
// We expose the most-common reads (getCalendars, getEvents) plus a
// uniform createEvent that picks the user's default calendar when no
// calendarId is provided. For richer scheduling UIs (recurrence rules,
// attendees), import from expo-calendar directly — this wrapper is
// intentionally narrow.
//
// Web is a no-op via lib/calendar.web.ts.
import * as Calendar from "expo-calendar";

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
  // No initialization required.
}

export async function available(): Promise<boolean> {
  try {
    return await Calendar.isAvailableAsync();
  } catch {
    return false;
  }
}

export async function requestPermissions(): Promise<PermissionResult> {
  try {
    const res = await Calendar.requestCalendarPermissionsAsync();
    if (res.granted) return { granted: true };
    return { granted: false, canAskAgain: res.canAskAgain };
  } catch {
    return { granted: false, canAskAgain: false };
  }
}

export async function getCalendars(): Promise<CalendarSummary[]> {
  try {
    const cals = await Calendar.getCalendarsAsync(Calendar.EntityTypes.EVENT);
    return cals.map((c) => ({
      id: c.id,
      title: c.title,
      source: c.source?.name ?? "",
      allowsModifications: c.allowsModifications,
      // expo-calendar exposes `isPrimary` on iOS only; default to false.
      isPrimary: (c as { isPrimary?: boolean }).isPrimary ?? false,
    }));
  } catch {
    return [];
  }
}

/**
 * Create an event. If `calendarId` is omitted we look up the device's
 * default calendar (typically iCloud on iOS, Google on Android). Returns
 * the new event's id, or null on failure.
 */
export async function createEvent(input: EventInput): Promise<string | null> {
  try {
    let calendarId = input.calendarId;
    if (!calendarId) {
      const def = await Calendar.getDefaultCalendarAsync();
      calendarId = def.id;
    }
    return await Calendar.createEventAsync(calendarId, {
      title: input.title,
      startDate: input.startDate,
      endDate: input.endDate,
      notes: input.notes,
      location: input.location,
      allDay: input.allDay,
      timeZone: input.timeZone,
    });
  } catch {
    return null;
  }
}
