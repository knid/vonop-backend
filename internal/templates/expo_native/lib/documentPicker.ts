// Wrapper over expo-document-picker. Returns a uniform `{canceled, assets}`
// shape so screens get the same code path on iOS, Android, and web.
//
// On iOS, the picker uses iCloud Documents — the iCloud container env
// is set to "Production" via the plugin block in app.json (P2 #21).
// On Android, the system file picker is used.
//
// Web stub uses the same pattern as lib/camera.web.ts (an
// <input type="file">). expo-document-picker also has a web build,
// but using a generic wrapper keeps the API surface consistent.
import * as DocumentPicker from "expo-document-picker";

export type PickedDocument = {
  uri: string;
  name: string;
  mimeType?: string;
  size?: number;
};

export type DocumentPickResult =
  | { canceled: true }
  | { canceled: false; assets: PickedDocument[] };

export type DocumentPickOptions = {
  type?: string | string[]; // MIME type(s); defaults to */*
  multiple?: boolean;
  copyToCacheDirectory?: boolean;
};

export function init(): void {
  // No initialization required.
}

export function available(): boolean {
  return true;
}

export async function pickDocument(
  options: DocumentPickOptions = {},
): Promise<DocumentPickResult> {
  try {
    const result = await DocumentPicker.getDocumentAsync({
      type: options.type ?? "*/*",
      multiple: options.multiple ?? false,
      copyToCacheDirectory: options.copyToCacheDirectory ?? true,
    });
    if (result.canceled) return { canceled: true };
    return {
      canceled: false,
      assets: result.assets.map((a) => ({
        uri: a.uri,
        name: a.name,
        mimeType: a.mimeType,
        size: a.size,
      })),
    };
  } catch {
    return { canceled: true };
  }
}
