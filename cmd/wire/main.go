// Copyright 2018 The Go Cloud Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Wire is a compile-time dependency injection tool.
//
// For an overview, see https://github.com/google/go-cloud/blob/master/wire/README.md
package main

import (
	"errors"
	"fmt"
	"go/build"
	"go/token"
	"go/types"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/google/go-cloud/wire/internal/wire"
	"golang.org/x/tools/go/types/typeutil"
)

const usage = "usage: wire [gen] [PKG] | wire show [...] | wire check [...]"

func main() {
	var err error
	switch {
	case len(os.Args) == 2 && (os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "-help" || os.Args[1] == "--help"):
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(0)
	case len(os.Args) == 1 || len(os.Args) == 2 && os.Args[1] == "gen":
		err = generate(".")
	case len(os.Args) == 2 && os.Args[1] == "show":
		err = show(".")
	case len(os.Args) > 2 && os.Args[1] == "show":
		err = show(os.Args[2:]...)
	case len(os.Args) == 2 && os.Args[1] == "check":
		err = check(".")
	case len(os.Args) > 2 && os.Args[1] == "check":
		err = check(os.Args[2:]...)
	case len(os.Args) == 2:
		err = generate(os.Args[1])
	case len(os.Args) == 3 && os.Args[1] == "gen":
		err = generate(os.Args[2])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(64)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "wire:", err)
		os.Exit(1)
	}
}

// generate runs the gen subcommand. Given a package, gen will create
// the wire_gen.go file.
func generate(pkg string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	pkgInfo, err := build.Default.Import(pkg, wd, build.FindOnly)
	if err != nil {
		return err
	}
	out, errs := wire.Generate(&build.Default, wd, pkg)
	if len(errs) > 0 {
		logErrors(errs)
		return errors.New("generate failed")
	}
	if len(out) == 0 {
		// No Wire directives, don't write anything.
		fmt.Fprintln(os.Stderr, "wire: no injector found for", pkg)
		return nil
	}
	p := filepath.Join(pkgInfo.Dir, "wire_gen.go")
	if err := ioutil.WriteFile(p, out, 0666); err != nil {
		return err
	}
	return nil
}

// show runs the show subcommand.
//
// Given one or more packages, show will find all the provider sets
// declared as top-level variables and print what other provider sets it
// imports and what outputs it can produce, given possible inputs.
// It also lists any injector functions defined in the package.
func show(pkgs ...string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	info, errs := wire.Load(&build.Default, wd, pkgs)
	if info != nil {
		keys := make([]wire.ProviderSetID, 0, len(info.Sets))
		for k := range info.Sets {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].ImportPath == keys[j].ImportPath {
				return keys[i].VarName < keys[j].VarName
			}
			return keys[i].ImportPath < keys[j].ImportPath
		})
		// ANSI color codes.
		// TODO(light): Possibly use github.com/fatih/color?
		const (
			reset   = "\x1b[0m"
			redBold = "\x1b[0;1;31m"
			blue    = "\x1b[0;34m"
			green   = "\x1b[0;32m"
		)
		for i, k := range keys {
			if i > 0 {
				fmt.Println()
			}
			outGroups, imports := gather(info, k)
			fmt.Printf("%s%s%s\n", redBold, k, reset)
			for _, imp := range sortSet(imports) {
				fmt.Printf("\t%s\n", imp)
			}
			for i := range outGroups {
				fmt.Printf("%sOutputs given %s:%s\n", blue, outGroups[i].name, reset)
				out := make(map[string]token.Pos, outGroups[i].outputs.Len())
				outGroups[i].outputs.Iterate(func(t types.Type, v interface{}) {
					switch v := v.(type) {
					case *wire.Provider:
						out[types.TypeString(t, nil)] = v.Pos
					case *wire.Value:
						out[types.TypeString(t, nil)] = v.Pos
					default:
						panic("unreachable")
					}
				})
				for _, t := range sortSet(out) {
					fmt.Printf("\t%s%s%s\n", green, t, reset)
					fmt.Printf("\t\tat %v\n", info.Fset.Position(out[t]))
				}
			}
		}
		if len(info.Injectors) > 0 {
			injectors := append([]*wire.Injector(nil), info.Injectors...)
			sort.Slice(injectors, func(i, j int) bool {
				if injectors[i].ImportPath == injectors[j].ImportPath {
					return injectors[i].FuncName < injectors[j].FuncName
				}
				return injectors[i].ImportPath < injectors[j].ImportPath
			})
			fmt.Printf("%sInjectors:%s\n", redBold, reset)
			for _, in := range injectors {
				fmt.Printf("\t%v\n", in)
			}
		}
	}
	if len(errs) > 0 {
		logErrors(errs)
		return errors.New("error loading packages")
	}
	return nil
}

// check runs the check subcommand.
//
// Given one or more packages, check will print any type-checking or
// Wire errors found with top-level variable provider sets or injector
// functions.
func check(pkgs ...string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	_, errs := wire.Load(&build.Default, wd, pkgs)
	if len(errs) > 0 {
		logErrors(errs)
		return errors.New("error loading packages")
	}
	return nil
}

type outGroup struct {
	name    string
	inputs  *typeutil.Map // values are not important
	outputs *typeutil.Map // values are *wire.Provider or *wire.Value
}

// gather flattens a provider set into outputs grouped by the inputs
// required to create them. As it flattens the provider set, it records
// the visited named provider sets as imports.
func gather(info *wire.Info, key wire.ProviderSetID) (_ []outGroup, imports map[string]struct{}) {
	set := info.Sets[key]
	hash := typeutil.MakeHasher()

	// Find imports.
	next := []*wire.ProviderSet{info.Sets[key]}
	visited := make(map[*wire.ProviderSet]struct{})
	imports = make(map[string]struct{})
	for len(next) > 0 {
		curr := next[len(next)-1]
		next = next[:len(next)-1]
		if _, found := visited[curr]; found {
			continue
		}
		visited[curr] = struct{}{}
		if curr.VarName != "" && !(curr.PkgPath == key.ImportPath && curr.VarName == key.VarName) {
			imports[formatProviderSetName(curr.PkgPath, curr.VarName)] = struct{}{}
		}
		for _, imp := range curr.Imports {
			next = append(next, imp)
		}
	}

	// Depth-first search to build groups.
	var groups []outGroup
	inputVisited := new(typeutil.Map) // values are int, indices into groups or -1 for input.
	inputVisited.SetHasher(hash)
	var stk []types.Type
	for _, k := range set.Outputs() {
		// Start a DFS by picking a random unvisited node.
		if inputVisited.At(k) == nil {
			stk = append(stk, k)
		}

		// Run DFS
	dfs:
		for len(stk) > 0 {
			curr := stk[len(stk)-1]
			stk = stk[:len(stk)-1]
			if inputVisited.At(curr) != nil {
				continue
			}
			switch pv := set.For(curr); {
			case pv.IsNil():
				// This is an input.
				inputVisited.Set(curr, -1)
			case pv.IsProvider():
				// Try to see if any args haven't been visited.
				p := pv.Provider()
				allPresent := true
				for _, arg := range p.Args {
					if inputVisited.At(arg.Type) == nil {
						allPresent = false
					}
				}
				if !allPresent {
					stk = append(stk, curr)
					for _, arg := range p.Args {
						if inputVisited.At(arg.Type) == nil {
							stk = append(stk, arg.Type)
						}
					}
					continue dfs
				}

				// Build up set of input types, match to a group.
				in := new(typeutil.Map)
				in.SetHasher(hash)
				for _, arg := range p.Args {
					i := inputVisited.At(arg.Type).(int)
					if i == -1 {
						in.Set(arg.Type, true)
					} else {
						mergeTypeSets(in, groups[i].inputs)
					}
				}
				for i := range groups {
					if sameTypeKeys(groups[i].inputs, in) {
						groups[i].outputs.Set(curr, p)
						inputVisited.Set(curr, i)
						continue dfs
					}
				}
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(curr, p)
				inputVisited.Set(curr, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			case pv.IsValue():
				v := pv.Value()
				for i := range groups {
					if groups[i].inputs.Len() == 0 {
						groups[i].outputs.Set(curr, v)
						inputVisited.Set(curr, i)
						continue dfs
					}
				}
				in := new(typeutil.Map)
				in.SetHasher(hash)
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(curr, v)
				inputVisited.Set(curr, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			default:
				panic("unreachable")
			}
		}
	}

	// Name and sort groups.
	for i := range groups {
		if groups[i].inputs.Len() == 0 {
			groups[i].name = "no inputs"
			continue
		}
		instr := make([]string, 0, groups[i].inputs.Len())
		groups[i].inputs.Iterate(func(k types.Type, _ interface{}) {
			instr = append(instr, types.TypeString(k, nil))
		})
		sort.Strings(instr)
		groups[i].name = strings.Join(instr, ", ")
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].inputs.Len() == groups[j].inputs.Len() {
			return groups[i].name < groups[j].name
		}
		return groups[i].inputs.Len() < groups[j].inputs.Len()
	})
	return groups, imports
}

func mergeTypeSets(dst, src *typeutil.Map) {
	src.Iterate(func(k types.Type, _ interface{}) {
		dst.Set(k, true)
	})
}

func sameTypeKeys(a, b *typeutil.Map) bool {
	if a.Len() != b.Len() {
		return false
	}
	same := true
	a.Iterate(func(k types.Type, _ interface{}) {
		if b.At(k) == nil {
			same = false
		}
	})
	return same
}

func sortSet(set interface{}) []string {
	rv := reflect.ValueOf(set)
	a := make([]string, 0, rv.Len())
	keys := rv.MapKeys()
	for _, k := range keys {
		a = append(a, k.String())
	}
	sort.Strings(a)
	return a
}

func formatProviderSetName(importPath, varName string) string {
	// Since varName is an identifier, it doesn't make sense to quote.
	return strconv.Quote(importPath) + "." + varName
}

func logErrors(errs []error) {
	for _, err := range errs {
		fmt.Fprintln(os.Stderr, strings.Replace(err.Error(), "\n", "\n\t", -1))
	}
}
