// Wrapper over expo-contacts. Permission usage description
// (NSContactsUsageDescription) is written by Wemob's Codemagic step
// (P0 #6) — you don't add it to app.json.
//
// This is a read-oriented wrapper; for adding/updating contacts, import
// expo-contacts directly. We narrow the result shape to the most-common
// fields (name, phone, email) so screens don't have to thread through
// expo-contacts' nested response.
//
// Web is a no-op via lib/contacts.web.ts.
import * as Contacts from "expo-contacts";

export type ContactSummary = {
  id: string;
  name: string;
  firstName?: string;
  lastName?: string;
  phoneNumbers: string[];
  emails: string[];
};

export type PermissionResult =
  | { granted: true }
  | { granted: false; canAskAgain: boolean };

export type GetContactsOptions = {
  // Cap returned results; expo-contacts can return tens of thousands
  // for "import your address book" flows. Pages are 1-indexed.
  pageSize?: number;
  pageOffset?: number;
};

export function init(): void {
  // No initialization required.
}

export async function available(): Promise<boolean> {
  try {
    return await Contacts.isAvailableAsync();
  } catch {
    return false;
  }
}

export async function requestPermissions(): Promise<PermissionResult> {
  try {
    const res = await Contacts.requestPermissionsAsync();
    if (res.granted) return { granted: true };
    return { granted: false, canAskAgain: res.canAskAgain };
  } catch {
    return { granted: false, canAskAgain: false };
  }
}

export async function getContacts(
  options: GetContactsOptions = {},
): Promise<ContactSummary[]> {
  try {
    const res = await Contacts.getContactsAsync({
      fields: [
        Contacts.Fields.FirstName,
        Contacts.Fields.LastName,
        Contacts.Fields.Name,
        Contacts.Fields.PhoneNumbers,
        Contacts.Fields.Emails,
      ],
      pageSize: options.pageSize,
      pageOffset: options.pageOffset,
    });
    return res.data.map((c) => ({
      id: c.id ?? "",
      name: c.name ?? "",
      firstName: c.firstName,
      lastName: c.lastName,
      phoneNumbers: (c.phoneNumbers ?? [])
        .map((p) => p.number)
        .filter((n): n is string => typeof n === "string"),
      emails: (c.emails ?? [])
        .map((e) => e.email)
        .filter((e): e is string => typeof e === "string"),
    }));
  } catch {
    return [];
  }
}
