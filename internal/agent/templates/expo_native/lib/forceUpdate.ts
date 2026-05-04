import { useEffect, useState } from "react";
import { Platform } from "react-native";
import * as Application from "expo-application";
import Constants from "expo-constants";
import * as SecureStore from "expo-secure-store";

/**
 * Force-update / min_version cold-start gate (P3.2).
 *
 * On every cold start, the app fetches the per-platform minimum native
 * build number from the Wemob backend and compares it against the
 * installed app's `Application.nativeBuildVersion`. When the installed
 * build is below the floor, the gate is rendered (see
 * components/ForceUpdateScreen + the wrapper in app/_layout.tsx).
 *
 * Network failure / fetch error → fail-OPEN: render the app normally so
 * a backend hiccup doesn't lock paying customers out of their app. The
 * floor only blocks users when the backend is actively reporting one.
 *
 * Cache: results are written to SecureStore with a 1-hour TTL so we
 * don't hammer the backend on every cold start. Stale entries are
 * served immediately and re-fetched in the background.
 */

const API_URL = process.env.EXPO_PUBLIC_API_URL ?? "";
const CACHE_TTL_MS = 60 * 60 * 1000; // 1 hour
const CACHE_KEY_PREFIX = "wemob.min_version";

export type MinVersionStatus = "loading" | "ok" | "update-required" | "error";

export type MinVersionState = {
  status: MinVersionStatus;
  /** The floor returned by the backend; undefined while loading or on error. */
  minBuild?: number;
  /** The currently-installed native build number. */
  currentBuild?: number;
  /** Store URL to open via Linking.openURL on the update button. */
  storeURL?: string;
  /** The (operator-customizable) message shown above the button. */
  message?: string;
};

type BackendResponse = {
  min_build_number: number;
  store_url: string;
  message: string;
};

type CachedResponse = BackendResponse & {
  fetchedAt: number;
};

function platformKey(): "ios" | "android" {
  // Treat "web" and anything unexpected as "ios" — Expo Web doesn't ship
  // through the App Store, so the gate effectively no-ops there because
  // the backend's iOS floor won't apply to the dev preview.
  return Platform.OS === "android" ? "android" : "ios";
}

function cacheKey(slug: string, platform: "ios" | "android"): string {
  return `${CACHE_KEY_PREFIX}.${slug}.${platform}`;
}

async function readCache(slug: string, platform: "ios" | "android"): Promise<CachedResponse | null> {
  try {
    const raw = await SecureStore.getItemAsync(cacheKey(slug, platform));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as CachedResponse;
    if (typeof parsed.min_build_number !== "number" || typeof parsed.fetchedAt !== "number") {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

async function writeCache(slug: string, platform: "ios" | "android", value: BackendResponse): Promise<void> {
  try {
    const payload: CachedResponse = { ...value, fetchedAt: Date.now() };
    await SecureStore.setItemAsync(cacheKey(slug, platform), JSON.stringify(payload));
  } catch {
    // Cache write failure is non-fatal — the gate just re-fetches next launch.
  }
}

async function fetchFromBackend(
  slug: string,
  platform: "ios" | "android"
): Promise<BackendResponse | null> {
  if (!API_URL || !slug) return null;
  try {
    const url = `${API_URL}/v1/mobile/${encodeURIComponent(slug)}/min-version?platform=${platform}`;
    const res = await fetch(url, { method: "GET" });
    if (!res.ok) return null;
    const body = (await res.json()) as Partial<BackendResponse>;
    if (typeof body.min_build_number !== "number") return null;
    return {
      min_build_number: body.min_build_number,
      store_url: body.store_url ?? "",
      message: body.message ?? "",
    };
  } catch {
    return null;
  }
}

function parseCurrentBuild(): number | undefined {
  const raw = Application.nativeBuildVersion;
  if (raw == null) return undefined;
  const parsed = parseInt(String(raw), 10);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function appSlug(): string {
  // codemagic.yaml's "Apply per-build values to app.json" step writes
  // CM_APP_SLUG into expo.slug at build time, so this resolves to the
  // project's slug at runtime in production builds.
  const slug = (Constants.expoConfig?.slug ?? "").toString().trim();
  return slug;
}

/**
 * useMinVersionCheck — drop into the root layout. While `status === "loading"`
 * render a placeholder; when `status === "update-required"` render the
 * ForceUpdateScreen instead of the app; otherwise render the app normally.
 */
export function useMinVersionCheck(): MinVersionState {
  const [state, setState] = useState<MinVersionState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const slug = appSlug();
      const platform = platformKey();
      const currentBuild = parseCurrentBuild();

      // Without a slug we can't query the backend — fail open and render
      // the app. Same when API_URL is missing (dev / unconfigured).
      if (!slug || !API_URL) {
        if (!cancelled) setState({ status: "ok", currentBuild });
        return;
      }

      // 1. Read cache. Serve a fresh-enough cached response immediately.
      const cached = await readCache(slug, platform);
      if (cached && Date.now() - cached.fetchedAt < CACHE_TTL_MS) {
        if (!cancelled) {
          setState(decideStatus(cached, currentBuild));
        }
        return;
      }

      // 2. Cache miss / stale — go to network. Failures are fail-open.
      const fresh = await fetchFromBackend(slug, platform);
      if (cancelled) return;

      if (!fresh) {
        // Try the (stale) cached value as a last resort: better than zero info.
        if (cached) {
          setState(decideStatus(cached, currentBuild));
        } else {
          // Genuine network failure with no cache — fail open.
          setState({ status: "error", currentBuild });
        }
        return;
      }

      await writeCache(slug, platform, fresh);
      if (!cancelled) setState(decideStatus(fresh, currentBuild));
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  return state;
}

function decideStatus(resp: BackendResponse, currentBuild: number | undefined): MinVersionState {
  // If we couldn't determine the installed build (Application.nativeBuildVersion
  // returns null on Expo Go), treat as ok — the gate is for store-deployed
  // builds, not in-development sessions.
  if (currentBuild === undefined) {
    return { status: "ok", minBuild: resp.min_build_number, storeURL: resp.store_url, message: resp.message };
  }
  if (resp.min_build_number > 0 && currentBuild < resp.min_build_number) {
    return {
      status: "update-required",
      minBuild: resp.min_build_number,
      currentBuild,
      storeURL: resp.store_url,
      message: resp.message,
    };
  }
  return {
    status: "ok",
    minBuild: resp.min_build_number,
    currentBuild,
    storeURL: resp.store_url,
    message: resp.message,
  };
}
