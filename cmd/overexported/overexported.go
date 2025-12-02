package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/willabides/overexported/internal/overexported"
)

const description = `
The overexported command reports exported identifiers that could be unexported.

The overexported command loads a Go program from source then uses Rapid Type
Analysis (RTA) to build a call graph of all the functions reachable from the
program's main function. Any exported identifiers (functions, types, methods,
variables, and constants) that are not referenced from outside their package
are reported as over-exported, grouped by package.

Packages are expressed in the notation of 'go list' (or other underlying build
system if you are using an alternative golang.org/x/go/packages driver). Only
executable (main) packages are considered starting points for the analysis.

The --test flag causes it to analyze test executables too. Tests sometimes make
use of identifiers that would otherwise appear to be over-exported, and public
API identifiers reported as over-exported with --test indicate possible gaps in
your test coverage or truly unnecessary exports.

The --filter flag restricts results to packages that match the provided regular
expression; its default value is the special string "<module>" which matches
the listed packages and any other packages belonging to the same modules. Use
--filter= to display all results.

The --exclude flag excludes packages matching the provided pattern from the
results. Patterns use the same syntax as 'go list' (e.g., "./...",
"github.com/foo/bar/..."). This flag can be specified multiple times.

Example: show all over-exported identifiers within a module:

  $ overexported --test ./...

By default, the tool does not report exports in generated files, as determined
by the special comment described in https://go.dev/s/generatedcode . Use the
--generated flag to include them.

Just because an identifier is reported as over-exported does not mean it is
unconditionally safe to unexport it. For example, an over-exported function may
be referenced by another over-exported function. Some judgement is required.

The analysis is valid only for a single GOOS/GOARCH configuration, so an
identifier reported as over-exported may be used in a different configuration.
Consider running the tool once for each configuration of interest.
`

type cliOptions struct {
	Chdir     string   `short:"C" help:"Change to this directory before running."`
	Test      bool     `help:"Include test packages and executables in the analysis."`
	Generated bool     `help:"Include exports in generated Go files."`
	JSON      bool     `help:"Output JSON records."`
	Filter    string   `default:"<module>" help:"Report only packages matching this regular expression. '<module>' matches the modules of all analyzed packages."`
	Exclude   []string `help:"Exclude packages matching this pattern from the results. Can be specified multiple times."`
	Packages  []string `arg:"" required:"" help:"Package patterns to analyze."`
}

func main() {
	err := run(os.Stdout, os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(stdout io.Writer, args []string) error {
	var cli cliOptions
	p, err := kong.New(&cli,
		kong.Description(strings.TrimSpace(description)),
	)
	if err != nil {
		return err
	}
	_, err = p.Parse(args)
	if err != nil {
		return err
	}
	result, err := overexported.Run(cli.Packages, &overexported.Options{
		Test:      cli.Test,
		Generated: cli.Generated,
		Filter:    cli.Filter,
		Exclude:   cli.Exclude,
		Dir:       cli.Chdir,
	})
	if err != nil {
		return err
	}
	if !cli.JSON {
		return printResult(stdout, result)
	}
	return printResultJSON(stdout, result)
}

func printResult(stdout io.Writer, result *overexported.Result) error {
	if len(result.Exports) == 0 {
		_, err := fmt.Fprintln(stdout, "No over-exported identifiers found.")
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}

	// Group by package
	pkgSet := make(map[string]bool)
	for _, exp := range result.Exports {
		pkgSet[exp.PkgPath] = true
	}

	var pkgs []string
	for pkg := range pkgSet {
		pkgs = append(pkgs, pkg)
	}
	slices.Sort(pkgs)

	for _, pkg := range pkgs {
		_, err = fmt.Fprintf(stdout, "\n%s:\n", pkg)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "  Can be unexported (only used internally):")
		if err != nil {
			return err
		}

		var pkgExports []overexported.Export
		for _, exp := range result.Exports {
			if exp.PkgPath == pkg {
				pkgExports = append(pkgExports, exp)
			}
		}
		slices.SortFunc(pkgExports, func(a, b overexported.Export) int {
			return strings.Compare(a.Name, b.Name)
		})

		for _, exp := range pkgExports {
			var relPath string
			relPath, err = filepath.Rel(cwd, exp.Position.File)
			if err != nil {
				relPath = exp.Position.File
			}
			_, err = fmt.Fprintf(stdout, "    %s (%s) ./%s:%d\n", exp.Name, exp.Kind, relPath, exp.Position.Line)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func printResultJSON(stdout io.Writer, result *overexported.Result) error {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result.Exports)
}
