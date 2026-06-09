package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAgentRuntimeKind_DefaultFallsBackToNative(t *testing.T) {
	t.Setenv("AGENT_RUNTIME", "")
	got := ResolveAgentRuntimeKind()
	assert.Equal(t, AgentRuntimeNativeGo, got)
}

func TestResolveAgentRuntimeKind_ExplicitNativeGo(t *testing.T) {
	t.Setenv("AGENT_RUNTIME", "native_go")
	got := ResolveAgentRuntimeKind()
	assert.Equal(t, AgentRuntimeNativeGo, got)
}

func TestResolveAgentRuntimeKind_ExplicitDeepAgents(t *testing.T) {
	t.Setenv("AGENT_RUNTIME", "deepagents")
	got := ResolveAgentRuntimeKind()
	assert.Equal(t, AgentRuntimeDeepAgents, got)
}

func TestResolveAgentRuntimeKind_UnknownFallsBackToNative(t *testing.T) {
	t.Setenv("AGENT_RUNTIME", "unsupported-runtime")
	got := ResolveAgentRuntimeKind()
	assert.Equal(t, AgentRuntimeNativeGo, got)
}

func TestNewAgentRuntimeFromEnv_NativeGo(t *testing.T) {
	t.Setenv("AGENT_RUNTIME", "native_go")
	r, err := NewRuntimeFromEnv()
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, AgentRuntimeNativeGo, r.Kind())
	assert.IsType(t, NativeGoRuntime{}, r)
}

func TestNewAgentRuntime_DeepAgentsMissingConfig(t *testing.T) {
	t.Setenv("AGENT_RUNTIME", "deepagents")
	t.Setenv("DEEPAGENTS_RUNTIME_URL", "")
	t.Setenv("DEEPAGENTS_INTERNAL_TOKEN", "")
	r, err := NewAgentRuntime(AgentRuntimeDeepAgents)
	require.Error(t, err)
	assert.Nil(t, r)
}

func TestNewAgentRuntime_DeepAgents(t *testing.T) {
	t.Setenv("AGENT_RUNTIME", "deepagents")
	t.Setenv("DEEPAGENTS_RUNTIME_URL", "http://127.0.0.1:1234")
	t.Setenv("DEEPAGENTS_INTERNAL_TOKEN", "token")
	r, err := NewAgentRuntime(AgentRuntimeDeepAgents)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, AgentRuntimeDeepAgents, r.Kind())
	assert.IsType(t, &DeepAgentsRuntime{}, r)
}
