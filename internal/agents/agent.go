package agents

import "context"

// Agent defines the unified execution contract for all x-agent agents.
type Agent interface {
	Run(ctx context.Context) error
}

// Reporter is an optional interface for agents that generate formatted text summaries.
type Reporter interface {
	Report() string
}
