# marco-api

Go REST API backend for Marco, an AI padel trainer. The React Native client lives in the sibling repo `../marco-app`.

## Stack

- **Go 1.25** with **Fiber v2** (HTTP framework)
- **pgx/v5** — PostgreSQL driver and connection pool (`pgxpool.Pool`)
- **golang-migrate** — SQL migration management (CLI only, not imported)
- **anthropic-sdk-go** — Claude streaming for the Marco coach chat
- **golang-jwt/v5** — HS256 access tokens; refresh tokens are random 32-byte values stored SHA-256-hashed and rotated on every refresh
- **testify** — assertions and test suites
- **godotenv** — `.env` loading at startup

## Folder conventions

```
internal/{domain}/
  handler.go   — Fiber route handlers, request parsing, response serialisation
  model.go     — Go structs that represent domain entities
  store.go     — All DB queries; accepts context.Context + pgxpool.Pool
```

New domain = new folder. No shared `models/` or `utils/` dumping grounds.

Cross-cutting packages (not domains):

- `internal/routes` — single place where every route, middleware, and handler is wired (`routes.Register`).
- `internal/middleware` — auth (JWT), CORS, logger, panic recovery, auth rate limiter.
- `internal/marco` — Marco's persona: system prompt (`prompt.md`), user-context assembly from the DB, and parsers for inline tokens the model emits (`[LESSON_REF: …]`, `[MATCH_LOG: …]`, `[MATCH_PREP: …]`).
- `internal/anthropic` — thin streaming client over the Anthropic SDK, plus `MockClient` for tests.
- `internal/config` — the only place `os.Getenv` is allowed.
- `internal/seeder` — curriculum seeding shared by `cmd/seed`.

## API surface

All `/api/v1/*` routes require `Authorization: Bearer <access_token>`. Auth endpoints are rate-limited (10/min per IP). The full wiring lives in `internal/routes/routes.go` — keep this table in sync when adding routes.

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | liveness + DB ping |
| POST | `/auth/google` | Google ID-token sign-in |
| POST | `/auth/signup` `/auth/signin` | email+password auth |
| POST | `/auth/refresh` | rotate refresh token, new access token |
| POST | `/auth/signout` | revoke all refresh tokens |
| GET/PATCH | `/api/v1/me` | profile read/update |
| GET | `/api/v1/lessons`, `/api/v1/lessons/:slug` | curriculum |
| PATCH | `/api/v1/lessons/:slug/progress` | viewed/learned/mastered |
| GET | `/api/v1/chat/messages` | paginated history (`limit`, `before`) |
| POST | `/api/v1/chat` | send message, response streams as SSE (`data:` chunks, then `match_log`/`match_prep`/`done` events) |
| PATCH/DELETE | `/api/v1/chat/:id/feedback`, `/api/v1/chat/:id` | thumbs feedback / soft delete |
| GET/POST/PATCH | `/api/v1/logs/match[/:id]` | match logs |
| GET | `/api/v1/logs/match/partners` | partner-name suggestions |
| CRUD | `/api/v1/match-preparation[/:id]` + `/drills`, `/drills/:drillId`, `/suggest-drills` | match prep plans |
| GET/POST | `/api/v1/exam/questions`, `/api/v1/exam/attempts`, `/api/v1/exam/attempts/latest` | skill exam |
| GET | `/api/v1/achievements` | achievements |

## Error handling

- Never `panic` inside a handler.
- Always return JSON with the matching HTTP status:
  ```go
  return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "message"})
  ```
- Error shape is always `{"error": "string"}` — no nested objects, no codes.
- Wrap internal errors with `fmt.Errorf("context: %w", err)` before returning 500.

## Database rules

- Always pass `context.Context` to every query — use `c.Context()` in handlers.
- Always use `pgxpool.Pool` — never `sql.DB`, never a bare `pgx.Conn`.
- Query in `store.go`, not inline in handlers.
- Scan into typed structs; avoid `pgx.Row.Scan` into `interface{}`.

## Migrations

- Never modify an existing migration file after it has been committed.
- Always create a new migration: `migrate create -ext sql -dir migrations -seq <name>`
- Keep `up` and `down` migrations in sync.
- Migration filenames are sequential integers: `000001_`, `000002_`, …
- Never have two migration files share the same numeric prefix. `golang-migrate` picks one alphabetically and silently skips the other, which leaves the schema partially applied. If you find duplicates (e.g. `000001_init_schema.up.sql` and `000001_initial_schema.up.sql`), consolidate them into a single file before running `migrate-up`.

## Tests

- Use `testify/assert` for assertions, `testify/require` when failure should stop the test.
- Write table-driven tests (`[]struct{ name, input, want }`).
- Handlers depend on small store interfaces (e.g. `chat.TurnSaver`); tests stub those interfaces by hand — see `internal/chat/handler_test.go` for the pattern. No DB-mocking library is used.
- Use `anthropic.MockClient` to script LLM stream responses in tests.
- Test files live next to the code they test (`handler_test.go`, `store_test.go`).
- Store tests (`store_test.go`) are integration tests against a real Postgres: they skip unless `TEST_DATABASE_URL` is set, and they `TRUNCATE` tables — never point that variable at `marco_dev`. Run them with `make test-integration`, which creates and migrates a dedicated `marco_test` database inside the `marco_db` container and runs the whole suite.
- The marco assembler has a golden-file test (`internal/marco/testdata/golden_context.json`). The dynamic `today` field is pinned to `2026-01-01` in the test; when the context shape changes intentionally, delete the golden file and re-run to regenerate, then review the diff.

## Naming

| Context | Convention | Example |
|---|---|---|
| DB columns | snake_case | `created_at`, `user_id` |
| Go struct fields | camelCase (unexported) / PascalCase (exported) | `UserID`, `createdAt` |
| Exported types & funcs | PascalCase | `type Handler struct`, `func NewHandler` |
| Packages | lowercase, single word | `package health` |

## What NOT to do

- **No ORM** — write raw SQL in `store.go`.
- **No global variables** — pass `*Config` and `*pgxpool.Pool` explicitly.
- **No `init()` functions** — initialise everything in `main.go`.
- **No `log.Fatal` outside `main.go`** — return errors up the call stack.
- **No `os.Getenv` outside `internal/config`** — read config once, pass it down.

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `PORT` | No | `8080` | TCP port the HTTP server binds to |
| `DATABASE_URL` | Yes | — | Full pgx connection string |
| `GOOGLE_CLIENT_ID` | Yes | — | iOS OAuth client ID; audience for Google ID-token validation |
| `JWT_SECRET` | Yes | — | HS256 signing key, minimum 32 characters |
| `JWT_ACCESS_TTL` | No | `15m` | Access-token lifetime (Go duration syntax) |
| `JWT_REFRESH_TTL` | No | `720h` | Refresh-token lifetime |
| `ANTHROPIC_API_KEY` | Yes | — | Claude API key for Marco chat |

The seeder (`cmd/seed`) only needs `DATABASE_URL` (plus optional `CURRICULUM_PATH`).

Example (copy `.env.example` → `.env`):
```
PORT=3143
DATABASE_URL=postgres://marco:marco@localhost:5432/marco_dev?sslmode=disable
GOOGLE_CLIENT_ID=your_ios_oauth_client_id
JWT_SECRET=a_random_32_char_string_minimum
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=720h
ANTHROPIC_API_KEY=sk-ant-…
```

## Common commands

```bash
make run          # start dev server
make build        # compile to bin/marco-api
make test         # run all tests with -race (DB-backed store tests skip)
make test-integration  # full suite incl. store tests against marco_test (needs db-up)
make db-up        # start postgres container
make db-down      # stop and remove containers
make migrate-up   # apply all pending migrations
make migrate-down # roll back one migration
make seed         # seed the curriculum (cmd/seed)
make qa           # run the Marco QA harness against the local server
make qa-group GROUP=D  # run a single QA group
```

## Database access (psql, ad-hoc queries)

Postgres runs in the `marco_db` container (see `docker-compose.yml`). Always run `psql` and any other database CLI tooling **through `docker exec`**, not against `localhost:5432` directly. This keeps commands working for anyone whose host machine doesn't have a `psql` client installed, and matches the rest of the dev workflow (`make db-up`, `make db-down`).

```bash
# One-shot query
docker exec -it marco_db psql -U marco -d marco_dev -c "\d+ messages"

# Interactive shell
docker exec -it marco_db psql -U marco -d marco_dev
```

Do not suggest `psql "$DATABASE_URL" …` or `psql -h localhost …` forms when generating commands for the user — always wrap with `docker exec -it marco_db …`.
