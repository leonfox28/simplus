package modemadapter

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
)

const usimAdministrativeDataFileID = 0x6fad

func deriveML307AIMSIdentity(ctx context.Context, query attransport.Query, imsi string, metadata agentapi.SIMIMSIdentityMaterial) (agentapi.SIMIMSIdentityMaterial, error) {
	if parseML307AIMSI([]string{imsi, "OK"}) == "" {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	mncLength, err := readML307AMNCLength(ctx, query)
	if err != nil || len(imsi) < 3+mncLength {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	mcc := imsi[:3]
	mnc := imsi[3 : 3+mncLength]
	if mncLength == 2 {
		mnc = "0" + mnc
	}
	domain := fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", mnc, mcc)
	privateIdentity := imsi + "@" + domain
	return agentapi.SIMIMSIdentityMaterial{
		Source: agentapi.SIMIMSIdentityDerived, PrivateIdentity: privateIdentity,
		HomeDomain: domain, PublicIdentities: []string{"sip:" + privateIdentity},
		ApplicationDiscovery: metadata.ApplicationDiscovery, ApplicationCandidates: metadata.ApplicationCandidates,
	}, nil
}

func readML307AMNCLength(ctx context.Context, query attransport.Query) (int, error) {
	return readSIMMNCLength(ctx, query)
}

func readSIMMNCLength(ctx context.Context, query attransport.Query) (int, error) {
	if query == nil {
		return 0, agentapi.ErrSIMAKAUnavailable
	}
	command := fmt.Sprintf("AT+CRSM=176,%d,0,0,4", usimAdministrativeDataFileID)
	lines, err := query(ctx, command, 3*time.Second)
	if err != nil || !attransport.HasTerminalOK(lines) {
		return 0, agentapi.ErrSIMAKAUnavailable
	}
	sw1, _, encoded, ok := parseCRSMResponse(lines)
	if !ok || sw1 != 0x90 && sw1 != 0x91 || len(encoded) != 8 {
		return 0, agentapi.ErrSIMAKAUnavailable
	}
	data, err := hex.DecodeString(encoded)
	if err != nil || len(data) != 4 {
		zeroSIMAKABytesLocal(data)
		return 0, agentapi.ErrSIMAKAUnavailable
	}
	defer zeroSIMAKABytesLocal(data)
	mncLength := int(data[3])
	if mncLength != 2 && mncLength != 3 {
		return 0, agentapi.ErrSIMAKAUnavailable
	}
	return mncLength, nil
}
