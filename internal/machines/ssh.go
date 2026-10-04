package machines

import (
	"context"
	"fmt"
	"time"
)

func (m *Manager) sshOptions(connectTimeout bool) []string {
	opts := []string{"-i", m.Config.KeyPath, "-o", "BatchMode=yes"}
	if connectTimeout {
		opts = append(opts, "-o", "ConnectTimeout=5")
	}
	return append(opts, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR")
}

func (m *Manager) SSHArgs(name string, command []string) ([]string, error) {
	if err := m.require(name); err != nil {
		return nil, err
	}
	ip := m.address(name)
	if ip == "" {
		return nil, fmt.Errorf("machine '%s' has no address yet", name)
	}
	args := append([]string{"ssh"}, m.sshOptions(false)...)
	args = append(args, "root@"+ip)
	return append(args, command...), nil
}

func (m *Manager) runSSH(ctx context.Context, timeout time.Duration, name string, command ...string) ([]byte, error) {
	ip := m.address(name)
	if ip == "" {
		return nil, fmt.Errorf("machine '%s' has no address yet", name)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := append(m.sshOptions(true), "root@"+ip)
	return m.Runner.Run(ctx, "ssh", append(args, command...)...)
}
