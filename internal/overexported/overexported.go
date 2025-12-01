package overexported

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Position represents a source code location.
type Position struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

// Export represents an exported symbol that can be unexported.
type Export struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Position Position `json:"position"`
	PkgPath  string   `json:"package"`
}

// Result contains the analysis results.
type Result struct {
	Exports []Export `json:"exports"`
}

func Run(patterns []string) (*Result, error) {
	// Load all packages with full syntax for SSA
	cfg := &packages.Config{
		Mode:  packages.LoadAllSyntax,
		Tests: true,
	}
	allPkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	if packages.PrintErrors(allPkgs) > 0 {
		return nil, fmt.Errorf("packages contain errors")
	}

	// Build target package paths from patterns
	targetPkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName}, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load target patterns: %w", err)
	}
	targetPaths := make(map[string]bool)
	for _, pkg := range targetPkgs {
		targetPaths[pkg.PkgPath] = true
	}

	// Build SSA program
	prog, pkgs := ssautil.AllPackages(allPkgs, ssa.InstantiateGenerics)
	prog.Build()

	// Collect exports from target packages
	exports, generated := collectExportsSSA(prog, allPkgs, targetPaths)
	if len(exports) == 0 {
		return &Result{}, nil
	}

	// Find main packages and entry points
	mains := ssautil.MainPackages(pkgs)
	if len(mains) == 0 {
		return nil, fmt.Errorf("no main packages found")
	}

	var roots []*ssa.Function
	for _, mainPkg := range mains {
		if init := mainPkg.Func("init"); init != nil {
			roots = append(roots, init)
		}
		if main := mainPkg.Func("main"); main != nil {
			roots = append(roots, main)
		}
	}

	// Run RTA analysis
	res := rta.Analyze(roots, true)
	if res == nil {
		return nil, fmt.Errorf("RTA analysis failed")
	}

	// Find externally used exports via call graph
	externallyUsed := findExternalUsageRTA(res, targetPaths)

	// Add types that appear in RuntimeTypes (interface satisfaction)
	res.RuntimeTypes.Iterate(func(t types.Type, _ any) {
		named, ok := t.(*types.Named)
		if !ok {
			return
		}
		if named.Obj() == nil || named.Obj().Pkg() == nil {
			return
		}
		pkgPath := named.Obj().Pkg().Path()
		if targetPaths[pkgPath] {
			key := pkgPath + "." + named.Obj().Name()
			externallyUsed[key] = true
			// Also mark all methods of this type as used (interface satisfaction)
			for i := range named.NumMethods() {
				m := named.Method(i)
				if m.Exported() {
					methodKey := pkgPath + "." + named.Obj().Name() + "." + m.Name()
					externallyUsed[methodKey] = true
				}
			}
		}
	})

	// Build result
	return buildResult(exports, externallyUsed, generated), nil
}

func collectExportsSSA(
	prog *ssa.Program,
	pkgs []*packages.Package,
	targetPaths map[string]bool,
) (exports map[string]Export, generated map[string]bool) {
	exports = make(map[string]Export)
	generated = make(map[string]bool)

	for _, pkg := range pkgs {
		if !targetPaths[pkg.PkgPath] {
			continue
		}

		// Track generated files
		for _, file := range pkg.Syntax {
			if ast.IsGenerated(file) {
				generated[pkg.Fset.File(file.Pos()).Name()] = true
			}
		}

		ssaPkg := prog.Package(pkg.Types)
		if ssaPkg == nil {
			continue
		}

		collectPackageExports(prog, pkg.PkgPath, ssaPkg, generated, exports)
	}
	return exports, generated
}

func collectPackageExports(
	prog *ssa.Program,
	pkgPath string,
	ssaPkg *ssa.Package,
	generated map[string]bool,
	exports map[string]Export,
) {
	for _, mem := range ssaPkg.Members {
		switch m := mem.(type) {
		case *ssa.Function:
			collectFunctionExport(prog, pkgPath, m, generated, exports)
		case *ssa.Type:
			collectTypeExport(prog, pkgPath, m, generated, exports)
		case *ssa.Global:
			collectGlobalExport(prog, pkgPath, m, generated, exports)
		case *ssa.NamedConst:
			collectConstExport(prog, pkgPath, m, generated, exports)
		}
	}
}

func collectFunctionExport(
	prog *ssa.Program,
	pkgPath string,
	fn *ssa.Function,
	generated map[string]bool,
	exports map[string]Export,
) {
	if !token.IsExported(fn.Name()) || fn.Synthetic != "" {
		return
	}
	posn := prog.Fset.Position(fn.Pos())
	if generated[posn.Filename] {
		return
	}
	key := pkgPath + "." + fn.Name()
	exports[key] = Export{
		Name:     fn.Name(),
		Kind:     "func",
		Position: Position{File: posn.Filename, Line: posn.Line, Col: posn.Column},
		PkgPath:  pkgPath,
	}
}

func collectTypeExport(
	prog *ssa.Program,
	pkgPath string,
	m *ssa.Type,
	generated map[string]bool,
	exports map[string]Export,
) {
	if !token.IsExported(m.Name()) {
		return
	}
	posn := prog.Fset.Position(m.Pos())
	if generated[posn.Filename] {
		return
	}
	key := pkgPath + "." + m.Name()
	exports[key] = Export{
		Name:     m.Name(),
		Kind:     "type",
		Position: Position{File: posn.Filename, Line: posn.Line, Col: posn.Column},
		PkgPath:  pkgPath,
	}

	// Collect methods on this type (both value and pointer receivers)
	named := m.Object().Type().(*types.Named)
	collectMethodsFromMethodSet(prog, pkgPath, m.Name(), prog.MethodSets.MethodSet(named), generated, exports)
	collectMethodsFromMethodSet(prog, pkgPath, m.Name(), prog.MethodSets.MethodSet(types.NewPointer(named)), generated, exports)
}

func collectMethodsFromMethodSet(
	prog *ssa.Program,
	pkgPath, typeName string,
	mset *types.MethodSet,
	generated map[string]bool,
	exports map[string]Export,
) {
	for i := range mset.Len() {
		sel := mset.At(i)
		if !sel.Obj().Exported() {
			continue
		}
		fn := prog.MethodValue(sel)
		if fn == nil || fn.Synthetic != "" {
			continue
		}
		mposn := prog.Fset.Position(fn.Pos())
		if generated[mposn.Filename] {
			continue
		}
		methodKey := pkgPath + "." + typeName + "." + sel.Obj().Name()
		if _, exists := exports[methodKey]; !exists {
			exports[methodKey] = Export{
				Name:     typeName + "." + sel.Obj().Name(),
				Kind:     "method",
				Position: Position{File: mposn.Filename, Line: mposn.Line, Col: mposn.Column},
				PkgPath:  pkgPath,
			}
		}
	}
}

func collectGlobalExport(
	prog *ssa.Program,
	pkgPath string,
	g *ssa.Global,
	generated map[string]bool,
	exports map[string]Export,
) {
	if !token.IsExported(g.Name()) {
		return
	}
	posn := prog.Fset.Position(g.Pos())
	if generated[posn.Filename] {
		return
	}
	key := pkgPath + "." + g.Name()
	exports[key] = Export{
		Name:     g.Name(),
		Kind:     "var",
		Position: Position{File: posn.Filename, Line: posn.Line, Col: posn.Column},
		PkgPath:  pkgPath,
	}
}

func collectConstExport(
	prog *ssa.Program,
	pkgPath string,
	c *ssa.NamedConst,
	generated map[string]bool,
	exports map[string]Export,
) {
	if !token.IsExported(c.Name()) {
		return
	}
	posn := prog.Fset.Position(c.Pos())
	if generated[posn.Filename] {
		return
	}
	key := pkgPath + "." + c.Name()
	exports[key] = Export{
		Name:     c.Name(),
		Kind:     "const",
		Position: Position{File: posn.Filename, Line: posn.Line, Col: posn.Column},
		PkgPath:  pkgPath,
	}
}

func findExternalUsageRTA(res *rta.Result, targetPaths map[string]bool) map[string]bool {
	used := make(map[string]bool)

	// Walk call graph edges to find cross-package calls
	for fn, node := range res.CallGraph.Nodes {
		if fn == nil || fn.Pkg == nil {
			continue
		}
		callerPkg := fn.Pkg.Pkg.Path()
		// Strip _test suffix for external test packages
		callerPkg = strings.TrimSuffix(callerPkg, "_test")

		for _, edge := range node.Out {
			callee := edge.Callee.Func
			if callee == nil || callee.Pkg == nil {
				continue
			}
			calleePkg := callee.Pkg.Pkg.Path()

			// Only care about calls to target packages
			if !targetPaths[calleePkg] {
				continue
			}

			// Check if this is an external call
			if callerPkg != calleePkg {
				key := buildSSAKey(callee)
				if key != "" {
					used[key] = true
				}
			}
		}
	}

	// Also check for type references in reachable functions
	for fn := range res.Reachable {
		if fn == nil || fn.Pkg == nil {
			continue
		}
		callerPkg := fn.Pkg.Pkg.Path()
		callerPkg = strings.TrimSuffix(callerPkg, "_test")

		// Check type references in function signature and body
		collectTypeRefsFromFunc(fn, callerPkg, targetPaths, used)
	}

	return used
}

func buildSSAKey(fn *ssa.Function) string {
	if fn == nil || fn.Pkg == nil {
		return ""
	}
	pkgPath := fn.Pkg.Pkg.Path()

	// Check if this is a method
	if recv := fn.Signature.Recv(); recv != nil {
		typeName := getReceiverTypeName(recv.Type())
		if typeName != "" {
			return pkgPath + "." + typeName + "." + fn.Name()
		}
	}
	return pkgPath + "." + fn.Name()
}

func getReceiverTypeName(t types.Type) string {
	switch t := t.(type) {
	case *types.Named:
		return t.Obj().Name()
	case *types.Pointer:
		return getReceiverTypeName(t.Elem())
	}
	return ""
}

func collectTypeRefsFromFunc(fn *ssa.Function, callerPkg string, targetPaths, used map[string]bool) {
	// Check parameter types
	for _, param := range fn.Params {
		collectTypeRefs(param.Type(), callerPkg, targetPaths, used)
	}

	// Check return types
	results := fn.Signature.Results()
	for i := range results.Len() {
		collectTypeRefs(results.At(i).Type(), callerPkg, targetPaths, used)
	}

	// Check types used in function body
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			// Type assertions and conversions
			if ta, ok := instr.(*ssa.TypeAssert); ok {
				collectTypeRefs(ta.AssertedType, callerPkg, targetPaths, used)
			}
			if cv, ok := instr.(*ssa.Convert); ok {
				collectTypeRefs(cv.Type(), callerPkg, targetPaths, used)
			}
			if cv, ok := instr.(*ssa.ChangeType); ok {
				collectTypeRefs(cv.Type(), callerPkg, targetPaths, used)
			}
			// Field accesses and struct literals
			if fa, ok := instr.(*ssa.FieldAddr); ok {
				collectTypeRefs(fa.X.Type(), callerPkg, targetPaths, used)
			}
			if f, ok := instr.(*ssa.Field); ok {
				collectTypeRefs(f.X.Type(), callerPkg, targetPaths, used)
			}
			// Allocations
			if alloc, ok := instr.(*ssa.Alloc); ok {
				collectTypeRefs(alloc.Type(), callerPkg, targetPaths, used)
			}
			// Make (slices, maps, chans)
			if mk, ok := instr.(*ssa.MakeSlice); ok {
				collectTypeRefs(mk.Type(), callerPkg, targetPaths, used)
			}
			if mk, ok := instr.(*ssa.MakeMap); ok {
				collectTypeRefs(mk.Type(), callerPkg, targetPaths, used)
			}
			if mk, ok := instr.(*ssa.MakeChan); ok {
				collectTypeRefs(mk.Type(), callerPkg, targetPaths, used)
			}
		}
	}
}

func collectTypeRefs(t types.Type, callerPkg string, targetPaths, used map[string]bool) {
	switch t := t.(type) {
	case *types.Named:
		if t.Obj() != nil && t.Obj().Pkg() != nil {
			pkgPath := t.Obj().Pkg().Path()
			if targetPaths[pkgPath] && callerPkg != pkgPath && token.IsExported(t.Obj().Name()) {
				used[pkgPath+"."+t.Obj().Name()] = true
			}
		}
		// Check type arguments for generics
		if ta := t.TypeArgs(); ta != nil {
			for i := range ta.Len() {
				collectTypeRefs(ta.At(i), callerPkg, targetPaths, used)
			}
		}
	case *types.Pointer:
		collectTypeRefs(t.Elem(), callerPkg, targetPaths, used)
	case *types.Slice:
		collectTypeRefs(t.Elem(), callerPkg, targetPaths, used)
	case *types.Array:
		collectTypeRefs(t.Elem(), callerPkg, targetPaths, used)
	case *types.Map:
		collectTypeRefs(t.Key(), callerPkg, targetPaths, used)
		collectTypeRefs(t.Elem(), callerPkg, targetPaths, used)
	case *types.Chan:
		collectTypeRefs(t.Elem(), callerPkg, targetPaths, used)
	case *types.Signature:
		params := t.Params()
		for i := range params.Len() {
			collectTypeRefs(params.At(i).Type(), callerPkg, targetPaths, used)
		}
		results := t.Results()
		for i := range results.Len() {
			collectTypeRefs(results.At(i).Type(), callerPkg, targetPaths, used)
		}
	case *types.Struct:
		for i := range t.NumFields() {
			collectTypeRefs(t.Field(i).Type(), callerPkg, targetPaths, used)
		}
	case *types.Interface:
		for i := range t.NumMethods() {
			collectTypeRefs(t.Method(i).Type(), callerPkg, targetPaths, used)
		}
	}
}

func buildResult(exports map[string]Export, externallyUsed, generated map[string]bool) *Result {
	var result []Export

	for key, exp := range exports {
		if externallyUsed[key] {
			continue
		}
		// Skip generated files
		if generated[exp.Position.File] {
			continue
		}
		result = append(result, exp)
	}

	return &Result{Exports: result}
}
