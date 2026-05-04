// Web stub for lib/contacts. Browsers don't expose the user's address
// book; the Contact Picker API only works in Chrome on Android and is
// not portable. We just no-op so screens fall back to manual contact
// entry on web.

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
  pageSize?: number;
  pageOffset?: number;
};

export function init(): void {
  // no-op
}

export async function available(): Promise<boolean> {
  return false;
}

export async function requestPermissions(): Promise<PermissionResult> {
  return { granted: false, canAskAgain: false };
}

export async function getContacts(
  _options: GetContactsOptions = {},
): Promise<ContactSummary[]> {
  return [];
}
