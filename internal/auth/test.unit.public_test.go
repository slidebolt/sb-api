package auth

import (
	"net/http"
	"testing"
)

func TestOnlyDocumentationDiscoveryAndPairingExchangeArePublic(t *testing.T) {
	public := []struct{ method, path string }{
		{http.MethodGet, "/openapi.json"},
		{http.MethodGet, "/.well-known/slidebolt"},
		{http.MethodPost, "/pairing/requests"},
		{http.MethodPost, "/pairing/requests/id/exchange"},
	}
	for _, test := range public {
		if !publicRequest(test.method, test.path) {
			t.Fatalf("%s %s must be public", test.method, test.path)
		}
	}
	private := []struct{ method, path string }{
		{http.MethodGet, "/devices"},
		{http.MethodGet, "/entities"},
		{http.MethodGet, "/pairing/requests/ABCD-EFGH"},
		{http.MethodPost, "/pairing/requests/ABCD-EFGH/approve"},
		{http.MethodGet, "/tokens"},
	}
	for _, test := range private {
		if publicRequest(test.method, test.path) {
			t.Fatalf("%s %s must require auth", test.method, test.path)
		}
	}
}
