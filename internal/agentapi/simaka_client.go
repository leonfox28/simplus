package agentapi

import (
	"context"
	"fmt"
	"net/http"
)

func (client *Client) SIMAKAIdentity(ctx context.Context, request SIMAKAIdentityRequest) (SIMAKAIdentityResponse, error) {
	var response SIMAKAIdentityResponse
	if err := client.request(ctx, http.MethodPost, "/v1/sim/aka/identity", request, &response); err != nil {
		return SIMAKAIdentityResponse{}, err
	}
	if response.ProtocolVersion != ProtocolVersion || response.AgentInstanceID != request.AgentInstanceID ||
		response.DeviceID != request.DeviceID || !simAKAIMSI.MatchString(response.IMSI) {
		return SIMAKAIdentityResponse{}, fmt.Errorf("invalid SIM AKA identity response")
	}
	return response, nil
}

func (client *Client) SIMIMSProfile(ctx context.Context, request SIMIMSProfileRequest) (SIMIMSProfileResponse, error) {
	var response SIMIMSProfileResponse
	if err := client.request(ctx, http.MethodPost, "/v1/sim/ims/profile", request, &response); err != nil {
		return SIMIMSProfileResponse{}, err
	}
	validSource := response.ISIMAvailable && response.IdentitySource == SIMIMSIdentityISIM ||
		!response.ISIMAvailable && response.IdentitySource == SIMIMSIdentityDerived
	if response.ProtocolVersion != ProtocolVersion || response.AgentInstanceID != request.AgentInstanceID ||
		response.DeviceID != request.DeviceID || !validSource {
		return SIMIMSProfileResponse{}, fmt.Errorf("invalid SIM IMS profile response")
	}
	return response, nil
}

func (client *Client) SIMIMSIdentity(ctx context.Context, request SIMIMSIdentityRequest) (SIMIMSIdentityResponse, error) {
	var response SIMIMSIdentityResponse
	if err := client.request(ctx, http.MethodPost, "/v1/sim/ims/identity", request, &response); err != nil {
		return SIMIMSIdentityResponse{}, err
	}
	material := SIMIMSIdentityMaterial{
		Source: response.IdentitySource, PrivateIdentity: response.PrivateIdentity,
		HomeDomain: response.HomeDomain, PublicIdentities: response.PublicIdentities,
		ApplicationDiscovery:  response.ApplicationDiscovery,
		ApplicationCandidates: response.ApplicationCandidates,
		SMSOverIP:             response.SMSOverIP,
	}
	if response.ProtocolVersion != ProtocolVersion || response.AgentInstanceID != request.AgentInstanceID ||
		response.DeviceID != request.DeviceID || !validSIMIMSIdentityMaterial(material) {
		return SIMIMSIdentityResponse{}, fmt.Errorf("invalid SIM IMS identity response")
	}
	return response, nil
}

func (client *Client) AuthenticateSIMAKA(ctx context.Context, request SIMAKAAuthenticationRequest) (SIMAKAAuthenticationResponse, error) {
	var response SIMAKAAuthenticationResponse
	if err := client.request(ctx, http.MethodPost, "/v1/sim/aka/authenticate", request, &response); err != nil {
		return SIMAKAAuthenticationResponse{}, err
	}
	if response.ProtocolVersion != ProtocolVersion || response.AgentInstanceID != request.AgentInstanceID ||
		response.DeviceID != request.DeviceID || response.ExchangeID != request.ExchangeID ||
		!validSIMAKAAuthenticationResult(response.Result) {
		return SIMAKAAuthenticationResponse{}, fmt.Errorf("invalid SIM AKA authentication response")
	}
	return response, nil
}
