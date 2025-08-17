package main

import (
	// "net/http"

	"context"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"

	_ "github.com/lib/pq"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/uptrace/bun/extra/bundebug"
)

func createSchema(db *bun.DB) error {
	models := []interface{}{
		(*models.User)(nil),
		(*models.Token)(nil),
		(*models.StandartUserLimits)(nil),
		(*models.StandartUserSubscription)(nil),
		(*models.Invoice)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.Release)(nil),
		(*models.Device)(nil),
		(*models.Notification)(nil),
		(*models.StorePageImage)(nil),
		(*models.AppUsageEvent)(nil),
		(*models.AppBuildTask)(nil),
		(*models.AppReleaseTask)(nil),
	}

	for _, model := range models {
		_, err := db.NewCreateTable().
			Model(model).
			IfNotExists().
			Exec(context.Background())
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {

	dsn := "postgres://postgres:asd123@localhost/wemob?sslmode=disable"
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	bundb := bun.NewDB(sqldb, pgdialect.New())

	bundb.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(true),
		bundebug.FromEnv("BUNDEBUG"),
	))

	createSchema(bundb)

	db := database.NewDatabase(bundb)
	controller := controllers.NewController(db)
	controller.CreateProject(requests.ProjectCreateRequest{Title: "TEST1"})

	httpViews := views.httpViews{
		Controller: *controller,
	}

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
			r.Route("/{projectId}", func(r chi.Router) {
				r.Get("/", httpViews.GetProject)
				r.Put("/", httpViews.UpdateProject)
				r.Delete("/", httpViews.DeleteProject)
				r.Route("/analytics", func(r chi.Router) {
					// TODO: Make more detailed analytics endpoints
					r.Get("/usage", httpViews.GetUsageAnalytics)
					r.Get("/notifications", httpViews.GetNotificationsAnalytics)
				})
				r.Route("/notifications", func(r chi.Router) {
					r.Get("/", httpViews.GetProjectNotifications)
					r.Post("/", httpViews.SendProjectNotification)
					r.Get("/{notificationId}", httpViews.GetProjectNotification)
				})
				r.Route("/builds", func(r chi.Router) {
					r.Get("/", httpViews.GetProjectBuilds)
					r.Post("/", httpViews.CreateProjectBuild)
					r.Route("{build}", func(r chi.Router) {
						r.Get("/", httpViews.GetProjectBuild)
						r.Post("/", httpViews.SetReleasedBuild)
					})
				})
			})
		})
		r.Route("/billing", func(r chi.Router) {
			r.Route("/subscription", func(r chi.Router) {
				r.Get("/", httpViews.GetSubscription)
				r.Put("/", httpViews.UpdateSubscription)
				r.Delete("/", httpViews.CancelSubscription)
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
