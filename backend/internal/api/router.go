package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"drishti-amr-health/internal/agent"
	"drishti-amr-health/internal/api/handlers"
	"drishti-amr-health/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/cors"
)

// NativeHandlers holds AMR Health's original handlers (wifi, connections,
// bad-zones, plant proxy) that were in the single-file main.go. These are
// injected from main.go so the router can serve both native and ported routes.
type NativeHandlers struct {
	Health              http.HandlerFunc
	Connections         http.HandlerFunc
	WifiTest            http.HandlerFunc
	Discovery           http.HandlerFunc
	WifiDiscover        http.HandlerFunc
	ReportSearchSuggest http.HandlerFunc
	ReportEvents        http.HandlerFunc
	BadZonesExport      http.HandlerFunc
	BadZoneReports      http.HandlerFunc
	PlantProxy          http.HandlerFunc
}

func NewRouter(db *pgxpool.Pool, cfg *config.Config, native *NativeHandlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RequestSize(1 << 20))

	authH := handlers.NewAuthHandler(db, cfg.AdminUsername, cfg.AdminPassword, cfg.SessionSecret)
	serverH := handlers.NewServerHandler(db, cfg.EncryptionKey)
	logH := handlers.NewLogHandler(db, cfg.OllamaURL, cfg.OllamaModel, cfg.LLMAPIKey)
	syncH := handlers.NewSyncHandler(db, cfg.EncryptionKey)
	actionH := handlers.NewActionHandler(db, cfg.EncryptionKey, cfg.AllowCustomCommands)
	ragH := handlers.NewRAGHandler(db)
	remediationH := handlers.NewRemediationHandler(db)
	heatmapH := handlers.NewHeatmapHandler(db)

	// After each scheduled sync, scan for new remediation suggestions.
	syncH.OnSyncComplete(func(ctx context.Context) {
		if err := remediationH.GenerateSuggestions(ctx); err != nil {
			log.Printf("remediation: generate suggestions: %v", err)
		}
	})
	rwH := handlers.NewRobowatchHandler(db, cfg.EncryptionKey)
	amrH := handlers.NewAMRHandler(db, cfg.OllamaURL, cfg.OllamaModel, cfg.LLMAPIKey)

	// Agent investigation orchestrator + config snapshotter.
	orch := agent.NewOrchestrator(db, agent.Config{
		OllamaURL:        cfg.OllamaURL,
		OllamaModel:      cfg.OllamaModel,
		LLMAPIKey:        cfg.LLMAPIKey,
		EncryptionKey:    cfg.EncryptionKey,
		SnapshotInterval: time.Duration(cfg.AgentSnapshotMinutes) * time.Minute,
	})
	snap := agent.NewSnapshotter(orch)
	agentH := handlers.NewAgentHandler(orch, snap)

	r.Route("/api", func(r chi.Router) {
		// ---- AMR Health native routes (wifi, connections, bad-zones) ----
		if native != nil {
			if native.Health != nil {
				r.Get("/health", native.Health)
			}
			if native.Connections != nil {
				r.Get("/connections", native.Connections)
				r.Post("/connections", native.Connections)
			}
			if native.WifiTest != nil {
				r.Post("/wifi/test", native.WifiTest)
			}
			if native.Discovery != nil {
				r.Get("/discovery", native.Discovery)
			}
			if native.WifiDiscover != nil {
				r.Post("/wifi/discover", native.WifiDiscover)
			}
			if native.ReportSearchSuggest != nil {
				r.Get("/reports/search/suggest", native.ReportSearchSuggest)
			}
			if native.ReportEvents != nil {
				r.Get("/reports/events", native.ReportEvents)
			}
			if native.BadZonesExport != nil {
				r.Get("/reports/bad-zones/export", native.BadZonesExport)
			}
			if native.BadZoneReports != nil {
				r.Get("/reports/bad-zones/{zone}", native.BadZoneReports)
			}
			if native.PlantProxy != nil {
				r.Get("/plants/{plant}/rds/{endpoint}", native.PlantProxy)
			}
		}

		// ---- Ported SiteOps routes (logs, monitoring, AMR, agent) ----
		r.Post("/auth/login", authH.Login)
		r.Post("/auth/logout", authH.Logout)

		r.Get("/logs", logH.List)
		r.Post("/logs/agent-explain", logH.AgentExplain)
		r.Get("/stats", logH.Stats)
		r.Get("/timeline", logH.Timeline)
		r.Get("/server-stats", logH.ServerStats)
		r.Get("/rag/suggestions", ragH.Suggestions)
		r.Post("/rag/query", ragH.Query)
		r.Get("/rag/history", ragH.History)

		// Tier 1/2 remediation bridge
		r.Get("/remediation/suggestions", remediationH.List)
		r.Get("/remediation/stats", remediationH.GetStats)

		r.Get("/rds/logs", rwH.ListLogs)

		r.Get("/amr/fleet", amrH.FleetStatus)
		r.Get("/amr/timeline", amrH.Timeline)
		r.Get("/amr/robot", amrH.RobotSummary)
		r.Get("/amr/badzones", amrH.BadZones)
		r.Get("/amr/summarize", amrH.Summarize)

		r.Group(func(r chi.Router) {
			r.Use(authH.Middleware)
			r.Get("/auth/me", authH.Me)
			r.Post("/auth/change-password", authH.ChangePassword)
			r.Get("/servers", serverH.List)
			r.Get("/agent/jobs/{job_id}", agentH.Get)
			r.Get("/agent/robots", agentH.Robots)
			r.Get("/agent/snapshots", agentH.Snapshots)
			r.Get("/rds/plants", rwH.ListPlants)
			r.Get("/rds/status/{plant}", rwH.GetStatus)

			r.Post("/sync/all", syncH.SyncAll)
			r.Post("/rds/test/{plant}", rwH.TestConnection)
			r.Post("/rds/discover/{plant}", rwH.DiscoverSources)
			r.Post("/rds/fetch/{plant}", rwH.FetchLogs)
			r.With(authH.AdminOnly).Put("/rds/credentials/{plant}", rwH.SaveCredentials)
			r.Get("/incidents/summary", logH.IncidentSummary)
			r.Get("/sync-history", logH.SyncHistory)

			r.Group(func(r chi.Router) {
				r.Use(authH.AdminOnly)
				r.With(authH.PermissionOnly("users")).Get("/users", authH.ListUsers)
				r.With(authH.PermissionOnly("users")).Post("/users", authH.CreateUser)
				r.With(authH.PermissionOnly("users")).Put("/users/{id}", authH.UpdateUser)
				r.With(authH.PermissionOnly("heatmap")).Post("/wifi-heatmap/points", heatmapH.SavePoint)
				r.With(authH.PermissionOnly("heatmap")).Post("/wifi-heatmap/route-points", heatmapH.SaveRoutePoint)
				r.With(authH.PermissionOnly("heatmap")).Get("/wifi-heatmap/query", heatmapH.Query)
				r.With(authH.PermissionOnly("heatmap")).Post("/wifi-heatmap/sessions", heatmapH.StartSession)
				r.With(authH.PermissionOnly("heatmap")).Get("/wifi-heatmap/sessions", heatmapH.Sessions)
				r.With(authH.PermissionOnly("heatmap")).Post("/wifi-heatmap/sessions/{id}/stop", heatmapH.StopSession)
				r.Post("/amr/tcp-diagnostics", serverH.DiagnoseAMRTCP)

				r.Post("/servers", serverH.Create)
				r.Put("/servers/{id}", serverH.Update)
				r.Delete("/servers/{id}", serverH.Delete)

				r.Post("/servers/{id}/sync", syncH.SyncServer)
				r.Post("/servers/{id}/deep-sync", syncH.DeepSync)
				r.Post("/sync/test", syncH.TestConnection)

				r.Post("/actions/run", actionH.Run)
				r.Get("/actions/history", actionH.History)

				r.Post("/agent/jobs", agentH.Start)
			})
		})
	})

	corsH := cors.New(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})

	go snap.Run()

	return corsH.Handler(r)
}
