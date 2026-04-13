package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/constants"
)

type ConfigureMobileBridgeTool struct {
	Deps *RunDependencies
}

func (ConfigureMobileBridgeTool) Name() string { return "configure_mobile_bridge" }
func (ConfigureMobileBridgeTool) Description() string {
	return "Enable or disable a native mobile bridge method (camera, geolocation, push, biometrics, share, etc.)."
}
func (ConfigureMobileBridgeTool) Schema() map[string]any {
	methods := make([]string, 0, len(constants.ALL_BRIDGE_METHODS))
	methods = append(methods, constants.ALL_BRIDGE_METHODS...)
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"method":  map[string]any{"type": "string", "enum": methods},
			"enabled": map[string]any{"type": "boolean"},
		},
		"required": []string{"method", "enabled"},
	}
}

func (c ConfigureMobileBridgeTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Method  string `json:"method"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	found := false
	for _, m := range constants.ALL_BRIDGE_METHODS {
		if m == args.Method {
			found = true
			break
		}
	}
	if !found {
		return fmt.Sprintf("unknown bridge method: %s", args.Method), true, nil
	}
	if c.Deps == nil || c.Deps.UpdateMobileBridgeMethod == nil {
		return "mobile bridge configuration not available", true, nil
	}
	if err := c.Deps.UpdateMobileBridgeMethod(ctx, rc.Project.ID.String(), args.Method, args.Enabled); err != nil {
		return fmt.Sprintf("update failed: %v", err), true, nil
	}
	verb := "enabled"
	if !args.Enabled {
		verb = "disabled"
	}
	return fmt.Sprintf("mobile bridge method %q %s", args.Method, verb), false, nil
}
