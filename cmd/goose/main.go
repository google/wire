// goose is a compile-time dependency injection tool.
//
// See README.md for an overview.
package main

import (
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

	"codename/goose/internal/goose"
	"golang.org/x/tools/go/types/typeutil"
)

func main() {
	var err error
	switch {
	case len(os.Args) == 1 || len(os.Args) == 2 && os.Args[1] == "gen":
		err = generate(".")
	case len(os.Args) == 2 && os.Args[1] == "show":
		err = show(".")
	case len(os.Args) == 2:
		err = generate(os.Args[1])
	case len(os.Args) > 2 && os.Args[1] == "show":
		err = show(os.Args[2:]...)
	case len(os.Args) == 3 && os.Args[1] == "gen":
		err = generate(os.Args[2])
	default:
		fmt.Fprintln(os.Stderr, "goose: usage: goose [gen] [PKG] | goose show [...]")
		os.Exit(64)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "goose:", err)
		os.Exit(1)
	}
}

// generate runs the gen subcommand. Given a package, gen will create
// the goose_gen.go file.
func generate(pkg string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	pkgInfo, err := build.Default.Import(pkg, wd, build.FindOnly)
	if err != nil {
		return err
	}
	out, err := goose.Generate(&build.Default, wd, pkg)
	if err != nil {
		return err
	}
	if len(out) == 0 {
		// No Goose directives, don't write anything.
		fmt.Fprintln(os.Stderr, "goose: no injector found for", pkg)
		return nil
	}
	p := filepath.Join(pkgInfo.Dir, "goose_gen.go")
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
func show(pkgs ...string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	info, err := goose.Load(&build.Default, wd, pkgs)
	if err != nil {
		return err
	}
	keys := make([]goose.ProviderSetID, 0, len(info.Sets))
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
				case *goose.Provider:
					out[types.TypeString(t, nil)] = v.Pos
				case *goose.IfaceBinding:
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
	return nil
}

type outGroup struct {
	name    string
	inputs  *typeutil.Map // values are not important
	outputs *typeutil.Map // values are either *goose.Provider or *goose.IfaceBinding
}

// gather flattens a provider set into outputs grouped by the inputs
// required to create them. As it flattens the provider set, it records
// the visited named provider sets as imports.
func gather(info *goose.Info, key goose.ProviderSetID) (_ []outGroup, imports map[string]struct{}) {
	hash := typeutil.MakeHasher()
	// Map types to providers and bindings.
	pm := new(typeutil.Map)
	pm.SetHasher(hash)
	next := []*goose.ProviderSet{info.Sets[key]}
	visited := make(map[*goose.ProviderSet]struct{})
	imports = make(map[string]struct{})
	for len(next) > 0 {
		curr := next[len(next)-1]
		next = next[:len(next)-1]
		if _, found := visited[curr]; found {
			continue
		}
		visited[curr] = struct{}{}
		if curr.Name != "" && !(curr.PkgPath == key.ImportPath && curr.Name == key.VarName) {
			imports[formatProviderSetName(curr.PkgPath, curr.Name)] = struct{}{}
		}
		for _, p := range curr.Providers {
			pm.Set(p.Out, p)
		}
		for _, b := range curr.Bindings {
			pm.Set(b.Iface, b)
		}
		for _, imp := range curr.Imports {
			next = append(next, imp)
		}
	}

	// Depth-first search to build groups.
	var groups []outGroup
	inputVisited := new(typeutil.Map) // values are int, indices into groups or -1 for input.
	inputVisited.SetHasher(hash)
	pmKeys := pm.Keys()
	var stk []types.Type
	for _, k := range pmKeys {
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
			switch p := pm.At(curr).(type) {
			case nil:
				// This is an input.
				inputVisited.Set(curr, -1)
			case *goose.Provider:
				// Try to see if any args haven't been visited.
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
						groups[i].outputs.Set(p.Out, p)
						inputVisited.Set(p.Out, i)
						continue dfs
					}
				}
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(p.Out, p)
				inputVisited.Set(p.Out, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			case *goose.IfaceBinding:
				i, ok := inputVisited.At(p.Provided).(int)
				if !ok {
					stk = append(stk, curr, p.Provided)
					continue dfs
				}
				if i != -1 {
					groups[i].outputs.Set(p.Iface, p)
					inputVisited.Set(p.Iface, i)
					continue dfs
				}
				// Binding must be provided. Find or add a group.
				for i := range groups {
					if groups[i].inputs.Len() != 1 {
						continue
					}
					if groups[i].inputs.At(p.Provided) != nil {
						groups[i].outputs.Set(p.Iface, p)
						inputVisited.Set(p.Iface, i)
						continue dfs
					}
				}
				in := new(typeutil.Map)
				in.SetHasher(hash)
				in.Set(p.Provided, true)
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(p.Iface, p)
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			default:
				panic("unreachable")
			}
		}
	}

	// Name and sort groups
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
