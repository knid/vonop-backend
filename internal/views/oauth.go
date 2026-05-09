package views

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/middleware"
	"github.com/knid/wemob/internal/utils"
	"github.com/markbates/goth/gothic"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) OAuthBegin(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	r = r.WithContext(context.WithValue(r.Context(), "provider", provider))

	// Bridge Meta Pixel cookies (set on the front-end origin) across the
	// Google → callback redirect. The frontend appends them as query params
	// on this begin request; we stash them in a SameSite=Lax cookie scoped
	// to api.wemob.io so they're sent back when the user returns from Google.
	q := r.URL.Query()
	controllers.SetMetaAttrCookie(w, r, controllers.MetaAttribution{
		Fbp:            q.Get("fbp"),
		Fbc:            q.Get("fbc"),
		EventSourceURL: q.Get("event_source_url"),
	})

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

	attr := controllers.ReadMetaAttrCookie(r)
	controllers.ClearMetaAttrCookie(w, r)

	// Seed the new user's Language from the browser's Accept-Language so
	// transactional emails and the first UI render match. Without this, OAuth
	// signups always default to "en" and the frontend's reconcileLanguage
	// then clobbers any browser-detected locale on first refresh().
	locale := middleware.ResolveLocale(r.Header.Get("Accept-Language"))
	userLoginRes, err := v.Controller.GoogleLogin(oauthUser, attr, locale)
	if err != nil {
		if errors.Is(err, controllers.ErrEmailHasCredentialsAccount) {
			frontendURL := strings.TrimRight(v.Controller.SecretManager.GetSecret("FRONTEND_URL"), "/")
			if frontendURL == "" {
				frontendURL = "https://dashboard.wemob.io"
			}
			redirect := fmt.Sprintf("%s/login/handleGoogle?error=%s", frontendURL, constants.ErrAuthEmailHasCredentialsAccount)
			http.Redirect(w, r, redirect, http.StatusTemporaryRedirect)
			return
		}
		utils.JSONErrorCode(w, constants.ErrAuthOAuthFailed, http.StatusInternalServerError, nil)
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
	if userLoginRes.IsNewUser {
		url += "&new=1"
	}
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}
