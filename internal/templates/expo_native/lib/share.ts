// Wrapper over React Native's built-in Share API for plain text/URL
// sharing, plus expo-sharing for file sharing.
//
// We prefer the built-in Share API for simple "share a message + URL"
// flows because it's available everywhere, returns a useful action
// result, and doesn't require a file on disk.
//
// For sharing actual files (a downloaded PDF, an image saved by the
// app), use shareFile() — that one routes through expo-sharing which
// supports `application/octet-stream` & friends.
//
// Web has a partial Share API (navigator.share); we use it where
// available, otherwise fall back to copying the URL — see lib/share.web.ts.
import { Platform, Share } from "react-native";
import * as Sharing from "expo-sharing";

export type ShareOptions = {
  title?: string;
  message?: string;
  url?: string;
};

export type ShareResult =
  | { success: true; action: "shared" | "dismissed" }
  | { success: false; error: string };

export function init(): void {
  // No initialization required.
}

export function available(): boolean {
  return true;
}

export async function share(options: ShareOptions): Promise<ShareResult> {
  try {
    // RN's Share requires a non-empty `message` (Android's intent system
    // can't share without text); we synthesise one from the URL if the
    // caller only provided a URL.
    const message = options.message ?? options.url ?? options.title ?? "";
    // iOS uses url + message; Android only uses message (URL is appended).
    const payload =
      Platform.OS === "ios"
        ? {
            title: options.title,
            message,
            url: options.url,
          }
        : {
            title: options.title,
            message: [message, options.url].filter(Boolean).join("\n"),
          };
    const result = await Share.share(payload);
    if (result.action === Share.sharedAction) return { success: true, action: "shared" };
    return { success: true, action: "dismissed" };
  } catch (e) {
    return { success: false, error: e instanceof Error ? e.message : "unknown_error" };
  }
}

export async function shareFile(
  uri: string,
  opts: { mimeType?: string; dialogTitle?: string } = {},
): Promise<ShareResult> {
  try {
    if (!(await Sharing.isAvailableAsync())) {
      return { success: false, error: "sharing_unavailable" };
    }
    await Sharing.shareAsync(uri, {
      mimeType: opts.mimeType,
      dialogTitle: opts.dialogTitle,
    });
    return { success: true, action: "shared" };
  } catch (e) {
    return { success: false, error: e instanceof Error ? e.message : "unknown_error" };
  }
}
