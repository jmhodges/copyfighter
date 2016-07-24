package main

import (
	"bytes"
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

func TestGoPackageRange(t *testing.T) {
	sites, fset, err := check("github.com/jmhodges/copyfighter/testdata/...", 16, 8, 8)
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
