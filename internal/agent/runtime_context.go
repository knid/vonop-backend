package agent

type RuntimeContextCompiler struct{}

func (RuntimeContextCompiler) Compile(input AgentContextInput) AgentContextPacket {
	return CompileAgentContext(input)
}
