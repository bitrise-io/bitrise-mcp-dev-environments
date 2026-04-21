package devenv

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

// SSHTarget describes a remote SSH endpoint.
type SSHTarget struct {
	Host     string
	Port     int
	User     string
	Password string
}

// SSHResult is the captured result of a remote command execution.
type SSHResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// SSHClient wraps an ssh.Client with a Run method that executes commands in a
// forced-interactive login bash shell over a fresh session.
type SSHClient struct {
	client *ssh.Client
}

const sshHandshakeTimeout = 15 * time.Second

// Dial establishes an SSH connection to the target. Auth is attempted in order:
// SSH agent (via SSH_AUTH_SOCK) → default key files (~/.ssh/id_ed25519,
// ~/.ssh/id_ecdsa, ~/.ssh/id_rsa, unencrypted only) → password from target.
// Host key verification is skipped — sessions are ephemeral, matching the UI
// terminal's InsecureIgnoreHostKey behavior.
func Dial(ctx context.Context, t SSHTarget) (*SSHClient, error) {
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
	return &SSHClient{client: ssh.NewClient(sshConn, chans, reqs)}, nil
}

// Close tears down the SSH connection.
func (c *SSHClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// Run executes userCmd in a forced-interactive login bash shell on a fresh SSH
// session, without allocating a PTY. stdout and stderr are captured separately.
// The two known bash-without-tty startup diagnostics are stripped from stderr.
// Context cancellation propagates by closing the session mid-run.
//
// ExitCode is 0 on success, the command's exit status on a clean non-zero exit,
// or -1 if the session was terminated by a signal or cancellation.
func (c *SSHClient) Run(ctx context.Context, userCmd string) (SSHResult, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return SSHResult{}, fmt.Errorf("ssh new session: %w", err)
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

	runErr := session.Run(BuildLoginShellCmd(userCmd))

	result := SSHResult{
		Stdout: stdout.Bytes(),
		Stderr: stripBashInteractiveNoise(stderr.Bytes()),
	}

	if runErr == nil {
		return result, nil
	}

	// ctx cancellation wins — even if we also got an ExitError, the command
	// was interrupted, not genuinely run to completion.
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

// BuildLoginShellCmd wraps userCmd so it runs inside a forced-interactive login
// bash shell. The `-i` flag forces `$-` to contain `i`, which is required for
// `.bashrc` to source past its `case $- in *i*) ;; *) return;; esac` guard. The
// `-l` flag makes it a login shell so /etc/profile + ~/.bash_profile/~/.profile
// are also sourced. With no TTY attached, bash emits two harmless startup
// diagnostic lines to stderr; stripBashInteractiveNoise removes them.
func BuildLoginShellCmd(userCmd string) string {
	return "bash -i -l -c " + shellSingleQuote(userCmd)
}

// shellSingleQuote wraps s in POSIX-safe single quotes. Embedded single quotes
// are escaped via the close-escape-reopen idiom: `'\''`.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// bashInteractiveNoisePatterns are the substrings identifying the two bash
// startup lines emitted when -i is used without a TTY.
var bashInteractiveNoisePatterns = []string{
	"cannot set terminal process group",
	"no job control in this shell",
}

// stripBashInteractiveNoise drops the two known bash -i-without-tty startup
// diagnostic lines from b. It is applied only to stderr; stdout is preserved
// verbatim, so legitimate command output containing these phrases is untouched.
// Substring match (not anchored regex) so minor bash wording drift across
// versions still gets filtered.
func stripBashInteractiveNoise(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	lines := bytes.Split(b, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		compareTarget := bytes.TrimRight(line, "\r")
		drop := false
		for _, pat := range bashInteractiveNoisePatterns {
			if bytes.Contains(compareTarget, []byte(pat)) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, line)
		}
	}
	return bytes.Join(out, []byte("\n"))
}

// sshAuthMethods builds the auth-method chain for Dial.
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

// agentAuthMethod returns an auth method backed by SSH_AUTH_SOCK, or nil if
// the env var is unset or the socket is not dialable.
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

// defaultKeyFilesAuthMethod returns an auth method using unencrypted private
// key files in ~/.ssh (id_ed25519, id_ecdsa, id_rsa, in that order), or nil
// if none are present and usable. Encrypted keys are skipped silently — we
// don't prompt for passphrases in an MCP-server context.
func defaultKeyFilesAuthMethod() ssh.AuthMethod {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{"id_ed25519", "id_ecdsa", "id_rsa"}
	var signers []ssh.Signer
	for _, name := range candidates {
		path := filepath.Join(home, ".ssh", name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	if len(signers) == 0 {
		return nil
	}
	return ssh.PublicKeys(signers...)
}

// Regexes for parsing the backend's ssh_address field (ported from
// backend/internal/api/ssh_parse.go).
var (
	userHostRegex     = regexp.MustCompile(`([\w.\-]+)@([\w.\-]+)`)
	sshAddrPortRegex  = regexp.MustCompile(`-p\s+(\d+)`)
	bareHostPortRegex = regexp.MustCompile(`^([\w.\-]+)(?::(\d+))?$`)
)

// ParseSSHAddress extracts user, host, and port from a backend-provided
// ssh_address string. The backend may emit either a full command like
//
//	ssh -o StrictHostKeyChecking=no ubuntu@host.example -p 27823
//
// or a bare "host:port". Returns an error if no user component is present —
// we deliberately refuse to guess a default because macOS sessions run as
// `vagrant` and Linux sessions as `ubuntu`, so a silent fallback would pick
// the wrong account for half the platforms.
func ParseSSHAddress(addr string) (SSHTarget, error) {
	if m := userHostRegex.FindStringSubmatch(addr); m != nil {
		t := SSHTarget{User: m[1], Host: m[2], Port: 22}
		if pm := sshAddrPortRegex.FindStringSubmatch(addr); pm != nil {
			p, err := strconv.Atoi(pm[1])
			if err != nil {
				return SSHTarget{}, fmt.Errorf("ssh address %q: invalid port %q: %w", addr, pm[1], err)
			}
			t.Port = p
		}
		return t, nil
	}
	if bareHostPortRegex.MatchString(addr) {
		return SSHTarget{}, fmt.Errorf("ssh address %q has no user component; cannot determine remote account", addr)
	}
	return SSHTarget{}, fmt.Errorf("unable to parse ssh address: %q", addr)
}
