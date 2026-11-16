package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slidebolt/sb-api/internal/auth"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

func TestPairingRequiresAdminApprovalAndExchangesTokenOnce(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()
	store := &fakeStore{searchResultsByPattern: map[string][]storage.Entry{}}
	seedAccessToken(store, auth.AccessAdmin, []string{"*"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()

	post := func(path string, body any, bearer string) *http.Response {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	created := post("/pairing/requests", map[string]any{"clientName": "Android TV", "clientType": "android"}, "")
	if created.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d", created.StatusCode)
	}
	var create struct{ ID, Code, Verifier string }
	decodeBody(t, created, &create)
	if create.ID == "" || create.Code == "" || create.Verifier == "" {
		t.Fatalf("incomplete create: %+v", create)
	}
	pending := append([]byte(nil), store.savedBody...)
	store.searchResultsByPattern["sb-api.pairing.>"] = []storage.Entry{{Key: store.savedKey, Data: pending}}

	exchange := post("/pairing/requests/"+create.ID+"/exchange", map[string]string{"verifier": create.Verifier}, "")
	if exchange.StatusCode != http.StatusAccepted {
		t.Fatalf("pending exchange=%d", exchange.StatusCode)
	}
	exchange.Body.Close()
	approved := post("/pairing/requests/"+create.Code+"/approve", map[string]any{"access": "control", "securityGroups": []string{"*"}}, testTokenSecret)
	if approved.StatusCode != http.StatusOK {
		t.Fatalf("approve=%d", approved.StatusCode)
	}
	approved.Body.Close()
	approvedBody := append([]byte(nil), store.savedBody...)
	store.searchResultsByPattern["sb-api.pairing.>"] = []storage.Entry{{Key: "sb-api.pairing." + create.ID, Data: approvedBody}}

	exchange = post("/pairing/requests/"+create.ID+"/exchange", map[string]string{"verifier": create.Verifier}, "")
	if exchange.StatusCode != http.StatusOK {
		t.Fatalf("exchange=%d", exchange.StatusCode)
	}
	var result struct {
		Status, Token  string
		Access         auth.Access
		SecurityGroups []string
	}
	decodeBody(t, exchange, &result)
	if result.Status != "approved" || result.Token == "" || result.Access != auth.AccessControl {
		t.Fatalf("result=%+v", result)
	}
	if store.deletedKey != "sb-api.pairing."+create.ID {
		t.Fatalf("pairing not consumed: %s", store.deletedKey)
	}
}

func TestCoreResourcesStillRejectMissingBearerToken(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()
	store := &fakeStore{}
	seedAccessToken(store, auth.AccessAdmin, []string{"*"})
	srv := httptest.NewServer(New(msg, store))
	defer srv.Close()
	for _, path := range []string{"/devices", "/entities", "/tokens", "/pairing/requests/ABCD-EFGH"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s without token=%d", path, resp.StatusCode)
		}
	}
}
