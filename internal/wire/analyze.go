// Copyright 2018 Google LLC
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

package wire

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/types/typeutil"
)

type callKind int

const (
	funcProviderCall callKind = iota
	structProvider
	valueExpr
)

// A call represents a step of an injector function.  It may be either a
// function call or a composite struct literal, depending on the value
// of kind.
type call struct {
	// kind indicates the code pattern to use.
	kind callKind

	// out is the type this step produces.
	out types.Type

	// importPath and name identify the provider to call for kind ==
	// funcProviderCall or the type to construct for kind ==
	// structProvider.
	importPath string
	name       string

	// args is a list of arguments to call the provider with.  Each element is:
	// a) one of the givens (args[i] < len(given)), or
	// b) the result of a previous provider call (args[i] >= len(given))
	//
	// This will be nil for kind == valueExpr.
	args []int

	// fieldNames maps the arguments to struct field names.
	// This will only be set if kind == structProvider.
	fieldNames []string

	// ins is the list of types this call receives as arguments.
	// This will be nil for kind == valueExpr.
	ins []types.Type

	// The following are only set for kind == funcProviderCall:

	// hasCleanup is true if the provider call returns a cleanup function.
	hasCleanup bool
	// hasErr is true if the provider call returns an error.
	hasErr bool

	// The following are only set for kind == valueExpr:

	valueExpr     ast.Expr
	valueTypeInfo *types.Info
}

// solve finds the sequence of calls required to produce an output type
// with an optional set of provided inputs.
func solve(fset *token.FileSet, out types.Type, given []types.Type, set *ProviderSet) ([]call, error) {
	for i, g := range given {
		for _, h := range given[:i] {
			if types.Identical(g, h) {
				return nil, fmt.Errorf("multiple inputs of the same type %s", types.TypeString(g, nil))
			}
		}
	}

	// Start building the mapping of type to local variable of the given type.
	// The first len(given) local variables are the given types.
	index := new(typeutil.Map)
	for i, g := range given {
		if pv := set.For(g); !pv.IsNil() {
			switch {
			case pv.IsProvider():
				return nil, fmt.Errorf("input of %s conflicts with provider %s at %s",
					types.TypeString(g, nil), pv.Provider().Name, fset.Position(pv.Provider().Pos))
			case pv.IsValue():
				return nil, fmt.Errorf("input of %s conflicts with value at %s",
					types.TypeString(g, nil), fset.Position(pv.Value().Pos))
			default:
				panic("unknown return value from ProviderSet.For")
			}
		}
		index.Set(g, i)
	}

	// Topological sort of the directed graph defined by the providers
	// using a depth-first search using a stack. Provider set graphs are
	// guaranteed to be acyclic.
	var calls []call
	type frame struct {
		t    types.Type
		from types.Type
	}
	stk := []frame{{t: out}}
	for len(stk) > 0 {
		curr := stk[len(stk)-1]
		stk = stk[:len(stk)-1]
		if index.At(curr.t) != nil {
			continue
		}

		switch pv := set.For(curr.t); {
		case pv.IsNil():
			if curr.from == nil {
				return nil, fmt.Errorf("no provider found for %s (output of injector)", types.TypeString(curr.t, nil))
			}
			// TODO(light): Give name of provider.
			return nil, fmt.Errorf("no provider found for %s (required by provider of %s)", types.TypeString(curr.t, nil), types.TypeString(curr.from, nil))
		case pv.IsProvider():
			p := pv.Provider()
			if !types.Identical(p.Out, curr.t) {
				// Interface binding.  Don't create a call ourselves.
				i := index.At(p.Out)
				if i == nil {
					stk = append(stk, curr, frame{t: p.Out, from: curr.t})
					continue
				}
				index.Set(curr.t, i)
				continue
			}
			// Ensure that all argument types have been visited. If not, push them
			// on the stack in reverse order so that calls are added in argument
			// order.
			visitedArgs := true
			for i := len(p.Args) - 1; i >= 0; i-- {
				a := p.Args[i]
				if index.At(a.Type) == nil {
					if visitedArgs {
						// Make sure to re-visit this type after visiting all arguments.
						stk = append(stk, curr)
						visitedArgs = false
					}
					stk = append(stk, frame{t: a.Type, from: curr.t})
				}
			}
			if !visitedArgs {
				continue
			}
			args := make([]int, len(p.Args))
			ins := make([]types.Type, len(p.Args))
			for i := range p.Args {
				ins[i] = p.Args[i].Type
				args[i] = index.At(p.Args[i].Type).(int)
			}
			index.Set(curr.t, len(given)+len(calls))
			kind := funcProviderCall
			if p.IsStruct {
				kind = structProvider
			}
			calls = append(calls, call{
				kind:       kind,
				importPath: p.ImportPath,
				name:       p.Name,
				args:       args,
				fieldNames: p.Fields,
				ins:        ins,
				out:        curr.t,
				hasCleanup: p.HasCleanup,
				hasErr:     p.HasErr,
			})
		case pv.IsValue():
			v := pv.Value()
			if !types.Identical(v.Out, curr.t) {
				// Interface binding.  Don't create a call ourselves.
				i := index.At(v.Out)
				if i == nil {
					stk = append(stk, curr, frame{t: v.Out, from: curr.t})
					continue
				}
				index.Set(curr.t, i)
				continue
			}
			index.Set(curr.t, len(given)+len(calls))
			calls = append(calls, call{
				kind:          valueExpr,
				out:           curr.t,
				valueExpr:     v.expr,
				valueTypeInfo: v.info,
			})
		default:
			panic("unknown return value from ProviderSet.For")
		}
	}
	return calls, nil
}

// buildProviderMap creates the providerMap field for a given provider set.
// The given provider set's providerMap field is ignored.
func buildProviderMap(fset *token.FileSet, hasher typeutil.Hasher, set *ProviderSet) (*typeutil.Map, error) {
	providerMap := new(typeutil.Map)
	providerMap.SetHasher(hasher)
	setMap := new(typeutil.Map) // to *ProviderSet, for error messages
	setMap.SetHasher(hasher)

	// Process imports first, verifying that there are no conflicts between sets.
	for _, imp := range set.Imports {
		for _, k := range imp.providerMap.Keys() {
			if providerMap.At(k) != nil {
				return nil, bindingConflictError(fset, imp.Pos, k, setMap.At(k).(*ProviderSet))
			}
			providerMap.Set(k, imp.providerMap.At(k))
			setMap.Set(k, imp)
		}
	}

	// Process non-binding providers in new set.
	for _, p := range set.Providers {
		if providerMap.At(p.Out) != nil {
			return nil, bindingConflictError(fset, p.Pos, p.Out, setMap.At(p.Out).(*ProviderSet))
		}
		providerMap.Set(p.Out, p)
		setMap.Set(p.Out, set)
	}
	for _, v := range set.Values {
		if providerMap.At(v.Out) != nil {
			return nil, bindingConflictError(fset, v.Pos, v.Out, setMap.At(v.Out).(*ProviderSet))
		}
		providerMap.Set(v.Out, v)
		setMap.Set(v.Out, set)
	}

	// Process bindings in set. Must happen after the other providers to
	// ensure the concrete type is being provided.
	for _, b := range set.Bindings {
		if providerMap.At(b.Iface) != nil {
			return nil, bindingConflictError(fset, b.Pos, b.Iface, setMap.At(b.Iface).(*ProviderSet))
		}
		concrete := providerMap.At(b.Provided)
		if concrete == nil {
			pos := fset.Position(b.Pos)
			typ := types.TypeString(b.Provided, nil)
			return nil, fmt.Errorf("%v: no binding for %s", pos, typ)
		}
		providerMap.Set(b.Iface, concrete)
		setMap.Set(b.Iface, set)
	}
	return providerMap, nil
}

func verifyAcyclic(providerMap *typeutil.Map, hasher typeutil.Hasher) error {
	// We must visit every provider type inside provider map, but we don't
	// have a well-defined starting point and there may be several
	// distinct graphs. Thus, we start a depth-first search at every
	// provider, but keep a shared record of visited providers to avoid
	// duplicating work.
	visited := new(typeutil.Map) // to bool
	visited.SetHasher(hasher)
	for _, root := range providerMap.Keys() {
		// Depth-first search using a stack of trails through the provider map.
		stk := [][]types.Type{{root}}
		for len(stk) > 0 {
			curr := stk[len(stk)-1]
			stk = stk[:len(stk)-1]
			head := curr[len(curr)-1]
			if v, _ := visited.At(head).(bool); v {
				continue
			}
			visited.Set(head, true)
			switch x := providerMap.At(head).(type) {
			case nil:
				// Leaf: input.
			case *Value:
				// Leaf: values do not have dependencies.
			case *Provider:
				for _, arg := range x.Args {
					a := arg.Type
					for i, b := range curr {
						if types.Identical(a, b) {
							sb := new(strings.Builder)
							fmt.Fprintf(sb, "cycle for %s:\n", types.TypeString(a, nil))
							for j := i; j < len(curr); j++ {
								p := providerMap.At(curr[j]).(*Provider)
								fmt.Fprintf(sb, "%s (%s.%s) ->\n", types.TypeString(curr[j], nil), p.ImportPath, p.Name)
							}
							fmt.Fprintf(sb, "%s\n", types.TypeString(a, nil))
							return errors.New(sb.String())
						}
					}
					next := append(append([]types.Type(nil), curr...), a)
					stk = append(stk, next)
				}
			default:
				panic("invalid provider map value")
			}
		}
	}
	return nil
}

// bindingConflictError creates a new error describing multiple bindings
// for the same output type.
func bindingConflictError(fset *token.FileSet, pos token.Pos, typ types.Type, prevSet *ProviderSet) error {
	position := fset.Position(pos)
	typString := types.TypeString(typ, nil)
	if prevSet.Name == "" {
		prevPosition := fset.Position(prevSet.Pos)
		return fmt.Errorf("%v: multiple bindings for %s (previous binding at %v)",
			position, typString, prevPosition)
	}
	return fmt.Errorf("%v: multiple bindings for %s (previous binding in %q.%s)",
		position, typString, prevSet.PkgPath, prevSet.Name)
}
