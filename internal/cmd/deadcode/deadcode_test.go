// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

// Test runs the deadcode command on each scenario
// described by a testdata/*.txtar file.
func Test(t *testing.T) {
	if runtime.GOOS == "android" {
		t.Skipf("the dependencies are not available on android")
	}

	exe := buildDeadcode(t)

	matches, err := filepath.Glob("testdata/*.txtar")
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range matches {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()

			var ar *txtar.Archive
			ar, err = txtar.ParseFile(filename)
			if err != nil {
				t.Fatal(err)
			}

			// Write the archive files to the temp directory.
			tmpdir := t.TempDir()
			for _, f := range ar.Files {
				fname := filepath.Join(tmpdir, f.Name)
				err = os.MkdirAll(filepath.Dir(fname), 0o777)
				if err != nil {
					t.Fatal(err)
				}
				err = os.WriteFile(fname, f.Data, 0o666)
				if err != nil {
					t.Fatal(err)
				}
			}

			// Parse archive comment as directives of these forms:
			//
			//  [!]deadcode args...	command-line arguments
			//  [!]want arg		expected/unwanted string in output (or stderr)
			//
			// Args may be Go-quoted strings.
			type testcase struct {
				linenum int
				args    []string
				wantErr bool
				want    map[string]bool // string -> sense
			}
			var cases []*testcase
			var current *testcase
			for i, line := range strings.Split(string(ar.Comment), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || line[0] == '#' {
					continue // skip blanks and comments
				}

				var lineWords []string
				lineWords, err = words(line)
				if err != nil {
					t.Fatalf("cannot break line into words: %v (%s)", err, line)
				}
				switch kind := lineWords[0]; kind {
				case "deadcode", "!deadcode":
					current = &testcase{
						linenum: i + 1,
						want:    make(map[string]bool),
						args:    lineWords[1:],
						wantErr: kind[0] == '!',
					}
					cases = append(cases, current)
				case "want", "!want":
					if current == nil {
						t.Fatalf("'want' directive must be after 'deadcode'")
					}
					if len(lineWords) != 2 {
						t.Fatalf("'want' directive needs argument <<%s>>", line)
					}
					current.want[lineWords[1]] = kind[0] != '!'
				default:
					t.Fatalf("%s: invalid directive %q", filename, kind)
				}
			}

			for _, tc := range cases {
				t.Run(fmt.Sprintf("L%d", tc.linenum), func(t *testing.T) {
					// Run the command.
					cmd := exec.Command(exe, tc.args...)
					cmd.Stdout = new(bytes.Buffer)
					cmd.Stderr = new(bytes.Buffer)
					cmd.Dir = tmpdir
					cmd.Env = append(os.Environ(), "GOPROXY=", "GO111MODULE=on")
					var got string
					err = cmd.Run()
					if err != nil {
						if !tc.wantErr {
							t.Fatalf("deadcode failed: %v (stderr=%s)", err, cmd.Stderr)
						}
						got = fmt.Sprint(cmd.Stderr)
					} else {
						if tc.wantErr {
							t.Fatalf("deadcode succeeded unexpectedly (stdout=%s)", cmd.Stdout)
						}
						got = fmt.Sprint(cmd.Stdout)
					}

					// Check each want directive.
					for str, sense := range tc.want {
						ok := true
						if strings.Contains(got, str) != sense {
							if sense {
								t.Errorf("missing %q", str)
							} else {
								t.Errorf("unwanted %q", str)
							}
							ok = false
						}
						if !ok {
							t.Errorf("got: <<%s>>", got)
						}
					}
				})
			}
		})
	}
}

// buildDeadcode builds the deadcode executable.
// It returns its path, and a cleanup function.
func buildDeadcode(t *testing.T) string {
	bin := filepath.Join(t.TempDir(), "deadcode")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Building deadcode: %v\n%s", err, out)
	}
	return bin
}

// words breaks a string into words, respecting
// Go string quotations around words with spaces.
func words(s string) ([]string, error) {
	var resWords []string
	for s != "" {
		s = strings.TrimSpace(s)
		var word string
		if s[0] == '"' || s[0] == '`' {
			prefix, err := strconv.QuotedPrefix(s)
			if err != nil {
				return nil, err
			}
			s = s[len(prefix):]
			word, err = strconv.Unquote(prefix)
			if err != nil {
				return nil, err
			}
		} else {
			prefix, rest, _ := strings.Cut(s, " ")
			s = rest
			word = prefix
		}
		resWords = append(resWords, word)
	}
	return resWords, nil
}
