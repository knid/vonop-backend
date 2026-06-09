package agent

import "context"

// NativeGoRuntime is the legacy in-process execution engine.
type NativeGoRuntime struct{}

func (NativeGoRuntime) Kind() RuntimeKind {
	return RuntimeNativeGo
}

func (NativeGoRuntime) Run(ctx context.Context, req RuntimeRequest, emitter EventEmitter) (*RuntimeResult, error) {
	if req.Deps == nil || req.Project == nil || req.User == nil {
		return nil, New(CodeInternal, "setup", "missing required run context", nil)
	}
	return runNative(
		ctx,
		req.Deps,
		req.Project,
		req.User,
		req.Message,
		req.Attachments,
		emitter,
	)
}
