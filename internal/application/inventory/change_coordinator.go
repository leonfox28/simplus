package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/application/realtime"
)

const (
	agentChangeWatchSeconds = 25
	initialAgentRetryDelay  = time.Second
	maximumAgentRetryDelay  = 30 * time.Second
)

var ErrAgentChangeCoordinatorConfiguration = errors.New("inventory Agent change coordinator dependencies are incomplete")

type AgentChangeSource interface {
	Snapshot(context.Context, bool) (agentapi.Snapshot, error)
	Changes(context.Context, string, uint64, int) (agentapi.ChangeResponse, error)
}

type AgentChangePublisher interface {
	Publish([]realtime.Topic, realtime.Attention)
}

type AgentChangeOperation string

const (
	AgentChangeSnapshot AgentChangeOperation = "snapshot"
	AgentChangeWatch    AgentChangeOperation = "changes"
)

type AgentChangeReport struct {
	Operation AgentChangeOperation
	Error     error
}

type AgentChangeCoordinator struct {
	source    AgentChangeSource
	publisher AgentChangePublisher
	wait      func(context.Context, time.Duration) bool
}

func NewAgentChangeCoordinator(source AgentChangeSource, publisher AgentChangePublisher) (*AgentChangeCoordinator, error) {
	if source == nil || publisher == nil {
		return nil, ErrAgentChangeCoordinatorConfiguration
	}
	return &AgentChangeCoordinator{source: source, publisher: publisher, wait: waitForAgentContext}, nil
}

func (coordinator *AgentChangeCoordinator) Run(ctx context.Context, report func(AgentChangeReport)) {
	var previous agentapi.Snapshot
	retryDelay := initialAgentRetryDelay
	for ctx.Err() == nil {
		snapshot, err := coordinator.source.Snapshot(ctx, false)
		if err != nil {
			publishAgentChangeReport(report, AgentChangeSnapshot, err)
			if !coordinator.wait(ctx, retryDelay) {
				return
			}
			retryDelay = nextAgentChangeRetryDelay(retryDelay)
			continue
		}
		if previous.AgentInstanceID != "" &&
			(snapshot.AgentInstanceID != previous.AgentInstanceID || snapshot.Generation != previous.Generation) {
			coordinator.publishInventoryChange()
		}
		previous = snapshot
		for ctx.Err() == nil {
			change, changeErr := coordinator.source.Changes(
				ctx,
				snapshot.AgentInstanceID,
				snapshot.Generation,
				agentChangeWatchSeconds,
			)
			if changeErr != nil {
				publishAgentChangeReport(report, AgentChangeWatch, changeErr)
				break
			}
			snapshot = change.Snapshot
			previous = snapshot
			retryDelay = initialAgentRetryDelay
			if change.Changed {
				coordinator.publishInventoryChange()
			}
		}
		if !coordinator.wait(ctx, retryDelay) {
			return
		}
		retryDelay = nextAgentChangeRetryDelay(retryDelay)
	}
}

func (coordinator *AgentChangeCoordinator) publishInventoryChange() {
	coordinator.publisher.Publish(
		[]realtime.Topic{realtime.TopicInventory, realtime.TopicModems, realtime.TopicLines},
		"",
	)
}

func publishAgentChangeReport(report func(AgentChangeReport), operation AgentChangeOperation, err error) {
	if report != nil {
		report(AgentChangeReport{Operation: operation, Error: err})
	}
}

func nextAgentChangeRetryDelay(previous time.Duration) time.Duration {
	if previous < initialAgentRetryDelay {
		return initialAgentRetryDelay
	}
	if previous >= maximumAgentRetryDelay {
		return maximumAgentRetryDelay
	}
	delay := previous * 2
	if delay > maximumAgentRetryDelay {
		return maximumAgentRetryDelay
	}
	return delay
}

func waitForAgentContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
