// Command qa is the manual QA harness for Marco. It seeds fixture users,
// drives /api/v1/chat against a live local server, streams the response to
// stdout, and prompts the human reviewer to pass/fail each case. Results are
// appended to a markdown tracking file.
//
// This is a developer tool, not production code. It calls the real Anthropic
// API and costs real money — ~EUR 0.05 per full run of 20 cases.
package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"marco-api/internal/auth"
	"marco-api/internal/config"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mattn/go-isatty"
)

//go:embed fixtures.sql
var fixturesSQL string

type flags struct {
	baseURL  string
	filter   string
	noPrompt bool
	output   string
	wipe     bool
}

func parseFlags() flags {
	var f flags
	flag.StringVar(&f.baseURL, "base-url", "http://localhost:8080", "Base URL of the local marco-api server")
	flag.StringVar(&f.filter, "filter", "", "Comma-separated case IDs to run (e.g. A1,D1,D2). Empty = all.")
	flag.BoolVar(&f.noPrompt, "no-prompt", false, "Skip the cost warning and the interactive pass/fail prompts (does NOT bypass the wipe guard)")
	flag.StringVar(&f.output, "output", "docs/qa_results_v1.0.md", "Markdown file to append results to")
	flag.BoolVar(&f.wipe, "wipe", false, "Permit the fixture seed to TRUNCATE users CASCADE when the target database holds real accounts")
	flag.Parse()
	return f
}

func main() {
	if os.Getenv("MARCO_DEV_MODE") != "true" {
		fmt.Fprintln(os.Stderr, "refusing to run: MARCO_DEV_MODE must be 'true' (use ./scripts/run_qa.sh)")
		os.Exit(2)
	}

	f := parseFlags()
	ui := newUI(os.Stdout)
	stdin := bufio.NewReader(os.Stdin)

	cfg, err := config.Load()
	if err != nil {
		fatal(ui, "load config: %v", err)
	}

	selected, err := filterCases(Cases, f.filter)
	if err != nil {
		fatal(ui, "%v", err)
	}

	runnable := 0
	for _, c := range selected {
		if c.UserMessage != "" {
			runnable++
		}
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal(ui, "connect db: %v", err)
	}
	defer pool.Close()

	// fixtures.sql opens with TRUNCATE TABLE users CASCADE, which takes every
	// account on the target database with it — chat history, exam attempts,
	// match logs, preps, progress, refresh tokens. MARCO_DEV_MODE does not
	// protect against that: run_qa.sh sets it for you, and a dev database is
	// exactly where someone's own test account lives. So count what is about to
	// be destroyed and make the caller say so out loud.
	realUsers, err := countRealUsers(ctx, pool)
	if err != nil {
		fatal(ui, "count users: %v", err)
	}
	if realUsers > 0 && !f.wipe {
		fatal(ui, "refusing to run: %s holds %d account(s) that are not @qa.local.\n"+
			"    Seeding fixtures runs TRUNCATE TABLE users CASCADE — those accounts and every\n"+
			"    message, exam attempt, match log, prep and lesson progress row attached to them\n"+
			"    are deleted, with no backup. Take one first (make db-dump), then pass --wipe.",
			safeDBLabel(cfg.DatabaseURL), realUsers)
	}

	if !f.noPrompt {
		if realUsers > 0 {
			ui.warn("DESTRUCTIVE: this wipes %d real account(s) on %s and everything attached to them. It cannot be undone.",
				realUsers, safeDBLabel(cfg.DatabaseURL))
			ui.prompt("Type 'wipe' to confirm: ")
			ans, _ := stdin.ReadString('\n')
			if strings.TrimSpace(ans) != "wipe" {
				ui.plain("Aborted.\n")
				return
			}
		}
		ui.warn("This run will call Anthropic ~%d times, est. cost EUR %.2f", runnable, float64(runnable)*0.0025)
		ui.prompt("Continue? [y/N]: ")
		ans, _ := stdin.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(ans), "y") {
			ui.plain("Aborted.\n")
			return
		}
	}

	ui.plain("Loading fixtures... ")
	if _, err := pool.Exec(ctx, fixturesSQL); err != nil {
		fatal(ui, "seed fixtures: %v", err)
	}
	ui.plain("done.\n")
	ui.plain("%d cases queued.\n\n", len(selected))

	jwtSvc := auth.NewJWTService(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)

	out, err := os.OpenFile(f.output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fatal(ui, "open output: %v", err)
	}
	defer out.Close()

	httpClient := &http.Client{Timeout: 120 * time.Second}
	var passed, failed, skipped int

	for _, c := range selected {
		ui.header("--- %s · %s · %s ---", c.ID, c.Group, c.Title)
		ui.dim("Context user: %s", c.UserUUID)
		ui.dim("Notes: %s", c.Notes)

		if c.UserMessage == "" {
			ui.warn("Skipped: no inbound message for this case (proactive/server-initiated flow).")
			skipped++
			appendResult(out, c, "skip", "no inbound message — proactive flow")
			ui.plain("\n")
			continue
		}

		ui.dim("> %q", c.UserMessage)
		ui.plain("\n")
		ui.bold("Marco: ")

		uid, err := uuid.Parse(c.UserUUID)
		if err != nil {
			ui.errf("invalid uuid in case %s: %v", c.ID, err)
			failed++
			appendResult(out, c, "fail", "internal: invalid uuid")
			continue
		}
		token, err := jwtSvc.GenerateAccessToken(uid, "coach")
		if err != nil {
			ui.errf("mint token: %v", err)
			failed++
			appendResult(out, c, "fail", "internal: token mint")
			continue
		}

		streamErr := streamChat(ctx, httpClient, f.baseURL, token, c.UserMessage, ui)
		ui.plain("\n\n")

		if streamErr != nil {
			if errors.Is(streamErr, errServerUnreachable) {
				fatal(ui, "Is cmd/server running on %s? Try: make run", f.baseURL)
			}
			ui.errf("stream error: %v", streamErr)
			failed++
			appendResult(out, c, "fail", "stream error: "+streamErr.Error())
			continue
		}

		if f.noPrompt {
			appendResult(out, c, "logged", "no-prompt mode")
			continue
		}

		ui.prompt("Pass / Fail / Skip / Quit [p/f/s/q]: ")
		ans, _ := stdin.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "p", "pass":
			ui.pass("Logged.")
			passed++
			appendResult(out, c, "pass", "")
		case "f", "fail":
			ui.prompt("Failure note: ")
			note, _ := stdin.ReadString('\n')
			ui.fail("Logged.")
			failed++
			appendResult(out, c, "fail", strings.TrimSpace(note))
		case "s", "skip":
			ui.plain("Skipped.\n")
			skipped++
			appendResult(out, c, "skip", "")
		case "q", "quit":
			ui.plain("Quitting.\n")
			summarise(ui, passed, failed, skipped)
			return
		default:
			ui.plain("Unrecognised, treating as skip.\n")
			skipped++
			appendResult(out, c, "skip", "ambiguous input")
		}
		ui.plain("\n")
	}

	summarise(ui, passed, failed, skipped)
}

// errServerUnreachable signals that the local server is down — main lifts
// this into a friendly hint instead of a stack-trace-style message.
var errServerUnreachable = errors.New("server unreachable")

// streamChat POSTs to /api/v1/chat and prints each SSE data: chunk inline as
// it arrives. Returns nil when the server emits "event: done", or an error if
// the stream errors or closes early.
func streamChat(ctx context.Context, client *http.Client, baseURL, token, message string, ui *ui) error {
	body, _ := json.Marshal(map[string]string{"message": message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			return errServerUnreachable
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	scanner := bufio.NewScanner(resp.Body)
	// Some chunks can be larger than the default 64 KiB. Bump to 1 MiB; SSE
	// frames stay well under that even for the longest training plan.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var sawDone bool
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			if strings.TrimSpace(strings.TrimPrefix(line, "event:")) == "done" {
				sawDone = true
			}
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "{}" {
			continue
		}
		var c struct {
			Text  string `json:"text"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			continue
		}
		if c.Error != "" {
			return fmt.Errorf("server: %s", c.Error)
		}
		ui.streamChunk(c.Text)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	if !sawDone {
		return errors.New("stream closed without done event")
	}
	return nil
}

func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "no such host") ||
		strings.Contains(err.Error(), "dial tcp")
}

// filterCases keeps the original ordering but limits to the IDs in the
// filter spec. An empty filter returns the full list.
func filterCases(all []TestCase, filter string) ([]TestCase, error) {
	if strings.TrimSpace(filter) == "" {
		return all, nil
	}
	wanted := map[string]bool{}
	for _, id := range strings.Split(filter, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			wanted[id] = true
		}
	}
	var out []TestCase
	for _, c := range all {
		if wanted[c.ID] {
			out = append(out, c)
			delete(wanted, c.ID)
		}
	}
	if len(wanted) > 0 {
		unknown := make([]string, 0, len(wanted))
		for id := range wanted {
			unknown = append(unknown, id)
		}
		return nil, fmt.Errorf("unknown case ids in --filter: %s", strings.Join(unknown, ","))
	}
	return out, nil
}

// appendResult writes one row to the markdown tracking table.
func appendResult(w io.Writer, c TestCase, result, note string) {
	row := fmt.Sprintf("| %s | %s | %s | %s |\n",
		time.Now().UTC().Format(time.RFC3339),
		c.ID,
		result,
		sanitise(note),
	)
	_, _ = io.WriteString(w, row)
}

// sanitise keeps notes single-line and pipe-safe so the markdown table stays
// intact even when the reviewer types newlines or pipes.
func sanitise(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return "—"
	}
	return s
}

func summarise(ui *ui, passed, failed, skipped int) {
	ui.plain("\n")
	ui.bold("Summary: %d passed, %d failed, %d skipped", passed, failed, skipped)
	ui.plain("\n")
}

// countRealUsers counts accounts that are NOT harness fixtures. The fixture
// users all live on @qa.local, so anything else is somebody's actual account.
func countRealUsers(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var n int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE email NOT LIKE '%@qa.local'`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("query users: %w", err)
	}
	return n, nil
}

// safeDBLabel renders "host/dbname" from a connection string so the warning can
// name the target without printing its password.
func safeDBLabel(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return "the configured database"
	}
	return u.Host + strings.TrimSuffix(u.Path, "/")
}

func fatal(ui *ui, format string, args ...any) {
	ui.errf(format, args...)
	os.Exit(1)
}

// -----------------------------------------------------------------------------
// Terminal output. ANSI colors when stdout is a TTY, plain text otherwise.
// -----------------------------------------------------------------------------

type ui struct {
	out     io.Writer
	colored bool
}

func newUI(w io.Writer) *ui {
	colored := false
	if f, ok := w.(*os.File); ok {
		colored = isatty.IsTerminal(f.Fd())
	}
	return &ui{out: w, colored: colored}
}

const (
	ansiReset    = "\x1b[0m"
	ansiBold     = "\x1b[1m"
	ansiDim      = "\x1b[2m"
	ansiBoldCyan = "\x1b[1;36m"
	ansiGreen    = "\x1b[32m"
	ansiRed      = "\x1b[31m"
	ansiYellow   = "\x1b[33m"
)

func (u *ui) wrap(code, s string) string {
	if !u.colored {
		return s
	}
	return code + s + ansiReset
}

func (u *ui) plain(format string, args ...any) { fmt.Fprintf(u.out, format, args...) }
func (u *ui) header(format string, args ...any) {
	fmt.Fprintln(u.out, u.wrap(ansiBoldCyan, fmt.Sprintf(format, args...)))
}
func (u *ui) bold(format string, args ...any) {
	fmt.Fprint(u.out, u.wrap(ansiBold, fmt.Sprintf(format, args...)))
}
func (u *ui) dim(format string, args ...any) {
	fmt.Fprintln(u.out, u.wrap(ansiDim, fmt.Sprintf(format, args...)))
}
func (u *ui) pass(format string, args ...any) {
	fmt.Fprintln(u.out, u.wrap(ansiGreen, fmt.Sprintf(format, args...)))
}
func (u *ui) fail(format string, args ...any) {
	fmt.Fprintln(u.out, u.wrap(ansiRed, fmt.Sprintf(format, args...)))
}
func (u *ui) warn(format string, args ...any) {
	fmt.Fprintln(u.out, u.wrap(ansiYellow, fmt.Sprintf(format, args...)))
}
func (u *ui) errf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, u.wrap(ansiRed, fmt.Sprintf(format, args...)))
}
func (u *ui) prompt(s string) { fmt.Fprint(u.out, u.wrap(ansiBold, s)) }

// streamChunk writes a streamed text fragment with no trailing newline so the
// response builds up inline like a real conversation.
func (u *ui) streamChunk(s string) { fmt.Fprint(u.out, s) }
