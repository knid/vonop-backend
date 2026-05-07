package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestApplyMetaAttribution(t *testing.T) {
	t.Run("populates empty user fields", func(t *testing.T) {
		u := &models.User{}
		attr := MetaAttribution{
			Fbp:            "fb.1.x",
			Fbc:            "fb.1.y",
			ClientIP:       "1.2.3.4",
			ClientUA:       "ua",
			EventSourceURL: "https://example.com/r",
		}
		if !applyMetaAttribution(u, attr) {
			t.Fatal("expected dirty=true on empty user")
		}
		if u.MetaFbp != "fb.1.x" || u.MetaClientIP != "1.2.3.4" {
			t.Errorf("fields not applied: %+v", u)
		}
	})

	t.Run("skip when values unchanged", func(t *testing.T) {
		u := &models.User{
			MetaFbp:      "fb.1.x",
			MetaClientIP: "1.2.3.4",
		}
		attr := MetaAttribution{Fbp: "fb.1.x", ClientIP: "1.2.3.4"}
		if applyMetaAttribution(u, attr) {
			t.Error("expected dirty=false when nothing changed")
		}
	})

	t.Run("empty attr fields don't clobber stored values", func(t *testing.T) {
		u := &models.User{
			MetaFbp:      "old",
			MetaClientIP: "old-ip",
			MetaClientUA: "old-ua",
		}
		attr := MetaAttribution{Fbp: "new"} // only fbp set
		if !applyMetaAttribution(u, attr) {
			t.Fatal("expected dirty=true when fbp changes")
		}
		if u.MetaFbp != "new" {
			t.Errorf("fbp not updated: %q", u.MetaFbp)
		}
		if u.MetaClientIP != "old-ip" {
			t.Errorf("ClientIP clobbered: %q, want old-ip", u.MetaClientIP)
		}
		if u.MetaClientUA != "old-ua" {
			t.Errorf("ClientUA clobbered: %q, want old-ua", u.MetaClientUA)
		}
	})

	t.Run("partial update marks dirty", func(t *testing.T) {
		u := &models.User{MetaFbp: "old"}
		if !applyMetaAttribution(u, MetaAttribution{Fbp: "new"}) {
			t.Error("expected dirty=true when one field changes")
		}
	})
}

func TestClientIPStripsPort(t *testing.T) {
	cases := []struct {
		name   string
		setReq func(*http.Request)
		want   string
	}{
		{
			name: "X-Forwarded-For first IP wins",
			setReq: func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")
				r.RemoteAddr = "10.0.0.1:54321"
			},
			want: "1.2.3.4",
		},
		{
			name: "X-Real-IP fallback",
			setReq: func(r *http.Request) {
				r.Header.Set("X-Real-IP", "5.6.7.8")
				r.RemoteAddr = "10.0.0.1:54321"
			},
			want: "5.6.7.8",
		},
		{
			name: "RemoteAddr port stripped",
			setReq: func(r *http.Request) {
				r.RemoteAddr = "9.10.11.12:54321"
			},
			want: "9.10.11.12",
		},
		{
			name: "RemoteAddr without port returned as-is",
			setReq: func(r *http.Request) {
				r.RemoteAddr = "9.10.11.12"
			},
			want: "9.10.11.12",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			tc.setReq(r)
			got := clientIP(r)
			if got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
