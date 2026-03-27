/**
 * Wemob Bridge SDK v1.1
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

    /** Camera & photo picker */
    camera: {
      /** Take a photo with the device camera. Returns { data (base64), mimeType, name } */
      takePhoto: function (opts) {
        return call("camera.takePhoto", opts || {});
      },
      /** Pick image(s) from gallery. opts: { quality?, multiple? } */
      pickImage: function (opts) {
        return call("camera.pickImage", opts || {});
      },
      /** Pick a video from gallery */
      pickVideo: function () {
        return call("camera.pickVideo");
      },
    },

    /** Contacts access */
    contacts: {
      /** Get all contacts. Returns { contacts: [{ id, name, phones, emails }] } */
      getAll: function () {
        return call("contacts.getAll");
      },
      /** Open native contact picker. Returns { contact: { id, name, phones, emails } } */
      pick: function () {
        return call("contacts.pick");
      },
    },

    /** Calendar */
    calendar: {
      /** Add an event. opts: { title, startDate (ISO), endDate?, location?, notes? } */
      addEvent: function (opts) {
        return call("calendar.addEvent", opts || {});
      },
    },

    /** File picker */
    files: {
      /** Pick file(s). opts: { multiple?, type? ("image"|"video"|"audio"|"pdf,doc") } */
      pick: function (opts) {
        return call("files.pick", opts || {});
      },
    },

    /** Bluetooth Low Energy */
    bluetooth: {
      /** Check if BLE is available and enabled. Returns { available, enabled } */
      isAvailable: function () {
        return call("bluetooth.isAvailable");
      },
      /** Scan for nearby BLE devices. opts: { duration? (seconds, default 5), serviceUuids?: string[] } Returns { devices: [{ id, name, rssi }] } */
      scan: function (opts) {
        return call("bluetooth.scan", opts || {});
      },
      /** Connect to a BLE device by ID. Returns { connected: true } or { error } */
      connect: function (deviceId) {
        return call("bluetooth.connect", { deviceId: deviceId });
      },
      /** Disconnect from a BLE device. Returns { disconnected: true } or { error } */
      disconnect: function (deviceId) {
        return call("bluetooth.disconnect", { deviceId: deviceId });
      },
      /** Discover services on a connected device. Returns { services: [{ uuid, characteristics: [{ uuid, properties }] }] } */
      getServices: function (deviceId) {
        return call("bluetooth.getServices", { deviceId: deviceId });
      },
      /** Read a characteristic value (base64). Returns { value } or { error } */
      read: function (deviceId, serviceUuid, characteristicUuid) {
        return call("bluetooth.read", {
          deviceId: deviceId,
          serviceUuid: serviceUuid,
          characteristicUuid: characteristicUuid,
        });
      },
      /** Write a value (base64) to a characteristic. Returns { success: true } or { error } */
      write: function (deviceId, serviceUuid, characteristicUuid, value) {
        return call("bluetooth.write", {
          deviceId: deviceId,
          serviceUuid: serviceUuid,
          characteristicUuid: characteristicUuid,
          value: value,
        });
      },
    },
  };
})();
