package armory

import (
	"context"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/shared/tree"
)

type RootNode struct {
	next Handler
}

func NewRootNode(next Handler) RootNode {
	return RootNode{next: next}
}

func (n RootNode) Apply(ctx context.Context, command model.ArmoryCommand, dynamic *DynamicContext) (model.RegisteredAgent, error) {
	return tree.Route(ctx, n.next, command, dynamic)
}
