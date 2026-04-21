package devenv

import (
	"strings"
	"testing"
)

func TestBuildLoginShellCmd(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"simple":                   {"ls", `bash -i -l -c 'ls'`},
		"empty":                    {"", `bash -i -l -c ''`},
		"spaces":                   {"echo hello world", `bash -i -l -c 'echo hello world'`},
		"single quote inside":      {"echo 'hi'", `bash -i -l -c 'echo '\''hi'\'''`},
		"multiple single quotes":   {"a'b'c", `bash -i -l -c 'a'\''b'\''c'`},
		"double quotes":            {`echo "hi"`, `bash -i -l -c 'echo "hi"'`},
		"dollar sign stays literal": {`echo $PATH`, `bash -i -l -c 'echo $PATH'`},
		"newline embedded":         {"a\nb", "bash -i -l -c 'a\nb'"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := BuildLoginShellCmd(tc.in)
			if got != tc.want {
				t.Fatalf("BuildLoginShellCmd(%q)\n  got:  %s\n  want: %s", tc.in, got, tc.want)
			}
			if !strings.HasPrefix(got, "bash -i -l -c ") {
				t.Fatalf("BuildLoginShellCmd result must start with `bash -i -l -c `, got: %s", got)
			}
		})
	}
}

func TestParseSSHAddress(t *testing.T) {
	cases := map[string]struct {
		addr     string
		wantErr  bool
		wantUser string
		wantHost string
		wantPort int
	}{
		"full ssh command with port": {
			addr:     "ssh -o StrictHostKeyChecking=no ubuntu@5.tcp.ngrok.io -p 27823",
			wantUser: "ubuntu", wantHost: "5.tcp.ngrok.io", wantPort: 27823,
		},
		"full ssh command macos vagrant": {
			addr:     "ssh -o StrictHostKeyChecking=no vagrant@macos-host.example.com -p 22",
			wantUser: "vagrant", wantHost: "macos-host.example.com", wantPort: 22,
		},
		"user@host no port defaults to 22": {
			addr:     "vagrant@5.tcp.ngrok.io",
			wantUser: "vagrant", wantHost: "5.tcp.ngrok.io", wantPort: 22,
		},
		"host:port without user fails": {
			addr:    "5.tcp.ngrok.io:27823",
			wantErr: true,
		},
		"bare host without user fails": {
			addr:    "some-host",
			wantErr: true,
		},
		"garbage fails": {
			addr:    "!!!",
			wantErr: true,
		},
		"empty fails": {
			addr:    "",
			wantErr: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseSSHAddress(tc.addr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseSSHAddress(%q) expected error, got target=%+v", tc.addr, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSSHAddress(%q) unexpected error: %v", tc.addr, err)
			}
			if got.User != tc.wantUser || got.Host != tc.wantHost || got.Port != tc.wantPort {
				t.Fatalf("ParseSSHAddress(%q) = {User:%q Host:%q Port:%d}, want {User:%q Host:%q Port:%d}",
					tc.addr, got.User, got.Host, got.Port, tc.wantUser, tc.wantHost, tc.wantPort)
			}
		})
	}
}

func TestStripBashInteractiveNoise(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"empty": {"", ""},
		"no noise preserved": {
			"build failed\npermission denied\n",
			"build failed\npermission denied\n",
		},
		"both noise lines stripped": {
			"bash: cannot set terminal process group (-1): Inappropriate ioctl for device\nbash: no job control in this shell\n",
			"",
		},
		"noise then real stderr": {
			"bash: cannot set terminal process group (-1): Inappropriate ioctl for device\nbash: no job control in this shell\nreal error here\n",
			"real error here\n",
		},
		"real stderr then noise then more": {
			"first error\nbash: no job control in this shell\nsecond error\n",
			"first error\nsecond error\n",
		},
		"CRLF line endings stripped too": {
			"bash: cannot set terminal process group (-1): Inappropriate ioctl for device\r\nbash: no job control in this shell\r\nactual\r\n",
			"actual\r\n",
		},
		"lookalike in stderr is dropped by design": {
			// We intentionally match as substring. This is fine: the filter runs
			// only on stderr, and a user command that writes the exact phrase to
			// stderr is pathological. stdout is preserved verbatim upstream.
			"echo: no job control in this shell\n",
			"",
		},
		"partial match on same line is dropped": {
			"prefix cannot set terminal process group suffix\n",
			"",
		},
		"wording drift: different phrasing still stripped if substring matches": {
			"bash: cannot set terminal process group (11): some other ioctl text\n",
			"",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(stripBashInteractiveNoise([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("stripBashInteractiveNoise(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestShellSingleQuoteRoundtrip(t *testing.T) {
	// Sanity check: shellSingleQuote output, when handed to a POSIX shell,
	// produces the exact original string. We can't invoke a shell in a unit
	// test reliably, but we can verify the escape contract: the function
	// output, with the outer quotes removed and the `'\''` idiom resolved,
	// matches the input.
	cases := []string{
		"",
		"simple",
		"with space",
		"with 'quote'",
		"multiple ' ' quotes",
		`mixed "quotes" and 'apostrophes'`,
		"newline\nhere",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			quoted := shellSingleQuote(in)
			if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
				t.Fatalf("shellSingleQuote(%q) = %q; not wrapped in single quotes", in, quoted)
			}
			inner := quoted[1 : len(quoted)-1]
			// Reverse the escape: `'\''` → `'`.
			restored := strings.ReplaceAll(inner, `'\''`, "'")
			if restored != in {
				t.Fatalf("shellSingleQuote(%q) roundtrip failed: restored=%q", in, restored)
			}
		})
	}
}
