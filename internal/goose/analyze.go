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
			pp := p.(*providerInfo)
			return nil, fmt.Errorf("input of %s conflicts with provider %s at %s", types.TypeString(g, nil), pp.name, mc.fset.Position(pp.pos))
		}
		index.Set(g, i)
	}

	// Topological sort of the directed graph defined by the providers
	// using a depth-first search. The graph may contain cycles, which
	// should trigger an error.
	var calls []call
	var visit func(trail []providerInput) error
	visit = func(trail []providerInput) error {
		typ := trail[len(trail)-1].typ
		if index.At(typ) != nil {
			return nil
		}
		for _, in := range trail[:len(trail)-1] {
			if types.Identical(typ, in.typ) {
				// TODO(light): describe cycle
				return fmt.Errorf("cycle for %s", types.TypeString(typ, nil))
			}
		}

		p, _ := providers.At(typ).(*providerInfo)
		if p == nil {
			if trail[len(trail)-1].optional {
				return nil
			}
			if len(trail) == 1 {
				return fmt.Errorf("no provider found for %s (output of injector)", types.TypeString(typ, nil))
			}
			// TODO(light): give name of provider
			return fmt.Errorf("no provider found for %s (required by provider of %s)", types.TypeString(typ, nil), types.TypeString(trail[len(trail)-2].typ, nil))
		}
		if !types.Identical(p.out, typ) {
			// Interface binding.  Don't create a call ourselves.
			if err := visit(append(trail, providerInput{typ: p.out})); err != nil {
				return err
			}
			index.Set(typ, index.At(p.out))
			return nil
		}
		for _, a := range p.args {
			// TODO(light): this will discard grown trail arrays.
			if err := visit(append(trail, a)); err != nil {
				return err
			}
		}
		args := make([]int, len(p.args))
		ins := make([]types.Type, len(p.args))
		for i := range p.args {
			ins[i] = p.args[i].typ
			if x := index.At(p.args[i].typ); x != nil {
				args[i] = x.(int)
			} else {
				args[i] = -1
			}
		}
		index.Set(typ, len(given)+len(calls))
		calls = append(calls, call{
			importPath: p.importPath,
			name:       p.name,
			args:       args,
			isStruct:   p.isStruct,
			fieldNames: p.fields,
			ins:        ins,
			out:        typ,
			hasCleanup: p.hasCleanup,
			hasErr:     p.hasErr,
		})
		return nil
	}
	if err := visit([]providerInput{{typ: out}}); err != nil {
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
		ifaceBinding
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
		for _, p := range pset.providers {
			if prev := pm.At(p.out); prev != nil {
				pos := mc.fset.Position(p.pos)
				typ := types.TypeString(p.out, nil)
				prevPos := mc.fset.Position(prev.(*providerInfo).pos)
				if curr.from.importPath == "" {
					// Provider set is imported directly by injector.
					return nil, fmt.Errorf("%v: multiple bindings for %s (added by injector, previous binding at %v)", pos, typ, prevPos)
				}
				return nil, fmt.Errorf("%v: multiple bindings for %s (imported by %v, previous binding at %v)", pos, typ, curr.from, prevPos)
			}
			pm.Set(p.out, p)
		}
		for _, b := range pset.bindings {
			bindings = append(bindings, binding{
				ifaceBinding: b,
				pset:         curr.to,
				from:         curr.from,
			})
		}
		for _, imp := range pset.imports {
			next = append(next, nextEnt{to: imp.symref, from: curr.to, pos: imp.pos})
		}
	}
	for _, b := range bindings {
		if prev := pm.At(b.iface); prev != nil {
			pos := mc.fset.Position(b.pos)
			typ := types.TypeString(b.iface, nil)
			// TODO(light): error message for conflicting with another interface binding will point at provider instead of binding.
			prevPos := mc.fset.Position(prev.(*providerInfo).pos)
			if b.from.importPath == "" {
				// Provider set is imported directly by injector.
				return nil, fmt.Errorf("%v: multiple bindings for %s (added by injector, previous binding at %v)", pos, typ, prevPos)
			}
			return nil, fmt.Errorf("%v: multiple bindings for %s (imported by %v, previous binding at %v)", pos, typ, b.from, prevPos)
		}
		concrete := pm.At(b.provided)
		if concrete == nil {
			pos := mc.fset.Position(b.pos)
			typ := types.TypeString(b.provided, nil)
			if b.from.importPath == "" {
				// Concrete provider is imported directly by injector.
				return nil, fmt.Errorf("%v: no binding for %s", pos, typ)
			}
			return nil, fmt.Errorf("%v: no binding for %s (imported by %v)", pos, typ, b.from)
		}
		pm.Set(b.iface, concrete)
	}
	return pm, nil
}
