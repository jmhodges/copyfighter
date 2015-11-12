package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	maxStructWidth = flag.Int64("max", 16, "maximum size in bytes a struct can be before by-value uses are flagged")
	wordSize       = flag.Int64("wordSize", 8, "word size to assume when calculation struct size")
	maxAlign       = flag.Int64("maxAlign", 8, "maximum word alignment to assume when calculating struct size")
)

func main() {
	log.SetPrefix("")
	log.SetFlags(0)
	flag.Parse()

	if len(os.Args) == 1 {
		log.Fatalf("usage: %s GO_PKG_DIR", os.Args[0])
	}
	p := os.Args[1]

	fset := token.NewFileSet()

	// TODO(#1): support '...' in filesystem dir
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, ".") {
		pkg, err := parsePkgDir(p, fset)
		if err != nil {
			log.Fatal(err)
		}
		sites, err := checkPkg(pkg, fset, *maxStructWidth, *wordSize, *maxAlign)
		if err != nil {
			log.Fatal(err)
		}
		printSitesAndExit(sites, fset)
	} else {
		pkgs, err := parseGoPkg(p, fset)
		if err != nil {
			log.Fatal(err)
		}
		sites := []copySite{}
		for _, pkg := range pkgs {
			s, err := checkPkg(pkg, fset, *maxStructWidth, *wordSize, *maxAlign)
			if err != nil {
				log.Fatal(err)
			}
			sites = append(sites, s...)
		}
		printSitesAndExit(sites, fset)
	}

}

func parsePkgDir(p string, fset *token.FileSet) (*ast.Package, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("unable to stat file %#v: %s", p, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%#v is not a directory", p)
	}

	mp, err := parser.ParseDir(fset, p, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("unable to parse package at %#v: %s", p, err)
	}
	if len(mp) != 1 {
		var ps []string
		for _, pkg := range mp {
			ps = append(ps, pkg.Name)
		}
		return nil, fmt.Errorf("more than one package found in %#v: %s", p, strings.Join(ps, ","))
	}
	var pkg *ast.Package
	for _, v := range mp {
		pkg = v
	}
	return pkg, nil
}

func pathToRegexp(p string) *regexp.Regexp {
	re := regexp.QuoteMeta(p)
	re = strings.Replace(re, `\.\.\.`, `.*`, -1)
	// Special case: foo/... matches foo too.
	if strings.HasSuffix(re, `/.*`) {
		re = re[:len(re)-len(`/.*`)] + `(/.*)?`
	}
	return regexp.MustCompile(`^` + re + `$`)
}

func parseGoPkg(p string, fset *token.FileSet) ([]*ast.Package, error) {
	p = filepath.Clean(p)
	dirs := []string{}
	re := pathToRegexp(p)
	buildContext := build.Default
	for _, src := range buildContext.SrcDirs() {
		src = filepath.Clean(src) + string(filepath.Separator)
		root := src
		filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil || !fi.IsDir() || path == src {
				return nil
			}

			// Avoid .foo, _foo, and testdata directory trees.
			_, elem := filepath.Split(path)
			if strings.HasPrefix(elem, ".") || strings.HasPrefix(elem, "_") || elem == "testdata" {
				return filepath.SkipDir
			}
			name := filepath.ToSlash(path[len(src):])
			if re.MatchString(name) {
				dirs = append(dirs, path)
			}
			return nil
		})
	}

	pkgs := []*ast.Package{}
	for _, d := range dirs {
		_, err := buildContext.ImportDir(d, 0)
		if err != nil {
			if _, noGo := err.(*build.NoGoError); noGo {
				continue
			}
			return nil, fmt.Errorf("unable to build code in %#v: %s", d, err)
		}
		pkg, err := parsePkgDir(d, fset)
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, pkg)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("unable to find packages matching %#v", p)
	}

	return pkgs, nil
}

func checkPkg(pkg *ast.Package, fset *token.FileSet, maxWidth, wordSize, maxAlign int64) ([]copySite, error) {
	sizes := &types.StdSizes{WordSize: wordSize, MaxAlign: maxAlign}
	info := &types.Info{
		// Types is required to prevent duplicates, it seems, in Defs.
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
	}
	conf := &types.Config{
		Importer:                 importer.Default(),
		DisableUnusedImportCheck: true,
		Sizes: sizes,
	}
	files := []*ast.File{}
	for _, f := range pkg.Files {
		files = append(files, f)
	}

	_, err := conf.Check("", fset, files, info)
	if err != nil {
		return nil, fmt.Errorf("unable to type check package %#v: %s", pkg.Name, err)
	}

	wideStructs := make(map[string]*types.TypeName)

	for _, obj := range info.Defs {
		if tn, ok := obj.(*types.TypeName); ok {
			if sizes.Sizeof(tn.Type()) > maxWidth {
				wideStructs[tn.Id()] = tn
			}
		}
	}

	sites := []copySite{}
	for _, obj := range info.Defs {
		if f, ok := obj.(*types.Func); ok {
			s := f.Type().(*types.Signature)
			shouldBe := []string{}
			if s.Recv() != nil {
				// If the func is a method, check the receiver
				rt := s.Recv().Type()
				if isWideStructTyped(rt, wideStructs) {
					shouldBe = append(shouldBe, "receiver")
				}
			}

			params := s.Params()
			for i := 0; i < params.Len(); i++ {
				v := params.At(i)
				if isWideStructTyped(v.Type(), wideStructs) {
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
				if isWideStructTyped(v.Type(), wideStructs) {
					shouldBe = append(shouldBe,
						fmt.Sprintf("return value '%s' at index %d", v.Type(), i))
				}
			}
			if len(shouldBe) > 0 {
				sites = append(sites, copySite{f, shouldBe})
			}
		}
	}
	return sites, nil
}

func printSitesAndExit(sites []copySite, fset *token.FileSet) {
	sort.Sort(sortedCopySites{sites: sites, fset: fset})
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
		fmt.Println("#", sb, msg)
		fmt.Printf("%s\n\n", f)
	}
	if len(sites) > 0 {
		os.Exit(1)
	}
}

type copySite struct {
	fun      *types.Func
	shouldBe []string
}

// sortedCopySites sorts copySites as ordered by the filename, line, and column
// the copySite was created at.
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

	if left.Filename == right.Filename {
		if left.Line == right.Line {
			return left.Column < right.Column
		}
		return left.Line < right.Line
	}

	return left.Filename < right.Filename
}

// isWideStructTyped returns true if the given type is a struct (not a pointer to
// a struct) that is in wideStructs.
func isWideStructTyped(t types.Type, wideStructs map[string]*types.TypeName) bool {
	if recvType, ok := t.(*types.Named); ok {
		_, found := wideStructs[recvType.Obj().Id()]
		return found
	}
	return false
}

// flattenToNamed takes type that is a *types.Pointer or a *types.Named and
// returns the *types.Named given or, if a *types.Pointer was given, the first
// *types.Named type that the Pointer type is referencing. In the latter case,
// if Pointers are pointing to Pointers, all of them will be followed until the
// Named is found. Returns nil if a Named cannot be found. It also returns the
// number of Pointers it had to walk to find the name.
func flattenToNamed(t types.Type) (*types.Named, int) {
	numPtr := 0
	for {
		if named, ok := t.(*types.Named); ok {
			return named, numPtr
		} else if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
			numPtr++
		} else {
			return nil, numPtr
		}
	}
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