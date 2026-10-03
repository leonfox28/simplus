package messaging

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
)

// RunInbound keeps a separate deadline and retry clock for every line. A
// completed line publishes its committed progress immediately; other lines
// may still be probing or waiting for the modem's serial operation gate.
func (service *Service) RunInbound(ctx context.Context, interval time.Duration, report func(InboundSyncResult, error)) {
	if report == nil {
		report = func(InboundSyncResult, error) {}
	}
	type completion struct {
		id     string
		result InboundSyncResult
		err    error
	}
	done := make(chan completion)
	var work sync.WaitGroup
	defer work.Wait()
	active := map[string]bool{}
	next := map[string]time.Time{}
	retries := map[string]time.Duration{}
	ticker := time.NewTicker(normalizedSyncInterval(interval))
	defer ticker.Stop()
	schedule := func() {
		cycle, cancel := context.WithTimeout(ctx, 5*time.Second)
		topology, err := service.lines.Topology(cycle)
		cancel()
		if err != nil {
			report(InboundSyncResult{}, err)
			return
		}
		for _, line := range topology.Lines {
			if line.State != inventory.LineReady || active[line.ID] || time.Now().Before(next[line.ID]) {
				continue
			}
			active[line.ID] = true
			work.Go(func() {
				cycle, cancel := context.WithTimeout(ctx, syncTimeout)
				defer cancel()
				result, err := service.syncResolvedLine(cycle, topology, line)
				select {
				case done <- completion{line.ID, result, err}:
				case <-ctx.Done():
				}
			})
		}
	}
	schedule()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			schedule()
		case result := <-done:
			delete(active, result.id)
			delay := normalizedSyncInterval(interval)
			if result.err != nil {
				delay = nextSyncRetryDelay(retries[result.id], interval)
				retries[result.id] = delay
			} else {
				delete(retries, result.id)
			}
			next[result.id] = time.Now().Add(delay)
			report(result.result, result.err)
		}
	}
}

func (service *Service) syncResolvedLine(ctx context.Context, topology inventory.Topology, line inventory.Line) (InboundSyncResult, error) {
	transport, err := service.resolveTransport(ctx, line)
	if err != nil {
		return InboundSyncResult{}, errors.Join(ErrInboundSync, err)
	}
	target := runtimeLine{line: line}
	if transport.resolveLine != nil {
		target, err = transport.resolveLine(topology, line)
		if err != nil {
			return InboundSyncResult{}, errors.Join(ErrInboundSync, err)
		}
	}
	return service.syncInboundLine(ctx, line, target, transport)
}
