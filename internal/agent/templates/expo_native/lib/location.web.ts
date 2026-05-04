// Web stub for lib/location. Browsers expose navigator.geolocation,
// which has the same conceptual API (request permission → get position
// → watch position) but a different shape. We bridge it here so screens
// can use a single `getCurrentPosition()` import on every platform.
//
// Permission is requested implicitly the first time you call any
// geolocation method on the web — there's no separate request step.

export type Coordinates = {
  latitude: number;
  longitude: number;
  accuracy: number | null;
  altitude: number | null;
  heading: number | null;
  speed: number | null;
};

export type PermissionResult =
  | { granted: true }
  | { granted: false; canAskAgain: boolean };

export type WatchOptions = {
  accuracy?: number;
  distanceIntervalMeters?: number;
  timeIntervalMs?: number;
};

export function init(): void {
  // no-op
}

export async function available(): Promise<boolean> {
  return typeof navigator !== "undefined" && "geolocation" in navigator;
}

export async function requestPermissions(): Promise<PermissionResult> {
  // The browser asks the user the first time we call getCurrentPosition.
  // We return granted optimistically so callers proceed; failures bubble
  // up through getCurrentPosition's null return.
  if (typeof navigator === "undefined" || !("geolocation" in navigator)) {
    return { granted: false, canAskAgain: false };
  }
  return { granted: true };
}

function fromBrowserPosition(pos: GeolocationPosition): Coordinates {
  return {
    latitude: pos.coords.latitude,
    longitude: pos.coords.longitude,
    accuracy: pos.coords.accuracy ?? null,
    altitude: pos.coords.altitude ?? null,
    heading: pos.coords.heading ?? null,
    speed: pos.coords.speed ?? null,
  };
}

export async function getCurrentPosition(): Promise<Coordinates | null> {
  if (typeof navigator === "undefined" || !("geolocation" in navigator)) return null;
  return new Promise((resolve) => {
    navigator.geolocation.getCurrentPosition(
      (pos) => resolve(fromBrowserPosition(pos)),
      () => resolve(null),
      { enableHighAccuracy: false, maximumAge: 0, timeout: 10000 },
    );
  });
}

export async function watchPosition(
  callback: (coords: Coordinates) => void,
  _options: WatchOptions = {},
): Promise<() => void> {
  if (typeof navigator === "undefined" || !("geolocation" in navigator)) {
    return () => {};
  }
  const id = navigator.geolocation.watchPosition(
    (pos) => callback(fromBrowserPosition(pos)),
    () => {},
    { enableHighAccuracy: false },
  );
  return () => navigator.geolocation.clearWatch(id);
}
