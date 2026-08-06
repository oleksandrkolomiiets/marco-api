-include .env
export

.PHONY: run build test test-integration migrate-up migrate-down db-up db-down db-logs db-dump db-restore seed qa qa-group

APP_NAME := marco-api
MIGRATE  := migrate -path ./migrations -database "$(DATABASE_URL)"
DB_USER  := $(or $(shell echo "$(DATABASE_URL)" | sed -E 's|.*://([^:@]+).*|\1|'),marco)

run:
	go run ./cmd/server

build:
	go build -o bin/$(APP_NAME) ./cmd/server

# TEST_DATABASE_URL is cleared deliberately. The bare `export` at the top of
# this file puts every variable into each recipe's environment, so the default
# below reached this target too and the DB-backed tests ran after all — against
# whatever state marco_test happened to be in, and without the -p 1 that
# test-integration uses, so the packages TRUNCATE each other's fixtures and
# fail at random. Plain `make test` skips them, as advertised.
test:
	TEST_DATABASE_URL= go test -v -race ./...

TEST_DATABASE_URL ?= postgres://marco:marco@localhost:5432/marco_test?sslmode=disable

# Runs the full suite including the DB-backed store/assembler tests, which are
# skipped by plain `make test`. Creates and migrates the marco_test database
# inside the running marco_db container (start it with `make db-up` first).
test-integration: .wait-db
	@docker compose exec -T postgres psql -U $(DB_USER) -d marco_dev -tAc "SELECT 1 FROM pg_database WHERE datname='marco_test'" | grep -q 1 || \
		docker compose exec -T postgres createdb -U $(DB_USER) marco_test
	migrate -path ./migrations -database "$(TEST_DATABASE_URL)" up
	# -p 1 serializes packages: the DB-backed tests all TRUNCATE the same
	# marco_test database and would wipe each other's fixtures if run in parallel.
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -p 1 ./...

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-logs:
	docker compose logs -f postgres

# Snapshot marco_dev to ./backups. Cheap insurance: the QA harness truncates
# users CASCADE, and there is no other way back from that.
db-dump: .wait-db ## Dump marco_dev to backups/marco_dev_<utc>.sql
	@mkdir -p backups
	@f=backups/marco_dev_$$(date -u +%Y%m%dT%H%M%SZ).sql; \
		docker compose exec -T postgres pg_dump -U $(DB_USER) -d marco_dev > $$f && \
		echo "wrote $$f ($$(wc -c < $$f | tr -d ' ') bytes)"

db-restore: .wait-db ## Restore a dump: make db-restore FILE=backups/marco_dev_....sql
	@test -n "$(FILE)" || { echo "usage: make db-restore FILE=backups/marco_dev_....sql"; exit 2; }
	@docker compose exec -T postgres psql -U $(DB_USER) -d marco_dev < $(FILE)
	@echo "restored $(FILE)"

.wait-db:
	@echo "Waiting for postgres to be ready..."
	@until docker compose exec -T postgres pg_isready -U $(DB_USER) -q 2>/dev/null; do \
		sleep 1; \
	done
	@echo "Postgres is ready."

migrate-up: .wait-db
	$(MIGRATE) up

migrate-down:
	$(MIGRATE) down 1

CURRICULUM_PATH ?= /Users/olekchannext/Downloads/marco_curriculum_v2.md

seed:
	@echo "Seeding lessons from $(CURRICULUM_PATH)..."
	@go run ./cmd/seed -curriculum "$(CURRICULUM_PATH)"
	@echo "Seeding exam..."
	@docker exec -i marco_db psql -U marco -d marco_dev -f - < migrations/seed_exam.sql
	@echo "Done."

qa: ## Run the Marco QA harness against the local server
	@./scripts/run_qa.sh

qa-group: ## Run a single group, e.g. make qa-group GROUP=D — runs every D case
	@./scripts/run_qa.sh --group $(GROUP)
