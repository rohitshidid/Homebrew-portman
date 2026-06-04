package main

import (
	"strings"
	"testing"

	"github.com/rohitshidid/portmap/internal/portmap"
)

func TestParsePort(t *testing.T) {
	port, err := parsePort("5432")
	if err != nil {
		t.Fatal(err)
	}
	if port != 5432 {
		t.Fatalf("got %d, want 5432", port)
	}
}

func TestParsePortRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"0", "65536", "postgres"} {
		if _, err := parsePort(value); err == nil {
			t.Fatalf("parsePort(%q) succeeded, want error", value)
		}
	}
}

func TestFormatCheckResultFree(t *testing.T) {
	got := formatCheckResult(5432, nil)
	want := "5432 is free"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatCheckResultOccupied(t *testing.T) {
	got := formatCheckResult(5432, []portmap.Entry{
		{Port: 5432, App: "postgres", PID: "1234"},
	})
	want := "5432 is in use by postgres, pid 1234"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatCheckResultIncludesContainerAndAdditionalOwners(t *testing.T) {
	got := formatCheckResult(5432, []portmap.Entry{
		{Port: 5432, App: "postgres", PID: "1234", Container: "pg"},
		{Port: 5432, App: "docker", Container: "pg"},
	})

	for _, want := range []string{"5432 is in use by postgres", "pid 1234", "container pg", "(+1 more)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("got %q, want it to contain %q", got, want)
		}
	}
}
