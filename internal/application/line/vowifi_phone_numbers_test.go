package line

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/leonfox28/simplus/internal/vowifisupervisor"
)

type fixedVoWiFiStatusSource struct {
	statuses []vowifisupervisor.Status
	err      error
}

func (source fixedVoWiFiStatusSource) List(context.Context) ([]vowifisupervisor.Status, error) {
	return source.statuses, source.err
}

func TestVoWiFiPhoneNumberSourceReturnsOnlyUniqueOnlineValidatedLineObservations(t *testing.T) {
	source := NewVoWiFiPhoneNumberSource(fixedVoWiFiStatusSource{statuses: []vowifisupervisor.Status{
		{LineID: "line_AQEBAQEBAQEBAQEBAQEBAQ", State: vowifisupervisor.StateOnline, Online: true, PhoneNumber: "+12025550123"},
		{LineID: "line_AgICAgICAgICAgICAgICAg", State: vowifisupervisor.StateStopped, PhoneNumber: "+12025550124"},
		{LineID: "line_AwMDAwMDAwMDAwMDAwMDAw", State: vowifisupervisor.StateOnline, Online: true, PhoneNumber: "12025550125"},
		{LineID: "line_BAQEBAQEBAQEBAQEBAQEBA", State: vowifisupervisor.StateOnline, Online: true, PhoneNumber: "+12025550126"},
		{LineID: "line_BAQEBAQEBAQEBAQEBAQEBA", State: vowifisupervisor.StateOnline, Online: true, PhoneNumber: "+12025550127"},
	}})
	numbers, err := source.CurrentPhoneNumbers(t.Context())
	if err != nil || !reflect.DeepEqual(numbers, map[string]string{"line_AQEBAQEBAQEBAQEBAQEBAQ": "+12025550123"}) {
		t.Fatalf("numbers=%#v error=%v", numbers, err)
	}
}

func TestVoWiFiPhoneNumberSourcePropagatesListFailureForLineBestEffortHandling(t *testing.T) {
	want := errors.New("unavailable")
	source := NewVoWiFiPhoneNumberSource(fixedVoWiFiStatusSource{err: want})
	if _, err := source.CurrentPhoneNumbers(t.Context()); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}
