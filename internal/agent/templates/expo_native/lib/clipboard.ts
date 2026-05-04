// Wrapper over expo-clipboard. Trivial — the only reason this lives
// here (rather than calling expo-clipboard directly from screens) is to
// keep the lib/ surface uniform: every native helper has the same
// init/available/verbs shape, and TS imports point at @/lib/* not
// expo-* packages.
//
// expo-clipboard works on iOS, Android, and web — there's no .web.ts
// stub; the package handles platform branching internally.
import * as Clipboard from "expo-clipboard";

export function init(): void {
  // No initialization required.
}

export function available(): boolean {
  return true;
}

export async function copy(value: string): Promise<boolean> {
  try {
    return await Clipboard.setStringAsync(value);
  } catch {
    return false;
  }
}

export async function paste(): Promise<string> {
  try {
    return await Clipboard.getStringAsync();
  } catch {
    return "";
  }
}

export async function hasString(): Promise<boolean> {
  try {
    return await Clipboard.hasStringAsync();
  } catch {
    return false;
  }
}
