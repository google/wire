package goose

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// A call represents a step of an injector function.  It may be either a
// function call or a composite struct literal, depending on the value
// of isStruct.
type call struct {
	// importPath and name identify the provider to call.
	importPath string
	name       string

	// args is a list of arguments to call the provider with.  Each element is:
	// a) one of the givens (args[i] < len(given)),
	// b) the result of a previous provider call (args[i] >= len(given)), or
	// c) the zero value for the type (args[i] == -1).
	args []int

	// isStruct indicates whether this should generate a struct composite
	// literal instead of a function call.
	isStruct bool

	// fieldNames maps the arguments to struct field names.
	// This will only be set if isStruct is true.
	fieldNames []string

	// ins is the list of types this call receives as arguments.
	ins []types.Type
	// out is the type produced by this provider call.
	out types.Type
	// hasCleanup is true if the provider call returns a cleanup function.
	hasCleanup bool
	// hasErr is true if the provider call returns an error.
	hasErr bool
}

// solve finds the sequence of calls required to produce an output type
// with an optional set of provided inputs.
func solve(mc *providerSetCache, out types.Type, given []types.Type, sets []symref) ([]call, error) {
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
			pp := p.(*Provider)
			return nil, fmt.Errorf("input of %s conflicts with provider %s at %s", types.TypeString(g, nil), pp.Name, mc.fset.Position(pp.Pos))
		}
		index.Set(g, i)
	}

	// Topological sort of the directed graph defined by the providers
	// using a depth-first search. The graph may contain cycles, which
	// should trigger an error.
	var calls []call
	var visit func(trail []ProviderInput) error
	visit = func(trail []ProviderInput) error {
		typ := trail[len(trail)-1].Type
		if index.At(typ) != nil {
			return nil
		}
		for _, in := range trail[:len(trail)-1] {
			if types.Identical(typ, in.Type) {
				// TODO(light): describe cycle
				return fmt.Errorf("cycle for %s", types.TypeString(typ, nil))
			}
		}

		p, _ := providers.At(typ).(*Provider)
		if p == nil {
			if trail[len(trail)-1].Optional {
				return nil
			}
			if len(trail) == 1 {
				return fmt.Errorf("no provider found for %s (output of injector)", types.TypeString(typ, nil))
			}
			// TODO(light): give name of provider
			return fmt.Errorf("no provider found for %s (required by provider of %s)", types.TypeString(typ, nil), types.TypeString(trail[len(trail)-2].Type, nil))
		}
		if !types.Identical(p.Out, typ) {
			// Interface binding.  Don't create a call ourselves.
			if err := visit(append(trail, ProviderInput{Type: p.Out})); err != nil {
				return err
			}
			index.Set(typ, index.At(p.Out))
			return nil
		}
		for _, a := range p.Args {
			// TODO(light): this will discard grown trail arrays.
			if err := visit(append(trail, a)); err != nil {
				return err
			}
		}
		args := make([]int, len(p.Args))
		ins := make([]types.Type, len(p.Args))
		for i := range p.Args {
			ins[i] = p.Args[i].Type
			if x := index.At(p.Args[i].Type); x != nil {
				args[i] = x.(int)
			} else {
				args[i] = -1
			}
		}
		index.Set(typ, len(given)+len(calls))
		calls = append(calls, call{
			importPath: p.ImportPath,
			name:       p.Name,
			args:       args,
			isStruct:   p.IsStruct,
			fieldNames: p.Fields,
			ins:        ins,
			out:        typ,
			hasCleanup: p.HasCleanup,
			hasErr:     p.HasErr,
		})
		return nil
	}
	if err := visit([]ProviderInput{{Type: out}}); err != nil {
		return nil, err
	}
	return calls, nil
}

func buildProviderMap(mc *providerSetCache, sets []symref) (*typeutil.Map, error) {
	type nextEnt struct {
		to symref

		from symref
		pos  token.Pos
	}
	type binding struct {
		IfaceBinding
		pset symref
		from symref
	}

	pm := new(typeutil.Map) // to *providerInfo
	var bindings []binding
	visited := make(map[symref]struct{})
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
		pset, err := mc.get(curr.to)
		if err != nil {
			if !curr.pos.IsValid() {
				return nil, err
			}
			return nil, fmt.Errorf("%v: %v", mc.fset.Position(curr.pos), err)
		}
		for _, p := range pset.Providers {
			if prev := pm.At(p.Out); prev != nil {
				pos := mc.fset.Position(p.Pos)
				typ := types.TypeString(p.Out, nil)
				prevPos := mc.fset.Position(prev.(*Provider).Pos)
				if curr.from.importPath == "" {
					// Provider set is imported directly by injector.
					return nil, fmt.Errorf("%v: multiple bindings for %s (added by injector, previous binding at %v)", pos, typ, prevPos)
				}
				return nil, fmt.Errorf("%v: multiple bindings for %s (imported by %v, previous binding at %v)", pos, typ, curr.from, prevPos)
			}
			pm.Set(p.Out, p)
		}
		for _, b := range pset.Bindings {
			bindings = append(bindings, binding{
				IfaceBinding: b,
				pset:         curr.to,
				from:         curr.from,
			})
		}
		for _, imp := range pset.Imports {
			next = append(next, nextEnt{to: imp.symref(), from: curr.to, pos: imp.Pos})
		}
	}
	for _, b := range bindings {
		if prev := pm.At(b.Iface); prev != nil {
			pos := mc.fset.Position(b.Pos)
			typ := types.TypeString(b.Iface, nil)
			// TODO(light): error message for conflicting with another interface binding will point at provider instead of binding.
			prevPos := mc.fset.Position(prev.(*Provider).Pos)
			if b.from.importPath == "" {
				// Provider set is imported directly by injector.
				return nil, fmt.Errorf("%v: multiple bindings for %s (added by injector, previous binding at %v)", pos, typ, prevPos)
			}
			return nil, fmt.Errorf("%v: multiple bindings for %s (imported by %v, previous binding at %v)", pos, typ, b.from, prevPos)
		}
		concrete := pm.At(b.Provided)
		if concrete == nil {
			pos := mc.fset.Position(b.Pos)
			typ := types.TypeString(b.Provided, nil)
			if b.from.importPath == "" {
				// Concrete provider is imported directly by injector.
				return nil, fmt.Errorf("%v: no binding for %s", pos, typ)
			}
			return nil, fmt.Errorf("%v: no binding for %s (imported by %v)", pos, typ, b.from)
		}
		pm.Set(b.Iface, concrete)
	}
	return pm, nil
}
