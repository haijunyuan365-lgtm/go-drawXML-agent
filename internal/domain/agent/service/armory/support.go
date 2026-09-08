package armory

import (
	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/shared/tree"
)

// 不是创建新类型，而是给一个复杂泛型类型起一个简短名字
type Handler = tree.Handler[
	model.ArmoryCommand,
	*DynamicContext,
	model.RegisteredAgent,
]
