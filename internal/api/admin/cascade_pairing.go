package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
	"depsilo/internal/cascade"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

const maxCascadePairingBodyBytes int64 = 16 << 10

type cascadePairingRequest struct {
	// URL is the address children reach this node at; Admin proposes the
	// browser's origin.
	URL string `json:"url"`
}

// Pairing returns the code a child pastes to use this node as its parent. It
// turns cascade on, with a generated shared token when none is set, so one
// click makes this node a parent.
func (h *CascadeConfigHandler) Pairing(c *gin.Context) {
	var request cascadePairingRequest
	if err := decodeCascadeBody(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	address, err := normalizePeerAddress(request.URL)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": string(config.StoreInvalidSetting), "message": "url: " + err.Error()})
		return
	}
	ctx := c.Request.Context()
	token, enabled, err := h.store.CascadeSharedToken(ctx)
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	enabledNow, restartRequired := false, false
	if !enabled || token == "" {
		patch := config.CascadeConfigPatch{Enabled: boolPointer(true)}
		if token == "" {
			generated, err := config.NewSecureToken()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "could not generate a token"})
				return
			}
			patch.SharedToken = &generated
		}
		result, err := h.store.UpdateCascadeConfig(ctx, patch)
		if err != nil {
			writeSettingsError(c, err)
			return
		}
		enabledNow, restartRequired = true, result.State.PendingRestart
		if token, _, err = h.store.CascadeSharedToken(ctx); err != nil {
			writeSettingsError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, contract.AdminCascadePairingResponse{
		Code:            cascade.EncodePairingCode(cascade.Pairing{URL: address.String(), Token: token, InstanceID: h.runtime.InstanceID()}),
		URL:             address.String(),
		EnabledNow:      enabledNow,
		Loopback:        loopbackHost(address.Hostname()),
		RestartRequired: restartRequired,
	})
}

type cascadeJoinRequest struct {
	Code string `json:"code"`
	// Name names the parent; empty derives one.
	Name string `json:"name"`
	// RouteAll binds every direct upstream to the parent; nil means true.
	RouteAll *bool `json:"route_all"`
}

// Join configures the parent a pairing code names and, by default, routes
// every upstream through it, after checking that the parent accepts the
// token. A parent already configured at the same address gets the new token.
func (h *CascadeConfigHandler) Join(c *gin.Context) {
	var request cascadeJoinRequest
	if err := decodeCascadeBody(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	pairing, err := cascade.DecodePairingCode(request.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PAIRING_CODE", "message": "the pairing code is not valid; copy it again from the parent's Admin"})
		return
	}
	if pairing.InstanceID == h.runtime.InstanceID() {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "CASCADE_SELF_PAIRING", "message": "this is this node's own pairing code; paste it on another node"})
		return
	}
	address, err := normalizePeerAddress(pairing.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PAIRING_CODE", "message": "the pairing code names an invalid address: " + err.Error()})
		return
	}
	ctx := c.Request.Context()
	parentID, err := h.registry.ProbePeer(ctx, upstream.PeerEgress{Name: "pairing", URL: address, Token: pairing.Token})
	if err != nil {
		code := "CASCADE_PARENT_UNREACHABLE"
		switch {
		case errors.Is(err, upstream.ErrPeerNotRelay):
			code = "CASCADE_PARENT_NOT_RELAY"
		case errors.Is(err, upstream.ErrPeerUnauthorized):
			code = "CASCADE_PARENT_UNAUTHORIZED"
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": code, "message": err.Error()})
		return
	}

	state, err := h.store.CascadeConfig(ctx)
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	name, err := joinPeerName(state.Peers, address.String(), request.Name)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": string(config.StoreInvalidSetting), "message": err.Error()})
		return
	}
	token := pairing.Token
	peers := make([]config.CascadePeerInput, 0, len(state.Peers)+1)
	found := false
	for _, peer := range state.Peers {
		input := config.CascadePeerInput{Name: peer.Name, URL: peer.URL, ForwardCredentials: peer.ForwardCredentials}
		if peer.Name == name {
			input.URL, input.Token, found = address.String(), &token, true
		}
		peers = append(peers, input)
	}
	if !found {
		peers = append(peers, config.CascadePeerInput{Name: name, URL: address.String(), Token: &token})
	}
	patch := config.CascadeConfigPatch{Enabled: boolPointer(true), Peers: &peers}
	if !state.TokenSet {
		// Cascade on needs a token of this node's own; nobody else learns it.
		own, err := config.NewSecureToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "could not generate a token"})
			return
		}
		patch.SharedToken = &own
	}
	insecure := address.Scheme == "http" && !loopbackHost(address.Hostname()) && !state.AllowInsecureHTTP
	if insecure {
		patch.AllowInsecureHTTP = boolPointer(true)
	}
	result, err := h.store.UpdateCascadeConfig(ctx, patch)
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	response := contract.AdminCascadeJoinResponse{
		Peer:                name,
		URL:                 address.String(),
		ParentInstanceID:    parentID,
		Skipped:             []string{},
		InsecureHTTPAllowed: insecure,
		RestartRequired:     result.State.PendingRestart,
		State:               toCascadeConfigState(result.State),
	}
	if (request.RouteAll == nil || *request.RouteAll) && !result.State.PendingRestart {
		routed, err := h.registry.RouteAllVia(ctx, name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "ROUTE_FAILED", "message": fmt.Sprintf("the parent is configured, but routing upstreams through it failed: %v", err)})
			return
		}
		response.Bound = routed.Bound
		if routed.Skipped != nil {
			response.Skipped = routed.Skipped
		}
	}
	c.JSON(http.StatusOK, response)
}

// joinPeerName keeps the name of a parent already configured at address,
// takes requested when given, and otherwise picks "parent", "parent-2", ...
func joinPeerName(existing []config.CascadePeerState, address, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	for _, peer := range existing {
		if strings.TrimRight(peer.URL, "/") == address && (requested == "" || requested == peer.Name) {
			return peer.Name, nil
		}
	}
	taken := make(map[string]bool, len(existing))
	for _, peer := range existing {
		taken[peer.Name] = true
	}
	if requested != "" {
		if taken[requested] {
			return "", fmt.Errorf("a parent named %q already exists at another address", requested)
		}
		return requested, nil
	}
	for index := 1; ; index++ {
		name := "parent"
		if index > 1 {
			name += "-" + strconv.Itoa(index)
		}
		if !taken[name] {
			return name, nil
		}
	}
}

// normalizePeerAddress accepts an http(s) origin, optionally with a path
// prefix for a reverse proxy, and drops a trailing slash.
func normalizePeerAddress(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	switch {
	case err != nil || parsed.Host == "":
		return nil, errors.New("must be an http or https address with a host")
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return nil, errors.New("must use http or https")
	case parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "":
		return nil, errors.New("must not carry credentials, a query, or a fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func boolPointer(value bool) *bool { return &value }

func decodeCascadeBody(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCascadePairingBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}
