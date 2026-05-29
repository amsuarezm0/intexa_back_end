package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/intexa/arca-api/internal/db"
	"github.com/intexa/arca-api/internal/handler"
	"github.com/intexa/arca-api/internal/middleware"
	"github.com/intexa/arca-api/internal/repository"
	"github.com/intexa/arca-api/internal/repository/memory"
	"github.com/intexa/arca-api/internal/repository/postgres"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// ── Store ─────────────────────────────────────────────────────────────────
	var store repository.Store
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Println("DATABASE_URL not set — using in-memory store (dev mode)")
		log.Println("  login: admin@arca.local / admin")
		store = memory.New()
	} else {
		pool, err := db.Connect(dsn)
		if err != nil {
			log.Fatalf("DB connect: %v", err)
		}
		if err := db.RunMigrations(pool); err != nil {
			log.Fatalf("DB migrate: %v", err)
		}
		store = postgres.New(pool)
		log.Println("store: PostgreSQL")
	}

	// ── Handlers ──────────────────────────────────────────────────────────────
	auth := handler.NewAuthHandler(store)
	dashboard := handler.NewDashboardHandler(store)
	transactions := handler.NewTransactionsHandler(store)
	cashflow := handler.NewCashFlowHandler(store)
	projections := handler.NewProjectionsHandler(store)
	reports := handler.NewReportsHandler(store)
	users := handler.NewUsersHandler(store)
	settings := handler.NewSettingsHandler(store)
	siigoH := handler.NewSiigoHandler(store)
	domains := handler.NewDomainsHandler(store)
	exchangeRates := handler.NewExchangeRateHandler()
	notifications := handler.NewNotificationsHandler(store)

	// ── Siigo auto-connect + scheduler ───────────────────────────────────────
	if siigoUser := os.Getenv("SIIGO_USERNAME"); siigoUser != "" {
		if err := siigoH.AutoConnect(siigoUser, os.Getenv("SIIGO_ACCESS_KEY"), os.Getenv("SIIGO_PARTNER_ID")); err != nil {
			log.Printf("siigo auto-connect failed: %v", err)
		} else {
			log.Println("siigo: connected")
			siigoH.StartScheduler()
			log.Println("siigo: daily scheduler started (06:00, reconcile on 1st)")
		}
	}

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	allowedOrigins := []string{"http://localhost:5173", "http://localhost:3000"}
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		allowedOrigins = strings.Split(raw, ",")
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok","service":"intexa-arca-api"}`)
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Public
		r.Post("/auth/login", auth.Login)
		r.Post("/auth/microsoft", auth.MicrosoftLogin)

		// Protected
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth)

			r.Post("/auth/logout", auth.Logout)

			// Read-only — all authenticated roles
			r.Get("/dashboard", dashboard.GetSummary)
			r.Get("/dashboard/bank-balance", dashboard.GetBankBalance)
			r.Get("/transactions", transactions.List)
			r.Get("/transactions/summary", transactions.Summary)
			r.Get("/transactions/{id}", transactions.Get)
			r.Get("/cashflow", cashflow.GetSummary)
			r.Get("/projections", projections.GetSummary)
			r.Post("/projections/simulate", projections.Simulate)
			r.Get("/reports", reports.GetSummary)
			r.Get("/reports/export", reports.Export)
			r.Get("/notifications", notifications.GetNotifications)
			r.Get("/settings", settings.Get)
			r.Put("/settings", settings.Update)
			r.Get("/exchange-rates", exchangeRates.GetRates)
			r.Get("/categories", settings.GetCategories)
			r.Get("/siigo/status", siigoH.Status)

			// Write — ADMINISTRADOR + TESORERÍA
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole("ADMINISTRADOR", "TESORERÍA"))

				r.Put("/dashboard/bank-balance", dashboard.UpdateBankBalance)
				r.Post("/transactions", transactions.Create)
				r.Put("/transactions/{id}", transactions.Update)
				r.Delete("/transactions/{id}", transactions.Delete)
				r.Post("/projections", projections.Create)
				r.Post("/siigo/sync", siigoH.Sync)
			})

			// Admin only — ADMINISTRADOR
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole("ADMINISTRADOR"))

				r.Get("/users", users.List)
				r.Post("/users", users.Create)
				r.Put("/users/{id}", users.Update)
				r.Delete("/users/{id}", users.Delete)
				r.Get("/activity-logs", settings.GetActivityLogs)
				r.Post("/siigo/connect", siigoH.Connect)
				r.Get("/allowed-domains", domains.List)
				r.Post("/allowed-domains", domains.Add)
				r.Delete("/allowed-domains/{domain}", domains.Remove)
			})
		})
	})

	log.Printf("intexa-arca-api listening on :%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}
