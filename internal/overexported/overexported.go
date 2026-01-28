package overexported

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
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

// Options configures the analysis.
type Options struct {
	// Test includes test packages and executables in the analysis.
	Test bool
	// Generated includes exports in generated Go files.
	Generated bool
	// Filter is a regular expression to filter which packages to report.
	// The special value "<module>" reports only packages matching the
	// modules of all analyzed packages.
	Filter string
	// Exclude is a list of package patterns to exclude from the results.
	// Patterns use the same syntax as 'go list' (e.g., "./...", "github.com/foo/...").
	Exclude []string
	// Dir is the directory to use for the analysis. If empty, the current
	// working directory is used.
	Dir string
}

func Run(patterns []string, opts *Options) (*Result, error) {
	if opts == nil {
		opts = &Options{}
	}

	allPkgs, needsTargetMatching, err := loadPackages(*opts, patterns)
	if err != nil {
		return nil, err
	}

	targetPaths := buildTargetPaths(allPkgs, patterns, needsTargetMatching)

	filter, err := buildFilterPattern(*opts, allPkgs)
	if err != nil {
		return nil, err
	}

	// Build SSA program.
	prog, pkgs := ssautil.Packages(allPkgs, ssa.InstantiateGenerics)
	prog.Build()

	exports, generated := collectExportsSSA(*opts, prog, allPkgs, targetPaths)
	if len(exports) == 0 {
		return &Result{}, nil
	}

	roots, err := findEntryPoints(pkgs)
	if err != nil {
		return nil, err
	}

	res := rta.Analyze(roots, true)
	if res == nil {
		return nil, fmt.Errorf("RTA analysis failed")
	}

	externallyUsed := findExternalUsage(*opts, res, allPkgs, targetPaths)
	markRuntimeTypes(res, targetPaths, externallyUsed)

	return buildResult(*opts, exports, externallyUsed, generated, filter), nil
}

func loadPackages(opts Options, patterns []string) ([]*packages.Package, bool, error) {
	loadPatterns := patterns
	needsTargetMatching := false
	for _, p := range patterns {
		if p != "./..." && p != "..." {
			loadPatterns = []string{"./..."}
			needsTargetMatching = true
			break
		}
	}

	cfg := &packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedModule,
		Tests: opts.Test,
		Dir:   opts.Dir,
	}
	allPkgs, err := packages.Load(cfg, loadPatterns...)
	if err != nil {
		return nil, false, fmt.Errorf("load packages: %w", err)
	}
	if packages.PrintErrors(allPkgs) > 0 {
		return nil, false, fmt.Errorf("packages contain errors")
	}
	return allPkgs, needsTargetMatching, nil
}

func buildTargetPaths(allPkgs []*packages.Package, patterns []string, needsTargetMatching bool) map[string]bool {
	targetPaths := make(map[string]bool)
	for _, pkg := range allPkgs {
		if !needsTargetMatching || matchPackagePatterns(patterns, pkg.PkgPath) {
			targetPaths[pkg.PkgPath] = true
		}
	}
	return targetPaths
}

func findEntryPoints(pkgs []*ssa.Package) ([]*ssa.Function, error) {
	mains := ssautil.MainPackages(pkgs)
	if len(mains) == 0 {
		return nil, fmt.Errorf("no main packages found")
	}

	var roots []*ssa.Function
	for _, mainPkg := range mains {
		init := mainPkg.Func("init")
		if init != nil {
			roots = append(roots, init)
		}
		main := mainPkg.Func("main")
		if main != nil {
			roots = append(roots, main)
		}
	}
	return roots, nil
}

func markRuntimeTypes(res *rta.Result, targetPaths, externallyUsed map[string]bool) {
	res.RuntimeTypes.Iterate(func(t types.Type, _ any) {
		named, ok := t.(*types.Named)
		if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
			return
		}
		pkgPath := named.Obj().Pkg().Path()
		if targetPaths[pkgPath] {
			externallyUsed[pkgPath+"."+named.Obj().Name()] = true
		}
	})
}

func collectExportsSSA(
	opts Options,
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

		// Pass nil for generated map when includeGenerated is true to skip filtering
		genMap := generated
		if opts.Generated {
			genMap = nil
		}
		c := &exportCollector{
			prog:      prog,
			exports:   exports,
			generated: genMap,
			pkgPath:   pkg.PkgPath,
		}
		c.collectPackageExports(ssaPkg)
	}
	return exports, generated
}

// exportCollector holds shared state for collecting exports from a package.
type exportCollector struct {
	prog      *ssa.Program
	exports   map[string]Export
	generated map[string]bool
	pkgPath   string
}

// addExport adds an export to the exports map if the position is not in a generated file.
// Returns true if the export was added, false if it was skipped (generated file).
func (c *exportCollector) addExport(name, kind string, pos token.Pos) bool {
	posn := c.prog.Fset.Position(pos)
	if c.generated[posn.Filename] {
		return false
	}
	key := c.pkgPath + "." + name
	c.exports[key] = Export{
		Name:     name,
		Kind:     kind,
		Position: Position{File: posn.Filename, Line: posn.Line, Col: posn.Column},
		PkgPath:  c.pkgPath,
	}
	return true
}

func (c *exportCollector) collectPackageExports(ssaPkg *ssa.Package) {
	for _, mem := range ssaPkg.Members {
		switch m := mem.(type) {
		case *ssa.Function:
			c.collectFunctionExport(m)
		case *ssa.Type:
			c.collectTypeExport(m)
		case *ssa.Global:
			c.collectGlobalExport(m)
		case *ssa.NamedConst:
			c.collectConstExport(m)
		}
	}
}

func (c *exportCollector) collectFunctionExport(fn *ssa.Function) {
	if !token.IsExported(fn.Name()) || fn.Synthetic != "" {
		return
	}
	c.addExport(fn.Name(), "func", fn.Pos())
}

func (c *exportCollector) collectTypeExport(m *ssa.Type) {
	if !token.IsExported(m.Name()) {
		return
	}
	if !c.addExport(m.Name(), "type", m.Pos()) {
		return
	}

	// Collect methods on this type (both value and pointer receivers)
	// Type aliases don't have their own methods, so skip method collection for them
	named, ok := m.Object().Type().(*types.Named)
	if !ok {
		return
	}
	c.collectMethodsFromMethodSet(m.Name(), c.prog.MethodSets.MethodSet(named))
	c.collectMethodsFromMethodSet(m.Name(), c.prog.MethodSets.MethodSet(types.NewPointer(named)))
}

func (c *exportCollector) collectMethodsFromMethodSet(typeName string, mset *types.MethodSet) {
	for sel := range mset.Methods() {
		if !sel.Obj().Exported() {
			continue
		}
		fn := c.prog.MethodValue(sel)
		if fn == nil || fn.Synthetic != "" {
			continue
		}
		methodName := typeName + "." + sel.Obj().Name()
		methodKey := c.pkgPath + "." + methodName
		_, exists := c.exports[methodKey]
		if exists {
			continue
		}
		c.addExport(methodName, "method", fn.Pos())
	}
}

func (c *exportCollector) collectGlobalExport(g *ssa.Global) {
	if !token.IsExported(g.Name()) {
		return
	}
	c.addExport(g.Name(), "var", g.Pos())
}

func (c *exportCollector) collectConstExport(cn *ssa.NamedConst) {
	if !token.IsExported(cn.Name()) {
		return
	}
	c.addExport(cn.Name(), "const", cn.Pos())
}

func findExternalUsage(
	opts Options,
	res *rta.Result,
	allPkgs []*packages.Package,
	targetPaths map[string]bool,
) map[string]bool {
	used := make(map[string]bool)
	findCrossPackageCalls(opts, res, targetPaths, used)
	findTypeRefsInReachable(opts, res, targetPaths, used)
	findExternalUsageTypesInfo(opts, allPkgs, targetPaths, used)
	return used
}

func findCrossPackageCalls(opts Options, res *rta.Result, targetPaths, used map[string]bool) {
	for fn, node := range res.CallGraph.Nodes {
		if fn == nil || fn.Pkg == nil {
			continue
		}
		callerPkg := normalizePkgPath(fn.Pkg.Pkg.Path(), opts)

		for _, edge := range node.Out {
			callee := edge.Callee.Func
			if callee == nil {
				continue
			}
			calleePkg := getSSAPkgPath(callee)
			if calleePkg == "" || !targetPaths[calleePkg] || callerPkg == calleePkg {
				continue
			}
			key := buildSSAKey(callee)
			if key != "" {
				used[key] = true
			}
		}
	}
}

func findTypeRefsInReachable(opts Options, res *rta.Result, targetPaths, used map[string]bool) {
	for fn := range res.Reachable {
		if fn == nil {
			continue
		}
		callerPkg := getSSAPkgPath(fn)
		if callerPkg == "" {
			continue
		}
		collectTypeRefsFromFunc(fn, normalizePkgPath(callerPkg, opts), targetPaths, used)
	}
}

func normalizePkgPath(pkgPath string, opts Options) string {
	if !opts.Test {
		return strings.TrimSuffix(pkgPath, "_test")
	}
	return pkgPath
}

// getSSAPkgPath returns the package path for an SSA function.
// For instantiated generic functions, Pkg is nil but Origin().Pkg is set.
func getSSAPkgPath(fn *ssa.Function) string {
	switch {
	case fn.Pkg != nil:
		return fn.Pkg.Pkg.Path()
	case fn.Origin() != nil && fn.Origin().Pkg != nil:
		return fn.Origin().Pkg.Pkg.Path()
	default:
		return ""
	}
}

// findExternalUsageTypesInfo finds externally used exports by examining
// TypesInfo.Uses across all packages. This catches references to consts,
// vars, types, and functions that RTA's call graph doesn't track.
func findExternalUsageTypesInfo(opts Options, allPkgs []*packages.Package, targetPaths, used map[string]bool) {
	for _, pkg := range allPkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		callerPkg := pkg.PkgPath
		// When not including tests, treat external test packages (foo_test)
		// as the same package as foo. When including tests, external test
		// packages are considered separate packages.
		if !opts.Test {
			callerPkg = strings.TrimSuffix(callerPkg, "_test")
		}

		for _, obj := range pkg.TypesInfo.Uses {
			if obj == nil || obj.Pkg() == nil {
				continue
			}
			objPkg := obj.Pkg().Path()

			// Only care about references to target packages
			if !targetPaths[objPkg] {
				continue
			}

			// Check if this is an external reference
			if callerPkg != objPkg && obj.Exported() {
				key := objPkg + "." + obj.Name()
				used[key] = true
			}
		}
	}
}

func buildSSAKey(fn *ssa.Function) string {
	if fn == nil || fn.Pkg == nil {
		return ""
	}
	pkgPath := fn.Pkg.Pkg.Path()

	// Check if this is a method
	recv := fn.Signature.Recv()
	if recv != nil {
		typeName := getReceiverTypeName(recv.Type())
		if typeName != "" {
			return pkgPath + "." + typeName + "." + fn.Name()
		}
	}
	return pkgPath + "." + fn.Name()
}

func getReceiverTypeName(t types.Type) string {
	switch tp := t.(type) {
	case *types.Named:
		return tp.Obj().Name()
	case *types.Pointer:
		return getReceiverTypeName(tp.Elem())
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
	for v := range results.Variables() {
		collectTypeRefs(v.Type(), callerPkg, targetPaths, used)
	}

	// Check types used in function body
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			switch v := instr.(type) {
			case *ssa.TypeAssert:
				collectTypeRefs(v.AssertedType, callerPkg, targetPaths, used)
			case *ssa.Convert, *ssa.ChangeType, *ssa.Alloc, *ssa.MakeSlice, *ssa.MakeMap, *ssa.MakeChan:
				collectTypeRefs(v.(ssa.Value).Type(), callerPkg, targetPaths, used)
			case *ssa.FieldAddr:
				collectTypeRefs(v.X.Type(), callerPkg, targetPaths, used)
			case *ssa.Field:
				collectTypeRefs(v.X.Type(), callerPkg, targetPaths, used)
			}
		}
	}
}

func collectTypeRefs(t types.Type, callerPkg string, targetPaths, used map[string]bool) {
	switch tp := t.(type) {
	case *types.Alias:
		collectAliasTypeRefs(tp, callerPkg, targetPaths, used)
	case *types.Named:
		collectNamedTypeRefs(tp, callerPkg, targetPaths, used)
	case *types.Pointer, *types.Slice, *types.Array, *types.Chan:
		type el interface{ Elem() types.Type }
		collectTypeRefs(tp.(el).Elem(), callerPkg, targetPaths, used)
	case *types.Map:
		collectTypeRefs(tp.Key(), callerPkg, targetPaths, used)
		collectTypeRefs(tp.Elem(), callerPkg, targetPaths, used)
	case *types.Signature:
		collectSignatureTypeRefs(tp, callerPkg, targetPaths, used)
	case *types.Struct:
		for field := range tp.Fields() {
			collectTypeRefs(field.Type(), callerPkg, targetPaths, used)
		}
	case *types.Interface:
		for method := range tp.Methods() {
			collectTypeRefs(method.Type(), callerPkg, targetPaths, used)
		}
	}
}

func collectAliasTypeRefs(tp *types.Alias, callerPkg string, targetPaths, used map[string]bool) {
	if tp.Obj() != nil && tp.Obj().Pkg() != nil {
		pkgPath := tp.Obj().Pkg().Path()
		if targetPaths[pkgPath] && callerPkg != pkgPath && token.IsExported(tp.Obj().Name()) {
			used[pkgPath+"."+tp.Obj().Name()] = true
		}
	}
	// Also check the underlying type
	collectTypeRefs(tp.Rhs(), callerPkg, targetPaths, used)
}

func collectNamedTypeRefs(tp *types.Named, callerPkg string, targetPaths, used map[string]bool) {
	if tp.Obj() != nil && tp.Obj().Pkg() != nil {
		pkgPath := tp.Obj().Pkg().Path()
		if targetPaths[pkgPath] && callerPkg != pkgPath && token.IsExported(tp.Obj().Name()) {
			used[pkgPath+"."+tp.Obj().Name()] = true
		}
	}
	ta := tp.TypeArgs()
	if ta != nil {
		for tat := range ta.Types() {
			collectTypeRefs(tat, callerPkg, targetPaths, used)
		}
	}
}

func collectSignatureTypeRefs(tp *types.Signature, callerPkg string, targetPaths, used map[string]bool) {
	for v := range tp.Params().Variables() {
		collectTypeRefs(v.Type(), callerPkg, targetPaths, used)
	}
	for v := range tp.Results().Variables() {
		collectTypeRefs(v.Type(), callerPkg, targetPaths, used)
	}
}

func buildResult(
	opts Options,
	exports map[string]Export,
	externallyUsed map[string]bool,
	generated map[string]bool,
	filter *regexp.Regexp,
) *Result {
	var result []Export

	for key, exp := range exports {
		if externallyUsed[key] {
			continue
		}
		// Skip generated files unless includeGenerated is true
		if !opts.Generated && generated[exp.Position.File] {
			continue
		}
		// Apply filter
		if filter != nil && !filter.MatchString(exp.PkgPath) {
			continue
		}
		// Apply exclude
		if len(opts.Exclude) > 0 && matchPackagePatterns(opts.Exclude, exp.PkgPath) {
			continue
		}
		result = append(result, exp)
	}

	return &Result{Exports: result}
}

// buildFilterPattern builds a regexp from the filter flag value.
// The special value "<module>" builds a pattern from module paths.
// An empty string returns nil (no filtering).
func buildFilterPattern(opts Options, initial []*packages.Package) (*regexp.Regexp, error) {
	filterPattern := opts.Filter
	if filterPattern == "" {
		return nil, nil
	}
	if filterPattern == "<module>" {
		seen := make(map[string]bool)
		var patterns []string
		for _, pkg := range initial {
			if pkg.Module != nil && pkg.Module.Path != "" && !seen[pkg.Module.Path] {
				seen[pkg.Module.Path] = true
				patterns = append(patterns, regexp.QuoteMeta(pkg.Module.Path))
			}
		}

		if len(patterns) == 0 {
			return nil, nil
		}
		filterPattern = "^(" + strings.Join(patterns, "|") + ")\\b"
	}
	filter, err := regexp.Compile(filterPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid filter pattern: %w", err)
	}
	return filter, nil
}

// matchPackagePatterns checks if a package path matches any of the given patterns.
func matchPackagePatterns(patterns []string, pkgPath string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, pkgPath) {
			return true
		}
	}
	return false
}

// matchPattern checks if a package path matches a Go package pattern.
func matchPattern(pattern, pkgPath string) bool {
	// Handle "./..." - matches everything
	if pattern == "./..." {
		return true
	}

	// Handle "..." suffix - matches package and all subpackages
	prefix, ok := strings.CutSuffix(pattern, "/...")
	if ok {
		return pkgPath == prefix || strings.HasPrefix(pkgPath, prefix+"/")
	}

	// Handle "..." alone - matches everything
	if pattern == "..." {
		return true
	}

	// Exact match
	return pattern == pkgPath
}
