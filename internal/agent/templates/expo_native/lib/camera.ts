// Wrapper over expo-image-picker. Handles both pick-from-library and
// take-photo-with-camera flows. Each verb requests its own permission
// (camera vs media library) on first call and returns a uniform shape
// so screens don't have to plumb ImagePicker's nested cancellation/
// success branches.
//
// Permission usage descriptions (NSCameraUsageDescription,
// NSPhotoLibraryUsageDescription) are written by Wemob's Codemagic
// step (P0 #6) — you don't add them to app.json yourself.
//
// Web uses an <input type="file"> via lib/camera.web.ts.
import * as ImagePicker from "expo-image-picker";

export type MediaType = "images" | "videos" | "all";

export type PickAsset = {
  uri: string;
  mimeType?: string;
  fileName?: string;
  fileSize?: number;
  width: number;
  height: number;
  type: "image" | "video";
};

export type PickResult =
  | { canceled: true }
  | { canceled: false; assets: PickAsset[] };

export type PickOptions = {
  mediaTypes?: MediaType;
  allowsEditing?: boolean;
  allowsMultipleSelection?: boolean;
  quality?: number; // 0..1
};

function toMediaTypes(t: MediaType | undefined): ImagePicker.MediaType[] {
  if (t === "videos") return ["videos"];
  if (t === "all") return ["images", "videos"];
  return ["images"];
}

function toAsset(a: ImagePicker.ImagePickerAsset): PickAsset {
  return {
    uri: a.uri,
    mimeType: a.mimeType,
    fileName: a.fileName ?? undefined,
    fileSize: a.fileSize ?? undefined,
    width: a.width,
    height: a.height,
    type: a.type === "video" ? "video" : "image",
  };
}

export function init(): void {
  // No initialization required.
}

export function available(): boolean {
  return true;
}

export async function pickFromLibrary(options: PickOptions = {}): Promise<PickResult> {
  const perm = await ImagePicker.requestMediaLibraryPermissionsAsync();
  if (!perm.granted) return { canceled: true };
  const result = await ImagePicker.launchImageLibraryAsync({
    mediaTypes: toMediaTypes(options.mediaTypes),
    allowsEditing: options.allowsEditing ?? false,
    allowsMultipleSelection: options.allowsMultipleSelection ?? false,
    quality: options.quality ?? 0.8,
  });
  if (result.canceled) return { canceled: true };
  return { canceled: false, assets: result.assets.map(toAsset) };
}

export async function takePhoto(options: PickOptions = {}): Promise<PickResult> {
  const perm = await ImagePicker.requestCameraPermissionsAsync();
  if (!perm.granted) return { canceled: true };
  const result = await ImagePicker.launchCameraAsync({
    mediaTypes: toMediaTypes(options.mediaTypes),
    allowsEditing: options.allowsEditing ?? false,
    quality: options.quality ?? 0.8,
  });
  if (result.canceled) return { canceled: true };
  return { canceled: false, assets: result.assets.map(toAsset) };
}
