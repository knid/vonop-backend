package constants

// ALL_BRIDGE_METHODS lists every available bridge method for system prompt injection.
var ALL_BRIDGE_METHODS = []string{
	"getDeviceInfo", "share", "haptic", "openExternal", "getConnectionStatus",
	"storage.get", "storage.set", "storage.remove",
	"camera.takePhoto", "camera.pickImage", "camera.pickVideo",
	"contacts.getAll", "contacts.pick",
	"calendar.addEvent",
	"files.pick",
	"bluetooth.isAvailable", "bluetooth.scan", "bluetooth.connect",
	"bluetooth.disconnect", "bluetooth.getServices", "bluetooth.read", "bluetooth.write",
}
