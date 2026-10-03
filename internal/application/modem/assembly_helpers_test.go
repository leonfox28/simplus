package modem

import (
	"context"

	domain "github.com/leonfox28/simplus/internal/domain/modem"
)

type RFController interface {
	State(context.Context, string) (string, error)
	Set(context.Context, string, bool) (string, error)
}
type legacyTestReader struct {
	controller RFController
	service    *Service
}

func (r legacyTestReader) Read(ctx context.Context, id string) (domain.RuntimeStatus, error) {
	state, err := r.controller.State(ctx, id)
	topology, _ := r.service.inventory.Topology(ctx)
	presence := observations(topology)[id].simPresence
	return domain.RuntimeStatus{RFState: state, SIMPresence: presence, Cellular: domain.UnavailableCellularStatus()}, err
}
func (s *Service) UseRFController(c RFController) {
	s.rf = c
	s.runtime = legacyTestReader{c, s}
	if r, ok := c.(RuntimeStatusReader); ok {
		s.runtime = r
	}
}
func (s *Service) UseEquipmentIdentityReader(r EquipmentIdentityReader) { s.identity = r }
