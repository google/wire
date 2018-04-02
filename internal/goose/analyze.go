package goose

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// A call represents a step of an injector function.
type call struct {
	// importPath and funcName identify the provider function to call.
	importPath string
	funcName   string

	// args is a list of arguments to call the provider with.  Each element is either:
	// a) one of the givens (args[i] < len(given)) or
	// b) the result of a previous provider call (args[i] >= len(given)).
	args []int

	// out is the type produced by this provider call.
	out types.Type

	// hasErr is true if the provider call returns an error.
	hasErr bool
}

// solve finds the sequence of calls required to produce an output type
// with an optional set of provided inputs.
func solve(mc *providerSetCache, out types.Type, given []types.Type, sets []providerSetRef) ([]call, error) {
	for i, g := range given {
		for _, h := range given[:i] {
			if types.Identical(g, h) {
				return nil, fmt.Errorf("multiple inputs of the same type %s", types.TypeString(g, nil))
			}
		}
	}
	providers, err := buildProviderMap(mc, sets)
	if err != nil {
		return nil, err
	}

	// Start building the mapping of type to local variable of the given type.
	// The first len(given) local variables are the given types.
	index := new(typeutil.Map)
	for i, g := range given {
		if p := providers.At(g); p != nil {
			pp := p.(*providerInfo)
			return nil, fmt.Errorf("input of %s conflicts with provider %s at %s", types.TypeString(g, nil), pp.funcName, mc.fset.Position(pp.pos))
		}
		index.Set(g, i)
	}

	// Topological sort of the directed graph defined by the providers
	// using a depth-first search. The graph may contain cycles, which
	// should trigger an error.
	var calls []call
	var visit func(trail []types.Type) error
	visit = func(trail []types.Type) error {
		typ := trail[len(trail)-1]
		if index.At(typ) != nil {
			return nil
		}
		for _, t := range trail[:len(trail)-1] {
			if types.Identical(typ, t) {
				// TODO(light): describe cycle
				return fmt.Errorf("cycle for %s", types.TypeString(typ, nil))
			}
		}

		p, _ := providers.At(typ).(*providerInfo)
		if p == nil {
			if len(trail) == 1 {
				return fmt.Errorf("no provider found for %s (output of injector)", types.TypeString(typ, nil))
			}
			// TODO(light): give name of provider
			return fmt.Errorf("no provider found for %s (required by provider of %s)", types.TypeString(typ, nil), types.TypeString(trail[len(trail)-2], nil))
		}
		for _, a := range p.args {
			// TODO(light): this will discard grown trail arrays.
			if err := visit(append(trail, a)); err != nil {
				return err
			}
		}
		args := make([]int, len(p.args))
		for i := range p.args {
			args[i] = index.At(p.args[i]).(int)
		}
		index.Set(typ, len(given)+len(calls))
		calls = append(calls, call{
			importPath: p.importPath,
			funcName:   p.funcName,
			args:       args,
			out:        typ,
			hasErr:     p.hasErr,
		})
		return nil
	}
	if err := visit([]types.Type{out}); err != nil {
		return nil, err
	}
	return calls, nil
}

func buildProviderMap(mc *providerSetCache, sets []providerSetRef) (*typeutil.Map, error) {
	type nextEnt struct {
		to providerSetRef

		from providerSetRef
		pos  token.Pos
	}

	pm := new(typeutil.Map) // to *providerInfo
	visited := make(map[providerSetRef]struct{})
	var next []nextEnt
	for _, ref := range sets {
		next = append(next, nextEnt{to: ref})
	}
	for len(next) > 0 {
		curr := next[0]
		copy(next, next[1:])
		next = next[:len(next)-1]
		if _, skip := visited[curr.to]; skip {
			continue
		}
		visited[curr.to] = struct{}{}
		mod, err := mc.get(curr.to)
		if err != nil {
			if !curr.pos.IsValid() {
				return nil, err
			}
			return nil, fmt.Errorf("%v: %v", mc.fset.Position(curr.pos), err)
		}
		for _, p := range mod.providers {
			if prev := pm.At(p.out); prev != nil {
				pos := mc.fset.Position(p.pos)
				typ := types.TypeString(p.out, nil)
				prevPos := mc.fset.Position(prev.(*providerInfo).pos)
				if curr.from.importPath != "" {
					return nil, fmt.Errorf("%v: multiple bindings for %s (added by injector, previous binding at %v)", pos, typ, prevPos)
				}
				return nil, fmt.Errorf("%v: multiple bindings for %s (imported by %v, previous binding at %v)", pos, typ, curr.from, prevPos)
			}
			pm.Set(p.out, p)
		}
		for _, imp := range mod.imports {
			next = append(next, nextEnt{to: imp.providerSetRef, from: curr.to, pos: imp.pos})
		}
	}
	return pm, nil
}
