package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/devusb/flox-machines/internal/machines"
)

type Ops interface {
	Create(ctx context.Context, name, owner string) error
	Status(ctx context.Context, name string) (machines.Status, error)
	Login(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Resize(ctx context.Context, name string, memMB, vcpu int) error
	ResizeReset(ctx context.Context, name string) error
	Grow(ctx context.Context, name, volume string, sizeMB int) error
	Reimage(ctx context.Context, name string) error
	Destroy(ctx context.Context, name string) error
	List(ctx context.Context) (string, error)
	GC(ctx context.Context) error
	SSHArgs(name string, command []string) ([]string, error)
}

var commands = [][2]string{
	{"create <name> [--owner <login>]", "create and start a machine"},
	{"status <name> [--json]", "report a machine's state"},
	{"login <name>", "start a Tailscale login on a machine"},
	{"ssh <name> [command...]", "run a command as root on a machine"},
	{"restart <name>", "restart a machine"},
	{"resize <name> <mem-MB> <vcpu>", "set a per-machine size and restart"},
	{"resize <name> --reset", "return to the template's size and restart"},
	{"grow <name> persist|store <MB>", "grow a machine's disk and restart"},
	{"reimage <name>", "wipe the machine's Nix store layer and restart"},
	{"destroy <name>", "stop and delete a machine and its volumes"},
	{"list", "list machines"},
	{"gc", "stop all machines, collect host garbage, start them"},
}

var usage = func() string {
	width := 0
	for _, c := range commands {
		width = max(width, len(c[0]))
	}
	var b strings.Builder
	b.WriteString("Usage: machine <command> [args]\n\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, c[0], c[1])
	}
	return b.String()
}()

var number = regexp.MustCompile(`^[0-9]+$`)

type usageError string

func (u usageError) Error() string { return "usage: " + string(u) }

func run(args []string, ops Ops, execFn func([]string) error, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 1
	}
	err := dispatch(context.Background(), args[0], args[1:], ops, execFn, stdout)
	if errors.Is(err, errUsage) {
		fmt.Fprint(stdout, usage)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "machine: %v\n", err)
		return 1
	}
	return 0
}

var errUsage = errors.New("usage")

func dispatch(ctx context.Context, command string, args []string, ops Ops, execFn func([]string) error, stdout io.Writer) error {
	switch command {
	case "create":
		const u = usageError("machine create <name> [--owner <login>]")
		if len(args) < 1 {
			return u
		}
		name, owner := args[0], ""
		rest := args[1:]
		for len(rest) > 0 {
			if rest[0] != "--owner" || len(rest) < 2 {
				return u
			}
			owner, rest = rest[1], rest[2:]
		}
		if err := ops.Create(ctx, name, owner); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "created machine-%s\n", name)
	case "status":
		if len(args) < 1 || len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			return usageError("machine status <name> [--json]")
		}
		s, err := ops.Status(ctx, args[0])
		if err != nil {
			return err
		}
		if len(args) == 2 {
			out, err := json.Marshal(s)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s\n", out)
			return nil
		}
		if !s.Exists {
			return fmt.Errorf("no machine named '%s'", args[0])
		}
		writeStatus(stdout, s)
	case "login", "restart", "reimage", "destroy":
		if len(args) != 1 {
			return usageError("machine " + command + " <name>")
		}
		switch command {
		case "login":
			return ops.Login(ctx, args[0])
		case "restart":
			return ops.Restart(ctx, args[0])
		case "reimage":
			return ops.Reimage(ctx, args[0])
		case "destroy":
			if err := ops.Destroy(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "destroyed machine-%s\n", args[0])
		}
	case "ssh":
		if len(args) < 1 {
			return usageError("machine ssh <name> [command...]")
		}
		argv, err := ops.SSHArgs(args[0], args[1:])
		if err != nil {
			return err
		}
		return execFn(argv)
	case "resize":
		const u = usageError("machine resize <name> <mem-MB> <vcpu>")
		if len(args) == 2 && args[1] == "--reset" {
			return ops.ResizeReset(ctx, args[0])
		}
		if len(args) < 3 || !number.MatchString(args[1]) || !number.MatchString(args[2]) {
			return u
		}
		mem, _ := strconv.Atoi(args[1])
		vcpu, _ := strconv.Atoi(args[2])
		return ops.Resize(ctx, args[0], mem, vcpu)
	case "grow":
		if len(args) != 3 || !number.MatchString(args[2]) {
			return usageError("machine grow <name> persist|store <MB>")
		}
		size, _ := strconv.Atoi(args[2])
		return ops.Grow(ctx, args[0], args[1], size)
	case "list":
		out, err := ops.List(ctx)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, out)
	case "gc":
		return ops.GC(ctx)
	default:
		return errUsage
	}
	return nil
}

func execSSH(argv []string) error {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, os.Environ())
}

func main() {
	cfg, err := machines.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "machine: %v\n", err)
		os.Exit(1)
	}
	os.Exit(run(os.Args[1:], machines.NewManager(cfg), execSSH, os.Stdout, os.Stderr))
}

func writeStatus(w io.Writer, s machines.Status) {
	yesNo := map[bool]string{true: "yes", false: "no"}
	fmt.Fprintf(w, "%s\n", s.Name)
	fmt.Fprintf(w, "  owner      %s\n", s.Owner)
	fmt.Fprintf(w, "  running    %s\n", yesNo[s.Running])
	fmt.Fprintf(w, "  reachable  %s\n", yesNo[s.Reachable])
	ts := s.Tailscale
	if ts == nil {
		fmt.Fprintf(w, "  tailscale  unknown\n")
		return
	}
	if ts.Owner != "" {
		fmt.Fprintf(w, "  tailscale  %s as %s\n", ts.State, ts.Owner)
	} else {
		fmt.Fprintf(w, "  tailscale  %s\n", ts.State)
	}
	if ts.DNSName != "" {
		fmt.Fprintf(w, "  hostname   %s\n", ts.DNSName)
	}
	if ts.AuthURL != "" {
		fmt.Fprintf(w, "  login URL  %s\n", ts.AuthURL)
	}
}
