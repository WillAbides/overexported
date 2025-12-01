package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/willabides/overexported/internal/overexported"
)

var cli struct {
	Patterns []string `arg:"" required:"" help:"Package patterns to analyze."`
}

func main() {
	kong.Parse(&cli)
	result, err := overexported.Run(cli.Patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printResult(result)
}

func printResult(result *overexported.Result) {
	if len(result.Exports) == 0 {
		fmt.Println("All exports are used by external packages.")
		return
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
		fmt.Printf("\n%s:\n", pkg)
		fmt.Println("  Can be unexported (only used internally):")

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
			relPath, err := filepath.Rel(cwd, exp.Position.File)
			if err != nil {
				relPath = exp.Position.File
			}
			fmt.Printf("    %s (%s) ./%s:%d\n", exp.Name, exp.Kind, relPath, exp.Position.Line)
		}
	}
}
