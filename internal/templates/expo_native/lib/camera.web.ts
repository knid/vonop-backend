// Web stub for lib/camera. We use a hidden <input type="file"> for
// pick-from-library — the only universal way to ask for a file in a
// browser. takePhoto() falls back to the same input on most browsers
// (which surfaces a "Take photo" option on mobile Chrome/Safari) since
// `getUserMedia` requires building a custom UI.

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
  quality?: number;
};

export function init(): void {
  // no-op
}

export function available(): boolean {
  return typeof document !== "undefined";
}

function acceptFor(t: MediaType | undefined): string {
  if (t === "videos") return "video/*";
  if (t === "all") return "image/*,video/*";
  return "image/*";
}

function fileToAsset(f: File, dataUrl: string): PickAsset {
  return {
    uri: dataUrl,
    mimeType: f.type,
    fileName: f.name,
    fileSize: f.size,
    width: 0,
    height: 0,
    type: f.type.startsWith("video/") ? "video" : "image",
  };
}

function readFile(f: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ""));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(f);
  });
}

async function openFilePicker(
  options: PickOptions,
  capture: boolean,
): Promise<PickResult> {
  if (typeof document === "undefined") return { canceled: true };
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = acceptFor(options.mediaTypes);
    input.multiple = options.allowsMultipleSelection ?? false;
    if (capture) input.setAttribute("capture", "environment");
    input.style.display = "none";
    input.onchange = async () => {
      const files = Array.from(input.files ?? []);
      if (files.length === 0) {
        resolve({ canceled: true });
        return;
      }
      const assets: PickAsset[] = [];
      for (const f of files) {
        const url = await readFile(f);
        assets.push(fileToAsset(f, url));
      }
      resolve({ canceled: false, assets });
    };
    input.oncancel = () => resolve({ canceled: true });
    document.body.appendChild(input);
    input.click();
    setTimeout(() => input.remove(), 0);
  });
}

export async function pickFromLibrary(options: PickOptions = {}): Promise<PickResult> {
  return openFilePicker(options, false);
}

export async function takePhoto(options: PickOptions = {}): Promise<PickResult> {
  return openFilePicker(options, true);
}
