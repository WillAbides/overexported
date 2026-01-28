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

func runOverexported(t *testing.T, args ...string) (stdout string, _ error) {
	t.Helper()
	var buf bytes.Buffer
	err := run(&buf, args)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
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

func Test_run(t *testing.T) {
	t.Parallel()

	// Table-driven tests for straightforward contains/not-contains checks
	t.Run("exports", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name            string
			dir             string
			args            []string
			wantContains    []string
			wantNotContains []string
		}{
			{
				name:         "basic",
				dir:          "testdata/foo",
				args:         []string{"./..."},
				wantContains: []string{"Bar"},
			},
			{
				name:            "types and methods",
				dir:             "testdata/types",
				args:            []string{"./..."},
				wantContains:    []string{"UnusedType", "UnusedType.UnusedTypeMethod", "UsedType.UnusedMethod"},
				wantNotContains: []string{"UsedType", "UsedType.UsedMethod"},
			},
			{
				name:            "interface satisfaction",
				dir:             "testdata/interfaces",
				args:            []string{"./..."},
				wantContains:    []string{"Impl.UnusedImplMethod", "UnusedImpl", "UnusedImpl.DoSomething"},
				wantNotContains: []string{"Impl", "Impl.Read"},
			},
			{
				name:            "consts and vars",
				dir:             "testdata/constvars",
				args:            []string{"./..."},
				wantContains:    []string{"UnusedConst", "UnusedVar", "UnusedFunc"},
				wantNotContains: []string{"UsedConst", "UsedVar", "UsedFunc"},
			},
			{
				name:            "generated files excluded by default",
				dir:             "testdata/generated",
				args:            []string{"./..."},
				wantContains:    []string{"ManualUnused"},
				wantNotContains: []string{"ManualUsed", "GeneratedUnused", "GeneratedUsed"},
			},
			{
				name:            "generated files included with --generated",
				dir:             "testdata/generated",
				args:            []string{"--generated", "./..."},
				wantContains:    []string{"ManualUnused", "GeneratedUnused"},
				wantNotContains: []string{"ManualUsed", "GeneratedUsed"},
			},
			{
				name:            "generics",
				dir:             "testdata/generics",
				args:            []string{"./..."},
				wantContains:    []string{"UnusedGeneric", "UnusedGenericType"},
				wantNotContains: []string{"UsedGeneric", "UsedGenericType"},
			},
			{
				name:            "type references",
				dir:             "testdata/typerefs",
				args:            []string{"./..."},
				wantContains:    []string{"UnusedType"},
				wantNotContains: []string{"UsedAsParam", "UsedAsReturn", "UsedInSlice", "UsedInMap", "TakesParam", "ReturnsType", "TakesSlice", "TakesMap"},
			},
			{
				name:            "type aliases",
				dir:             "testdata/typealiases",
				args:            []string{"./..."},
				wantContains:    []string{"UnusedTimestamp", "UnusedString"},
				wantNotContains: []string{"Timestamp", "UsedString", "Now"},
			},
			{
				name:         "target pattern filtering",
				dir:          "testdata/foo",
				args:         []string{"baz/foo"},
				wantContains: []string{"Bar"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				args := append([]string{"-C", tt.dir, "--json", "--test"}, tt.args...)
				stdout, err := runOverexported(t, args...)
				require.NoError(t, err)

				exports := parseJSONOutput(t, stdout)
				names := exportNames(exports)

				for _, want := range tt.wantContains {
					assert.Contains(t, names, want)
				}
				for _, notWant := range tt.wantNotContains {
					assert.NotContains(t, names, notWant)
				}
			})
		}
	})

	t.Run("external test package", func(t *testing.T) {
		t.Parallel()

		t.Run("with --test", func(t *testing.T) {
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
		})

		t.Run("without --test", func(t *testing.T) {
			t.Parallel()
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
		})
	})

	t.Run("filter", func(t *testing.T) {
		t.Parallel()

		t.Run("default filter finds exports", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			names := exportNames(exports)
			assert.Contains(t, names, "Bar")
		})

		t.Run("non-matching filter finds nothing", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--filter=^nonexistent$", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			assert.Empty(t, exports)
		})

		t.Run("matching filter finds exports", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--filter=^baz/foo$", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			names := exportNames(exports)
			assert.Contains(t, names, "Bar")
		})

		t.Run("invalid regex returns error", func(t *testing.T) {
			t.Parallel()
			_, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--filter=[", "./...")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid filter pattern")
		})
	})

	t.Run("exclude", func(t *testing.T) {
		t.Parallel()

		t.Run("without exclude finds exports", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/constvars", "--json", "--test", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			names := exportNames(exports)
			assert.Contains(t, names, "UnusedConst")
			assert.Contains(t, names, "UnusedVar")
			assert.Contains(t, names, "UnusedFunc")
		})

		t.Run("exact match excludes package", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/constvars", "--json", "--test", "--exclude=constvars", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			assert.Empty(t, exports)
		})

		t.Run("ellipsis pattern excludes packages", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/types", "--json", "--test", "--exclude=types/...", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			assert.Empty(t, exports)
		})

		t.Run("partial match excludes specific package", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--exclude=baz/foo", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			assert.Empty(t, exports)
		})

		t.Run("ellipsis partial match excludes packages", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--exclude=baz/...", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			assert.Empty(t, exports)
		})

		t.Run("non-matching exclude has no effect", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "--exclude=nonexistent", "./...")
			require.NoError(t, err)
			exports := parseJSONOutput(t, stdout)
			names := exportNames(exports)
			assert.Contains(t, names, "Bar")
		})
	})

	t.Run("empty result", func(t *testing.T) {
		t.Parallel()
		stdout, err := runOverexported(t, "-C", "testdata/foo", "--json", "--test", "baz/foo/cmd/foo")
		require.NoError(t, err)

		// Empty result should be [] not null
		assert.Equal(t, "[]\n", stdout)

		exports := parseJSONOutput(t, stdout)
		assert.Empty(t, exports)
	})

	t.Run("export fields", func(t *testing.T) {
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
	})

	t.Run("text output", func(t *testing.T) {
		t.Parallel()

		t.Run("with results", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/foo", "--test", "./...")
			require.NoError(t, err)

			assert.Contains(t, stdout, "baz/foo:")
			assert.Contains(t, stdout, "Bar")
			assert.Contains(t, stdout, "func")
		})

		t.Run("empty results", func(t *testing.T) {
			t.Parallel()
			stdout, err := runOverexported(t, "-C", "testdata/foo", "--test", "baz/foo/cmd/foo")
			require.NoError(t, err)
			assert.Contains(t, stdout, "No over-exported identifiers found")
		})
	})
}
