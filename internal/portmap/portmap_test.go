package portmap

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseAddressPort(t *testing.T) {
	tests := []struct {
		input   string
		address string
		port    int
	}{
		{"*:5432", "*", 5432},
		{"127.0.0.1:27017", "127.0.0.1", 27017},
		{"[::1]:8080", "::1", 8080},
		{":::80", "::", 80},
	}

	for _, tt := range tests {
		address, port, ok := parseAddressPort(tt.input)
		if !ok {
			t.Fatalf("parseAddressPort(%q) failed", tt.input)
		}
		if address != tt.address || port != tt.port {
			t.Fatalf("parseAddressPort(%q) = %q/%d, want %q/%d", tt.input, address, port, tt.address, tt.port)
		}
	}
}

func TestParseLsofFields(t *testing.T) {
	out := strings.Join([]string{
		"p123",
		"cpostgres",
		"Lalice",
		"f9",
		"tIPv4",
		"n127.0.0.1:5432",
		"f10",
		"tIPv6",
		"n[::1]:5432",
		"p456",
		"cChrome",
		"Lalice",
		"f11",
		"tIPv6",
		"n[::1]:64505->[::2]:443",
	}, "\n")

	listeners := parseLsofFields(out, "tcp")
	if len(listeners) != 2 {
		t.Fatalf("got %d listeners, want 2", len(listeners))
	}
	for _, item := range listeners {
		if item.App != "postgres" || item.PID != "123" || item.User != "alice" || item.Port != 5432 {
			t.Fatalf("unexpected listener: %+v", item)
		}
	}
}

func TestParseDockerPorts(t *testing.T) {
	out := "abc123\tpg\t0.0.0.0:5432->5432/tcp, :::5432->5432/tcp\n" +
		"def456\tredis\t127.0.0.1:6379->6379/tcp\n" +
		"ghi789\tinternal\t27017/tcp\n"

	ports := parseDockerPorts(out)
	if len(ports) != 3 {
		t.Fatalf("got %d docker ports, want 3", len(ports))
	}
	if ports[0].Container != "pg" || ports[0].HostPort != 5432 || ports[0].Protocol != "tcp" {
		t.Fatalf("unexpected first port: %+v", ports[0])
	}
	if ports[2].Container != "redis" || ports[2].HostIP != "127.0.0.1" || ports[2].HostPort != 6379 {
		t.Fatalf("unexpected third port: %+v", ports[2])
	}
}

func TestAssembleEntriesAnnotatesDockerAndService(t *testing.T) {
	listeners := []listener{
		{Port: 5432, Protocol: "tcp", Address: "127.0.0.1", App: "postgres", PID: "123"},
		{Port: 5432, Protocol: "tcp", Address: "[::1]", App: "postgres", PID: "123"},
	}
	ports := []dockerPort{
		{HostIP: "127.0.0.1", HostPort: 5432, ContainerPort: 5432, Protocol: "tcp", Container: "pg", ContainerID: "abc123"},
	}

	entries := assembleEntries(listeners, ports, defaultServices())
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	var annotated Entry
	for _, entry := range entries {
		if entry.Container == "pg" {
			annotated = entry
			break
		}
	}
	if annotated.Container != "pg" || annotated.ContainerID != "abc123" || annotated.Service != "postgres" {
		t.Fatalf("entry was not annotated: %+v", annotated)
	}
}

func TestScanWithFakeRunner(t *testing.T) {
	runner := fakeRunner{
		"lsof\x00-nP\x00-iTCP\x00-sTCP:LISTEN\x00-F\x00pcLtn":         "p123\ncpostgres\nLalice\nf9\ntIPv4\nn127.0.0.1:5432\n",
		"lsof\x00-nP\x00-iUDP\x00-F\x00pcLtn":                         "",
		"docker\x00ps\x00--format\x00{{.ID}}\t{{.Names}}\t{{.Ports}}": "abc123\tpg\t127.0.0.1:5432->5432/tcp\n",
	}

	entries, err := Scan(context.Background(), Options{
		IncludeDocker: true,
		Protocol:      "tcp",
		Runner:        runner,
		ServicesPath:  "/path/that/does/not/exist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Container != "pg" || entries[0].Service != "postgres" {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
}

var errCommandNotMapped = errors.New("command not mapped")

type fakeRunner map[string]string

func (runner fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := strings.Join(append([]string{name}, args...), "\x00")
	out, ok := runner[key]
	if !ok {
		return "", errCommandNotMapped
	}
	return out, nil
}

func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	err := WriteTable(&buf, []Entry{
		{Port: 5432, Protocol: "tcp", Addresses: []string{"127.0.0.1"}, App: "postgres", PID: "123", Service: "postgres"},
	})
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	for _, want := range []string{"PORT", "PROTO", "SERVICE", "5432", "postgres"} {
		if !strings.Contains(output, want) {
			t.Fatalf("table output missing %q:\n%s", want, output)
		}
	}
}
