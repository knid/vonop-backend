package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {

	httpViews := views.httpViews{}

	r := chi.NewRouter()

	r.Use(middleware.Logger)

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/users", func(r chi.Router) {
			r.Get("/me", httpViews.GetUser)
			r.Put("/me", httpViews.UpdateUser)
			r.Delete("/me", httpViews.DeleteUser)

			r.Post("/login", httpViews.UserLogin)
			r.Post("/register", httpViews.UserRegister)
			r.Post("/forgot-password", httpViews.UserForgotPassword)
			r.Post("/reset-password", httpViews.UserResetPassword)
			r.Post("verify-email", httpViews.UserVerifyEmail)
			r.Post("resend-verifaction", httpViews.UserResendVerifactionEmail)
		})
		r.Route("/projects", func(r chi.Router) {
			r.Get("/", httpViews.GetProjects)
			r.Post("/", httpViews.CreateProject)
			r.Get("/analytics/usage", httpViews.GetUsageAnalytics)
			r.Get("/analytics/notifications", httpViews.GetNotificationsAnalytics)
			r.Route("/{projectId}", func(r chi.Router) {
				r.Get("/", httpViews.GetProject)
				r.Put("/", httpViews.UpdateProject)
				r.Delete("/", httpViews.DeleteProject)
				r.Route("/notifications", func(r chi.Router) {
					r.Get("/", httpViews.GetProjectNotifications)
					r.Post("/", httpViews.SendProjectNotification)
					r.Get("/{notificationId}", httpViews.GetProjectNotification)
				})
				r.Route("/releases", func(r chi.Router) {
					r.Get("/", httpViews.GetProjectReleases)
					r.Post("/", httpViews.CreateProjectRelease)
					r.Route("{releaseId}", func(r chi.Router) {
						r.Get("/", httpViews.GetProjectRelease)
						r.Post("/", httpViews.RollbackProjectRelease)
					})
				})
			})
		})
		r.Route("/billing", func(r chi.Router) {
			r.Route("/subscription", func(r chi.Router) {
				r.Get("/", httpViews.GetSubscription)
				r.Put("/", httpViews.UpdateSubscription)
			    r.Delete("/", httpViews.DeleteSubscription)
			})
			r.Route("/invoices", func(r chi.Router) {
    			r.Get("/", httpViews.GetInvoices)
	    		r.Get("/{invoiceId}", httpViews.GetInvoice)
			})
		})

		r.Get("/health", httpViews.Health)
	})

	http.ListenAndServe(":8001", r)
}
