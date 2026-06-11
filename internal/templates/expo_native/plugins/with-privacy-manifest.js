// Wemob — iOS PrivacyInfo.xcprivacy injector.
//
// Apple requires every app submitted to the App Store (since May 2024) to
// declare its use of "Required Reason APIs" via a PrivacyInfo.xcprivacy
// resource bundled with the iOS binary. React Native + Expo touch four of
// these API categories out of the box (file timestamps, NSUserDefaults,
// system boot time, and free disk space), so every Wemob-built app needs
// one or it will be rejected at upload / review time.
//
// This plugin runs at `expo prebuild` (and at every `expo run:ios`) and:
//   1. Copies the canonical xcprivacy from assets/ into ios/<App>/.
//   2. Registers that file in the Xcode project's Resources build phase so
//      it lands inside the .app bundle at compile time.
//
// The xcprivacy itself is intentionally a static baseline — see
// assets/PrivacyInfo.xcprivacy. A follow-up will derive a per-app manifest
// from MobileAppConfig (declared SDKs, tracking, etc.); for now every
// Wemob app ships the same safe defaults.
//
// Note: @expo/config-plugins ships its own `IOSConfig.PrivacyInfo`
// (driven by `expo.ios.privacyManifests` in app.json). We use a custom
// plugin instead because a static checked-in file is easier to audit,
// keeps the source of truth as a real plist (not JSON-encoded), and gives
// us a hook to template later from MobileAppConfig.

const fs = require('fs');
const path = require('path');

const {
  withDangerousMod,
  withXcodeProject,
  IOSConfig,
} = require('@expo/config-plugins');

const PRIVACY_FILENAME = 'PrivacyInfo.xcprivacy';
const SOURCE_REL_PATH = path.join('assets', PRIVACY_FILENAME);

const withPrivacyManifest = (config) => {
  // Step 1: at the prebuild "dangerous" stage, copy the xcprivacy from
  // assets/ into ios/<ProjectName>/PrivacyInfo.xcprivacy. Dangerous mods
  // are the only mods that get a fully prebuilt ios/ directory to write
  // into, which is exactly what we need.
  config = withDangerousMod(config, [
    'ios',
    async (cfg) => {
      const projectRoot = cfg.modRequest.projectRoot;
      const platformProjectRoot = cfg.modRequest.platformProjectRoot; // .../ios
      const projectName = IOSConfig.XcodeUtils.getProjectName(projectRoot);

      const src = path.join(projectRoot, SOURCE_REL_PATH);
      if (!fs.existsSync(src)) {
        throw new Error(
          `[with-privacy-manifest] missing source file at ${src}. ` +
            `Expected a PrivacyInfo.xcprivacy under assets/.`,
        );
      }

      const destDir = path.join(platformProjectRoot, projectName);
      fs.mkdirSync(destDir, { recursive: true });
      const dest = path.join(destDir, PRIVACY_FILENAME);
      fs.copyFileSync(src, dest);

      return cfg;
    },
  ]);

  // Step 2: register the copied file in the Xcode project's Resources
  // build phase. Without this, the file sits next to the source but is
  // never copied into the .app bundle and Apple still rejects the build.
  // We use the same `addResourceFileToGroup` helper that Expo's own
  // `setPrivacyInfo` uses — it handles PBXFileReference, PBXBuildFile,
  // and PBXResourcesBuildPhase wiring in one call, and is a no-op if the
  // file is already registered.
  config = withXcodeProject(config, (cfg) => {
    const projectRoot = cfg.modRequest.projectRoot;
    const projectName = IOSConfig.XcodeUtils.getProjectName(projectRoot);
    const filepath = path.join(projectName, PRIVACY_FILENAME);

    if (cfg.modResults.hasFile(filepath)) {
      return cfg;
    }

    cfg.modResults = IOSConfig.XcodeUtils.addResourceFileToGroup({
      filepath,
      groupName: projectName,
      project: cfg.modResults,
      isBuildFile: true,
      verbose: false,
    });

    return cfg;
  });

  return config;
};

module.exports = withPrivacyManifest;
