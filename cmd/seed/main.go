// Command seed truncates lesson-related tables and reseeds them from the
// curriculum markdown file. Source of truth is the markdown — this binary
// holds no lesson content of its own.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"marco-api/internal/config"
	"marco-api/internal/seeder"
)

func main() {
	curriculumFlag := flag.String("curriculum", "", "path to marco_curriculum_v2.md (overrides CURRICULUM_PATH env)")
	flag.Parse()

	cfg, err := config.LoadSeeder()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	path := *curriculumFlag
	if path == "" {
		path = cfg.CurriculumPath
	}
	if path == "" {
		log.Fatalf("curriculum path is required: pass -curriculum or set CURRICULUM_PATH")
	}

	lessons, err := seeder.ParseFile(path)
	if err != nil {
		log.Fatalf("parse curriculum: %v", err)
	}
	log.Printf("parsed %d lessons from %s", len(lessons), path)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	if err := seeder.Seed(ctx, pool, lessons); err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}
}
