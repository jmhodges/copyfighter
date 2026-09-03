package main

import (
	"errors"
	"flag"
	"fmt"
	"go/token"
	"go/types"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

var (
	maxStructWidth = flag.Int64("max", 64, "maximum size in bytes a struct can be before by-value uses are flagged")
	wordSize       = flag.Int64("wordSize", 8, "word size to assume when calculation struct size")
	maxAlign       = flag.Int64("maxAlign", 8, "maximum word alignment to assume when calculating struct size")
)

func main() {
	log.SetPrefix("")
	log.SetFlags(0)
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatalf("usage: %s GO_PKG_DIR_OR_PATTERN", os.Args[0])
	}
	p := flag.Arg(0)
	sites, fset, err := check(p, *maxStructWidth, *wordSize, *maxAlign)
	if err != nil {
		log.Fatal(err)
	}
	printSites(sites, fset, os.Stdout)
	if len(sites) > 0 {
		os.Exit(2)
	}

}

// check loads the packages matching p and reports every function in them that
// uses a struct wider than maxStructWidth bytes without a pointer to it. p is
// anything the go tool accepts as a package pattern: a directory ("./pkg"), an
// import path ("github.com/foo/bar"), or a pattern with wildcards
// ("github.com/foo/bar/..."). A bare name of a directory that exists on disk is
// treated as that directory rather than as an import path.
func check(p string, maxStructWidth, wordSize, maxAlign int64) ([]copySite, *token.FileSet, error) {
	fset := token.NewFileSet()
	pkgs, err := loadPkgs(p, fset)
	if err != nil {
		return nil, nil, err
	}

	sizes := &types.StdSizes{WordSize: wordSize, MaxAlign: maxAlign}
	sites := []copySite{}
	for _, pkg := range pkgs {
		sites = append(sites, checkPkg(pkg, sizes, maxStructWidth)...)
	}
	return sites, fset, nil
}

// loadTarget splits p into the directory to run the go tool in and the
// pattern to give it. When p names a directory on disk, optionally followed by
// "/...", the go tool runs inside that directory so that the module the
// directory belongs to is found even when copyfighter is run from somewhere
// else, and a bare "pkg" works the way it always has instead of being taken
// for an import path. Anything else is an import path pattern, resolved from
// the current directory.
func loadTarget(p string) (dir, pattern string, err error) {
	d, all := strings.CutSuffix(filepath.ToSlash(p), "/...")
	d = filepath.FromSlash(d)
	if d == "" {
		return "", p, nil
	}
	fi, err := os.Stat(d)
	if err != nil {
		if os.IsNotExist(err) {
			return "", p, nil
		}
		return "", "", fmt.Errorf("unable to stat %#v: %s", d, err)
	}
	if !fi.IsDir() {
		return "", "", fmt.Errorf("%#v is not a directory", d)
	}
	if all {
		return d, "./...", nil
	}
	return d, ".", nil
}

// loadPkgs parses and type checks the packages matching p, using the module
// and build context the go tool would use. Dependencies are loaded from their
// compiled export data rather than from source, so only the packages matching
// p are type checked.
func loadPkgs(p string, fset *token.FileSet) ([]*packages.Package, error) {
	dir, pattern, err := loadTarget(p)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  dir,
		Fset: fset,
	}
	if dir != "" && !inModule(dir) {
		// A directory that belongs to no module can still be checked on
		// its own, the way a stray directory of Go files always could be.
		// GOPATH mode is the only way to get the go tool to list one.
		cfg.Env = append(os.Environ(), "GO111MODULE=off")
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("unable to load packages matching %#v: %s", p, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("unable to find packages matching %#v", p)
	}
	for _, pkg := range pkgs {
		if err := pkgError(p, pkg); err != nil {
			return nil, err
		}
	}
	return pkgs, nil
}

// inModule returns true if dir or one of its parents holds a go.mod file.
func inModule(dir string) bool {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// pkgError turns the errors the go tool and type checker reported for pkg
// into one error that says which step went wrong, or returns nil if there were
// none. p is what the user asked to check.
func pkgError(p string, pkg *packages.Package) error {
	var listErrs, compileErrs, parseErrs, typeErrs, otherErrs []error
	for _, pe := range pkg.Errors {
		e := pkgErr(pe)
		switch pe.Kind {
		case packages.ListError:
			// The go tool compiles a package to produce its export data,
			// and reports one that fails to compile with the compiler's
			// output, which starts with "# path". The type checker reports
			// the same problems from source, so those are preferred.
			if strings.HasPrefix(pe.Msg, "# ") {
				compileErrs = append(compileErrs, e)
			} else {
				listErrs = append(listErrs, e)
			}
		case packages.ParseError:
			parseErrs = append(parseErrs, e)
		case packages.TypeError:
			typeErrs = append(typeErrs, e)
		default:
			otherErrs = append(otherErrs, e)
		}
	}
	switch {
	case len(listErrs) > 0 && len(pkg.GoFiles) == 0:
		return fmt.Errorf("unable to find packages matching %#v: %w", p, errors.Join(listErrs...))
	case len(listErrs) > 0:
		return fmt.Errorf("unable to load package %#v: %w", pkg.PkgPath, errors.Join(listErrs...))
	case len(parseErrs) > 0:
		return fmt.Errorf("unable to parse package %#v: %w", pkg.PkgPath, errors.Join(parseErrs...))
	case len(typeErrs) > 0:
		return fmt.Errorf("unable to type check package %#v: %w", pkg.Name, errors.Join(typeErrs...))
	case len(otherErrs) > 0:
		return fmt.Errorf("unable to load package %#v: %w", pkg.PkgPath, errors.Join(otherErrs...))
	case len(compileErrs) > 0:
		return fmt.Errorf("unable to compile package %#v: %w", pkg.PkgPath, errors.Join(compileErrs...))
	}
	return nil
}

// pkgErr returns e as an error, without the "-: " that go/packages puts in
// front of an error that has no position.
func pkgErr(e packages.Error) error {
	if e.Pos == "" || e.Pos == "-" {
		return errors.New(e.Msg)
	}
	return e
}

// checkPkg finds the functions in pkg that use a struct wider than maxWidth,
// as measured by sizes, without a pointer to it.
func checkPkg(pkg *packages.Package, sizes types.Sizes, maxWidth int64) []copySite {
	checker := &wideStructChecker{
		sizes:        sizes,
		maxWidth:     maxWidth,
		localStructs: make(map[*types.TypeName]bool),
	}

	funcs := []*types.Func{}
	for _, obj := range pkg.TypesInfo.Defs {
		if tn, ok := obj.(*types.TypeName); ok {
			if _, ok := tn.Type().Underlying().(*types.Struct); ok {
				checker.localStructs[tn] = true
			}
		}
		if f, ok := obj.(*types.Func); ok {
			funcs = append(funcs, f)
		}
	}

	return findCopySites(funcs, checker)
}

// findCopySites returns a slice of copySites that represent Go function calls
// that use a large struct without a pointer to it.
func findCopySites(funcs []*types.Func, checker *wideStructChecker) []copySite {
	sites := []copySite{}
	for _, f := range funcs {
		s := f.Type().(*types.Signature)
		shouldBe := []string{}

		// If the func is a method, check the receiver
		if s.Recv() != nil {
			rt := s.Recv().Type()
			if checker.isWide(rt) {
				shouldBe = append(shouldBe, "receiver")
			}
		}

		params := s.Params()
		for i := 0; i < params.Len(); i++ {
			v := params.At(i)
			if checker.isWide(v.Type()) {
				name := v.Name()
				parameter := "parameter"
				if name != "" {
					parameter = fmt.Sprintf("parameter '%s'", name)
				}
				shouldBe = append(shouldBe,
					fmt.Sprintf("%s at index %d", parameter, i))
			}
		}

		results := s.Results()
		for i := 0; i < results.Len(); i++ {
			v := results.At(i)
			if checker.isWide(v.Type()) {
				shouldBe = append(shouldBe,
					fmt.Sprintf("return value '%s' at index %d", types.TypeString(v.Type(), qualifier(f.Pkg())), i))
			}
		}
		if len(shouldBe) > 0 {
			sites = append(sites, copySite{f, shouldBe})
		}
	}
	return sites
}

// qualifier returns a types.Qualifier that prints names from pkg bare and
// names from any other package with that package's name, the way they would
// be written in pkg's source.
func qualifier(pkg *types.Package) types.Qualifier {
	return func(other *types.Package) string {
		if other == pkg {
			return ""
		}
		return other.Name()
	}
}

func printSites(sites []copySite, fset *token.FileSet, w io.Writer) {
	sort.Sort(sortedCopySites{sites: sites, fset: fset})
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	for _, site := range sites {
		f := site.fun
		shouldBe := site.shouldBe
		sb := sentence(shouldBe)
		msg := "should be made into"
		if len(shouldBe) > 1 {
			msg += " pointers"
		} else {
			msg += " a pointer"
		}
		position := fset.Position(f.Pos())
		fmt.Fprintf(w, "%s:%d:%d: %s %s (%s)\n", displayPath(cwd, position.Filename), position.Line, position.Column, sb, msg, types.ObjectString(f, qualifier(f.Pkg())))
	}
}

// displayPath returns name relative to cwd when name is inside cwd, and name
// unchanged otherwise. Loaded packages report absolute filenames, and the
// relative form is what a user running the tool from their module expects to
// read.
func displayPath(cwd, name string) string {
	if cwd == "" || !filepath.IsAbs(name) {
		return name
	}
	rel, err := filepath.Rel(cwd, name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return name
	}
	return rel
}

type copySite struct {
	fun      *types.Func
	shouldBe []string
}

// sortedCopySites sorts copySites as ordered by the filename, line, and column
// the func was found at.
type sortedCopySites struct {
	sites []copySite
	fset  *token.FileSet
}

func (s sortedCopySites) Len() int {
	return len(s.sites)
}
func (s sortedCopySites) Swap(i, j int) {
	s.sites[i], s.sites[j] = s.sites[j], s.sites[i]
}

func (s sortedCopySites) Less(i, j int) bool {
	left := s.fset.Position(s.sites[i].fun.Pos())
	right := s.fset.Position(s.sites[j].fun.Pos())

	if left.Filename != right.Filename {
		return left.Filename < right.Filename
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.Column < right.Column
}

// wideStructChecker decides whether a type is a struct defined in the package
// being checked that is too wide to be passed around by value.
type wideStructChecker struct {
	sizes    types.Sizes
	maxWidth int64
	// localStructs holds the named struct types declared in the package being
	// checked. Only those are reported.
	localStructs map[*types.TypeName]bool
}

// isWide returns true if the given type is a struct (not a pointer to a
// struct) declared in the package being checked and wider than maxWidth. An
// alias of such a struct counts too.
func (c *wideStructChecker) isWide(t types.Type) bool {
	// An alias is just another name for its target, so a wide struct is
	// still copied when it is passed under an alias.
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	// For an instantiated generic type, Obj returns the type name of the
	// generic declaration, so this matches G[int] to G.
	if !c.localStructs[named.Obj()] {
		return false
	}
	// A generic struct has no size until it is instantiated with concrete
	// types, and the sizer panics on a type parameter. That rules out the
	// declaration itself and a generic method's receiver, both of which are
	// still written in terms of the struct's type parameters.
	if hasTypeParam(named) {
		return false
	}
	return c.sizes.Sizeof(t) > c.maxWidth
}

// hasTypeParam returns true if computing the size of t would require the size
// of a type parameter. It only descends into the parts of a type that
// contribute to its size: struct fields, array elements, and the underlying
// types of named types. Anything behind a pointer, slice, map, channel,
// function, or interface has a fixed size and is not visited.
func hasTypeParam(t types.Type) bool {
	switch t := t.(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		return hasTypeParam(t.Underlying())
	case *types.Array:
		return hasTypeParam(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if hasTypeParam(t.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

func sentence(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	last := len(parts) - 1
	return strings.Join(parts[:last], ", ") + ", and " + parts[last]
}
