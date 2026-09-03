package main

import (
	"bytes"
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenPath(t *testing.T) {
	sites, fset, err := check("./testdata", 16, 8, 8)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	b := &bytes.Buffer{}
	printSites(sites, fset, b)
	actual := string(b.Bytes())
	if goldenData != actual {
		t.Errorf("output doesn't match, want:\n%s\n=============\ngot:\n%s", goldenData, actual)
	}
}

const goldenData = `testdata/inner.go:24:6: parameter 'f' at index 0 should be made into a pointer (func CallsFoo(f Foo))
testdata/inner.go:28:14: receiver, and parameter 'o' at index 0 should be made into pointers (func (Foo).OnOtherToo(o other))
testdata/inner.go:32:16: receiver should be made into a pointer (func (other).OnStruct())
testdata/inner.go:35:16: receiver should be made into a pointer (func (other).OnStruct2())
`

// The sites come out of checkPkg in map iteration order, so printSites has to
// sort them itself for the output to be stable.
func TestPrintSitesSortsByPosition(t *testing.T) {
	sites, fset, err := check("./testdata", 16, 8, 8)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(sites) < 2 {
		t.Fatalf("need at least two sites to test ordering, got %d", len(sites))
	}
	// Put them in the reverse of the expected order.
	b := &bytes.Buffer{}
	printSites(sites, fset, b)
	for i, j := 0, len(sites)-1; i < j; i, j = i+1, j-1 {
		sites[i], sites[j] = sites[j], sites[i]
	}
	b.Reset()
	printSites(sites, fset, b)
	if actual := b.String(); actual != goldenData {
		t.Errorf("output not sorted, want:\n%s\n=============\ngot:\n%s", goldenData, actual)
	}
}

// In testdata, Foo is an http.Client (hundreds of bytes) and other is exactly
// 32 bytes on a 64-bit word size: an int64, a pointer, and an interface.
func TestMaxStructWidth(t *testing.T) {
	tests := []struct {
		name string
		max  int64
		want string
	}{
		{
			name: "default flags everything wider than two words",
			max:  16,
			want: goldenData,
		},
		{
			name: "struct exactly at the limit is not flagged",
			max:  32,
			want: `testdata/inner.go:24:6: parameter 'f' at index 0 should be made into a pointer (func CallsFoo(f Foo))
testdata/inner.go:28:14: receiver should be made into a pointer (func (Foo).OnOtherToo(o other))
`,
		},
		{
			name: "struct one byte below the limit is flagged",
			max:  31,
			want: goldenData,
		},
		{
			name: "huge limit flags nothing",
			max:  1 << 20,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, fset, err := check("./testdata", tt.max, 8, 8)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			b := &bytes.Buffer{}
			printSites(sites, fset, b)
			if actual := b.String(); actual != tt.want {
				t.Errorf("max=%d: want:\n%s\n=============\ngot:\n%s", tt.max, tt.want, actual)
			}
		})
	}
}

// With 4 byte words the other struct in testdata shrinks from 32 bytes to 20
// (int64 + pointer + interface), so a limit of 24 bytes flags it on a 64-bit
// word size but not on a 32-bit one.
func TestWordSize(t *testing.T) {
	tests := []struct {
		name               string
		wordSize, maxAlign int64
		want               string
	}{
		{
			name:     "64-bit",
			wordSize: 8,
			maxAlign: 8,
			want:     goldenData,
		},
		{
			name:     "32-bit",
			wordSize: 4,
			maxAlign: 4,
			want: `testdata/inner.go:24:6: parameter 'f' at index 0 should be made into a pointer (func CallsFoo(f Foo))
testdata/inner.go:28:14: receiver should be made into a pointer (func (Foo).OnOtherToo(o other))
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, fset, err := check("./testdata", 24, tt.wordSize, tt.maxAlign)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			b := &bytes.Buffer{}
			printSites(sites, fset, b)
			if actual := b.String(); actual != tt.want {
				t.Errorf("want:\n%s\n=============\ngot:\n%s", tt.want, actual)
			}
		})
	}
}

// checkSource writes src to a fresh directory as a single Go file, runs check
// over it with the default flags, and returns the printed output with the
// temporary file path replaced by "src.go".
func checkSource(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "src.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sites, fset, err := check(dir, 16, 8, 8)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	b := &bytes.Buffer{}
	printSites(sites, fset, b)
	return strings.ReplaceAll(b.String(), path, "src.go")
}

func TestCheckSites(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "pointers are never flagged",
			src: `package p
type big struct{ a, b, c int64 }
func (b *big) M(o *big) *big { return o }
func F(o *big) *big { return o }
`,
			want: "",
		},
		{
			name: "narrow structs are not flagged",
			src: `package p
type small struct{ a, b int64 }
func (s small) M(o small) small { return o }
`,
			want: "",
		},
		{
			name: "struct one byte over the limit is flagged after padding",
			src: `package p
type s16 struct{ a, b int64 }
type s17 struct{ a, b int64; c byte }
func F(x s16, y s17) {}
`,
			want: "src.go:4:6: parameter 'y' at index 1 should be made into a pointer (func F(x s16, y s17))\n",
		},
		{
			name: "unnamed parameter and return values",
			src: `package p
type big struct{ a, b, c int64 }
func F(big) (int, big) { return 0, big{} }
`,
			want: "src.go:3:6: parameter at index 0, and return value 'big' at index 1 should be made into pointers (func F(big) (int, big))\n",
		},
		{
			name: "receiver, parameter, and return value all flagged",
			src: `package p
type big struct{ a, b, c int64 }
func (b big) M(x int, o big) big { return o }
`,
			want: "src.go:3:14: receiver, parameter 'o' at index 1, and return value 'big' at index 0 should be made into pointers (func (big).M(x int, o big) big)\n",
		},
		{
			name: "embedded struct counts toward width",
			src: `package p
type inner struct{ a, b int64 }
type outer struct{ inner; c int64 }
func F(i inner, o outer) {}
`,
			want: "src.go:4:6: parameter 'o' at index 1 should be made into a pointer (func F(i inner, o outer))\n",
		},
		{
			name: "named type whose underlying type is a wide struct from another package",
			src: `package p
import "net/http"
type client http.Client
func F(c client) {}
`,
			want: "src.go:4:6: parameter 'c' at index 0 should be made into a pointer (func F(c client))\n",
		},
		{
			name: "interface method signatures are checked",
			src: `package p
type big struct{ a, b, c int64 }
type I interface{ M(b big) }
`,
			want: "src.go:3:19: parameter 'b' at index 0 should be made into a pointer (func (I).M(b big))\n",
		},
		{
			name: "non-struct named types are not flagged",
			src: `package p
type arr [64]byte
type str string
func F(a arr, s str) arr { return a }
`,
			want: "",
		},
		{
			name: "sites in one file are ordered by line",
			src: `package p
type big struct{ a, b, c int64 }
func (b big) Z() {}
func (b big) A() {}
func Y(b big) {}
`,
			want: `src.go:3:14: receiver should be made into a pointer (func (big).Z())
src.go:4:14: receiver should be made into a pointer (func (big).A())
src.go:5:6: parameter 'b' at index 0 should be made into a pointer (func Y(b big))
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := checkSource(t, tt.src); actual != tt.want {
				t.Errorf("want:\n%s\n=============\ngot:\n%s", tt.want, actual)
			}
		})
	}
}

// Files excluded by build constraints must not be parsed or type checked, or a
// package could never be checked on a platform it isn't targeting.
func TestCheckSkipsFilesExcludedByBuildConstraints(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"good.go": `package p
type big struct{ a, b, c int64 }
func F(b big) {}
`,
		// An explicit ignore tag with a wide struct func and a type error.
		"ignored.go": `//go:build ignore

package p
type huge struct{ a, b, c, d int64 }
func G(h huge) {}
var broken int = "not an int"
`,
		// A file for an OS nothing runs on.
		"other_plan9.go": `package p
type huge2 struct{ a, b, c, d int64 }
func H(h huge2) {}
`,
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sites, fset, err := check(dir, 16, 8, 8)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	b := &bytes.Buffer{}
	printSites(sites, fset, b)
	want := filepath.Join(dir, "good.go") + ":3:6: parameter 'b' at index 0 should be made into a pointer (func F(b big))\n"
	if actual := b.String(); actual != want {
		t.Errorf("want:\n%s\n=============\ngot:\n%s", want, actual)
	}
}

func TestCheckErrors(t *testing.T) {
	writeFiles := func(t *testing.T, files map[string]string) string {
		t.Helper()
		dir := t.TempDir()
		for name, src := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	tests := []struct {
		name string
		path func(t *testing.T) string
		want string
	}{
		{
			name: "path that is neither a directory nor an import path",
			path: func(t *testing.T) string {
				return "this/import/path/does/not/exist/anywhere"
			},
			want: `unable to find packages matching "this/import/path/does/not/exist/anywhere"`,
		},
		{
			name: "path is a file rather than a directory",
			path: func(t *testing.T) string {
				dir := writeFiles(t, map[string]string{"a.go": "package a\n"})
				return filepath.Join(dir, "a.go")
			},
			want: "is not a directory",
		},
		{
			name: "directory with no Go files",
			path: func(t *testing.T) string {
				return writeFiles(t, map[string]string{"README": "nothing to see"})
			},
			want: "unable to parse package",
		},
		{
			name: "directory with two packages",
			path: func(t *testing.T) string {
				return writeFiles(t, map[string]string{
					"a.go": "package a\n",
					"b.go": "package b\n",
				})
			},
			want: "unable to parse package",
		},
		{
			name: "directory with an external test package",
			path: func(t *testing.T) string {
				return writeFiles(t, map[string]string{
					"a.go":      "package a\n",
					"a_test.go": "package a_test\n",
				})
			},
			want: "more than one package found",
		},
		{
			name: "package that does not type check",
			path: func(t *testing.T) string {
				return writeFiles(t, map[string]string{
					"a.go": "package a\n\nvar x int = \"not an int\"\n",
				})
			},
			want: `unable to type check package "a"`,
		},
		{
			name: "package with a syntax error",
			path: func(t *testing.T) string {
				return writeFiles(t, map[string]string{
					"a.go": "package a\n\nfunc F( {\n",
				})
			},
			want: "unable to parse package",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, _, err := check(tt.path(t), 16, 8, 8)
			if err == nil {
				t.Fatalf("expected an error, got %d sites", len(sites))
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// Import paths are resolved by walking GOPATH, and a trailing "..." matches
// the named package and everything beneath it.
func TestCheckImportPath(t *testing.T) {
	gopath := t.TempDir()
	// parseGoPkg copies build.Default when it runs, so swapping GOPATH here
	// takes effect.
	origGOPATH := build.Default.GOPATH
	build.Default.GOPATH = gopath
	t.Cleanup(func() { build.Default.GOPATH = origGOPATH })

	const root = "copyfightertest/pattern"
	files := map[string]string{
		root + "/a.go": `package pattern
type big struct{ a, b, c int64 }
func Top(b big) {}
`,
		root + "/sub/b.go": `package sub
type big struct{ a, b, c int64 }
func Nested(b big) {}
`,
		// Directories named testdata, or starting with . or _, are skipped.
		root + "/testdata/c.go": `package testdata
type big struct{ a, b, c int64 }
func Skipped(b big) {}
`,
		root + "/_hidden/d.go": `package hidden
type big struct{ a, b, c int64 }
func Skipped(b big) {}
`,
		// A directory with no Go files is silently skipped rather than an error.
		root + "/docs/README": "no go here\n",
	}
	for name, src := range files {
		path := filepath.Join(gopath, "src", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	topSite := filepath.Join(gopath, "src", root, "a.go") + ":3:6: parameter 'b' at index 0 should be made into a pointer (func Top(b big))\n"
	nestedSite := filepath.Join(gopath, "src", root, "sub", "b.go") + ":3:6: parameter 'b' at index 0 should be made into a pointer (func Nested(b big))\n"

	tests := []struct {
		pkg  string
		want string
	}{
		{root, topSite},
		{root + "/sub", nestedSite},
		{root + "/...", topSite + nestedSite},
		{"copyfightertest/...", topSite + nestedSite},
		{"copyfightertest/pat...", topSite + nestedSite},
	}
	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			sites, fset, err := check(tt.pkg, 16, 8, 8)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			b := &bytes.Buffer{}
			printSites(sites, fset, b)
			if actual := b.String(); actual != tt.want {
				t.Errorf("want:\n%s\n=============\ngot:\n%s", tt.want, actual)
			}
		})
	}

	for _, pkg := range []string{root + "/testdata", root + "/_hidden", root + "/docs", root + "/nope/..."} {
		t.Run("no match for "+pkg, func(t *testing.T) {
			_, _, err := check(pkg, 16, 8, 8)
			if err == nil || !strings.Contains(err.Error(), "unable to find packages matching") {
				t.Errorf("want an unable to find packages error, got %v", err)
			}
		})
	}
}

func TestPathToRegexp(t *testing.T) {
	tests := []struct {
		pattern string
		matches []string
		misses  []string
	}{
		{
			pattern: "foo/bar",
			matches: []string{"foo/bar"},
			misses:  []string{"foo", "foo/bar/baz", "foo/barx", "xfoo/bar", "foo/ba"},
		},
		{
			pattern: "foo/...",
			matches: []string{"foo", "foo/bar", "foo/bar/baz"},
			misses:  []string{"foobar", "fo", "bar/foo"},
		},
		{
			pattern: "foo/b...",
			matches: []string{"foo/b", "foo/bar", "foo/bar/baz"},
			misses:  []string{"foo", "foo/car"},
		},
		{
			pattern: "foo/.../baz",
			matches: []string{"foo/bar/baz", "foo/a/b/baz", "foo//baz"},
			misses:  []string{"foo/baz", "foo/bar/baz/qux"},
		},
		{
			// Regexp metacharacters in the path are literal.
			pattern: "foo.bar/v2+",
			matches: []string{"foo.bar/v2+"},
			misses:  []string{"fooxbar/v2+", "foo.bar/v2", "foo.bar/v22"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re := pathToRegexp(tt.pattern)
			for _, m := range tt.matches {
				if !re.MatchString(m) {
					t.Errorf("%q should match %q (regexp %s)", tt.pattern, m, re)
				}
			}
			for _, m := range tt.misses {
				if re.MatchString(m) {
					t.Errorf("%q should not match %q (regexp %s)", tt.pattern, m, re)
				}
			}
		})
	}
}

func TestSentence(t *testing.T) {
	tests := []struct {
		parts []string
		want  string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"receiver"}, "receiver"},
		{[]string{"receiver", "parameter 'o' at index 0"}, "receiver, and parameter 'o' at index 0"},
		{[]string{"a", "b", "c"}, "a, b, and c"},
		{[]string{"a", "b", "c", "d"}, "a, b, c, and d"},
	}
	for _, tt := range tests {
		if actual := sentence(tt.parts); actual != tt.want {
			t.Errorf("sentence(%q) = %q, want %q", tt.parts, actual, tt.want)
		}
	}
}
