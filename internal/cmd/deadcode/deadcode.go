// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"log"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

//go:embed doc.go
var doc string

type config struct {
	test      bool
	tags      string
	filter    string
	generated bool
	whyLive   string
	format    string
	json      bool
	args      []string
}

func usage() {
	// Extract the content of the /* ... */ comment in doc.go.
	_, after, _ := strings.Cut(doc, "/*\n")
	cutDoc, _, _ := strings.Cut(after, "*/")
	_, err := io.WriteString(flag.CommandLine.Output(), cutDoc+`
Flags:

`)
	if err != nil {
		panic(err)
	}
	flag.PrintDefaults()
}

func main() {
	log.SetPrefix("deadcode: ")
	log.SetFlags(0) // no time prefix

	cfg, ok := parseFlags()
	if !ok {
		os.Exit(2)
	}

	err := run(cfg)
	if err != nil {
		log.Fatal(err)
	}
}

func parseFlags() (config, bool) {
	var cfg config
	flag.BoolVar(&cfg.test, "test", false, "include implicit test packages and executables")
	flag.StringVar(&cfg.tags, "tags", "", "comma-separated list of extra build tags (see: go help buildconstraint)")
	flag.StringVar(&cfg.filter, "filter", "<module>", "report only packages matching this regular expression (default: module of first package)")
	flag.BoolVar(&cfg.generated, "generated", false, "include dead functions in generated Go files")
	flag.StringVar(&cfg.whyLive, "whylive", "", "show a path from main to the named function")
	flag.StringVar(&cfg.format, "f", "", "format output records using template")
	flag.BoolVar(&cfg.json, "json", false, "output JSON records")

	flag.Usage = usage
	flag.Parse()

	cfg.args = flag.Args()

	if len(cfg.args) == 0 {
		usage()
		return config{}, false
	}

	return cfg, true
}

func run(cfg config) error {
	// Reject bad output options early.
	if cfg.format != "" {
		if cfg.json {
			return fmt.Errorf("you cannot specify both -f=template and -json")
		}
		_, err := template.New("deadcode").Parse(cfg.format)
		if err != nil {
			return fmt.Errorf("invalid -f: %v", err)
		}
	}

	// Load, parse, and type-check the complete program(s).
	pkgCfg := &packages.Config{
		BuildFlags: []string{"-tags=" + cfg.tags},
		Mode:       packages.LoadAllSyntax | packages.NeedModule,
		Tests:      cfg.test,
	}
	initial, err := packages.Load(pkgCfg, cfg.args...)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	if len(initial) == 0 {
		return fmt.Errorf("no packages")
	}
	if packages.PrintErrors(initial) > 0 {
		return fmt.Errorf("packages contain errors")
	}

	filter, err := buildFilterPattern(cfg.filter, initial)
	if err != nil {
		return err
	}

	// Create SSA-form program representation
	// and find main packages.
	prog, pkgs := ssautil.AllPackages(initial, ssa.InstantiateGenerics)
	prog.Build()

	mains := ssautil.MainPackages(pkgs)
	if len(mains) == 0 {
		return fmt.Errorf("no main packages")
	}
	var roots []*ssa.Function
	for _, mainPkg := range mains {
		roots = append(roots, mainPkg.Func("init"), mainPkg.Func("main"))
	}

	sourceFuncs, generated, interfaceTypes := gatherSourceInfo(prog, initial)

	// Compute the reachabilty from main.
	// (Build a call graph only for -whylive.)
	res := rta.Analyze(roots, cfg.whyLive != "")

	if res == nil {
		return fmt.Errorf("RTA analysis failed")
	}

	// Subtle: the -test flag causes us to analyze test variants
	// such as "package p as compiled for p.test" or even "for q.test".
	// This leads to multiple distinct ssa.Function instances that
	// represent the same source declaration, and it is essentially
	// impossible to discover this from the SSA representation
	// (since it has lost the connection to go/packages.Package.ID).
	//
	// So, we de-duplicate such variants by position:
	// if any one of them is live, we consider all of them live.
	// (We use Position not Pos to avoid assuming that files common
	// to packages "p" and "p [p.test]" were parsed only once.)
	reachablePosn := make(map[token.Position]bool)
	for fn := range res.Reachable {
		if fn.Pos().IsValid() || fn.Name() == "init" {
			reachablePosn[prog.Fset.Position(fn.Pos())] = true
		}
	}

	// The -whylive=fn flag causes deadcode to explain why a function
	// is not dead, by showing a path to it from some root.
	if cfg.whyLive != "" {
		return handleWhyLive(cfg, prog, sourceFuncs, reachablePosn, roots, res)
	}

	// Group unreachable functions by package path.
	byPkgPath := make(map[string]map[*ssa.Function]bool)
	for _, fn := range sourceFuncs {
		posn := prog.Fset.Position(fn.Pos())

		if !reachablePosn[posn] {
			reachablePosn[posn] = true // suppress dups with same pos

			pkgpath := fn.Pkg.Pkg.Path()
			m, ok := byPkgPath[pkgpath]
			if !ok {
				m = make(map[*ssa.Function]bool)
				byPkgPath[pkgpath] = m
			}
			m[fn] = true
		}
	}

	jsonPkgs := buildJSONPackages(cfg, prog, byPkgPath, filter, generated, interfaceTypes)

	// Default line-oriented format: "a/b/c.go:1:2: unreachable func: T.f"
	format := `{{range .Funcs}}{{printf "%s: unreachable func: %s\n" .Position .Name}}{{end}}`
	if cfg.format != "" {
		format = cfg.format
	}
	return printObjects(cfg, format, jsonPkgs)
}

func buildFilterPattern(filterFlag string, initial []*packages.Package) (*regexp.Regexp, error) {
	filterPattern := filterFlag
	if filterPattern == "<module>" {
		seen := make(map[string]bool)
		var patterns []string
		for _, pkg := range initial {
			if pkg.Module != nil && pkg.Module.Path != "" && !seen[pkg.Module.Path] {
				seen[pkg.Module.Path] = true
				patterns = append(patterns, regexp.QuoteMeta(pkg.Module.Path))
			}
		}

		filterPattern = ""
		if patterns != nil {
			filterPattern = "^(" + strings.Join(patterns, "|") + ")\\b"
		}
	}
	filter, err := regexp.Compile(filterPattern)
	if err != nil {
		return nil, fmt.Errorf("-filter: %v", err)
	}
	return filter, nil
}

func gatherSourceInfo(
	prog *ssa.Program,
	initial []*packages.Package,
) (
	sourceFuncs []*ssa.Function,
	generated map[string]bool,
	interfaceTypes map[*types.Package][]*types.Interface,
) {
	// Gather all source-level functions,
	// as the user interface is expressed in terms of them.
	//
	// We ignore synthetic wrappers, and nested functions. Literal
	// functions passed as arguments to other functions are of
	// course address-taken and there exists a dynamic call of
	// that signature, so when they are unreachable, it is
	// invariably because the parent is unreachable.
	generated = make(map[string]bool)
	interfaceTypes = make(map[*types.Package][]*types.Interface)
	packages.Visit(initial, nil, func(p *packages.Package) {
		// Collect interfaces by package for marker method identification.
		var interfaces []*types.Interface
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			typeName, ok := scope.Lookup(name).(*types.TypeName)
			if ok && types.IsInterface(typeName.Type()) {
				interfaces = append(interfaces, typeName.Type().Underlying().(*types.Interface))
			}
		}
		interfaceTypes[p.Types] = interfaces

		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				astDecl, ok := decl.(*ast.FuncDecl)
				if ok {
					obj := p.TypesInfo.Defs[astDecl.Name].(*types.Func)
					fn := prog.FuncValue(obj)
					sourceFuncs = append(sourceFuncs, fn)
				}
			}

			if ast.IsGenerated(file) {
				generated[p.Fset.File(file.Pos()).Name()] = true
			}
		}
	})
	return sourceFuncs, generated, interfaceTypes
}

func buildJSONPackages(
	cfg config,
	prog *ssa.Program,
	byPkgPath map[string]map[*ssa.Function]bool,
	filter *regexp.Regexp,
	generated map[string]bool,
	interfaceTypes map[*types.Package][]*types.Interface,
) []any {
	var jsonPkgs []any
	for _, pkgpath := range slices.Sorted(maps.Keys(byPkgPath)) {
		if !filter.MatchString(pkgpath) {
			continue
		}

		m := byPkgPath[pkgpath]

		// Print functions that appear within the same file in
		// declaration order. This tends to keep related
		// methods such as (T).Marshal and (*T).Unmarshal
		// together better than sorting.
		fns := slices.Collect(maps.Keys(m))
		sort.Slice(fns, func(i, j int) bool {
			xposn := prog.Fset.Position(fns[i].Pos())
			yposn := prog.Fset.Position(fns[j].Pos())
			if xposn.Filename != yposn.Filename {
				return xposn.Filename < yposn.Filename
			}
			return xposn.Line < yposn.Line
		})

		var functions []jsonFunction
		for _, fn := range fns {
			posn := prog.Fset.Position(fn.Pos())

			// Without -generated, skip functions declared in
			// generated Go files.
			// (Functions called by them may still be reported.)
			gen := generated[posn.Filename]
			if gen && !cfg.generated {
				continue
			}

			// Marker methods should not be reported
			marker := isMarkerMethod(fn, interfaceTypes[fn.Pkg.Pkg])
			if marker {
				continue
			}

			functions = append(functions, jsonFunction{
				Name:      prettyName(fn, false),
				Position:  toJSONPosition(posn),
				Generated: gen,
				Marker:    marker,
			})
		}
		if len(functions) > 0 {
			jsonPkgs = append(jsonPkgs, jsonPackage{
				Name:  fns[0].Pkg.Pkg.Name(),
				Path:  pkgpath,
				Funcs: functions,
			})
		}
	}
	return jsonPkgs
}

func handleWhyLive(
	cfg config,
	prog *ssa.Program,
	sourceFuncs []*ssa.Function,
	reachablePosn map[token.Position]bool,
	roots []*ssa.Function,
	res *rta.Result,
) error {
	targets := make(map[*ssa.Function]bool)
	for _, fn := range sourceFuncs {
		if prettyName(fn, true) == cfg.whyLive {
			targets[fn] = true
		}
	}
	if len(targets) == 0 {
		// Function is not part of the program.
		//
		// TODO(adonovan): improve the UX here in case
		// of spelling or syntax mistakes. Some ideas:
		// - a cmd/callgraph command to enumerate
		//   available functions.
		// - a deadcode -live flag to compute the complement.
		// - a syntax hint: example.com/pkg.Func or (example.com/pkg.Type).Method
		// - report the element of AllFunctions with the smallest
		//   Levenshtein distance from cfg.whyLive.
		// - permit -whylive=regexp. But beware of spurious
		//   matches (e.g. fmt.Print matches fmt.Println)
		//   and the annoyance of having to quote parens (*T).f.
		return fmt.Errorf("function %q not found in program", cfg.whyLive)
	}

	// Opt: remove the unreachable ones.
	for fn := range targets {
		if !reachablePosn[prog.Fset.Position(fn.Pos())] {
			delete(targets, fn)
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("function %s is dead code", cfg.whyLive)
	}

	res.CallGraph.DeleteSyntheticNodes() // inline synthetic wrappers (except inits)
	root, path := pathSearch(roots, res, targets)
	if root == nil {
		// RTA doesn't add callgraph edges for reflective calls.
		return fmt.Errorf("%s is reachable only through reflection", cfg.whyLive)
	}
	if len(path) == 0 {
		// No edges => one of the targets is a root.
		// Rather than (confusingly) print nothing, make this an error.
		return fmt.Errorf("%s is a root", root.Func)
	}

	// Build a list of jsonEdge records
	// to print as -json or -f=template.
	var edges []any
	for _, edge := range path {
		edges = append(edges, jsonEdge{
			Initial:  cond(len(edges) == 0, prettyName(edge.Caller.Func, true), ""),
			Kind:     cond(isStaticCall(edge), "static", "dynamic"),
			Position: toJSONPosition(prog.Fset.Position(edge.Pos())),
			Callee:   prettyName(edge.Callee.Func, true),
		})
	}
	format := `{{if .Initial}}{{printf "%19s%s\n" "" .Initial}}{{end}}{{printf "%8s@L%.4d --> %s" .Kind .Position.Line .Callee}}`
	if cfg.format != "" {
		format = cfg.format
	}
	return printObjects(cfg, format, edges)
}

// prettyName is a fork of Function.String designed to reduce
// go/ssa's fussy punctuation symbols, e.g. "(*pkg.T).F" -> "pkg.T.F".
//
// It only works for functions that remain after
// callgraph.Graph.DeleteSyntheticNodes: source-level named functions
// and methods, their anonymous functions, and synthetic package
// initializers.
func prettyName(fn *ssa.Function, qualified bool) string {
	var buf strings.Builder

	// optional package qualifier
	if qualified && fn.Pkg != nil {
		fmt.Fprintf(&buf, "%s.", fn.Pkg.Pkg.Path())
	}

	var format func(*ssa.Function)
	format = func(fn *ssa.Function) {
		// anonymous?
		if fn.Parent() != nil {
			format(fn.Parent())
			i := slices.Index(fn.Parent().AnonFuncs, fn)
			fmt.Fprintf(&buf, "$%d", i+1)
			return
		}

		// method receiver?
		recv := fn.Signature.Recv()
		if recv != nil {
			_, named := receiverNamed(recv)
			buf.WriteString(named.Obj().Name())
			buf.WriteByte('.')
		}

		// function/method name
		buf.WriteString(fn.Name())
	}
	format(fn)

	return buf.String()
}

// printObjects formats an array of objects, either as JSON or using a
// template, following the manner of 'go list (-json|-f=template)'.
func printObjects(cfg config, format string, objects []any) error {
	if cfg.json {
		out, err := json.MarshalIndent(objects, "", "\t")
		if err != nil {
			return fmt.Errorf("internal error: %w", err)
		}
		_, err = os.Stdout.Write(out)
		if err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		return nil
	}

	// -f=template. Parse can't fail: we checked it earlier.
	tmpl := template.Must(template.New("deadcode").Parse(format))
	for _, object := range objects {
		var buf bytes.Buffer
		err := tmpl.Execute(&buf, object)
		if err != nil {
			return err
		}
		n := buf.Len()
		if n == 0 || buf.Bytes()[n-1] != '\n' {
			buf.WriteByte('\n')
		}
		_, err = os.Stdout.Write(buf.Bytes())
		if err != nil {
			return err
		}
	}
	return nil
}

// pathSearch returns the shortest path from one of the roots to one
// of the targets (along with the root itself), or zero if no path was found.
func pathSearch(
	roots []*ssa.Function,
	res *rta.Result,
	targets map[*ssa.Function]bool,
) (*callgraph.Node, []*callgraph.Edge) {
	// Search breadth-first (for shortest path) from the root.
	//
	// We don't use the virtual CallGraph.Root node as we wish to
	// choose the order in which we search entrypoints:
	// non-test packages before test packages,
	// main functions before init functions.

	// Sort roots into preferred order.
	importsTesting := func(fn *ssa.Function) bool {
		isTesting := func(p *types.Package) bool { return p.Path() == "testing" }
		return slices.ContainsFunc(fn.Pkg.Pkg.Imports(), isTesting)
	}
	sort.Slice(roots, func(i, j int) bool {
		x, y := roots[i], roots[j]
		xtest := importsTesting(x)
		ytest := importsTesting(y)
		if xtest != ytest {
			return !xtest // non-tests before tests
		}
		xinit := x.Name() == "init"
		yinit := y.Name() == "init"
		if xinit != yinit {
			return !xinit // mains before inits
		}
		return false
	})

	search := func(allowDynamic bool) (*callgraph.Node, []*callgraph.Edge) {
		// seen maps each encountered node to its predecessor on the
		// path to a root node, or to nil for root itself.
		seen := make(map[*callgraph.Node]*callgraph.Edge)
		bfs := func(root *callgraph.Node) []*callgraph.Edge {
			queue := []*callgraph.Node{root}
			seen[root] = nil
			for len(queue) > 0 {
				node := queue[0]
				queue = queue[1:]

				// found a path?
				if targets[node.Func] {
					path := []*callgraph.Edge{} // non-nil in case len(path)=0
					for {
						edge := seen[node]
						if edge == nil {
							slices.Reverse(path)
							return path
						}
						path = append(path, edge)
						node = edge.Caller
					}
				}

				for _, edge := range node.Out {
					if allowDynamic || isStaticCall(edge) {
						if _, ok := seen[edge.Callee]; !ok {
							seen[edge.Callee] = edge
							queue = append(queue, edge.Callee)
						}
					}
				}
			}
			return nil
		}
		for _, rootFn := range roots {
			root := res.CallGraph.Nodes[rootFn]
			if root == nil {
				// Missing call graph node for root.
				// TODO(adonovan): seems like a bug in rta.
				continue
			}
			if path := bfs(root); path != nil {
				return root, path
			}
		}
		return nil, nil
	}

	for _, allowDynamic := range []bool{false, true} {
		if root, path := search(allowDynamic); path != nil {
			return root, path
		}
	}

	return nil, nil
}

// -- utilities --

func isStaticCall(edge *callgraph.Edge) bool {
	return edge.Site != nil && edge.Site.Common().StaticCallee() != nil
}

var cwd = func() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}()

func toJSONPosition(posn token.Position) jsonPosition {
	// Use cwd-relative filename if possible.
	filename := posn.Filename
	rel, err := filepath.Rel(cwd, filename)
	if err == nil && !strings.HasPrefix(rel, "..") {
		filename = rel
	}

	return jsonPosition{filename, posn.Line, posn.Column}
}

func cond[T any](cond bool, t, f T) T {
	if cond {
		return t
	}
	return f
}

// isMarkerMethod reports whether fn is a marker method:
// an unexported, empty-bodied method with no parameters or results
// that implements some named interface type in the same package.
func isMarkerMethod(fn *ssa.Function, interfaceTypes []*types.Interface) bool {
	// Is it an unexported method of no params/results?
	if fn.Signature.Recv() == nil ||
		ast.IsExported(fn.Name()) ||
		fn.Signature.Params() != nil ||
		fn.Signature.Results() != nil {
		return false
	}

	// Does the method have an empty body?
	body := fn.Syntax().(*ast.FuncDecl).Body
	if body == nil || len(body.List) > 0 {
		return false
	}

	// Does it implement some named interface type in this package?
	return slices.ContainsFunc(interfaceTypes, func(iface *types.Interface) bool {
		return types.Implements(fn.Signature.Recv().Type(), iface)
	})
}

// -- output protocol (for JSON or text/template) --

// Keep in sync with doc comment!

type jsonFunction struct {
	Name      string       // name (sans package qualifier)
	Position  jsonPosition // file/line/column of declaration
	Generated bool         // function is declared in a generated .go file
	Marker    bool         // function is a marker interface method
}

func (f jsonFunction) String() string { return f.Name }

type jsonPackage struct {
	Name  string         // declared name
	Path  string         // full import path
	Funcs []jsonFunction // non-empty list of package's dead functions
}

func (p jsonPackage) String() string { return p.Path }

// The Initial and Callee names are package-qualified.
type jsonEdge struct {
	Initial  string `json:",omitempty"` // initial entrypoint (main or init); first edge only
	Kind     string // = static | dynamic
	Position jsonPosition
	Callee   string
}

type jsonPosition struct {
	File      string
	Line, Col int
}

func (p jsonPosition) String() string {
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}

// receiverNamed returns the named type (if any) associated with the
// type of recv, which may be of the form N or *N, or aliases thereof.
// It also reports whether a Pointer was present.
//
// The named result may be nil if recv is from a method on an
// anonymous interface or struct types or in ill-typed code.
func receiverNamed(recv *types.Var) (isPtr bool, named *types.Named) {
	t := recv.Type()
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		isPtr = true
		t = ptr.Elem()
	}
	named, _ = types.Unalias(t).(*types.Named)
	return isPtr, named
}
