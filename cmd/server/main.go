package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"marco-api/internal/config"
	"marco-api/internal/database"
	"marco-api/internal/routes"

	"github.com/gofiber/fiber/v2"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := database.Connect(context.Background(), cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	app := fiber.New(fiber.Config{
		// Catch-all for errors that escape handlers. Router-level errors
		// (404, 405, body too large, …) keep their status and message; anything
		// else is logged server-side and returned as an opaque 500 so internal
		// details (SQL, file paths, dependency errors) never reach clients.
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			message := "internal server error"
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) && fiberErr.Code < fiber.StatusInternalServerError {
				code = fiberErr.Code
				message = fiberErr.Message
			} else {
				log.Printf("unhandled error %s %s: %v", c.Method(), c.Path(), err)
			}
			return c.Status(code).JSON(fiber.Map{"error": message})
		},
	})

	if err := routes.Register(app, cfg, db); err != nil {
		log.Fatalf("failed to register routes: %v", err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down server...")
	if err := app.Shutdown(); err != nil {
		log.Fatalf("server shutdown error: %v", err)
	}
}
