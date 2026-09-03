package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI builds the binary and runs it the way a user would, checking the
// exit codes documented by main: 0 for a clean package, 2 when copy sites were
// found, and 1 for usage and other errors.
func TestCLI(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not found in PATH")
	}
	bin := filepath.Join(t.TempDir(), "copyfighter")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %s\n%s", err, out)
	}

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "copy sites found",
			args:       []string{"./testdata"},
			wantCode:   2,
			wantStdout: defaultGoldenData,
		},
		{
			name:     "flags before the package narrow the results",
			args:     []string{"-max", "32", "./testdata"},
			wantCode: 2,
			wantStdout: `testdata/inner.go:24:6: parameter 'f' at index 0 should be made into a pointer (func CallsFoo(f Foo))
testdata/inner.go:28:14: receiver should be made into a pointer (func (Foo).OnOtherToo(o other))
testdata/inner.go:59:6: parameter 'c' at index 0 should be made into a pointer (func Configure(c config))
testdata/inner.go:63:17: receiver should be made into a pointer (func (config).Validate())
`,
		},
		{
			name:     "32-bit word size",
			args:     []string{"-max", "24", "-wordSize", "4", "-maxAlign", "4", "./testdata"},
			wantCode: 2,
			wantStdout: `testdata/inner.go:24:6: parameter 'f' at index 0 should be made into a pointer (func CallsFoo(f Foo))
testdata/inner.go:28:14: receiver should be made into a pointer (func (Foo).OnOtherToo(o other))
testdata/inner.go:59:6: parameter 'c' at index 0 should be made into a pointer (func Configure(c config))
testdata/inner.go:63:17: receiver should be made into a pointer (func (config).Validate())
`,
		},
		{
			name:       "clean package exits zero with no output",
			args:       []string{"-max", "1000000", "./testdata"},
			wantCode:   0,
			wantStdout: "",
		},
		{
			name:       "no arguments is a usage error",
			args:       nil,
			wantCode:   1,
			wantStderr: "usage:",
		},
		{
			name:       "too many arguments is a usage error",
			args:       []string{"./testdata", "./testdata"},
			wantCode:   1,
			wantStderr: "usage:",
		},
		{
			name:       "unknown package",
			args:       []string{"no/such/package/anywhere"},
			wantCode:   1,
			wantStderr: `unable to find packages matching "no/such/package/anywhere"`,
		},
		{
			name:       "unknown flag",
			args:       []string{"-bogus", "./testdata"},
			wantCode:   2,
			wantStderr: "flag provided but not defined: -bogus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bin, tt.args...)
			stderr := &strings.Builder{}
			cmd.Stderr = stderr
			stdout, err := cmd.Output()
			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("running %s: %s", bin, err)
			}
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
			if string(stdout) != tt.wantStdout {
				t.Errorf("stdout mismatch, want:\n%s\n=============\ngot:\n%s", tt.wantStdout, stdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr %q does not contain %q", stderr, tt.wantStderr)
			}
		})
	}
}
