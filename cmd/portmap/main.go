package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/rohitshidid/portmap/internal/portmap"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "check" {
		return runCheck(args[1:])
	}

	fs := flag.NewFlagSet("portmap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	jsonOutput := fs.Bool("json", false, "print JSON instead of a table")
	noDocker := fs.Bool("no-docker", false, "skip Docker container port annotations")
	protocol := fs.String("protocol", "all", "protocol to show: all, tcp, or udp")
	showVersion := fs.Bool("version", false, "print version and exit")
	timeout := fs.Duration("timeout", 5*time.Second, "maximum scan duration")
	watch := fs.Bool("watch", false, "refresh the table until interrupted")
	interval := fs.Duration("interval", 2*time.Second, "refresh interval for --watch")
	portFilter := 0

	fs.Func("port", "show only one listening port", func(value string) error {
		port, err := parsePort(value)
		if err != nil {
			return err
		}
		portFilter = port
		return nil
	})

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *showVersion {
		fmt.Fprintf(os.Stdout, "portmap %s\n", version)
		return 0
	}

	opts := portmap.Options{
		IncludeDocker: !*noDocker,
		Protocol:      *protocol,
		Port:          portFilter,
	}

	if *watch {
		if *jsonOutput {
			fmt.Fprintln(os.Stderr, "portmap: --watch does not support --json")
			return 2
		}
		return runWatch(opts, *timeout, *interval)
	}

	entries, err := scanWithTimeout(*timeout, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "portmap: %v\n", err)
		return 1
	}

	if *jsonOutput {
		if err := portmap.WriteJSON(os.Stdout, entries); err != nil {
			fmt.Fprintf(os.Stderr, "portmap: %v\n", err)
			return 1
		}
		return 0
	}

	if err := portmap.WriteTable(os.Stdout, entries); err != nil {
		fmt.Fprintf(os.Stderr, "portmap: %v\n", err)
		return 1
	}
	return 0
}

func runCheck(args []string) int {
	fs := flag.NewFlagSet("portmap check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	noDocker := fs.Bool("no-docker", false, "skip Docker container annotations")
	protocol := fs.String("protocol", "all", "protocol to check: all, tcp, or udp")
	timeout := fs.Duration("timeout", 5*time.Second, "maximum scan duration")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: portmap check [--no-docker] [--protocol all|tcp|udp] [--timeout 5s] <port>")
		return 2
	}

	port, err := parsePort(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "portmap check: %v\n", err)
		return 2
	}

	entries, err := scanWithTimeout(*timeout, portmap.Options{
		IncludeDocker: !*noDocker,
		Protocol:      *protocol,
		Port:          port,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "portmap check: %v\n", err)
		return 2
	}

	fmt.Fprintln(os.Stdout, formatCheckResult(port, entries))
	if len(entries) > 0 {
		return 1
	}
	return 0
}

func runWatch(opts portmap.Options, scanTimeout time.Duration, interval time.Duration) int {
	if interval <= 0 {
		fmt.Fprintln(os.Stderr, "portmap: --interval must be greater than 0")
		return 2
	}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		entries, err := scanWithTimeout(scanTimeout, opts)
		writeWatchFrame(entries, err, interval)

		select {
		case <-interrupts:
			return 0
		case <-ticker.C:
		}
	}
}

func scanWithTimeout(timeout time.Duration, opts portmap.Options) ([]portmap.Entry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return portmap.Scan(ctx, opts)
}

func writeWatchFrame(entries []portmap.Entry, scanErr error, interval time.Duration) {
	fmt.Fprint(os.Stdout, "\033[2J\033[H")
	fmt.Fprintf(os.Stdout, "portmap watch - updated %s - refresh %s - Ctrl-C to stop\n\n", time.Now().Format("15:04:05"), interval)
	if scanErr != nil {
		fmt.Fprintf(os.Stdout, "scan error: %v\n", scanErr)
		return
	}
	if err := portmap.WriteTable(os.Stdout, entries); err != nil {
		fmt.Fprintf(os.Stdout, "render error: %v\n", err)
	}
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", value)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}
	return port, nil
}

func formatCheckResult(port int, entries []portmap.Entry) string {
	if len(entries) == 0 {
		return fmt.Sprintf("%d is free", port)
	}

	suffix := ""
	if len(entries) > 1 {
		suffix = fmt.Sprintf(" (+%d more)", len(entries)-1)
	}
	return fmt.Sprintf("%d is in use by %s%s", port, describeOwner(entries[0]), suffix)
}

func describeOwner(entry portmap.Entry) string {
	parts := []string{ownerName(entry)}
	if entry.PID != "" {
		parts = append(parts, "pid "+entry.PID)
	}
	if entry.Container != "" {
		parts = append(parts, "container "+entry.Container)
	}
	return strings.Join(parts, ", ")
}

func ownerName(entry portmap.Entry) string {
	switch {
	case entry.App != "":
		return entry.App
	case entry.Container != "":
		return "docker"
	case entry.Service != "":
		return entry.Service
	default:
		return "unknown process"
	}
}
