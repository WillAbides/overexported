package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/willabides/overexported/internal/overexported"
)

func runOverexported(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	var buf bytes.Buffer
	err = run(&buf, args)
	return buf.String(), err
}

func parseJSONOutput(t *testing.T, output string) []overexported.Export {
	t.Helper()
	var exports []overexported.Export
	err := json.Unmarshal([]byte(output), &exports)
	require.NoError(t, err, "failed to parse JSON output: %s", output)
	return exports
}

func exportNames(exports []overexported.Export) []string {
	names := make([]string, len(exports))
	for i, e := range exports {
		names[i] = e.Name
	}
	return names
}

func TestBasic(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	require.Len(t, exports, 1)
	assert.Equal(t, "Bar", exports[0].Name)
	assert.Equal(t, "func", exports[0].Kind)
}

func TestTypesAndMethods(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/types", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

	// UnusedType and its method should be reported
	assert.Contains(t, names, "UnusedType")
	assert.Contains(t, names, "UnusedType.UnusedTypeMethod")

	// UnusedMethod on UsedType should be reported
	assert.Contains(t, names, "UsedType.UnusedMethod")

	// UsedType and UsedMethod should NOT be reported
	assert.NotContains(t, names, "UsedType")
	assert.NotContains(t, names, "UsedType.UsedMethod")
}

func TestInterfaceSatisfaction(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/interfaces", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

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

func TestConstsAndVars(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/constvars", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

	// Unused exports should be reported
	assert.Contains(t, names, "UnusedConst")
	assert.Contains(t, names, "UnusedVar")
	assert.Contains(t, names, "UnusedFunc")

	// Used exports should NOT be reported
	assert.NotContains(t, names, "UsedConst")
	assert.NotContains(t, names, "UsedVar")
	assert.NotContains(t, names, "UsedFunc")
}

func TestGeneratedFiles(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/generated", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

	// ManualUnused should be reported (in hand-written file)
	assert.Contains(t, names, "ManualUnused")

	// ManualUsed should NOT be reported (used externally)
	assert.NotContains(t, names, "ManualUsed")

	// Generated file exports should NOT be reported regardless of usage
	assert.NotContains(t, names, "GeneratedUnused")
	assert.NotContains(t, names, "GeneratedUsed")
}

func TestGeneratedFiles_IncludeGenerated(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/generated", "--json", "--test", "--generated", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

	// ManualUnused should be reported (in hand-written file)
	assert.Contains(t, names, "ManualUnused")

	// ManualUsed should NOT be reported (used externally)
	assert.NotContains(t, names, "ManualUsed")

	// With --generated, unused exports in generated files SHOULD be reported
	assert.Contains(t, names, "GeneratedUnused")

	// GeneratedUsed should NOT be reported (used externally)
	assert.NotContains(t, names, "GeneratedUsed")
}

func TestExternalTestPackage(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/external_test", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

	// NotUsedInTests should be reported (not used anywhere)
	assert.Contains(t, names, "NotUsedInTests")

	// UsedInExternalTest and UsedInInternalTest should NOT be reported
	// (used by cmd/main.go which is an external package)
	assert.NotContains(t, names, "UsedInExternalTest")
	assert.NotContains(t, names, "UsedInInternalTest")

	// OnlyUsedInTests should NOT be reported with --test because
	// it's used by the external test package (lib_test), which is now
	// treated as a separate package when --test is enabled.
	assert.NotContains(t, names, "OnlyUsedInTests")
}

func TestExternalTestPackage_NoTest(t *testing.T) {
	t.Parallel()
	// Without --test, functions only used by test files should be reported
	stdout, err := runOverexported(t, "-C", "testdata/external_test", "--json", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

	// NotUsedInTests should still be reported (not used anywhere)
	assert.Contains(t, names, "NotUsedInTests")

	// UsedInExternalTest and UsedInInternalTest are used by cmd/main.go,
	// so they should NOT be reported even without test packages
	assert.NotContains(t, names, "UsedInExternalTest")
	assert.NotContains(t, names, "UsedInInternalTest")

	// OnlyUsedInTests SHOULD be reported without --test
	// (it's only used by test files which are excluded)
	assert.Contains(t, names, "OnlyUsedInTests")
}

func TestGenerics(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/generics", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

	// Unused generic function and type should be reported
	assert.Contains(t, names, "UnusedGeneric")
	assert.Contains(t, names, "UnusedGenericType")

	// Used generic function and type should NOT be reported
	assert.NotContains(t, names, "UsedGeneric")
	assert.NotContains(t, names, "UsedGenericType")
}

func TestTypeReferences(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/typerefs", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

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

func TestTargetPatternFiltering(t *testing.T) {
	t.Parallel()
	// Only analyze the baz/foo package, not baz/foo/cmd/foo
	stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "baz/foo")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)

	// Should still find Bar as unused since it's only used internally
	assert.Contains(t, names, "Bar")
}

func TestEmptyResult(t *testing.T) {
	t.Parallel()
	// When all exports are used, result should be empty
	stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "baz/foo/cmd/foo")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	assert.Empty(t, exports)
}

func TestExportFields(t *testing.T) {
	t.Parallel()
	stdout, err := runOverexported(t, "-C", "testdata/types", "--json", "--test", "./...")
	require.NoError(t, err)

	exports := parseJSONOutput(t, stdout)
	require.NotEmpty(t, exports)

	// Find UnusedType and verify its fields
	idx := slices.IndexFunc(exports, func(e overexported.Export) bool {
		return e.Name == "UnusedType"
	})
	require.GreaterOrEqual(t, idx, 0, "UnusedType should be in exports")

	exp := exports[idx]
	assert.Equal(t, "UnusedType", exp.Name)
	assert.Equal(t, "type", exp.Kind)
	assert.Equal(t, "types", exp.PkgPath)
	assert.NotEmpty(t, exp.Position.File)
	assert.Greater(t, exp.Position.Line, 0)
	assert.Greater(t, exp.Position.Col, 0)
}

func TestFilter(t *testing.T) {
	t.Parallel()
	// Without filter (default is <module>), should find Bar
	stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "./...")
	require.NoError(t, err)
	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)
	assert.Contains(t, names, "Bar")

	// With filter that doesn't match, should find nothing
	stdout, err = runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--filter=^nonexistent$", "./...")
	require.NoError(t, err)
	exports = parseJSONOutput(t, stdout)
	assert.Empty(t, exports)

	// With filter that matches baz/foo package
	stdout, err = runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--filter=^baz/foo$", "./...")
	require.NoError(t, err)
	exports = parseJSONOutput(t, stdout)
	names = exportNames(exports)
	assert.Contains(t, names, "Bar")
}

func TestFilter_InvalidRegex(t *testing.T) {
	t.Parallel()
	// Invalid regex should return error
	_, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--filter=[", "./...")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter pattern")
}

func TestExclude(t *testing.T) {
	t.Parallel()
	// Without exclude, constvars package has unused exports
	stdout, err := runOverexported(t, "-C", "testdata/constvars", "--json", "--test", "./...")
	require.NoError(t, err)
	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)
	assert.Contains(t, names, "UnusedConst")
	assert.Contains(t, names, "UnusedVar")
	assert.Contains(t, names, "UnusedFunc")

	// With exclude matching the constvars package exactly
	stdout, err = runOverexported(t, "-C", "testdata/constvars", "--json", "--test", "--exclude=constvars", "./...")
	require.NoError(t, err)
	exports = parseJSONOutput(t, stdout)
	assert.Empty(t, exports)
}

func TestExclude_WithEllipsis(t *testing.T) {
	t.Parallel()
	// Test exclude with ... pattern
	stdout, err := runOverexported(t, "-C", "testdata/types", "--json", "--test", "--exclude=types/...", "./...")
	require.NoError(t, err)
	exports := parseJSONOutput(t, stdout)
	assert.Empty(t, exports)
}

func TestExclude_Multiple(t *testing.T) {
	t.Parallel()
	// Without exclude, types package has multiple unused exports
	stdout, err := runOverexported(t, "-C", "testdata/types", "--json", "--test", "./...")
	require.NoError(t, err)
	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)
	assert.Contains(t, names, "UnusedType")
	assert.Contains(t, names, "UsedType.UnusedMethod")

	// Exclude types package but not cmd - should still have no results since
	// the only exports are from types package
	stdout, err = runOverexported(t, "-C", "testdata/types", "--json", "--test", "--exclude=types", "./...")
	require.NoError(t, err)
	exports = parseJSONOutput(t, stdout)
	assert.Empty(t, exports)
}

func TestExclude_PartialMatch(t *testing.T) {
	t.Parallel()
	// Exclude only one package, leaving others
	// Using foo testdata which has baz/foo package
	stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "./...")
	require.NoError(t, err)
	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)
	assert.Contains(t, names, "Bar")

	// Exclude baz/foo package
	stdout, err = runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--exclude=baz/foo", "./...")
	require.NoError(t, err)
	exports = parseJSONOutput(t, stdout)
	assert.Empty(t, exports)

	// Exclude with ... should also work
	stdout, err = runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--exclude=baz/...", "./...")
	require.NoError(t, err)
	exports = parseJSONOutput(t, stdout)
	assert.Empty(t, exports)
}

func TestExclude_NoMatch(t *testing.T) {
	t.Parallel()
	// Exclude pattern that doesn't match anything should have no effect
	stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--exclude=nonexistent", "./...")
	require.NoError(t, err)
	exports := parseJSONOutput(t, stdout)
	names := exportNames(exports)
	assert.Contains(t, names, "Bar")
}

func TestTextOutput(t *testing.T) {
	t.Parallel()
	// Test that text output works (no --json flag)
	stdout, err := runOverexported(t, "-C", "testdata/foo", "--test", "./...")
	require.NoError(t, err)

	// Should contain package name and Bar
	assert.Contains(t, stdout, "baz/foo:")
	assert.Contains(t, stdout, "Bar")
	assert.Contains(t, stdout, "func")
}

func TestEmptyTextOutput(t *testing.T) {
	t.Parallel()
	// When all exports are used, should show "No over-exported" message
	stdout, err := runOverexported(t, "-C", "testdata/foo", "--test", "baz/foo/cmd/foo")
	require.NoError(t, err)
	assert.Contains(t, stdout, "No over-exported identifiers found")
}
