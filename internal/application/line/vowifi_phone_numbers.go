package line

import (
	"context"
	"regexp"

	"github.com/leonfox28/simplus/internal/vowifisupervisor"
)

var imsPhoneNumberPattern = regexp.MustCompile(`^\+[1-9][0-9]{2,14}$`)

type voWiFiStatusSource interface {
	List(context.Context) ([]vowifisupervisor.Status, error)
}

type voWiFiPhoneNumberSource struct {
	supervisor voWiFiStatusSource
}

func NewVoWiFiPhoneNumberSource(supervisor voWiFiStatusSource) PhoneNumberSource {
	if supervisor == nil {
		return nil
	}
	return voWiFiPhoneNumberSource{supervisor: supervisor}
}

func (source voWiFiPhoneNumberSource) CurrentPhoneNumbers(ctx context.Context) (map[string]string, error) {
	statuses, err := source.supervisor.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	duplicates := make(map[string]struct{})
	for _, status := range statuses {
		if status.State != vowifisupervisor.StateOnline || !status.Online || !imsPhoneNumberPattern.MatchString(status.PhoneNumber) {
			continue
		}
		if _, exists := result[status.LineID]; exists {
			delete(result, status.LineID)
			duplicates[status.LineID] = struct{}{}
			continue
		}
		if _, duplicate := duplicates[status.LineID]; !duplicate {
			result[status.LineID] = status.PhoneNumber
		}
	}
	return result, nil
}
