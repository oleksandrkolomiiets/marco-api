package routes

import (
	"marco-api/internal/achievements"
	"marco-api/internal/anthropic"
	"marco-api/internal/auth"
	"marco-api/internal/chat"
	"marco-api/internal/config"
	"marco-api/internal/exam"
	"marco-api/internal/health"
	"marco-api/internal/lessons"
	"marco-api/internal/logs"
	"marco-api/internal/marco"
	"marco-api/internal/match_preparation"
	"marco-api/internal/middleware"
	"marco-api/internal/users"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Register(app *fiber.App, cfg *config.Config, db *pgxpool.Pool) error {
	app.Use(middleware.Recovery())
	app.Use(middleware.Logger())
	app.Use(middleware.CORS())

	userStore := users.NewUserStore(db)
	authStore := auth.NewAuthStore(db)
	jwtSvc := auth.NewJWTService(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	authMW := middleware.NewAuthMiddleware(jwtSvc)

	// Unauthenticated routes
	app.Get("/health", health.NewHandler(db).Check)

	authHandler := auth.NewHandler(userStore, authStore, jwtSvc, cfg)
	authLimiter := middleware.AuthRateLimit()
	app.Post("/auth/google", authLimiter, authHandler.GoogleSignIn)
	app.Post("/auth/signup", authLimiter, authHandler.EmailSignUp)
	app.Post("/auth/signin", authLimiter, authHandler.EmailSignIn)
	app.Post("/auth/refresh", authLimiter, authHandler.Refresh)
	app.Post("/auth/signout", authMW, authHandler.SignOut)

	// Authenticated API routes
	v1 := app.Group("/api/v1", authMW)

	userHandler := users.NewHandler(userStore)
	v1.Get("/me", userHandler.GetMe)
	v1.Patch("/me", userHandler.UpdateMe)

	lessonStore := lessons.NewLessonStore(db)
	lessonHandler := lessons.NewHandler(userStore, lessonStore)
	v1.Get("/lessons", lessonHandler.ListLessons)
	v1.Get("/lessons/:slug", lessonHandler.GetLesson)
	v1.Patch("/lessons/:slug/progress", lessonHandler.UpdateProgress)

	anthClient := anthropic.NewClient(cfg.AnthropicAPIKey)
	assembler := marco.NewAssembler(db)
	chatStore := chat.NewStore(db)
	chatHandler := chat.NewHandler(assembler, anthClient, chatStore)
	matchStore := logs.NewMatchStore(db)
	v1.Get("/chat/messages", chatHandler.Get)
	v1.Post("/chat", chatHandler.Post)
	v1.Patch("/chat/:id/feedback", chatHandler.PatchFeedback)
	v1.Delete("/chat/:id", chatHandler.Delete)

	logsHandler := logs.NewHandler(matchStore)
	v1.Get("/logs/match", logsHandler.ListMatches)
	v1.Post("/logs/match", logsHandler.CreateMatch)
	v1.Patch("/logs/match/:id", logsHandler.UpdateMatch)
	v1.Get("/logs/match/partners", logsHandler.ListPartners)

	preparationStore := match_preparation.NewStore(db)
	preparationHandler := match_preparation.NewHandler(preparationStore, anthClient, assembler)
	v1.Get("/match-preparation", preparationHandler.List)
	v1.Post("/match-preparation", preparationHandler.Create)
	v1.Get("/match-preparation/:id", preparationHandler.Get)
	v1.Patch("/match-preparation/:id", preparationHandler.Update)
	v1.Delete("/match-preparation/:id", preparationHandler.Delete)
	v1.Put("/match-preparation/:id/drills", preparationHandler.ReplaceDrills)
	v1.Patch("/match-preparation/:id/drills/:drillId", preparationHandler.ToggleDrill)
	v1.Post("/match-preparation/:id/suggest-drills", preparationHandler.SuggestDrills)

	examStore := exam.NewStore(db)
	examHandler := exam.NewHandler(examStore)
	v1.Get("/exam/questions", examHandler.ListQuestions)
	v1.Post("/exam/attempts", examHandler.SubmitAttempt)
	v1.Get("/exam/attempts/latest", examHandler.GetLatestAttempt)

	achievementsStore := achievements.NewStore(db)
	achievementsHandler := achievements.NewHandler(achievementsStore)
	v1.Get("/achievements", achievementsHandler.List)

	return nil
}
