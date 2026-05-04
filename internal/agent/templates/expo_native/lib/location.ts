// Wrapper over expo-location. Foreground-only by default — background
// location requires a separate background task entitlement and an
// explicit `Always` permission grant which Apple/Google scrutinise
// heavily, so we don't enable it by default.
//
// To enable background location: add `expo-task-manager` to package.json,
// register a TaskManager.defineTask, and call
// Location.startLocationUpdatesAsync(taskName, options) — see the Expo
// docs at https://docs.expo.dev/versions/latest/sdk/location/.
//
// Web uses navigator.geolocation via lib/location.web.ts.
import * as Location from "expo-location";

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
  accuracy?: Location.LocationAccuracy;
  distanceIntervalMeters?: number;
  timeIntervalMs?: number;
};

export function init(): void {
  // No initialization required.
}

export async function available(): Promise<boolean> {
  try {
    return await Location.hasServicesEnabledAsync();
  } catch {
    return false;
  }
}

export async function requestPermissions(): Promise<PermissionResult> {
  try {
    const res = await Location.requestForegroundPermissionsAsync();
    if (res.granted) return { granted: true };
    return { granted: false, canAskAgain: res.canAskAgain };
  } catch {
    return { granted: false, canAskAgain: false };
  }
}

export async function getCurrentPosition(): Promise<Coordinates | null> {
  try {
    const pos = await Location.getCurrentPositionAsync({
      accuracy: Location.LocationAccuracy.Balanced,
    });
    return {
      latitude: pos.coords.latitude,
      longitude: pos.coords.longitude,
      accuracy: pos.coords.accuracy ?? null,
      altitude: pos.coords.altitude ?? null,
      heading: pos.coords.heading ?? null,
      speed: pos.coords.speed ?? null,
    };
  } catch {
    return null;
  }
}

/**
 * Subscribe to position updates. Returns a function that cancels the
 * subscription — call it when your screen unmounts to avoid burning
 * battery on a background timer.
 */
export async function watchPosition(
  callback: (coords: Coordinates) => void,
  options: WatchOptions = {},
): Promise<() => void> {
  try {
    const sub = await Location.watchPositionAsync(
      {
        accuracy: options.accuracy ?? Location.LocationAccuracy.Balanced,
        distanceInterval: options.distanceIntervalMeters ?? 10,
        timeInterval: options.timeIntervalMs ?? 5000,
      },
      (pos) => {
        callback({
          latitude: pos.coords.latitude,
          longitude: pos.coords.longitude,
          accuracy: pos.coords.accuracy ?? null,
          altitude: pos.coords.altitude ?? null,
          heading: pos.coords.heading ?? null,
          speed: pos.coords.speed ?? null,
        });
      },
    );
    return () => sub.remove();
  } catch {
    return () => {};
  }
}
