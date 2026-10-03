package vowifisupervisor

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	stream "github.com/leonfox28/simplus/internal/domain/connectivity"
)

type ChangeAPI interface {
	ConnectionSnapshot(context.Context) (stream.Snapshot, error)
	ConnectionChanges(context.Context, stream.Cursor) (stream.Changes, error)
}

func (local *Local) connectionCursorLocked() stream.Cursor {
	if local.connectionInstance == "" {
		local.connectionInstance = uuid.NewString()
	}
	return stream.Cursor{Instance: local.connectionInstance, Sequence: local.connectionSequence}
}
func (local *Local) recordConnectionLocked(current *instance, before bool, reason string) {
	if before == current.status.Online {
		return
	}
	local.connectionCursorLocked()
	local.connectionSequence++
	at := local.Now().UTC()
	current.observedAt = at
	if current.status.Online {
		reason = "registered"
	}
	change := stream.Change{Sequence: local.connectionSequence, State: stream.State{ObjectID: current.status.LineID, Connected: current.status.Online, ObservedAt: at, Reason: reason}}
	local.connectionEvents = append(local.connectionEvents, change)
	if len(local.connectionEvents) > stream.Capacity {
		copy(local.connectionEvents, local.connectionEvents[len(local.connectionEvents)-stream.Capacity:])
		local.connectionEvents = local.connectionEvents[:stream.Capacity]
	}
}
func (local *Local) ConnectionSnapshot(context.Context) (stream.Snapshot, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	result := stream.Snapshot{Version: stream.Version, Cursor: local.connectionCursorLocked(), States: []stream.State{}}
	for _, current := range local.instances {
		at := current.observedAt
		if at.IsZero() {
			at = local.Now().UTC()
		}
		result.States = append(result.States, stream.State{ObjectID: current.status.LineID, Connected: current.status.Online, ObservedAt: at})
	}
	sort.Slice(result.States, func(i, j int) bool { return result.States[i].ObjectID < result.States[j].ObjectID })
	return result, nil
}
func (local *Local) ConnectionChanges(_ context.Context, after stream.Cursor) (stream.Changes, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	result := stream.Changes{Version: stream.Version, Cursor: local.connectionCursorLocked(), Events: []stream.Change{}}
	if after.Instance != result.Cursor.Instance || after.Sequence > result.Cursor.Sequence || result.Cursor.Sequence-after.Sequence > stream.Capacity {
		result.Reset = true
		return result, nil
	}
	for _, event := range local.connectionEvents {
		if event.Sequence > after.Sequence {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}
func (client *Client) ConnectionSnapshot(ctx context.Context) (stream.Snapshot, error) {
	var result stream.Snapshot
	err := client.request(ctx, http.MethodGet, "/v1/vowifi/connections/snapshot", nil, &result)
	if err != nil {
		return result, err
	}
	if result.Version != stream.Version || !validConnectionCursor(result.Cursor) || len(result.States) > 256 {
		return stream.Snapshot{}, ErrRequestInvalid
	}
	seen := map[string]bool{}
	for _, s := range result.States {
		if !validConnectionState(s) || seen[s.ObjectID] {
			return stream.Snapshot{}, ErrRequestInvalid
		}
		seen[s.ObjectID] = true
	}
	return result, nil
}
func (client *Client) ConnectionChanges(ctx context.Context, after stream.Cursor) (stream.Changes, error) {
	var result stream.Changes
	if !validConnectionCursor(after) {
		return result, ErrRequestInvalid
	}
	if err := client.request(ctx, http.MethodPost, "/v1/vowifi/connections/changes", after, &result); err != nil {
		return result, err
	}
	if result.Version != stream.Version || !validConnectionCursor(result.Cursor) || len(result.Events) > stream.Capacity {
		return stream.Changes{}, ErrRequestInvalid
	}
	if result.Reset {
		if len(result.Events) != 0 {
			return stream.Changes{}, ErrRequestInvalid
		}
		return result, nil
	}
	if result.Cursor.Instance != after.Instance || result.Cursor.Sequence < after.Sequence || uint64(len(result.Events)) != result.Cursor.Sequence-after.Sequence {
		return stream.Changes{}, ErrRequestInvalid
	}
	for i, event := range result.Events {
		if event.Sequence != after.Sequence+uint64(i)+1 || !validConnectionState(event.State) {
			return stream.Changes{}, ErrRequestInvalid
		}
	}
	return result, nil
}
func validConnectionCursor(c stream.Cursor) bool {
	_, err := uuid.Parse(c.Instance)
	return err == nil && len(c.Instance) == 36 && c.Sequence <= uint64(^uint64(0)>>1)
}
func validConnectionState(s stream.State) bool {
	if !managedLinePattern.MatchString(s.ObjectID) || s.ObservedAt.IsZero() || s.ObservedAt.After(time.Now().Add(time.Minute)) {
		return false
	}
	switch s.Reason {
	case "", "registered", "stopped", "reconnecting", "worker_exited":
		return true
	}
	return false
}
func registerConnectionHandlers(mux *http.ServeMux, api ChangeAPI) {
	mux.HandleFunc("GET /connections/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if api == nil {
			writeJSON(w, 503, errorResponse{Code: "CONNECTION_SOURCE_UNAVAILABLE"})
			return
		}
		result, err := api.ConnectionSnapshot(r.Context())
		if err != nil {
			writeJSON(w, 503, errorResponse{Code: "CONNECTION_SOURCE_UNAVAILABLE"})
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("POST /connections/changes", func(w http.ResponseWriter, r *http.Request) {
		var cursor stream.Cursor
		if api == nil || !decodeRequest(w, r, &cursor) || !validConnectionCursor(cursor) {
			writeJSON(w, 400, errorResponse{Code: "REQUEST_INVALID"})
			return
		}
		result, err := api.ConnectionChanges(r.Context(), cursor)
		if err != nil {
			writeJSON(w, 503, errorResponse{Code: "CONNECTION_SOURCE_UNAVAILABLE"})
			return
		}
		writeJSON(w, 200, result)
	})
}
