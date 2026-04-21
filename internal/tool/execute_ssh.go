package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// sshTarget is the remote SSH endpoint parsed from a session's ssh_address.
type sshTarget struct {
	Host     string
	Port     int
	User     string
	Password string
}

// sshResult is the captured result of a remote command execution.
type sshResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// sshClient wraps an ssh.Client with a run method that executes commands in a
// forced-interactive login bash shell over a fresh session.
type sshClient struct {
	client *ssh.Client
}

const sshHandshakeTimeout = 15 * time.Second

// dialSSH opens an SSH connection to the target, trying auth methods in order:
// SSH agent (via SSH_AUTH_SOCK) → default key files (~/.ssh/id_ed25519,
// id_ecdsa, id_rsa, unencrypted only) → password. Host key verification is
// skipped — sessions are ephemeral, matching the UI terminal's behavior.
func dialSSH(ctx context.Context, t sshTarget) (*sshClient, error) {
	if t.Host == "" {
		return nil, fmt.Errorf("ssh target: host is empty")
	}
	if t.User == "" {
		return nil, fmt.Errorf("ssh target: user is empty")
	}
	if t.Port == 0 {
		t.Port = 22
	}

	methods := sshAuthMethods(t.Password)
	if len(methods) == 0 {
		return nil, fmt.Errorf("ssh target: no auth methods available (no agent, no default keys, no password)")
	}

	cfg := &ssh.ClientConfig{
		User:            t.User,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // sessions are ephemeral, matches UI terminal behavior
		Timeout:         sshHandshakeTimeout,
	}

	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))

	d := &net.Dialer{Timeout: sshHandshakeTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	handshakeDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-handshakeDone:
		}
	}()
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	close(handshakeDone)
	if err != nil {
		return nil, fmt.Errorf("ssh handshake %s: %w", addr, err)
	}
	return &sshClient{client: ssh.NewClient(sshConn, chans, reqs)}, nil
}

// Close tears down the SSH connection.
func (c *sshClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// run executes userCmd in a forced-interactive login bash shell on a fresh SSH
// session, without allocating a PTY. stdout and stderr are captured separately.
// Context cancellation propagates by closing the session mid-run.
//
// The `-i -l` combination is required because RDE warmup scripts commonly write
// PATH to ~/.bashrc, and .bashrc short-circuits on its `case $- in *i*)` guard
// when the shell is non-interactive. `-i` forces $- to contain 'i' so the guard
// passes; `-l` ensures /etc/profile and ~/.bash_profile/~/.profile are sourced.
//
// ExitCode is 0 on success, the command's exit status on a clean non-zero
// exit, or -1 if the session was terminated by a signal or cancellation.
func (c *sshClient) run(ctx context.Context, userCmd string) (sshResult, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return sshResult{}, fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-done:
		}
	}()
	defer close(done)

	runErr := session.Run(buildLoginShellCmd(userCmd))

	result := sshResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}

	if runErr == nil {
		return result, nil
	}

	if ctx.Err() != nil {
		result.ExitCode = -1
		return result, fmt.Errorf("ssh run cancelled: %w", ctx.Err())
	}

	var exitErr *ssh.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitStatus()
		return result, nil
	}

	result.ExitCode = -1
	return result, fmt.Errorf("ssh run: %w", runErr)
}

// buildLoginShellCmd wraps userCmd as `bash -i -l -c '<escaped>'`. See run()
// for why -i -l is required.
func buildLoginShellCmd(userCmd string) string {
	return "bash -i -l -c '" + strings.ReplaceAll(userCmd, "'", `'\''`) + "'"
}

// sshAuthMethods builds the auth-method chain for dialSSH.
func sshAuthMethods(password string) []ssh.AuthMethod {
	var methods []ssh.AuthMethod
	if m := agentAuthMethod(); m != nil {
		methods = append(methods, m)
	}
	if m := defaultKeyFilesAuthMethod(); m != nil {
		methods = append(methods, m)
	}
	if password != "" {
		methods = append(methods, ssh.Password(password))
	}
	return methods
}

func agentAuthMethod() ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	return ssh.PublicKeysCallback(agent.NewClient(conn).Signers)
}

func defaultKeyFilesAuthMethod() ssh.AuthMethod {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var signers []ssh.Signer
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		data, err := os.ReadFile(filepath.Join(home, ".ssh", name))
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			// Skip encrypted or malformed keys silently — we don't prompt
			// for passphrases in an MCP-server context.
			continue
		}
		signers = append(signers, signer)
	}
	if len(signers) == 0 {
		return nil
	}
	return ssh.PublicKeys(signers...)
}

var (
	userHostRegex     = regexp.MustCompile(`([\w.\-]+)@([\w.\-]+)`)
	sshAddrPortRegex  = regexp.MustCompile(`-p\s+(\d+)`)
	bareHostPortRegex = regexp.MustCompile(`^([\w.\-]+)(?::(\d+))?$`)
)

// parseSSHAddress extracts user, host, and port from a backend-provided
// ssh_address, which may be a full command like
//
//	ssh -o StrictHostKeyChecking=no ubuntu@host.example -p 27823
//
// or a bare "host:port". Returns an error if no user is present — macOS
// sessions run as `vagrant` and Linux sessions as `ubuntu`, so a silent
// fallback would misroute half the platforms.
func parseSSHAddress(addr string) (sshTarget, error) {
	if m := userHostRegex.FindStringSubmatch(addr); m != nil {
		t := sshTarget{User: m[1], Host: m[2], Port: 22}
		if pm := sshAddrPortRegex.FindStringSubmatch(addr); pm != nil {
			p, err := strconv.Atoi(pm[1])
			if err != nil {
				return sshTarget{}, fmt.Errorf("ssh address %q: invalid port %q: %w", addr, pm[1], err)
			}
			t.Port = p
		}
		return t, nil
	}
	if bareHostPortRegex.MatchString(addr) {
		return sshTarget{}, fmt.Errorf("ssh address %q has no user component; cannot determine remote account", addr)
	}
	return sshTarget{}, fmt.Errorf("unable to parse ssh address: %q", addr)
}
