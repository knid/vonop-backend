/**
 * Wemob Bridge SDK v1.0
 * Enables communication between web content and the native Wemob app.
 * This SDK is a no-op when not running inside a Wemob native app.
 */
(function () {
  "use strict";

  var isNative = function () {
    return !!(
      window.flutter_inappwebview &&
      window.flutter_inappwebview.callHandler
    );
  };

  var call = function (method, args) {
    if (!isNative()) {
      return Promise.resolve(null);
    }
    return window.flutter_inappwebview.callHandler("WemobBridge", {
      method: method,
      args: args || {},
    });
  };

  window.WemobBridge = {
    /** Check if running inside a Wemob native app */
    isNative: isNative,

    /** Low-level call — use convenience methods below instead */
    call: call,

    /** Get device info (platform, model, OS version, app version) */
    getDeviceInfo: function () {
      return call("getDeviceInfo");
    },

    /** Open the native share sheet */
    share: function (text, url) {
      return call("share", { text: text, url: url });
    },

    /** Trigger haptic feedback: "light", "medium", or "heavy" */
    haptic: function (type) {
      return call("haptic", { type: type || "light" });
    },

    /** Open a URL in the system browser */
    openExternal: function (url) {
      return call("openExternal", { url: url });
    },

    /** Get network connection status */
    getConnectionStatus: function () {
      return call("getConnectionStatus");
    },

    /** Persistent key-value storage (backed by SharedPreferences) */
    storage: {
      get: function (key) {
        return call("storage.get", { key: key });
      },
      set: function (key, value) {
        return call("storage.set", { key: key, value: value });
      },
      remove: function (key) {
        return call("storage.remove", { key: key });
      },
    },
  };
})();
