//go:build linux && amd64

package helper

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const networkOutputLimit = 64 << 10

// NetworkExecutor adds only the fixed bridge IPv6 DROP rules after Authorize.
// No configuration/request can replace the executable, arguments or journal.
type NetworkExecutor struct {
	Backend         domain.NetworkCreationProvider
	JournalPath     string // optional fixture path, never populated from a request/policy
	run             func(context.Context, []string) (networkCommandResult, error)
	journalOwnerUID uint32             // ordinary-user generated fixture override; zero in production
	beforePublish   func(string) error // fixture fault before a durable record write
}

type networkCommandResult struct {
	stdout, stderr string
	exit           int
}
type networkOutput struct {
	bytes.Buffer
	exceeded bool
}

func (b *networkOutput) Write(p []byte) (int, error) {
	if len(p) > networkOutputLimit-b.Len() {
		b.exceeded = true
		return 0, errors.New("firewalld output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func runNetworkCommand(ctx context.Context, args []string) (networkCommandResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/firewall-cmd", args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin", "LANG=C", "LC_ALL=C", "HOME=/", "PYTHONNOUSERSITE=1", "PYTHONSAFEPATH=1", "DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket"}
	cmd.WaitDelay = time.Second
	var stdout, stderr networkOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return networkCommandResult{}, ctx.Err()
	}
	if stdout.exceeded || stderr.exceeded {
		return networkCommandResult{}, domain.Fail("UNSUPPORTED_CAPABILITY", "firewalld response exceeded bounds")
	}
	result := networkCommandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		var status *exec.ExitError
		if !errors.As(err, &status) {
			return networkCommandResult{}, domain.Fail("UNSUPPORTED_CAPABILITY", "fixed firewalld executable could not be invoked")
		}
		result.exit = status.ExitCode()
	}
	return result, nil
}

func (e NetworkExecutor) command(ctx context.Context, args ...string) (networkCommandResult, error) {
	if err := ctx.Err(); err != nil {
		return networkCommandResult{}, err
	}
	run := e.run
	if run == nil {
		run = runNetworkCommand
	}
	out, err := run(ctx, append([]string(nil), args...))
	if err != nil {
		return networkCommandResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return networkCommandResult{}, err
	}
	if len(out.stdout) > networkOutputLimit || len(out.stderr) > networkOutputLimit || out.stderr != "" {
		return networkCommandResult{}, domain.Fail("UNSUPPORTED_CAPABILITY", "firewalld response is oversized or includes an unexpected diagnostic")
	}
	return out, nil
}

// Four runtime and four permanent rules; indices and argument ordering are a
// versioned journal contract. Only the validated generated bridge varies.
func networkRules(bridge string) [8][]string {
	var rules [8][]string
	for i, pair := range [][2]string{{"INPUT", "--logical-in"}, {"OUTPUT", "--logical-out"}, {"FORWARD", "--logical-in"}, {"FORWARD", "--logical-out"}} {
		for _, offset := range []int{0, 4} {
			args := []string{}
			if offset != 0 {
				args = append(args, "--permanent")
			}
			rules[i+offset] = append(args, "--direct", "--add-rule", "eb", "filter", pair[0], "-32768", pair[1], bridge, "-p", "IPv6", "-j", "DROP")
		}
	}
	return rules
}

func networkRuleLine(args []string) string {
	for i, a := range args {
		if a == "eb" {
			return strings.Join(args[i:], " ")
		}
	}
	return ""
}

type networkRuleInventory [2]map[string]bool

func parseNetworkRules(raw string) (map[string]bool, error) {
	bad := func() (map[string]bool, error) {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "unknown, ambiguous or potentially bypassing firewalld direct rules")
	}
	if len(raw) > networkOutputLimit || (raw != "" && !strings.HasSuffix(raw, "\n")) {
		return bad()
	}
	out := map[string]bool{}
	if raw == "" {
		return out, nil
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) > 256 {
		return bad()
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(line) > 2048 || len(fields) < 4 || out[line] || strings.ContainsAny(line, "\r\t\x00") || strings.TrimSpace(line) != line {
			return bad()
		}
		for _, c := range line {
			if c < 32 || c > 126 {
				return bad()
			}
		}
		switch fields[0] {
		case "ipv4", "ipv6": // Retained exactly, never executed or edited.
		case "eb":
			if len(fields) != 10 || line != strings.Join(fields, " ") || fields[1] != "filter" || fields[3] != "-32768" || fields[6] != "-p" || fields[7] != "IPv6" || fields[8] != "-j" || fields[9] != "DROP" {
				return bad()
			}
			if len(fields[5]) != 14 || !strings.HasPrefix(fields[5], "vm") {
				return bad()
			}
			hexBytes, err := hex.DecodeString(fields[5][2:])
			if err != nil || hex.EncodeToString(hexBytes) != fields[5][2:] {
				return bad()
			}
			if !(fields[2] == "INPUT" && fields[4] == "--logical-in" || fields[2] == "OUTPUT" && fields[4] == "--logical-out" || fields[2] == "FORWARD" && (fields[4] == "--logical-in" || fields[4] == "--logical-out")) {
				return bad()
			}
		default:
			return bad()
		}
		out[line] = true
	}
	return out, nil
}

// Tracked passthroughs and custom chains are separate firewalld API inventories.
// Unknown bridge-family entries can bypass our direct chains, so none are
// accepted. Other families are retained as opaque, bounded comparison keys.
func parseNetworkOther(raw, kind string) (map[string]bool, error) {
	bad := func() (map[string]bool, error) {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "unknown bridge passthrough/custom chain or incomplete direct inventory")
	}
	if len(raw) > networkOutputLimit || raw != "" && !strings.HasSuffix(raw, "\n") {
		return bad()
	}
	out := map[string]bool{}
	if raw == "" {
		return out, nil
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) > 256 {
		return bad()
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(line) > 2048 || len(fields) < 2 || (kind == "chains" && len(fields) != 3) || strings.TrimSpace(line) != line || strings.ContainsAny(line, "\r\t\x00") {
			return bad()
		}
		for _, c := range line {
			if c < 32 || c > 126 {
				return bad()
			}
		}
		if fields[0] != "ipv4" && fields[0] != "ipv6" {
			return bad()
		}
		key := "@" + kind + " " + line
		if out[key] {
			return bad()
		}
		out[key] = true
	}
	return out, nil
}

func (e NetworkExecutor) inventory(ctx context.Context) (networkRuleInventory, error) {
	var result networkRuleInventory
	for i := 0; i < 2; i++ {
		args := []string{"--direct", "--get-all-rules"}
		if i == 1 {
			args = append([]string{"--permanent"}, args...)
		}
		out, err := e.command(ctx, args...)
		if err != nil {
			return result, err
		}
		if out.exit != 0 {
			return result, domain.Fail("UNSUPPORTED_CAPABILITY", "firewalld direct bridge rule inventory unavailable")
		}
		result[i], err = parseNetworkRules(out.stdout)
		if err != nil {
			return result, err
		}
		for _, kind := range []string{"passthroughs", "chains"} {
			extraArgs := []string{"--direct", "--get-all-" + kind}
			if i == 1 {
				extraArgs = append([]string{"--permanent"}, extraArgs...)
			}
			extra, readErr := e.command(ctx, extraArgs...)
			if readErr != nil {
				return result, readErr
			}
			if extra.exit != 0 {
				return result, domain.Fail("UNSUPPORTED_CAPABILITY", "firewalld tracked passthrough/custom chain inventory unavailable")
			}
			parsed, parseErr := parseNetworkOther(extra.stdout, kind)
			if parseErr != nil {
				return result, parseErr
			}
			for key := range parsed {
				result[i][key] = true
			}
		}
	}
	return result, nil
}

func (e NetworkExecutor) query(ctx context.Context, args []string) (bool, error) {
	query := append([]string(nil), args...)
	for i := range query {
		if query[i] == "--add-rule" {
			query[i] = "--query-rule"
		}
	}
	out, err := e.command(ctx, query...)
	if err != nil {
		return false, err
	}
	if out.exit == 0 && out.stdout == "yes\n" {
		return true, nil
	}
	if out.exit == 1 && out.stdout == "no\n" {
		return false, nil
	}
	return false, domain.Fail("UNSUPPORTED_CAPABILITY", "firewalld exact rule query returned an unsupported response")
}

func (e NetworkExecutor) queryAll(ctx context.Context, rules [8][]string, inventory networkRuleInventory) ([8]bool, error) {
	var present [8]bool
	for i, args := range rules {
		v, err := e.query(ctx, args)
		if err != nil {
			return present, err
		}
		if v != inventory[i/4][networkRuleLine(args)] {
			return present, domain.Fail("SOURCE_CHANGED", "firewalld direct inventory changed during exact rule observation")
		}
		present[i] = v
	}
	return present, nil
}

func (e NetworkExecutor) native(ctx context.Context, r Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.Backend == nil {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "independent native network observer unavailable")
	}
	n, err := e.Backend.InspectCreatedNetwork(ctx, "qemu:///system", r.Network.Definition)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	d := r.Network.Definition
	if n.Key != (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "network", UUID: r.ResourceID}) || n.Name != d.Name || !n.Persistent || n.Autostart || (r.Mode == "apply" && n.Active) {
		return domain.Fail("SOURCE_CHANGED", "IPv6 filter requires the exact persistent no-autostart network; apply requires inactive state")
	}
	if err = networkxml.Match(n.PersistentXML, d); err != nil {
		return err
	}
	if n.Active {
		return networkxml.Match(n.LiveXML, d)
	}
	if n.LiveXML != "" {
		return domain.Fail("SOURCE_CHANGED", "inactive network has unexpected live XML")
	}
	return nil
}

type networkOwner struct {
	Version    int                      `json:"version"`
	ActorUID   uint32                   `json:"actorUID"`
	KeyID      string                   `json:"keyID"`
	ResourceID string                   `json:"resourceID"`
	Definition domain.NetworkDefinition `json:"definition"`
}
type networkRecord struct {
	Version    int      `json:"version"`
	Binding    string   `json:"resourceBinding"`
	JobID      string   `json:"jobID"`
	PlanDigest string   `json:"planDigest"`
	Kind       string   `json:"kind"`
	Rule       int      `json:"rule"`
	Arguments  []string `json:"arguments"`
}
type networkHistory struct {
	plan     string
	exists   bool
	rules    [8]bool
	complete bool
}
type networkJournal struct {
	file          *os.File
	path          string
	owner         uint32
	beforePublish func(string) error
}

func (e NetworkExecutor) openJournal() (*networkJournal, error) {
	path := e.JournalPath
	if path == "" {
		path = JournalPath
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, domain.Fail("INVALID_INPUT", "canonical helper journal path required")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, err
	}
	j := &networkJournal{file: os.NewFile(uintptr(fd), "network helper journal"), path: path, owner: e.journalOwnerUID, beforePublish: e.beforePublish}
	if err = j.check(); err != nil {
		j.file.Close()
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		j.file.Close()
		return nil, domain.Fail("RESOURCE_BUSY", "private helper journal unavailable or held by another writer")
	}
	return j, nil
}

func (j *networkJournal) check() error {
	var held, named unix.Stat_t
	if err := unix.Fstat(int(j.file.Fd()), &held); err != nil {
		return err
	}
	if err := unix.Fstatat(unix.AT_FDCWD, j.path, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if held.Uid != j.owner || held.Mode&unix.S_IFMT != unix.S_IFDIR || held.Mode&0777 != 0700 || held.Dev != named.Dev || held.Ino != named.Ino || named.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("helper journal directory ownership or generation changed")
	}
	return nil
}

func (j *networkJournal) write(name string, value any) error {
	if err := j.check(); err != nil {
		return err
	}
	if j.beforePublish != nil {
		if err := j.beforePublish(name); err != nil {
			return err
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > 16<<10 || filepath.Base(name) != name {
		return errors.New("invalid bounded network journal record")
	}
	fd, err := unix.Openat(int(j.file.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "new network helper record")
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = j.file.Sync()
	}
	if err == nil {
		err = j.check()
	}
	return err
}

func (j *networkJournal) read(name string, value any) error {
	if filepath.Base(name) != name {
		return errors.New("invalid network journal record name")
	}
	fd, err := unix.Openat(int(j.file.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "held network helper record")
	defer f.Close()
	var before, after, named unix.Stat_t
	if err = unix.Fstat(fd, &before); err != nil {
		return err
	}
	if before.Uid != j.owner || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0777 != 0600 || before.Nlink != 1 || before.Size <= 0 || before.Size > 16<<10 {
		return errors.New("invalid network journal file metadata")
	}
	data, err := io.ReadAll(io.LimitReader(f, (16<<10)+1))
	if err != nil {
		return err
	}
	if len(data) > 16<<10 {
		return errors.New("network journal record exceeds bounds")
	}
	if err = unix.Fstat(fd, &after); err != nil {
		return err
	}
	if err = unix.Fstatat(int(j.file.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim || after.Nlink != 1 || after.Dev != named.Dev || after.Ino != named.Ino {
		return errors.New("network journal record changed during observation")
	}
	return wire.Decode(data, value)
}

func networkPrefix(resource string) string { return "network-" + resource + "." }
func networkRecordName(r Request, suffix string) string {
	return networkPrefix(r.ResourceID) + "job-" + r.JobID + "." + suffix + ".json"
}
func newNetworkRecord(r Request, binding, kind string, index int, args []string) networkRecord {
	if args == nil {
		args = []string{}
	}
	return networkRecord{1, binding, r.JobID, r.PlanDigest, kind, index, args}
}

func (j *networkJournal) history(ctx context.Context, r Request, binding string, rules [8][]string) (map[string]networkHistory, [8]bool, error) {
	jobs := map[string]networkHistory{}
	var owned [8]bool
	// A fresh descriptor gives a fresh directory cursor while retaining the held
	// no-symlink root. Unrelated helper records are not read or altered.
	fd, err := unix.Openat(int(j.file.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, owned, err
	}
	dir := os.NewFile(uintptr(fd), "network journal inventory")
	defer dir.Close()
	entries, err := dir.ReadDir(4097)
	if err != nil && err != io.EOF {
		return nil, owned, err
	}
	if len(entries) > 4096 {
		return nil, owned, errors.New("helper journal inventory exceeds bound")
	}
	names := []string{}
	prefix := networkPrefix(r.ResourceID)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) && entry.Name() != prefix+"owner.json" {
			names = append(names, entry.Name())
		}
	}
	if len(names) > 640 {
		return nil, owned, errors.New("network recovery history exceeds bound")
	}
	sort.Strings(names)
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return nil, owned, err
		}
		part := strings.TrimPrefix(name, prefix)
		if !strings.HasPrefix(part, "job-") || len(part) < 42 || !uuid.MatchString(part[4:40]) || part[40] != '.' {
			return nil, owned, errors.New("unknown network journal entry")
		}
		job := part[4:40]
		suffix := part[41:]
		var rec networkRecord
		if err = j.read(name, &rec); err != nil {
			return nil, owned, err
		}
		copyReq := r
		copyReq.JobID = job
		copyReq.PlanDigest = rec.PlanDigest
		if !networkDigest(rec.PlanDigest) {
			return nil, owned, errors.New("invalid network job plan binding")
		}
		var expected networkRecord
		h := jobs[job]
		if h.plan != "" && h.plan != rec.PlanDigest {
			return nil, owned, errors.New("inconsistent network job plan binding")
		}
		h.plan = rec.PlanDigest
		switch suffix {
		case "intent.json":
			expected = newNetworkRecord(copyReq, binding, "job", -1, nil)
			h.exists = true
		case "complete.json":
			expected = newNetworkRecord(copyReq, binding, "complete", -1, nil)
			h.complete = true
		default:
			if len(suffix) != 10 || !strings.HasPrefix(suffix, "rule") || !strings.HasSuffix(suffix, ".json") || suffix[4] < '0' || suffix[4] > '7' {
				return nil, owned, errors.New("unknown network journal record")
			}
			i := int(suffix[4] - '0')
			expected = newNetworkRecord(copyReq, binding, "rule", i, rules[i])
			h.rules[i] = true
		}
		if !reflect.DeepEqual(rec, expected) {
			return nil, owned, errors.New("network journal declaration differs from immutable resource/job binding")
		}
		jobs[job] = h
	}
	if len(jobs) > 64 {
		return nil, owned, errors.New("network job history exceeds bound")
	}
	for _, h := range jobs {
		if !h.exists {
			return nil, owned, errors.New("network rule record lacks durable job intent")
		}
		for i, v := range h.rules {
			owned[i] = owned[i] || v
			if h.complete && !v {
				return nil, owned, errors.New("network completion lacks every rule intent")
			}
		}
	}
	return jobs, owned, j.check()
}

func networkDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

// Execute returns the zero response on every incomplete/uncertain boundary.
// Presence is configuration evidence only; PacketVerified always remains false.
func (e NetworkExecutor) Execute(ctx context.Context, r Request, p Policy) (NetworkResponse, error) {
	var empty NetworkResponse
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if r.APIVersion != domain.APIVersion || p.APIVersion != domain.APIVersion || r.Operation != "network.ipv6-filter" || !uuid.MatchString(r.JobID) || !networkDigest(r.PlanDigest) || r.ActorUID == 0 || len(r.KeyID) == 0 || len(r.KeyID) > 128 || !time.Now().Before(r.ExpiresAt) {
		return empty, domain.Fail("INVALID_INPUT", "authenticated exact network filter request required")
	}
	if err := authorizeNetwork(r, p); err != nil {
		return empty, domain.Fail("PERMISSION_DENIED", err.Error())
	}
	if e.Backend == nil {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "independent native network observer unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ctx, expire := context.WithDeadline(ctx, r.ExpiresAt)
	defer expire()
	j, err := e.openJournal()
	if err != nil {
		return empty, err
	}
	defer j.file.Close()
	owner := networkOwner{1, r.ActorUID, r.KeyID, r.ResourceID, r.Network.Definition}
	binding, err := operations.Digest(owner)
	if err != nil {
		return empty, err
	}
	ownerName := networkPrefix(r.ResourceID) + "owner.json"
	var prior networkOwner
	err = j.read(ownerName, &prior)
	ownerExists := err == nil
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return empty, domain.Fail("RECOVERY_REQUIRED", "network resource journal is unreadable or incomplete")
	}
	if ownerExists && prior != owner {
		return empty, domain.Fail("SOURCE_CHANGED", "network resource belongs to a different immutable helper binding")
	}
	rules := networkRules(r.Network.Definition.Bridge)
	history, owned, err := j.history(ctx, r, binding, rules)
	if err != nil {
		return empty, domain.Fail("RECOVERY_REQUIRED", "network job journal is incomplete or inconsistent")
	}
	if !ownerExists && len(history) > 0 {
		return empty, domain.Fail("RECOVERY_REQUIRED", "network job history lacks resource ownership")
	}
	if r.Mode == "apply" && history[r.JobID].exists {
		return empty, domain.Fail("RECOVERY_REQUIRED", "network helper job already exists; observe or authorize a new resume job")
	}
	if r.Mode != "check" {
		if err = e.native(ctx, r); err != nil {
			return empty, err
		}
	}
	state, err := e.command(ctx, "--state")
	if err != nil {
		return empty, err
	}
	if state.exit != 0 || state.stdout != "running\n" {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "firewalld must be running without startup failure")
	}
	inventory, err := e.inventory(ctx)
	if err != nil {
		return empty, err
	}
	present, err := e.queryAll(ctx, rules, inventory)
	if err != nil {
		return empty, err
	}
	for i, v := range present {
		if v && !owned[i] {
			return empty, domain.Fail("RESOURCE_BUSY", "existing exact IPv6 rule has no matching durable Virmill ownership intent")
		}
	}
	response := func() NetworkResponse {
		out := NetworkResponse{Version: 1, ResourceID: r.ResourceID, Bridge: r.Network.Definition.Bridge, PlanDigest: r.PlanDigest, JobID: r.JobID, RuntimePresent: true, PermanentPresent: true}
		for i, v := range present {
			if !v {
				if i < 4 {
					out.RuntimePresent = false
				} else {
					out.PermanentPresent = false
				}
			}
		}
		return out
	}
	if r.Mode == "check" {
		if err = ctx.Err(); err != nil {
			return empty, err
		}
		return response(), nil
	}
	if r.Mode == "observe" {
		h := history[r.JobID]
		if !h.exists || h.plan != r.PlanDigest {
			return empty, domain.Fail("SOURCE_CHANGED", "observation requires the exact durable network job")
		}
		for i, v := range present {
			if !v || !h.rules[i] {
				return empty, domain.Fail("RECOVERY_REQUIRED", "network job lacks all eight durable intents and current rules")
			}
		}
		if err = e.native(ctx, r); err != nil {
			return empty, err
		}
		if err = j.check(); err != nil {
			return empty, err
		}
		if err = ctx.Err(); err != nil {
			return empty, err
		}
		return response(), nil
	}
	if !ownerExists {
		if err = j.write(ownerName, owner); err != nil {
			return empty, domain.Fail("RECOVERY_REQUIRED", "network resource intent publication failed; preserve journal and inspect")
		}
	}
	if err = j.write(networkRecordName(r, "intent"), newNetworkRecord(r, binding, "job", -1, nil)); err != nil {
		return empty, domain.Fail("RECOVERY_REQUIRED", "network job intent publication failed; preserve journal and inspect")
	}
	for i, args := range rules {
		if err = ctx.Err(); err != nil {
			return empty, err
		}
		if err = e.native(ctx, r); err != nil {
			return empty, err
		}
		current, checkErr := e.inventory(ctx)
		if checkErr != nil {
			return empty, checkErr
		}
		if !reflect.DeepEqual(current, inventory) {
			return empty, domain.Fail("SOURCE_CHANGED", "firewalld direct rules changed outside this reviewed job")
		}
		if err = j.write(networkRecordName(r, "rule"+strconv.Itoa(i)), newNetworkRecord(r, binding, "rule", i, args)); err != nil {
			return empty, domain.Fail("RECOVERY_REQUIRED", "per-rule durable intent publication failed; no rule replay is allowed")
		}
		if err = ctx.Err(); err != nil {
			return empty, err
		}
		if !present[i] {
			out, addErr := e.command(ctx, args...)
			if addErr != nil {
				return empty, domain.Fail("RECOVERY_REQUIRED", "IPv6 rule addition was interrupted or unacknowledged; preserve intent and observe")
			}
			if out.exit != 0 || out.stdout != "success\n" {
				return empty, domain.Fail("RECOVERY_REQUIRED", "IPv6 rule addition did not return exact success; preserve intent and observe")
			}
			inventory[i/4][networkRuleLine(args)] = true
		}
		present[i], err = e.query(ctx, args)
		if err != nil || !present[i] {
			return empty, domain.Fail("RECOVERY_REQUIRED", "IPv6 rule intent is durable but presence is unproven")
		}
	}
	current, err := e.inventory(ctx)
	if err != nil {
		return empty, err
	}
	if !reflect.DeepEqual(current, inventory) {
		return empty, domain.Fail("SOURCE_CHANGED", "firewalld direct rules changed before completion")
	}
	present, err = e.queryAll(ctx, rules, current)
	if err != nil {
		return empty, err
	}
	for _, v := range present {
		if !v {
			return empty, domain.Fail("RECOVERY_REQUIRED", "IPv6 filter set became incomplete")
		}
	}
	if err = e.native(ctx, r); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = j.write(networkRecordName(r, "complete"), newNetworkRecord(r, binding, "complete", -1, nil)); err != nil {
		return empty, domain.Fail("RECOVERY_REQUIRED", "IPv6 rules are present but completion publication failed; observe exact job")
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return response(), nil
}
