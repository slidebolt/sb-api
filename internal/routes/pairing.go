package routes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/sb-api/internal/auth"
)

const pairingPrefix = "sb-api.pairing."

type PairingConfig struct{ PublicURL, SystemID, SystemName string }

type pairingRequest struct {
	ID                      string      `json:"id"`
	Code                    string      `json:"code"`
	VerifierHash            string      `json:"verifierHash"`
	ClientName              string      `json:"clientName"`
	ClientType              string      `json:"clientType"`
	RequestedAccess         auth.Access `json:"requestedAccess"`
	RequestedSecurityGroups []string    `json:"requestedSecurityGroups"`
	ApprovedAccess          auth.Access `json:"approvedAccess,omitempty"`
	ApprovedSecurityGroups  []string    `json:"approvedSecurityGroups,omitempty"`
	Status                  string      `json:"status"`
	CreatedAt               time.Time   `json:"createdAt"`
	ExpiresAt               time.Time   `json:"expiresAt"`
}

func (p pairingRequest) Key() string { return pairingPrefix + p.ID }

type PairingCreateInput struct {
	Body struct {
		ClientName              string      `json:"clientName"`
		ClientType              string      `json:"clientType"`
		RequestedAccess         auth.Access `json:"requestedAccess,omitempty"`
		RequestedSecurityGroups []string    `json:"requestedSecurityGroups,omitempty"`
	}
}
type PairingCreateOutput struct {
	Body struct {
		ID               string    `json:"id"`
		Code             string    `json:"code"`
		Verifier         string    `json:"verifier"`
		ExpiresAt        time.Time `json:"expiresAt"`
		PollAfterSeconds int       `json:"pollAfterSeconds"`
	}
}
type PairingExchangeInput struct {
	ID   string `path:"id"`
	Body struct {
		Verifier string `json:"verifier"`
	}
}
type PairingExchangeOutput struct {
	Status int
	Body   struct {
		Status         string      `json:"status"`
		Token          string      `json:"token,omitempty"`
		Access         auth.Access `json:"access,omitempty"`
		SecurityGroups []string    `json:"securityGroups,omitempty"`
		APIURL         string      `json:"apiUrl,omitempty"`
	}
}
type PairingCodeInput struct {
	Code string `path:"code"`
}
type PairingApprovalInput struct {
	Code string `path:"code"`
	Body struct {
		Access         auth.Access `json:"access"`
		SecurityGroups []string    `json:"securityGroups"`
	}
}
type PairingAdminOutput struct {
	Body struct {
		Code                    string      `json:"code"`
		ClientName              string      `json:"clientName"`
		ClientType              string      `json:"clientType"`
		RequestedAccess         auth.Access `json:"requestedAccess"`
		RequestedSecurityGroups []string    `json:"requestedSecurityGroups"`
		Status                  string      `json:"status"`
		ExpiresAt               time.Time   `json:"expiresAt"`
	}
}
type WellKnownOutput struct {
	Body struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		APIVersion string `json:"apiVersion"`
		APIURL     string `json:"apiUrl"`
		OpenAPIURL string `json:"openapiUrl"`
		Pairing    bool   `json:"pairing"`
	}
}

var pairingMu sync.Mutex

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func pairingCode() (string, error) {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 8)
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = letters[int(raw[i])%len(letters)]
	}
	return string(b[:4]) + "-" + string(b[4:]), nil
}
func loadPairings(store storage.Storage) ([]pairingRequest, error) {
	entries, err := store.Search(pairingPrefix + ">")
	if err != nil {
		return nil, err
	}
	out := []pairingRequest{}
	for _, e := range entries {
		var p pairingRequest
		if json.Unmarshal(e.Data, &p) == nil {
			out = append(out, p)
		}
	}
	return out, nil
}
func findPairing(store storage.Storage, match func(pairingRequest) bool) (pairingRequest, error) {
	all, err := loadPairings(store)
	if err != nil {
		return pairingRequest{}, err
	}
	for _, p := range all {
		if match(p) {
			return p, nil
		}
	}
	return pairingRequest{}, fmt.Errorf("pairing request not found")
}
func validPairingGroups(groups []string) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if !domain.ValidSecurityGroup(g) {
			return false
		}
	}
	return true
}

func RegisterPairing(api huma.API, store storage.Storage, cfg PairingConfig) {
	name := strings.TrimSpace(cfg.SystemName)
	if name == "" {
		name = "SlideBolt"
	}
	huma.Register(api, huma.Operation{Method: http.MethodGet, Path: "/.well-known/slidebolt", Summary: "Describe this SlideBolt installation", Tags: []string{"pairing"}}, func(context.Context, *struct{}) (*WellKnownOutput, error) {
		out := &WellKnownOutput{}
		out.Body.ID = cfg.SystemID
		out.Body.Name = name
		out.Body.APIVersion = "1.0.33"
		out.Body.APIURL = cfg.PublicURL
		out.Body.OpenAPIURL = cfg.PublicURL + "/openapi.json"
		out.Body.Pairing = true
		return out, nil
	})

	huma.Register(api, huma.Operation{Method: http.MethodPost, Path: "/pairing/requests", Summary: "Request client pairing", Tags: []string{"pairing"}}, func(ctx context.Context, in *PairingCreateInput) (*PairingCreateOutput, error) {
		if strings.TrimSpace(in.Body.ClientName) == "" {
			return nil, huma.Error422UnprocessableEntity("clientName is required")
		}
		access := in.Body.RequestedAccess
		if access == "" {
			access = auth.AccessControl
		}
		if !auth.ValidAccess(string(access)) {
			return nil, huma.Error422UnprocessableEntity("requestedAccess is invalid")
		}
		groups := in.Body.RequestedSecurityGroups
		if len(groups) == 0 {
			groups = []string{"*"}
		}
		if !validPairingGroups(groups) {
			return nil, huma.Error422UnprocessableEntity("requestedSecurityGroups is invalid")
		}
		id, err := randomHex(12)
		if err != nil {
			return nil, err
		}
		verifier, err := auth.GenerateSecret()
		if err != nil {
			return nil, err
		}
		code, err := pairingCode()
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		p := pairingRequest{ID: id, Code: code, VerifierHash: auth.HashSecret(verifier), ClientName: strings.TrimSpace(in.Body.ClientName), ClientType: strings.TrimSpace(in.Body.ClientType), RequestedAccess: access, RequestedSecurityGroups: groups, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
		if err := store.Save(p); err != nil {
			return nil, huma.Error500InternalServerError("save pairing request failed", err)
		}
		out := &PairingCreateOutput{}
		out.Body.ID = id
		out.Body.Code = code
		out.Body.Verifier = verifier
		out.Body.ExpiresAt = p.ExpiresAt
		out.Body.PollAfterSeconds = 2
		return out, nil
	})

	huma.Register(api, huma.Operation{Method: http.MethodGet, Path: "/pairing/requests/{code}", Summary: "Look up a pairing code", Tags: []string{"pairing"}}, func(ctx context.Context, in *PairingCodeInput) (*PairingAdminOutput, error) {
		p, err := findPairing(store, func(p pairingRequest) bool { return strings.EqualFold(p.Code, in.Code) })
		if err != nil {
			return nil, huma.Error404NotFound("pairing request not found")
		}
		return pairingAdmin(p), nil
	})
	huma.Register(api, huma.Operation{Method: http.MethodPost, Path: "/pairing/requests/{code}/approve", Summary: "Approve client pairing", Tags: []string{"pairing"}}, func(ctx context.Context, in *PairingApprovalInput) (*PairingAdminOutput, error) {
		if !auth.ValidAccess(string(in.Body.Access)) || !validPairingGroups(in.Body.SecurityGroups) {
			return nil, huma.Error422UnprocessableEntity("valid access and securityGroups are required")
		}
		pairingMu.Lock()
		defer pairingMu.Unlock()
		p, err := findPairing(store, func(p pairingRequest) bool { return strings.EqualFold(p.Code, in.Code) })
		if err != nil {
			return nil, huma.Error404NotFound("pairing request not found")
		}
		if time.Now().UTC().After(p.ExpiresAt) {
			return nil, huma.Error410Gone("pairing request expired")
		}
		p.Status = "approved"
		p.ApprovedAccess = in.Body.Access
		p.ApprovedSecurityGroups = in.Body.SecurityGroups
		if err := store.Save(p); err != nil {
			return nil, err
		}
		return pairingAdmin(p), nil
	})
	huma.Register(api, huma.Operation{Method: http.MethodPost, Path: "/pairing/requests/{code}/reject", Summary: "Reject client pairing", Tags: []string{"pairing"}}, func(ctx context.Context, in *PairingCodeInput) (*PairingAdminOutput, error) {
		pairingMu.Lock()
		defer pairingMu.Unlock()
		p, err := findPairing(store, func(p pairingRequest) bool { return strings.EqualFold(p.Code, in.Code) })
		if err != nil {
			return nil, huma.Error404NotFound("pairing request not found")
		}
		p.Status = "rejected"
		if err := store.Save(p); err != nil {
			return nil, err
		}
		return pairingAdmin(p), nil
	})

	huma.Register(api, huma.Operation{Method: http.MethodPost, Path: "/pairing/requests/{id}/exchange", Summary: "Exchange an approved pairing request", Tags: []string{"pairing"}}, func(ctx context.Context, in *PairingExchangeInput) (*PairingExchangeOutput, error) {
		pairingMu.Lock()
		defer pairingMu.Unlock()
		p, err := findPairing(store, func(p pairingRequest) bool { return p.ID == in.ID })
		if err != nil {
			return nil, huma.Error404NotFound("pairing request not found")
		}
		if auth.HashSecret(in.Body.Verifier) != p.VerifierHash {
			return nil, huma.Error401Unauthorized("invalid pairing verifier")
		}
		if time.Now().UTC().After(p.ExpiresAt) {
			return nil, huma.Error410Gone("pairing request expired")
		}
		if p.Status == "rejected" {
			return nil, huma.Error403Forbidden("pairing request rejected")
		}
		out := &PairingExchangeOutput{}
		out.Status = http.StatusOK
		if p.Status != "approved" {
			out.Status = http.StatusAccepted
			out.Body.Status = "pending"
			return out, nil
		}
		secret, err := auth.GenerateSecret()
		if err != nil {
			return nil, err
		}
		tokenID, err := auth.GenerateID()
		if err != nil {
			return nil, err
		}
		tok := auth.Token{ID: tokenID, Name: p.ClientName, Hash: auth.HashSecret(secret), Access: p.ApprovedAccess, SecurityGroups: p.ApprovedSecurityGroups, CreatedAt: time.Now().UTC()}
		if err := store.Save(tok); err != nil {
			return nil, err
		}
		if err := store.Delete(p); err != nil {
			return nil, err
		}
		out.Body.Status = "approved"
		out.Body.Token = secret
		out.Body.Access = tok.Access
		out.Body.SecurityGroups = tok.SecurityGroups
		out.Body.APIURL = cfg.PublicURL
		return out, nil
	})
}

func pairingAdmin(p pairingRequest) *PairingAdminOutput {
	out := &PairingAdminOutput{}
	out.Body.Code = p.Code
	out.Body.ClientName = p.ClientName
	out.Body.ClientType = p.ClientType
	out.Body.RequestedAccess = p.RequestedAccess
	out.Body.RequestedSecurityGroups = p.RequestedSecurityGroups
	out.Body.Status = p.Status
	out.Body.ExpiresAt = p.ExpiresAt
	return out
}
