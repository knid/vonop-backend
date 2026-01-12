package views

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/markbates/goth/gothic"
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

	html := fmt.Sprintf(`
	<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"/></head>
		<body>
		<script>
		(function () {
		var token = %q;

		try {
			if (window.opener && window.opener.postMessage) {
			window.opener.postMessage({ token: token }, "*");
			}
		} catch (e) {}

		try {
			if (window.flutter_inappwebview && window.flutter_inappwebview.callHandler) {
			window.flutter_inappwebview.callHandler("wemob_oauth", token);
			}
		} catch (e) {}

		try { window.close(); } catch (e) {}

		setTimeout(function () {
			document.body.innerHTML = "<p style='font-family: -apple-system; padding:16px'>Done. You can close this window.</p>";
		}, 300);
		})();
		</script>
	</body></html>`, userLoginRes.Token)

	w.Write([]byte(html))
}
