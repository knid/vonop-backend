package storeauth

import (
	"encoding/json"
	"testing"
	"time"
)

// TestParseCertificatesResponse verifies that we correctly parse the ASC
// /v1/certificates response shape into our AppleCertificate slice.
func TestParseCertificatesResponse(t *testing.T) {
	body := []byte(`{
        "data": [
            {
                "id": "cert-1",
                "type": "certificates",
                "attributes": {
                    "serialNumber": "ABC123",
                    "certificateType": "DISTRIBUTION",
                    "displayName": "iOS Distribution: Wemob Inc",
                    "expirationDate": "2027-01-15T00:00:00Z"
                }
            },
            {
                "id": "cert-2",
                "type": "certificates",
                "attributes": {
                    "serialNumber": "DEF456",
                    "certificateType": "DEVELOPMENT",
                    "displayName": "iPhone Developer",
                    "expirationDate": "2026-06-01T00:00:00Z"
                }
            }
        ]
    }`)
	certs, err := parseCertificatesResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(certs) != 2 {
		t.Fatalf("expected 2 certs, got %d", len(certs))
	}
	if certs[0].ID != "cert-1" || certs[0].SerialNumber != "ABC123" {
		t.Errorf("cert[0] wrong: %+v", certs[0])
	}
	if certs[0].CertificateType != "DISTRIBUTION" {
		t.Errorf("cert[0] type: got %q want DISTRIBUTION", certs[0].CertificateType)
	}
	expectedExp, _ := time.Parse(time.RFC3339, "2027-01-15T00:00:00Z")
	if !certs[0].ExpiresAt.Equal(expectedExp) {
		t.Errorf("cert[0] expires: got %v want %v", certs[0].ExpiresAt, expectedExp)
	}
}

func TestParseProfilesResponse(t *testing.T) {
	body := []byte(`{
        "data": [
            {
                "id": "prof-1",
                "type": "profiles",
                "attributes": {
                    "name": "Wemob App Store",
                    "uuid": "abc-uuid",
                    "profileType": "IOS_APP_STORE",
                    "expirationDate": "2026-12-01T00:00:00Z"
                }
            }
        ]
    }`)
	profiles, err := parseProfilesResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Name != "Wemob App Store" {
		t.Errorf("name: got %q", profiles[0].Name)
	}
	if profiles[0].ProfileType != "IOS_APP_STORE" {
		t.Errorf("type: got %q", profiles[0].ProfileType)
	}
}

func TestParseCertificatesResponse_EmptyData(t *testing.T) {
	body := []byte(`{"data": []}`)
	certs, err := parseCertificatesResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(certs) != 0 {
		t.Errorf("expected 0 certs for empty data, got %d", len(certs))
	}
}

func TestParseCertificatesResponse_MalformedJSON(t *testing.T) {
	if _, err := parseCertificatesResponse([]byte("not json")); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

// Use json import keep-alive
var _ = json.Marshal
