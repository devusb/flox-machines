package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/devusb/flox-machines/internal/machines"
)

type stubOps struct {
	calls []string
	err   error
}

func (s *stubOps) rec(call string) error {
	s.calls = append(s.calls, call)
	return s.err
}

func (s *stubOps) Create(_ context.Context, name, owner string) error {
	return s.rec("create " + name + " " + owner)
}
func (s *stubOps) Status(_ context.Context, name string) (machines.Status, error) {
	return machines.Status{Name: name}, s.rec("status " + name)
}
func (s *stubOps) Login(_ context.Context, name string) error { return s.rec("login " + name) }
func (s *stubOps) Restart(_ context.Context, name string) error {
	return s.rec("restart " + name)
}
func (s *stubOps) Resize(_ context.Context, name string, mem, vcpu int) error {
	return s.rec("resize " + name)
}
func (s *stubOps) ResizeReset(_ context.Context, name string) error {
	return s.rec("resize-reset " + name)
}
func (s *stubOps) Grow(_ context.Context, name, volume string, size int) error {
	return s.rec(fmt.Sprintf("grow %s %s %d", name, volume, size))
}
func (s *stubOps) Reimage(_ context.Context, name string) error { return s.rec("reimage " + name) }
func (s *stubOps) Destroy(_ context.Context, name string) error { return s.rec("destroy " + name) }
func (s *stubOps) List(_ context.Context) (string, error) {
	return "machine-a: current\n", s.rec("list")
}
func (s *stubOps) GC(_ context.Context) error { return s.rec("gc") }
func (s *stubOps) SSHArgs(name string, command []string) ([]string, error) {
	return append([]string{"ssh", name}, command...), s.rec("ssh " + name)
}

func invoke(args []string, ops *stubOps) (int, string, string, []string) {
	var out, errOut bytes.Buffer
	var execd []string
	code := run(args, ops, func(a []string) error { execd = a; return nil }, &out, &errOut)
	return code, out.String(), errOut.String(), execd
}

const usageText = `Usage: machine <command> [args]

  create <name> [--owner <login>]  create and start a machine
  status <name> --json          report a machine's state as JSON
  login <name>                  start a Tailscale login on a machine
  ssh <name> [command...]       run a command as root on a machine
  restart <name>                restart a machine
  resize <name> <mem-MB> <vcpu> set a per-machine size and restart
  resize <name> --reset         return to the template's size and restart
  grow <name> persist|store <MB> grow a machine's disk and restart
  reimage <name>                wipe the machine's Nix store layer and restart
  destroy <name>                stop and delete a machine and its volumes
  list                          list machines
  gc                            stop all machines, collect host garbage, start them
`

func TestUsageNoArgs(t *testing.T) {
	code, out, _, _ := invoke(nil, &stubOps{})
	if code != 1 || out != usageText {
		t.Errorf("code %d, out %q", code, out)
	}
	code, out, _, _ = invoke([]string{"bogus"}, &stubOps{})
	if code != 1 || out != usageText {
		t.Errorf("unknown command: code %d, out %q", code, out)
	}
}

func TestCreateOwnerFlag(t *testing.T) {
	ops := &stubOps{}
	code, out, _, _ := invoke([]string{"create", "bob", "--owner", "bob@x"}, ops)
	if code != 0 || out != "created machine-bob\n" || ops.calls[0] != "create bob bob@x" {
		t.Errorf("code %d, out %q, calls %v", code, out, ops.calls)
	}
}

func TestCreateBadFlag(t *testing.T) {
	for _, args := range [][]string{{"create", "bob", "--x"}, {"create", "bob", "--owner"}, {"create"}} {
		code, _, errOut, _ := invoke(args, &stubOps{})
		if code != 1 || errOut != "machine: usage: machine create <name> [--owner <login>]\n" {
			t.Errorf("%v: code %d, err %q", args, code, errOut)
		}
	}
}

func TestStatusNeedsJSON(t *testing.T) {
	code, _, errOut, _ := invoke([]string{"status", "a"}, &stubOps{})
	if code != 1 || errOut != "machine: usage: machine status <name> --json\n" {
		t.Errorf("code %d, err %q", code, errOut)
	}
	code, out, _, _ := invoke([]string{"status", "a", "--json"}, &stubOps{})
	if code != 0 || out != `{"name":"a","exists":false}`+"\n" {
		t.Errorf("code %d, out %q", code, out)
	}
}

func TestResizeReset(t *testing.T) {
	ops := &stubOps{}
	if code, _, _, _ := invoke([]string{"resize", "a", "--reset"}, ops); code != 0 || ops.calls[0] != "resize-reset a" {
		t.Errorf("code %d, calls %v", code, ops.calls)
	}
	ops = &stubOps{}
	if code, _, _, _ := invoke([]string{"resize", "a", "2048", "4"}, ops); code != 0 || ops.calls[0] != "resize a" {
		t.Errorf("code %d, calls %v", code, ops.calls)
	}
}

func TestResizeBadArgs(t *testing.T) {
	for _, args := range [][]string{{"resize", "a", "x", "2"}, {"resize", "a", "2048"}, {"resize", "a"}} {
		code, _, errOut, _ := invoke(args, &stubOps{})
		if code != 1 || errOut != "machine: usage: machine resize <name> <mem-MB> <vcpu>\n" {
			t.Errorf("%v: code %d, err %q", args, code, errOut)
		}
	}
}

func TestErrorPrefix(t *testing.T) {
	code, _, errOut, _ := invoke([]string{"restart", "a"}, &stubOps{err: errors.New("no machine 'a'")})
	if code != 1 || errOut != "machine: no machine 'a'\n" {
		t.Errorf("code %d, err %q", code, errOut)
	}
}

func TestExactArgCounts(t *testing.T) {
	for cmd, msg := range map[string]string{
		"login":   "machine: usage: machine login <name>\n",
		"restart": "machine: usage: machine restart <name>\n",
		"reimage": "machine: usage: machine reimage <name>\n",
		"destroy": "machine: usage: machine destroy <name>\n",
	} {
		code, _, errOut, _ := invoke([]string{cmd, "a", "b"}, &stubOps{})
		if code != 1 || errOut != msg {
			t.Errorf("%s: code %d, err %q", cmd, code, errOut)
		}
	}
}

func TestDestroyAndListOutput(t *testing.T) {
	if code, out, _, _ := invoke([]string{"destroy", "a"}, &stubOps{}); code != 0 || out != "destroyed machine-a\n" {
		t.Errorf("destroy: code %d, out %q", code, out)
	}
	if code, out, _, _ := invoke([]string{"list"}, &stubOps{}); code != 0 || out != "machine-a: current\n" {
		t.Errorf("list: code %d, out %q", code, out)
	}
}

func TestSSHExecs(t *testing.T) {
	code, _, _, execd := invoke([]string{"ssh", "a", "cat", "/x"}, &stubOps{})
	if code != 0 || strings.Join(execd, " ") != "ssh a cat /x" {
		t.Errorf("code %d, exec %v", code, execd)
	}
}

func TestGrow(t *testing.T) {
	ops := &stubOps{}
	if code, _, _, _ := invoke([]string{"grow", "a", "store", "4096"}, ops); code != 0 || ops.calls[0] != "grow a store 4096" {
		t.Errorf("code %d, calls %v", code, ops.calls)
	}
	for _, args := range [][]string{{"grow", "a", "store"}, {"grow", "a", "store", "4G"}, {"grow", "a", "store", "1", "2"}} {
		code, _, errOut, _ := invoke(args, &stubOps{})
		if code != 1 || errOut != "machine: usage: machine grow <name> persist|store <MB>\n" {
			t.Errorf("%v: code %d, err %q", args, code, errOut)
		}
	}
}
