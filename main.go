package main

import (
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

var (
	pkgs            = make(map[string]*build.Package)
	erroredPkgs     = make(map[string]bool)
	ids             = make(map[string]string)
	module          = ""
	cwd             = ""
	requiredModules = make([]string, 0)

	ignoreModFile = flag.Bool("mod", true, "use the mod file")
	stopOnError   = flag.Bool("stoponerror", true, "stop on package import errors")
	horizontal    = flag.Bool("horizontal", false, "lay out the dependency graph horizontally instead of vertically")
	withTests     = flag.Bool("withtests", false, "include test packages")
	maxLevel      = flag.Int("maxlevel", 256, "max level of go dependency graph")

	buildContext = build.Default
)

func init() {
	flag.BoolVar(ignoreModFile, "m", true, "ignore the package in mod file")
	flag.BoolVar(withTests, "t", false, "(alias for -withtests) include test packages")
	flag.IntVar(maxLevel, "l", 256, "(alias for -maxlevel) maximum level of the go dependency graph")
}

func mustGetCwd() string {
	current, err := os.Getwd()
	if err != nil {
		die(err, "failed to get current dir")
	}
	return current
}

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		log.Fatal("need one package name to process")
	}
	cwd = mustGetCwd()
	module, requiredModules = mustParseModFile()
	for _, a := range args {
		if err := processPackage(cwd, a, 0, "", *stopOnError); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Println("digraph godep {")
	if *horizontal {
		fmt.Println(`rankdir="LR"`)
	}
	fmt.Print(`splines=spline
nodesep=0.4
ranksep=0.8
node [shape="box",style="rounded,filled"]
edge [arrowsize="0.5"]
`)

	// sort packages
	pkgKeys := []string{}
	for k := range pkgs {
		pkgKeys = append(pkgKeys, k)
	}
	sort.Strings(pkgKeys)

	for _, pkgName := range pkgKeys {
		pkg := pkgs[pkgName]
		pkgId := getId(pkgName)

		if isIgnored(pkg) {
			continue
		}

		color := nodeColor(pkg)
		fmt.Printf("%s [label=\"%s\" color=\"%s\" target=\"_blank\"];\n", pkgId, pkgName, color)

		for _, imp := range getImports(pkg) {
			impPkg := pkgs[imp]
			if impPkg == nil || isIgnored(impPkg) {
				continue
			}

			impId := getId(imp)
			fmt.Printf("%s -> %s;\n", pkgId, impId)
		}
	}
	fmt.Println("}")
}

func nodeColor(pkg *build.Package) string {

	var color string
	switch {
	case pkg.Goroot:
		color = "palegreen"
	case len(pkg.CgoFiles) > 0:
		color = "darkgoldenrod1"
	case isInModFile(pkg.ImportPath):
		color = "palegoldenrod"
	case hasBuildErrors(pkg):
		color = "red"
	default:
		color = "paleturquoise"
	}
	return color
}

func processPackage(root string, pkgName string, level int, importedBy string, stopOnError bool) error {
	if level++; level > *maxLevel {
		return nil
	}

	pkg, buildErr := buildContext.Import(pkgName, root, 0)
	if buildErr != nil {
		if stopOnError {
			return fmt.Errorf("failed to import %s (imported at level %d by %s):\n%s", pkgName, level, importedBy, buildErr)
		}
	}

	if isIgnored(pkg) {
		return nil
	}

	importPath := pkgName
	if buildErr != nil {
		erroredPkgs[importPath] = true
	}

	pkgs[importPath] = pkg

	for _, imp := range getImports(pkg) {
		if _, ok := pkgs[imp]; !ok {
			if err := processPackage(pkg.Dir, imp, level, pkgName, stopOnError); err != nil {
				return err
			}
		}
	}
	return nil
}

func getImports(pkg *build.Package) []string {
	allImports := pkg.Imports
	if *withTests {
		allImports = append(allImports, pkg.TestImports...)
		allImports = append(allImports, pkg.XTestImports...)
	}
	var imports []string
	found := make(map[string]struct{})
	for _, imp := range allImports {
		if imp == pkg.ImportPath {
			// Don't draw a self-reference when foo_test depends on foo.
			continue
		}
		if _, ok := found[imp]; ok {
			continue
		}
		found[imp] = struct{}{}
		imports = append(imports, imp)
	}
	return imports
}

func deriveNodeID(packageName string) string {
	//TODO: improve implementation?
	id := "\"" + packageName + "\""
	return id
}

func getId(name string) string {
	id, ok := ids[name]
	if !ok {
		id = deriveNodeID(name)
		ids[name] = id
	}
	return id
}

func isIgnored(pkg *build.Package) bool {
	if isInModFile(getId(pkg.ImportPath)) {
		return true
	}
	return pkg.Goroot
	// return pkg.ImportPath || (pkg.Goroot && *ignoreStdlib) || hasPrefixes(pkg.ImportPath, ignoredPrefixes)]
}

func hasBuildErrors(pkg *build.Package) bool {
	if len(erroredPkgs) == 0 {
		return false
	}

	v, ok := erroredPkgs[pkg.ImportPath]
	if !ok {
		return false
	}
	return v
}

func isInModFile(path string) bool {
	for _, p := range requiredModules {
		if strings.Contains(path, p) {
			return true
		}
	}
	return false
}

func die(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func mustParseModFile() (module string, required []string) {
	cwd, err := os.Getwd()
	if err != nil {
		die(err, "cannot get current dir")
	}
	file := filepath.Join(cwd, "go.mod")
	data, err := os.ReadFile(file)
	if err != nil {
		die(err, "cannot read go.mod")
	}

	modFile, err := modfile.Parse(file, data, nil)
	if err != nil {
		die(err, "failed to parse mod file")
	}
	module = modFile.Module.Mod.Path
	for _, r := range modFile.Require {
		required = append(required, r.Mod.Path)
	}
	return
}
