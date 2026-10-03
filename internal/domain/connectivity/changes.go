package connectivity

import "time"

const Version = 1
const Capacity = 1024

type Cursor struct {
	Instance string `json:"instance"`
	Sequence uint64 `json:"sequence"`
}
type State struct {
	ObjectID   string    `json:"objectId"`
	Connected  bool      `json:"connected"`
	ObservedAt time.Time `json:"observedAt"`
	Reason     string    `json:"reason"`
}
type Change struct {
	Sequence uint64 `json:"sequence"`
	State    State  `json:"state"`
}
type Snapshot struct {
	Version int     `json:"version"`
	Cursor  Cursor  `json:"cursor"`
	States  []State `json:"states"`
}
type Changes struct {
	Version int      `json:"version"`
	Cursor  Cursor   `json:"cursor"`
	Reset   bool     `json:"reset"`
	Events  []Change `json:"events"`
}
