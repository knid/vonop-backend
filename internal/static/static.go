package static

import _ "embed"

//go:embed nextjs_template_package_lock.json
var NextJSTemplatePackageLock string

//go:embed wemob-bridge.js
var WemobBridgeJS string
