package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
// forced-interactive login bash shell over a fresh session. If a local SSH
// agent was available at dial time, each session gets agent forwarding so the
// remote command (e.g. `git push git@github.com:...`) can authenticate with
// the caller's local SSH keys.
type sshClient struct {
	client      *ssh.Client
	localAgent  agent.ExtendedAgent // nil when SSH_AUTH_SOCK was unset/unusable
	agentSocket io.Closer           // underlying unix socket conn; nil when no agent
}

const sshHandshakeTimeout = 15 * time.Second

// dialSSH opens an SSH connection to the target, trying auth methods in order:
// SSH agent (via SSH_AUTH_SOCK) → default key files (~/.ssh/id_ed25519,
// id_ecdsa, id_rsa, unencrypted only) → password. Host key verification is
// skipped — sessions are ephemeral, matching the UI terminal's behavior.
//
// When a local SSH agent is available, it is wired up for agent forwarding on
// the returned client so the remote session can authenticate outbound SSH
// (e.g. git@github.com) with the caller's keys.
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

	localAgent, agentSocket := dialLocalAgent()

	methods := sshAuthMethods(t.Password, localAgent)
	if len(methods) == 0 {
		if agentSocket != nil {
			_ = agentSocket.Close()
		}
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
		if agentSocket != nil {
			_ = agentSocket.Close()
		}
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
		if agentSocket != nil {
			_ = agentSocket.Close()
		}
		return nil, fmt.Errorf("ssh handshake %s: %w", addr, err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)

	// Install an agent-forwarding handler on the client. This is a no-op
	// until a session later calls RequestAgentForwarding. Installing it here
	// (once per client) is the documented pattern in x/crypto/ssh/agent.
	if localAgent != nil {
		if err := agent.ForwardToAgent(client, localAgent); err != nil {
			_ = client.Close()
			if agentSocket != nil {
				_ = agentSocket.Close()
			}
			return nil, fmt.Errorf("ssh install agent forwarding: %w", err)
		}
	}

	return &sshClient{
		client:      client,
		localAgent:  localAgent,
		agentSocket: agentSocket,
	}, nil
}

// Close tears down the SSH connection and the local agent socket (if any).
func (c *sshClient) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.client != nil {
		if err := c.client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.agentSocket != nil {
		if err := c.agentSocket.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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

	// Best-effort agent forwarding per session. If the remote sshd refuses
	// (AllowAgentForwarding=no) or the request fails for any other reason,
	// the user's command proceeds without a forwarded agent — any git-over-
	// SSH step will then fail with an auth error, which surfaces clearly
	// through stderr/exit_code. We intentionally don't fail the whole
	// execute call here.
	if c.localAgent != nil {
		_ = agent.RequestAgentForwarding(session)
	}

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
//
// Password is tried FIRST. Session sshd is password-authenticated by default
// (see backend/internal/api/session_terminal.go:209), so password auth almost
// always succeeds on the first attempt. Trying publickey methods first would
// risk exhausting sshd's MaxAuthTries (default 6) when the user's local agent
// holds many keys that aren't authorized on the session — none of them would
// authenticate, and we'd fail before reaching the password that works.
//
// Agent and default key files remain as fallbacks for the rare case of a
// session where the user manually installed a key. The agent is passed in
// from dialSSH so the same connection is reused for agent forwarding.
func sshAuthMethods(password string, a agent.ExtendedAgent) []ssh.AuthMethod {
	var methods []ssh.AuthMethod
	if password != "" {
		methods = append(methods, ssh.Password(password))
	}
	if a != nil {
		methods = append(methods, ssh.PublicKeysCallback(a.Signers))
	}
	if m := defaultKeyFilesAuthMethod(); m != nil {
		methods = append(methods, m)
	}
	return methods
}

// dialLocalAgent connects to the local SSH agent via SSH_AUTH_SOCK. Returns
// (nil, nil) if the env var is unset or the socket is not dialable — the
// caller treats that as "no agent available" and skips forwarding.
func dialLocalAgent() (agent.ExtendedAgent, io.Closer) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil
	}
	return agent.NewClient(conn), conn
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
