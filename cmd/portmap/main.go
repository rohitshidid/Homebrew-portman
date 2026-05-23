package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rohitshidid/portmap/internal/portmap"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("portmap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	jsonOutput := fs.Bool("json", false, "print JSON instead of a table")
	noDocker := fs.Bool("no-docker", false, "skip Docker container port annotations")
	protocol := fs.String("protocol", "all", "protocol to show: all, tcp, or udp")
	showVersion := fs.Bool("version", false, "print version and exit")
	timeout := fs.Duration("timeout", 5*time.Second, "maximum scan duration")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		fmt.Fprintf(os.Stdout, "portmap %s\n", version)
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	entries, err := portmap.Scan(ctx, portmap.Options{
		IncludeDocker: !*noDocker,
		Protocol:      *protocol,
	})
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
