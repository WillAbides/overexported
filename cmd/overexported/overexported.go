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

type cliOptions struct {
	Chdir     string   `short:"C" help:"Change to this directory before running."`
	Test      bool     `help:"Include test packages and executables in the analysis."`
	Generated bool     `help:"Include exports in generated Go files."`
	JSON      bool     `help:"Output JSON records."`
	Filter    string   `default:"<module>" help:"Report only packages matching this regular expression. '<module>' matches the modules of all analyzed packages."`
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
		kong.Description("Find exported Go identifiers that are not used outside their package and could be unexported."),
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
