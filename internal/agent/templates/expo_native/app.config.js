// app.config.js — Expo CLI calls this with `config` set to the parsed
// app.json contents. We patch a few fields conditionally based on env vars,
// then return the final config.
//
// Used by Wemob's preview pipeline to strip EAS / fingerprint config when
// running the Expo Go-targeted Metro instance (port 19000), which would
// otherwise try to validate the placeholder EAS projectId against Expo's
// account API and fail with "Input is required, but 'npx expo' is in
// non-interactive mode" because the sandbox runs without an EXPO_TOKEN.
//
// Triggered by EXPO_GO_MODE=1 — set only by the managed sandbox preview
// runtime. The web Metro on port 8081 and
// Codemagic production builds do NOT set this env var, so they get the
// unmodified app.json (real EAS projectId, fingerprint policy intact for
// OTA matching).

module.exports = ({ config }) => {
  if (process.env.EXPO_GO_MODE === "1") {
    // Expo Go uses its own SDK runtime, not the user's. The runtimeVersion
    // and updates URL only matter for production OTA against a Wemob-signed
    // build — neither is exercised in Expo Go. Stripping them avoids the
    // fingerprint computation pass that triggers the auth prompt.
    delete config.runtimeVersion;
    delete config.updates;

    // Same story for the EAS projectId. Our placeholder UUID isn't a real
    // EAS project; Expo CLI tries to validate it against the EAS API and
    // hits the non-interactive auth wall.
    if (config.extra && config.extra.eas) {
      delete config.extra.eas;
    }
  }

  return config;
};
