package portmap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Entry is a single row in the unified port table.
type Entry struct {
	Port        int      `json:"port"`
	Protocol    string   `json:"protocol"`
	Addresses   []string `json:"addresses"`
	App         string   `json:"app,omitempty"`
	PID         string   `json:"pid,omitempty"`
	User        string   `json:"user,omitempty"`
	Container   string   `json:"container,omitempty"`
	ContainerID string   `json:"container_id,omitempty"`
	Service     string   `json:"service,omitempty"`
}

type Options struct {
	IncludeDocker bool
	Protocol      string
	Runner        Runner
	ServicesPath  string
}

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

type RunnerFunc func(ctx context.Context, name string, args ...string) (string, error)

func (f RunnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

type listener struct {
	Port     int
	Protocol string
	Address  string
	App      string
	PID      string
	User     string
}

type dockerPort struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string
	Container     string
	ContainerID   string
}

type serviceKey struct {
	Port     int
	Protocol string
}

var defaultRunner = RunnerFunc(runCommand)

func Scan(ctx context.Context, opts Options) ([]Entry, error) {
	protocol, err := normalizeProtocol(opts.Protocol)
	if err != nil {
		return nil, err
	}
	if opts.Runner == nil {
		opts.Runner = defaultRunner
	}

	listeners := collectListeners(ctx, opts.Runner)
	var dockerPorts []dockerPort
	if opts.IncludeDocker {
		dockerPorts = collectDockerPorts(ctx, opts.Runner)
	}

	services := loadServices(opts.ServicesPath)
	entries := assembleEntries(listeners, dockerPorts, services)
	entries = filterEntries(entries, protocol)
	sortEntries(entries)
	return entries, nil
}

func WriteTable(w io.Writer, entries []Entry) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "PORT\tPROTO\tADDRESS\tAPP\tPID\tCONTAINER\tSERVICE"); err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(
			tw,
			"%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			entry.Port,
			entry.Protocol,
			valueOrDash(strings.Join(entry.Addresses, ",")),
			valueOrDash(entry.App),
			valueOrDash(entry.PID),
			valueOrDash(entry.Container),
			valueOrDash(entry.Service),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func WriteJSON(w io.Writer, entries []Entry) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func collectListeners(ctx context.Context, runner Runner) []listener {
	if runtime.GOOS == "darwin" {
		return collectLsof(ctx, runner)
	}

	listeners := collectSS(ctx, runner)
	if len(listeners) > 0 {
		return listeners
	}

	listeners = collectNetstat(ctx, runner)
	if len(listeners) > 0 {
		return listeners
	}

	return collectLsof(ctx, runner)
}

func collectLsof(ctx context.Context, runner Runner) []listener {
	var listeners []listener

	if out, err := runner.Run(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcLtn"); err == nil {
		listeners = append(listeners, parseLsofFields(out, "tcp")...)
	}
	if out, err := runner.Run(ctx, "lsof", "-nP", "-iUDP", "-F", "pcLtn"); err == nil {
		listeners = append(listeners, parseLsofFields(out, "udp")...)
	}

	return listeners
}

func collectSS(ctx context.Context, runner Runner) []listener {
	out, err := runner.Run(ctx, "ss", "-H", "-lntu", "-p")
	if err != nil {
		return nil
	}
	return parseSS(out)
}

func collectNetstat(ctx context.Context, runner Runner) []listener {
	out, err := runner.Run(ctx, "netstat", "-lntup")
	if err != nil {
		return nil
	}
	return parseNetstat(out)
}

func collectDockerPorts(ctx context.Context, runner Runner) []dockerPort {
	out, err := runner.Run(ctx, "docker", "ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Ports}}")
	if err != nil {
		return nil
	}
	return parseDockerPorts(out)
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.TrimSpace(stderr.String()) != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func parseLsofFields(out string, protocol string) []listener {
	var listeners []listener
	current := listener{Protocol: protocol}

	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}

		prefix := line[0]
		value := line[1:]
		switch prefix {
		case 'p':
			current = listener{PID: value, Protocol: protocol}
		case 'c':
			current.App = value
		case 'L':
			current.User = value
		case 'n':
			if strings.Contains(value, "->") {
				continue
			}
			address, port, ok := parseAddressPort(value)
			if !ok {
				continue
			}
			next := current
			next.Address = address
			next.Port = port
			listeners = append(listeners, next)
		}
	}

	return listeners
}

func parseSS(out string) []listener {
	var listeners []listener
	processRe := regexp.MustCompile(`users:\(\("([^"]+)",pid=([0-9]+)`)

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		protocol := strings.ToLower(fields[0])
		if protocol != "tcp" && protocol != "udp" {
			continue
		}
		address, port, ok := parseAddressPort(fields[4])
		if !ok {
			continue
		}

		item := listener{
			Port:     port,
			Protocol: protocol,
			Address:  address,
		}
		if match := processRe.FindStringSubmatch(line); len(match) == 3 {
			item.App = match[1]
			item.PID = match[2]
		}
		listeners = append(listeners, item)
	}

	return listeners
}

func parseNetstat(out string) []listener {
	var listeners []listener

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Proto") || strings.HasPrefix(line, "Active") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		protocol := strings.ToLower(strings.TrimRight(fields[0], "46"))
		if protocol != "tcp" && protocol != "udp" {
			continue
		}
		if protocol == "tcp" && !strings.Contains(line, "LISTEN") {
			continue
		}

		address, port, ok := parseAddressPort(fields[3])
		if !ok {
			continue
		}

		item := listener{
			Port:     port,
			Protocol: protocol,
			Address:  address,
		}

		if len(fields) > 5 {
			process := fields[len(fields)-1]
			if slash := strings.Index(process, "/"); slash > 0 {
				item.PID = process[:slash]
				item.App = process[slash+1:]
			}
		}

		listeners = append(listeners, item)
	}

	return listeners
}

func parseDockerPorts(out string) []dockerPort {
	var ports []dockerPort

	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}

		id := strings.TrimSpace(fields[0])
		name := strings.TrimSpace(fields[1])
		for _, chunk := range strings.Split(fields[2], ",") {
			chunk = strings.TrimSpace(chunk)
			if !strings.Contains(chunk, "->") {
				continue
			}

			parts := strings.SplitN(chunk, "->", 2)
			host := strings.TrimSpace(parts[0])
			target := strings.TrimSpace(parts[1])

			targetPort, protocol, ok := parseDockerTarget(target)
			if !ok {
				continue
			}
			hostIP, hostPorts, ok := parseAddressPortRange(host)
			if !ok {
				continue
			}

			for idx, hostPort := range hostPorts {
				containerPort := targetPort
				if idx > 0 {
					containerPort = targetPort + idx
				}
				ports = append(ports, dockerPort{
					HostIP:        hostIP,
					HostPort:      hostPort,
					ContainerPort: containerPort,
					Protocol:      protocol,
					Container:     name,
					ContainerID:   id,
				})
			}
		}
	}

	return ports
}

func parseDockerTarget(target string) (int, string, bool) {
	target = strings.TrimSpace(target)
	slash := strings.LastIndex(target, "/")
	if slash < 0 {
		return 0, "", false
	}

	protocol := strings.ToLower(target[slash+1:])
	if protocol != "tcp" && protocol != "udp" {
		return 0, "", false
	}

	portPart := target[:slash]
	if dash := strings.Index(portPart, "-"); dash >= 0 {
		portPart = portPart[:dash]
	}
	port, err := strconv.Atoi(portPart)
	if err != nil {
		return 0, "", false
	}

	return port, protocol, true
}

func parseAddressPort(value string) (string, int, bool) {
	address, ports, ok := parseAddressPortRange(value)
	if !ok || len(ports) != 1 {
		return "", 0, false
	}
	return address, ports[0], true
}

func parseAddressPortRange(value string) (string, []int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil, false
	}
	if idx := strings.Index(value, "->"); idx >= 0 {
		value = value[:idx]
	}

	colon := strings.LastIndex(value, ":")
	if colon < 0 || colon == len(value)-1 {
		return "", nil, false
	}

	address := value[:colon]
	portPart := value[colon+1:]
	if address == "" {
		address = "*"
	}
	if portPart == "*" {
		return "", nil, false
	}

	ports, ok := parsePortRange(portPart)
	if !ok {
		return "", nil, false
	}

	return cleanAddress(address), ports, true
}

func parsePortRange(value string) ([]int, bool) {
	parts := strings.Split(value, "-")
	if len(parts) == 1 {
		port, err := strconv.Atoi(parts[0])
		if err != nil || !validPort(port) {
			return nil, false
		}
		return []int{port}, true
	}
	if len(parts) != 2 {
		return nil, false
	}

	start, err := strconv.Atoi(parts[0])
	if err != nil || !validPort(start) {
		return nil, false
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil || !validPort(end) || end < start || end-start > 1024 {
		return nil, false
	}

	ports := make([]int, 0, end-start+1)
	for port := start; port <= end; port++ {
		ports = append(ports, port)
	}
	return ports, true
}

func assembleEntries(listeners []listener, dockerPorts []dockerPort, services map[serviceKey]string) []Entry {
	aggregates := map[string]*entryAggregate{}

	for _, item := range listeners {
		containers := matchingDockerPorts(item, dockerPorts)
		entry := Entry{
			Port:        item.Port,
			Protocol:    item.Protocol,
			App:         item.App,
			PID:         item.PID,
			User:        item.User,
			Container:   joinDockerNames(containers),
			ContainerID: joinDockerIDs(containers),
			Service:     serviceFor(services, item.Port, item.Protocol),
		}
		addEntry(aggregates, entry, item.Address)
	}

	for _, item := range dockerPorts {
		if dockerPortHasListener(item, listeners) {
			continue
		}
		entry := Entry{
			Port:        item.HostPort,
			Protocol:    item.Protocol,
			App:         "docker",
			Container:   item.Container,
			ContainerID: item.ContainerID,
			Service:     serviceFor(services, item.HostPort, item.Protocol),
		}
		addEntry(aggregates, entry, item.HostIP)
	}

	entries := make([]Entry, 0, len(aggregates))
	for _, aggregate := range aggregates {
		sort.Strings(aggregate.entry.Addresses)
		entries = append(entries, aggregate.entry)
	}

	return entries
}

type entryAggregate struct {
	entry     Entry
	addresses map[string]struct{}
}

func addEntry(aggregates map[string]*entryAggregate, entry Entry, address string) {
	key := entryKey(entry)
	aggregate, ok := aggregates[key]
	if !ok {
		aggregate = &entryAggregate{
			entry:     entry,
			addresses: map[string]struct{}{},
		}
		aggregates[key] = aggregate
	}

	address = cleanAddress(address)
	if address == "" {
		address = "*"
	}
	if _, ok := aggregate.addresses[address]; !ok {
		aggregate.addresses[address] = struct{}{}
		aggregate.entry.Addresses = append(aggregate.entry.Addresses, address)
	}
}

func entryKey(entry Entry) string {
	return strings.Join([]string{
		strconv.Itoa(entry.Port),
		entry.Protocol,
		entry.App,
		entry.PID,
		entry.User,
		entry.Container,
		entry.ContainerID,
		entry.Service,
	}, "\x00")
}

func matchingDockerPorts(item listener, ports []dockerPort) []dockerPort {
	var matches []dockerPort
	seen := map[string]struct{}{}
	for _, port := range ports {
		if item.Port != port.HostPort || item.Protocol != port.Protocol {
			continue
		}
		if !addressesCompatible(item.Address, port.HostIP) {
			continue
		}
		key := port.ContainerID + "\x00" + port.Container
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		matches = append(matches, port)
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Container < matches[j].Container
	})
	return matches
}

func dockerPortHasListener(port dockerPort, listeners []listener) bool {
	for _, item := range listeners {
		if item.Port == port.HostPort && item.Protocol == port.Protocol && addressesCompatible(item.Address, port.HostIP) {
			return true
		}
	}
	return false
}

func joinDockerNames(ports []dockerPort) string {
	return joinDockerField(ports, func(port dockerPort) string { return port.Container })
}

func joinDockerIDs(ports []dockerPort) string {
	return joinDockerField(ports, func(port dockerPort) string { return port.ContainerID })
}

func joinDockerField(ports []dockerPort, field func(dockerPort) string) string {
	if len(ports) == 0 {
		return ""
	}
	values := make([]string, 0, len(ports))
	seen := map[string]struct{}{}
	for _, port := range ports {
		value := field(port)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func filterEntries(entries []Entry, protocol string) []Entry {
	if protocol == "all" {
		return entries
	}

	filtered := entries[:0]
	for _, entry := range entries {
		if entry.Protocol == protocol {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.Port != right.Port {
			return left.Port < right.Port
		}
		if left.Protocol != right.Protocol {
			return left.Protocol < right.Protocol
		}
		if left.App != right.App {
			return left.App < right.App
		}
		return strings.Join(left.Addresses, ",") < strings.Join(right.Addresses, ",")
	})
}

func loadServices(path string) map[serviceKey]string {
	services := defaultServices()
	if path == "" {
		path = "/etc/services"
	}

	if data, err := os.ReadFile(path); err == nil {
		parseServices(data, services)
	}
	applyServiceAliases(services)
	return services
}

func defaultServices() map[serviceKey]string {
	services := map[serviceKey]string{}
	for _, item := range []struct {
		port     int
		protocol string
		name     string
	}{
		{20, "tcp", "ftp-data"},
		{21, "tcp", "ftp"},
		{22, "tcp", "ssh"},
		{25, "tcp", "smtp"},
		{53, "tcp", "dns"},
		{53, "udp", "dns"},
		{80, "tcp", "http"},
		{110, "tcp", "pop3"},
		{123, "udp", "ntp"},
		{143, "tcp", "imap"},
		{443, "tcp", "https"},
		{465, "tcp", "smtps"},
		{587, "tcp", "submission"},
		{993, "tcp", "imaps"},
		{995, "tcp", "pop3s"},
		{2049, "tcp", "nfs"},
		{2049, "udp", "nfs"},
		{2375, "tcp", "docker"},
		{2376, "tcp", "docker-tls"},
		{3000, "tcp", "dev-http"},
		{3306, "tcp", "mysql"},
		{5432, "tcp", "postgres"},
		{5432, "udp", "postgres"},
		{6379, "tcp", "redis"},
		{8000, "tcp", "http-alt"},
		{8080, "tcp", "http-alt"},
		{9200, "tcp", "elasticsearch"},
		{27017, "tcp", "mongodb"},
	} {
		services[serviceKey{Port: item.port, Protocol: item.protocol}] = item.name
	}
	return services
}

func parseServices(data []byte, services map[serviceKey]string) {
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if hash := strings.Index(line, "#"); hash >= 0 {
			line = strings.TrimSpace(line[:hash])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		portProto := strings.Split(fields[1], "/")
		if len(portProto) != 2 {
			continue
		}
		port, err := strconv.Atoi(portProto[0])
		if err != nil || !validPort(port) {
			continue
		}
		protocol := strings.ToLower(portProto[1])
		if protocol != "tcp" && protocol != "udp" {
			continue
		}

		key := serviceKey{Port: port, Protocol: protocol}
		if _, ok := services[key]; !ok {
			services[key] = fields[0]
		}
	}
}

func applyServiceAliases(services map[serviceKey]string) {
	for _, protocol := range []string{"tcp", "udp"} {
		services[serviceKey{Port: 5432, Protocol: protocol}] = "postgres"
	}
}

func serviceFor(services map[serviceKey]string, port int, protocol string) string {
	if service, ok := services[serviceKey{Port: port, Protocol: protocol}]; ok {
		return service
	}
	return ""
}

func normalizeProtocol(protocol string) (string, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" {
		protocol = "all"
	}
	switch protocol {
	case "all", "tcp", "udp":
		return protocol, nil
	default:
		return "", fmt.Errorf("unknown protocol %q, expected all, tcp, or udp", protocol)
	}
}

func addressesCompatible(left string, right string) bool {
	left = normalizeAddress(left)
	right = normalizeAddress(right)
	return isWildcardAddress(left) || isWildcardAddress(right) || left == right
}

func cleanAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return "*"
	}
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		address = strings.TrimPrefix(strings.TrimSuffix(address, "]"), "[")
	}
	return address
}

func normalizeAddress(address string) string {
	address = cleanAddress(address)
	switch address {
	case "", "*":
		return "*"
	case "0.0.0.0":
		return "*"
	case "::", "[::]":
		return "*"
	default:
		return address
	}
}

func isWildcardAddress(address string) bool {
	return address == "*"
}

func validPort(port int) bool {
	return port > 0 && port <= 65535
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
