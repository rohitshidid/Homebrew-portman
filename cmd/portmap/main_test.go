package main

import (
	"bytes"
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

func TestMainUsageIncludesModesAndFlags(t *testing.T) {
	var buf bytes.Buffer
	writeMainUsage(&buf)
	output := buf.String()

	for _, want := range []string{
		"portmap [flags]",
		"portmap check [flags] <port>",
		"portmap --watch",
		"portmap --watch --interval 1s --port 8080",
		"--json",
		"--no-docker",
		"--port <port>",
		"--protocol <value>",
		"--timeout <duration>",
		"--version",
		"--watch",
		"--interval <duration>",
		"Check exit codes:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("main usage missing %q:\n%s", want, output)
		}
	}
}

func TestCheckUsageIncludesExitCodes(t *testing.T) {
	var buf bytes.Buffer
	writeCheckUsage(&buf)
	output := buf.String()

	for _, want := range []string{
		"portmap check [flags] <port>",
		"portmap check 5432",
		"--no-docker",
		"--protocol <value>",
		"--timeout <duration>",
		"0  port is free",
		"1  port is occupied",
		"2  bad input or scan error",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("check usage missing %q:\n%s", want, output)
		}
	}
}
