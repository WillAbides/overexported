package overexported

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Chdir("testdata/foo")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)
	require.Len(t, got.Exports, 1)
	assert.Equal(t, "Bar", got.Exports[0].Name)
	assert.Equal(t, "func", got.Exports[0].Kind)
}

func TestRun_TypesAndMethods(t *testing.T) {
	t.Chdir("testdata/types")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)

	names := exportNames(got)

	// UnusedType and its method should be reported
	assert.Contains(t, names, "UnusedType")
	assert.Contains(t, names, "UnusedType.UnusedTypeMethod")

	// UnusedMethod on UsedType should be reported
	assert.Contains(t, names, "UsedType.UnusedMethod")

	// UsedType and UsedMethod should NOT be reported
	assert.NotContains(t, names, "UsedType")
	assert.NotContains(t, names, "UsedType.UsedMethod")
}

func TestRun_InterfaceSatisfaction(t *testing.T) {
	t.Chdir("testdata/interfaces")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)

	names := exportNames(got)

	// Impl is used (assigned to io.Reader) via RuntimeTypes
	assert.NotContains(t, names, "Impl")
	// Read is used (implements io.Reader)
	assert.NotContains(t, names, "Impl.Read")

	// UnusedImplMethod is not part of any interface and not called externally
	assert.Contains(t, names, "Impl.UnusedImplMethod")

	// UnusedImpl and its method are completely unused
	assert.Contains(t, names, "UnusedImpl")
	assert.Contains(t, names, "UnusedImpl.DoSomething")
}

func TestRun_ConstsAndVars(t *testing.T) {
	t.Chdir("testdata/constvars")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)

	names := exportNames(got)

	// Unused exports should be reported
	assert.Contains(t, names, "UnusedConst")
	assert.Contains(t, names, "UnusedVar")
	assert.Contains(t, names, "UnusedFunc")

	// Used exports should NOT be reported
	assert.NotContains(t, names, "UsedConst")
	assert.NotContains(t, names, "UsedVar")
	assert.NotContains(t, names, "UsedFunc")
}

func TestRun_GeneratedFiles(t *testing.T) {
	t.Chdir("testdata/generated")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)

	names := exportNames(got)

	// ManualUnused should be reported (in hand-written file)
	assert.Contains(t, names, "ManualUnused")

	// ManualUsed should NOT be reported (used externally)
	assert.NotContains(t, names, "ManualUsed")

	// Generated file exports should NOT be reported regardless of usage
	assert.NotContains(t, names, "GeneratedUnused")
	assert.NotContains(t, names, "GeneratedUsed")
}

func TestRun_GeneratedFiles_IncludeGenerated(t *testing.T) {
	t.Chdir("testdata/generated")

	got, err := Run([]string{"./..."}, &Options{Test: true, Generated: true})
	require.NoError(t, err)

	names := exportNames(got)

	// ManualUnused should be reported (in hand-written file)
	assert.Contains(t, names, "ManualUnused")

	// ManualUsed should NOT be reported (used externally)
	assert.NotContains(t, names, "ManualUsed")

	// With Generated: true, unused exports in generated files SHOULD be reported
	assert.Contains(t, names, "GeneratedUnused")

	// GeneratedUsed should NOT be reported (used externally)
	assert.NotContains(t, names, "GeneratedUsed")
}

func TestRun_ExternalTestPackage(t *testing.T) {
	t.Chdir("testdata/external_test")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)

	names := exportNames(got)

	// NotUsedInTests should be reported (not used anywhere)
	assert.Contains(t, names, "NotUsedInTests")

	// UsedInExternalTest and UsedInInternalTest should NOT be reported
	// (used by cmd/main.go which is an external package)
	assert.NotContains(t, names, "UsedInExternalTest")
	assert.NotContains(t, names, "UsedInInternalTest")

	// OnlyUsedInTests SHOULD be reported even with Test: true because
	// it's only used by the external test package (lib_test), which is
	// treated as the same package as lib for the purpose of determining
	// external usage. The --test flag includes test packages in the analysis
	// but doesn't change what counts as "external" usage.
	assert.Contains(t, names, "OnlyUsedInTests")
}

func TestRun_ExternalTestPackage_NoTest(t *testing.T) {
	t.Chdir("testdata/external_test")

	// Without Test: true, functions only used by test files should be reported
	got, err := Run([]string{"./..."}, &Options{Test: false})
	require.NoError(t, err)

	names := exportNames(got)

	// NotUsedInTests should still be reported (not used anywhere)
	assert.Contains(t, names, "NotUsedInTests")

	// UsedInExternalTest and UsedInInternalTest are used by cmd/main.go,
	// so they should NOT be reported even without test packages
	assert.NotContains(t, names, "UsedInExternalTest")
	assert.NotContains(t, names, "UsedInInternalTest")

	// OnlyUsedInTests SHOULD be reported when Test: false
	// (it's only used by test files which are excluded)
	assert.Contains(t, names, "OnlyUsedInTests")
}

func TestRun_NilOptions(t *testing.T) {
	t.Chdir("testdata/external_test")

	// nil options should default to Test: false
	got, err := Run([]string{"./..."}, nil)
	require.NoError(t, err)

	names := exportNames(got)

	// OnlyUsedInTests should be reported (same as Test: false)
	assert.Contains(t, names, "OnlyUsedInTests")
}

func TestRun_Generics(t *testing.T) {
	t.Chdir("testdata/generics")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)

	names := exportNames(got)

	// Unused generic function and type should be reported
	assert.Contains(t, names, "UnusedGeneric")
	assert.Contains(t, names, "UnusedGenericType")

	// Used generic function and type should NOT be reported.
	// Position-based tracking handles instantiated generics correctly.
	assert.NotContains(t, names, "UsedGeneric")
	assert.NotContains(t, names, "UsedGenericType")
}

func TestRun_TypeReferences(t *testing.T) {
	t.Chdir("testdata/typerefs")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)

	names := exportNames(got)

	// Types used in function signatures should NOT be reported
	assert.NotContains(t, names, "UsedAsParam")
	assert.NotContains(t, names, "UsedAsReturn")
	assert.NotContains(t, names, "UsedInSlice")
	assert.NotContains(t, names, "UsedInMap")

	// Functions using those types should NOT be reported
	assert.NotContains(t, names, "TakesParam")
	assert.NotContains(t, names, "ReturnsType")
	assert.NotContains(t, names, "TakesSlice")
	assert.NotContains(t, names, "TakesMap")

	// UnusedType should be reported
	assert.Contains(t, names, "UnusedType")
}

func TestRun_TargetPatternFiltering(t *testing.T) {
	t.Chdir("testdata/foo")

	// Only analyze the foo package, not cmd/foo
	got, err := Run([]string{"foo"}, &Options{Test: true})
	require.NoError(t, err)

	// Should still find Bar as unused since it's only used internally
	names := exportNames(got)
	assert.Contains(t, names, "Bar")
}

func TestRun_EmptyResult(t *testing.T) {
	t.Chdir("testdata/foo")

	// When all exports are used, result should be empty
	got, err := Run([]string{"foo/cmd/foo"}, &Options{Test: true})
	require.NoError(t, err)
	assert.Empty(t, got.Exports)
}

func TestExport_Fields(t *testing.T) {
	t.Chdir("testdata/types")

	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)
	require.NotEmpty(t, got.Exports)

	// Find UnusedType and verify its fields
	idx := slices.IndexFunc(got.Exports, func(e Export) bool {
		return e.Name == "UnusedType"
	})
	require.GreaterOrEqual(t, idx, 0, "UnusedType should be in exports")

	exp := got.Exports[idx]
	assert.Equal(t, "UnusedType", exp.Name)
	assert.Equal(t, "type", exp.Kind)
	assert.Equal(t, "types", exp.PkgPath)
	assert.NotEmpty(t, exp.Position.File)
	assert.Greater(t, exp.Position.Line, 0)
	assert.Greater(t, exp.Position.Col, 0)
}

func TestRun_Filter(t *testing.T) {
	t.Chdir("testdata/foo")

	// Without filter, should find Bar
	got, err := Run([]string{"./..."}, &Options{Test: true})
	require.NoError(t, err)
	names := exportNames(got)
	assert.Contains(t, names, "Bar")

	// With filter that doesn't match, should find nothing
	got, err = Run([]string{"./..."}, &Options{Test: true, Filter: "^nonexistent$"})
	require.NoError(t, err)
	assert.Empty(t, got.Exports)

	// With filter that matches foo package
	got, err = Run([]string{"./..."}, &Options{Test: true, Filter: "^foo$"})
	require.NoError(t, err)
	names = exportNames(got)
	assert.Contains(t, names, "Bar")
}

func TestRun_Filter_Module(t *testing.T) {
	t.Chdir("testdata/foo")

	// With <module> filter, should find Bar (module is "foo")
	got, err := Run([]string{"./..."}, &Options{Test: true, Filter: "<module>"})
	require.NoError(t, err)
	names := exportNames(got)
	assert.Contains(t, names, "Bar")
}

func TestRun_Filter_InvalidRegex(t *testing.T) {
	t.Chdir("testdata/foo")

	// Invalid regex should return error
	_, err := Run([]string{"./..."}, &Options{Test: true, Filter: "["})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter pattern")
}

func exportNames(r *Result) []string {
	names := make([]string, len(r.Exports))
	for i, e := range r.Exports {
		names[i] = e.Name
	}
	return names
}
