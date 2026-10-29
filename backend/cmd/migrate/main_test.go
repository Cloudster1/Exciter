package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// closedPrimary is an address nothing listens on, so every attempt is refused at once.
const closedPrimary = "postgres://exciter:secret@127.0.0.1:1/exciter?sslmode=disable&connect_timeout=1"

// quickPause keeps the attempts apart without the test waiting out connectPause thirty times.
const quickPause = time.Millisecond

func TestEveryCommandIsRead(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		command string
		version int64
	}{
		{nil, "up", 0},
		{[]string{"up"}, "up", 0},
		{[]string{"up-to", "42"}, "up-to", 42},
		{[]string{"down"}, "down", 0},
		{[]string{"status"}, "status", 0},
		{[]string{"version"}, "version", 0},
	} {
		got, err := parseArgs(tc.args)
		if err != nil || got.command != tc.command || got.version != tc.version {
			t.Errorf("parseArgs(%q) = %+v, %v; want %s at %d", tc.args, got, err, tc.command, tc.version)
		}
	}
}

func TestAnUnknownCommandNamesTheFive(t *testing.T) {
	_, err := parseArgs([]string{"frobnicate"})
	want := `"frobnicate" is not a migrate command; use up, up-to, down, status or version`
	if err == nil || err.Error() != want {
		t.Fatalf("parseArgs(frobnicate) = %v, want %q", err, want)
	}
	for _, c := range commands {
		if !strings.Contains(err.Error(), c) {
			t.Errorf("the refusal leaves out %s", c)
		}
	}
	if len(commands) != 5 {
		t.Errorf("migrate knows %d commands, want the five the refusal names", len(commands))
	}
}

func TestUpToNeedsANumber(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"up-to"}, "up-to needs a version, such as: migrate up-to 1"},
		{[]string{"up-to", "x"}, `"x" is not a migration version; give its number, such as 1`},
	} {
		if _, err := parseArgs(tc.args); err == nil || err.Error() != tc.want {
			t.Errorf("parseArgs(%q) = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestWaitingForThePrimaryGivesUpAndSaysHowOften(t *testing.T) {
	pool, err := waitForPrimary(context.Background(), closedPrimary, quickPause)
	if pool != nil {
		pool.Close()
		t.Fatal("waitForPrimary returned a pool for an address nothing listens on")
	}
	want := fmt.Sprintf("the database did not answer after %d attempts", connectAttempts)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("waitForPrimary = %v, want it to say %q", err, want)
	}
}

// A stop from outside ends the wait at once rather than after the remaining attempts.
func TestWaitingForThePrimaryStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitForPrimary(ctx, closedPrimary, time.Hour); err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("waitForPrimary on a stopped context = %v, want it cancelled", err)
	}
}
