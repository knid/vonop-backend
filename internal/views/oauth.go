package views

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/markbates/goth/gothic"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) OAuthBegin(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	r = r.WithContext(context.WithValue(r.Context(), "provider", provider))
	gothic.BeginAuthHandler(w, r)
}

func (v *HttpViews) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	r = r.WithContext(context.WithValue(r.Context(), "provider", provider))

	oauthUser, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userLoginRes, err := v.Controller.GoogleLogin(oauthUser)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go func() {
		if u, lookupErr := v.Controller.DB.GetUserByEmail(oauthUser.Email); lookupErr == nil {
			v.Controller.EnsureIdentified(u)
			v.Controller.TrackUserEvent(u, "user_oauth_logged_in", posthog.NewProperties().
				Set("provider", "Google"))
		}
	}()

	frontendURL := strings.TrimRight(v.Controller.SecretManager.GetSecret("FRONTEND_URL"), "/")
	if frontendURL == "" {
		frontendURL = "https://dashboard.wemob.io"
	}
	url := fmt.Sprintf("%s/login/handleGoogle?token=%s", frontendURL, userLoginRes.Token)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}
