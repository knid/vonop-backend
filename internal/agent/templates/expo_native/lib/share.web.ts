// Web stub for lib/share. Modern browsers (Chrome, Edge, Safari) ship a
// `navigator.share` API that opens the OS share sheet on supported
// platforms. We use it when available and fall back to copying the URL
// to clipboard otherwise.
//
// File sharing on web isn't sensible (the browser's download dialog is
// the moral equivalent), so shareFile() just returns an error.

export type ShareOptions = {
  title?: string;
  message?: string;
  url?: string;
};

export type ShareResult =
  | { success: true; action: "shared" | "dismissed" }
  | { success: false; error: string };

export function init(): void {
  // no-op
}

export function available(): boolean {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const nav: any =
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    typeof globalThis !== "undefined" ? (globalThis as any).navigator : undefined;
  return Boolean(nav && typeof nav.share === "function");
}

export async function share(options: ShareOptions): Promise<ShareResult> {
  try {
    // We're under react-native-web here, but @types/react-native shadows
    // the DOM lib types. Cast through unknown so we can poke at
    // navigator.share / navigator.clipboard without TS complaining.
    const nav: unknown =
      typeof globalThis !== "undefined"
        ? // eslint-disable-next-line @typescript-eslint/no-explicit-any
          (globalThis as any).navigator
        : undefined;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const navAny = nav as any;
    if (navAny && typeof navAny.share === "function") {
      await navAny.share({
        title: options.title,
        text: options.message,
        url: options.url,
      });
      return { success: true, action: "shared" };
    }
    // Fallback: copy URL to clipboard if the message has one.
    if (options.url && navAny && navAny.clipboard) {
      await navAny.clipboard.writeText(options.url);
      return { success: true, action: "shared" };
    }
    return { success: false, error: "share_unavailable" };
  } catch (e) {
    return { success: false, error: e instanceof Error ? e.message : "unknown_error" };
  }
}

export async function shareFile(
  _uri: string,
  _opts: { mimeType?: string; dialogTitle?: string } = {},
): Promise<ShareResult> {
  return { success: false, error: "file_share_not_supported_on_web" };
}
