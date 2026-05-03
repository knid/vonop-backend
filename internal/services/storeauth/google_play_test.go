package storeauth

import (
	"encoding/json"
	"testing"
)

func TestParsePlayApplicationResponse_Enrolled(t *testing.T) {
	// Real responses include many fields; we only need to check whether
	// the app exists and (optionally) whether signing is configured.
	// If the app exists in Play Console + service account has access,
	// the GET /androidpublisher/v3/applications/{packageName} returns 200
	// with a JSON body. App Signing enrolment is a state we infer from
	// the API: if the API allows release uploads, signing is set up.
	//
	// For now, the simplest signal: HTTP 200 = enrolled (or at least
	// accessible by this service account); HTTP 404 = not registered;
	// HTTP 403 = service account lacks access.
	body := []byte(`{"packageName": "com.wemob.testapp"}`)
	enrolled, err := parsePlayApplicationResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !enrolled {
		t.Error("expected enrolled=true for valid app response")
	}
}

func TestParsePlayApplicationResponse_MalformedJSON(t *testing.T) {
	enrolled, err := parsePlayApplicationResponse([]byte("not json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
	if enrolled {
		t.Error("expected enrolled=false on parse error")
	}
}

var _ = json.Marshal
