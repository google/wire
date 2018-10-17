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

package wire

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/types/typeutil"
)

// A providerSetSrc captures the source for a type provided by a ProviderSet.
// Exactly one of the fields will be set.
type providerSetSrc struct {
	Provider *Provider
	Binding  *IfaceBinding
	Value    *Value
	Import   *ProviderSet
}

// description returns a string describing the source of p, including line numbers.
func (p *providerSetSrc) description(fset *token.FileSet, typ types.Type) string {
	quoted := func(s string) string {
		if s == "" {
			return ""
		}
		return fmt.Sprintf("%q ", s)
	}
	switch {
	case p.Provider != nil:
		kind := "provider"
		if p.Provider.IsStruct {
			kind = "struct provider"
		}
		return fmt.Sprintf("%s %s(%s)", kind, quoted(p.Provider.Name), fset.Position(p.Provider.Pos))
	case p.Binding != nil:
		return fmt.Sprintf("wire.Bind (%s)", fset.Position(p.Binding.Pos))
	case p.Value != nil:
		return fmt.Sprintf("wire.Value (%s)", fset.Position(p.Value.Pos))
	case p.Import != nil:
		return fmt.Sprintf("provider set %s(%s)", quoted(p.Import.VarName), fset.Position(p.Import.Pos))
	}
	panic("providerSetSrc with no fields set")
}

// trace returns a slice of strings describing the (possibly recursive) source
// of p, including line numbers.
func (p *providerSetSrc) trace(fset *token.FileSet, typ types.Type) []string {
	var retval []string
	// Only Imports need recursion.
	if p.Import != nil {
		if parent := p.Import.srcMap.At(typ); parent != nil {
			retval = append(retval, parent.(*providerSetSrc).trace(fset, typ)...)
		}
	}
	retval = append(retval, p.description(fset, typ))
	return retval
}

// A ProviderSet describes a set of providers.  The zero value is an empty
// ProviderSet.
type ProviderSet struct {
	// Pos is the position of the call to wire.NewSet or wire.Build that
	// created the set.
	Pos token.Pos
	// PkgPath is the import path of the package that declared this set.
	PkgPath string
	// VarName is the variable name of the set, if it came from a package
	// variable.
	VarName string

	Providers []*Provider
	Bindings  []*IfaceBinding
	Values    []*Value
	Imports   []*ProviderSet

	// providerMap maps from provided type to a *ProvidedType.
	// It includes all of the imported types.
	providerMap *typeutil.Map

	// srcMap maps from provided type to a *providerSetSrc capturing the
	// Provider, Binding, Value, or Import that provided the type.
	srcMap *typeutil.Map
}

// Outputs returns a new slice containing the set of possible types the
// provider set can produce. The order is unspecified.
func (set *ProviderSet) Outputs() []types.Type {
	return set.providerMap.Keys()
}

// For returns a ProvidedType for the given type, or the zero ProvidedType.
func (set *ProviderSet) For(t types.Type) ProvidedType {
	pt := set.providerMap.At(t)
	if pt == nil {
		return ProvidedType{}
	}
	return *pt.(*ProvidedType)
}

// An IfaceBinding declares that a type should be used to satisfy inputs
// of the given interface type.
type IfaceBinding struct {
	// Iface is the interface type, which is what can be injected.
	Iface types.Type

	// Provided is always a type that is assignable to Iface.
	Provided types.Type

	// Pos is the position where the binding was declared.
	Pos token.Pos
}

// Provider records the signature of a provider. A provider is a
// single Go object, either a function or a named struct type.
type Provider struct {
	// ImportPath is the package path that the Go object resides in.
	ImportPath string

	// Name is the name of the Go object.
	Name string

	// Pos is the source position of the func keyword or type spec
	// defining this provider.
	Pos token.Pos

	// Args is the list of data dependencies this provider has.
	Args []ProviderInput

	// IsStruct is true if this provider is a named struct type.
	// Otherwise it's a function.
	IsStruct bool

	// Fields lists the field names to populate. This will map 1:1 with
	// elements in Args.
	Fields []string

	// Out is the set of types this provider produces. It will always
	// contain at least one type.
	Out []types.Type

	// HasCleanup reports whether the provider function returns a cleanup
	// function.  (Always false for structs.)
	HasCleanup bool

	// HasErr reports whether the provider function can return an error.
	// (Always false for structs.)
	HasErr bool
}

// ProviderInput describes an incoming edge in the provider graph.
type ProviderInput struct {
	Type types.Type

	// TODO(light): Move field name into this struct.
}

// Value describes a value expression.
type Value struct {
	// Pos is the source position of the expression defining this value.
	Pos token.Pos

	// Out is the type this value produces.
	Out types.Type

	// expr is the expression passed to wire.Value.
	expr ast.Expr

	// info is the type info for the expression.
	info *types.Info
}

// Load finds all the provider sets in the given packages, as well as
// the provider sets' transitive dependencies. It may return both errors
// and Info.
func Load(bctx *build.Context, wd string, pkgs []string) (*Info, []error) {
	prog, errs := load(bctx, wd, pkgs)
	if len(errs) > 0 {
		return nil, errs
	}
	info := &Info{
		Fset: prog.Fset,
		Sets: make(map[ProviderSetID]*ProviderSet),
	}
	oc := newObjectCache(prog)
	ec := new(errorCollector)
	for _, pkgInfo := range prog.InitialPackages() {
		if isWireImport(pkgInfo.Pkg.Path()) {
			// The marker function package confuses analysis.
			continue
		}
		scope := pkgInfo.Pkg.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if !isProviderSetType(obj.Type()) {
				continue
			}
			item, errs := oc.get(obj)
			if len(errs) > 0 {
				ec.add(notePositionAll(prog.Fset.Position(obj.Pos()), errs)...)
				continue
			}
			pset := item.(*ProviderSet)
			// pset.Name may not equal name, since it could be an alias to
			// another provider set.
			id := ProviderSetID{ImportPath: pset.PkgPath, VarName: name}
			info.Sets[id] = pset
		}
		for _, f := range pkgInfo.Files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				buildCall, err := findInjectorBuild(&pkgInfo.Info, fn)
				if err != nil {
					ec.add(notePosition(prog.Fset.Position(fn.Pos()), fmt.Errorf("inject %s: %v", fn.Name.Name, err)))
					continue
				}
				if buildCall == nil {
					continue
				}
				set, errs := oc.processNewSet(pkgInfo, buildCall, "")
				if len(errs) > 0 {
					ec.add(notePositionAll(prog.Fset.Position(fn.Pos()), errs)...)
					continue
				}
				sig := pkgInfo.ObjectOf(fn.Name).Type().(*types.Signature)
				ins, out, err := injectorFuncSignature(sig)
				if err != nil {
					if w, ok := err.(*wireErr); ok {
						ec.add(notePosition(w.position, fmt.Errorf("inject %s: %v", fn.Name.Name, w.error)))
					} else {
						ec.add(notePosition(prog.Fset.Position(fn.Pos()), fmt.Errorf("inject %s: %v", fn.Name.Name, err)))
					}
					continue
				}
				_, errs = solve(prog.Fset, out.out, ins, set)
				if len(errs) > 0 {
					ec.add(mapErrors(errs, func(e error) error {
						if w, ok := e.(*wireErr); ok {
							return notePosition(w.position, fmt.Errorf("inject %s: %v", fn.Name.Name, w.error))
						}
						return notePosition(prog.Fset.Position(fn.Pos()), fmt.Errorf("inject %s: %v", fn.Name.Name, e))
					})...)
					continue
				}
				info.Injectors = append(info.Injectors, &Injector{
					ImportPath: pkgInfo.Pkg.Path(),
					FuncName:   fn.Name.Name,
				})
			}
		}
	}
	return info, ec.errors
}

// load typechecks the packages, including function body type checking
// for the packages directly named.
func load(bctx *build.Context, wd string, pkgs []string) (*loader.Program, []error) {
	var foundPkgs []*build.Package
	ec := new(errorCollector)
	for _, name := range pkgs {
		p, err := bctx.Import(name, wd, build.FindOnly)
		if err != nil {
			ec.add(err)
			continue
		}
		foundPkgs = append(foundPkgs, p)
	}
	if len(ec.errors) > 0 {
		return nil, ec.errors
	}
	conf := &loader.Config{
		Build: bctx,
		Cwd:   wd,
		TypeChecker: types.Config{
			Error: func(err error) {
				ec.add(err)
			},
		},
		TypeCheckFuncBodies: func(path string) bool {
			return importPathInPkgList(foundPkgs, path)
		},
		FindPackage: func(bctx *build.Context, importPath, fromDir string, mode build.ImportMode) (*build.Package, error) {
			// Optimistically try to load in the package with normal build tags.
			pkg, err := bctx.Import(importPath, fromDir, mode)

			// If this is the generated package, then load it in with the
			// wireinject build tag to pick up the injector template. Since
			// the *build.Context is shared between calls to FindPackage, this
			// uses a copy.
			if pkg != nil && importPathInPkgList(foundPkgs, pkg.ImportPath) {
				bctx2 := new(build.Context)
				*bctx2 = *bctx
				n := len(bctx2.BuildTags)
				bctx2.BuildTags = append(bctx2.BuildTags[:n:n], "wireinject")
				pkg, err = bctx2.Import(importPath, fromDir, mode)
			}
			return pkg, err
		},
	}
	for _, name := range pkgs {
		conf.Import(name)
	}

	prog, err := conf.Load()
	if len(ec.errors) > 0 {
		return nil, ec.errors
	}
	if err != nil {
		return nil, []error{err}
	}
	return prog, nil
}

func importPathInPkgList(pkgs []*build.Package, path string) bool {
	for _, p := range pkgs {
		if path == p.ImportPath {
			return true
		}
	}
	return false
}

// Info holds the result of Load.
type Info struct {
	Fset *token.FileSet

	// Sets contains all the provider sets in the initial packages.
	Sets map[ProviderSetID]*ProviderSet

	// Injectors contains all the injector functions in the initial packages.
	// The order is undefined.
	Injectors []*Injector
}

// A ProviderSetID identifies a named provider set.
type ProviderSetID struct {
	ImportPath string
	VarName    string
}

// String returns the ID as ""path/to/pkg".Foo".
func (id ProviderSetID) String() string {
	return strconv.Quote(id.ImportPath) + "." + id.VarName
}

// An Injector describes an injector function.
type Injector struct {
	ImportPath string
	FuncName   string
}

// String returns the injector name as ""path/to/pkg".Foo".
func (in *Injector) String() string {
	return strconv.Quote(in.ImportPath) + "." + in.FuncName
}

// objectCache is a lazily evaluated mapping of objects to Wire structures.
type objectCache struct {
	prog    *loader.Program
	objects map[objRef]objCacheEntry
	hasher  typeutil.Hasher
}

type objRef struct {
	importPath string
	name       string
}

type objCacheEntry struct {
	val  interface{} // *Provider, *ProviderSet, *IfaceBinding, or *Value
	errs []error
}

func newObjectCache(prog *loader.Program) *objectCache {
	return &objectCache{
		prog:    prog,
		objects: make(map[objRef]objCacheEntry),
		hasher:  typeutil.MakeHasher(),
	}
}

// get converts a Go object into a Wire structure. It may return a
// *Provider, an *IfaceBinding, a *ProviderSet, or a *Value.
func (oc *objectCache) get(obj types.Object) (val interface{}, errs []error) {
	ref := objRef{
		importPath: obj.Pkg().Path(),
		name:       obj.Name(),
	}
	if ent, cached := oc.objects[ref]; cached {
		return ent.val, append([]error(nil), ent.errs...)
	}
	defer func() {
		oc.objects[ref] = objCacheEntry{
			val:  val,
			errs: append([]error(nil), errs...),
		}
	}()
	switch obj := obj.(type) {
	case *types.Var:
		spec := oc.varDecl(obj)
		if len(spec.Values) == 0 {
			return nil, []error{fmt.Errorf("%v is not a provider or a provider set", obj)}
		}
		var i int
		for i = range spec.Names {
			if spec.Names[i].Name == obj.Name() {
				break
			}
		}
		return oc.processExpr(oc.prog.Package(obj.Pkg().Path()), spec.Values[i], obj.Name())
	case *types.Func:
		return processFuncProvider(oc.prog.Fset, obj)
	default:
		return nil, []error{fmt.Errorf("%v is not a provider or a provider set", obj)}
	}
}

// varDecl finds the declaration that defines the given variable.
func (oc *objectCache) varDecl(obj *types.Var) *ast.ValueSpec {
	// TODO(light): Walk files to build object -> declaration mapping, if more performant.
	// Recommended by https://golang.org/s/types-tutorial
	pkg := oc.prog.Package(obj.Pkg().Path())
	pos := obj.Pos()
	for _, f := range pkg.Files {
		tokenFile := oc.prog.Fset.File(f.Pos())
		if base := tokenFile.Base(); base <= int(pos) && int(pos) < base+tokenFile.Size() {
			path, _ := astutil.PathEnclosingInterval(f, pos, pos)
			for _, node := range path {
				if spec, ok := node.(*ast.ValueSpec); ok {
					return spec
				}
			}
		}
	}
	return nil
}

// processExpr converts an expression into a Wire structure. It may
// return a *Provider, an *IfaceBinding, a *ProviderSet, or a *Value.
func (oc *objectCache) processExpr(pkg *loader.PackageInfo, expr ast.Expr, varName string) (interface{}, []error) {
	exprPos := oc.prog.Fset.Position(expr.Pos())
	expr = astutil.Unparen(expr)
	if obj := qualifiedIdentObject(&pkg.Info, expr); obj != nil {
		item, errs := oc.get(obj)
		return item, mapErrors(errs, func(err error) error {
			return notePosition(exprPos, err)
		})
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		fnObj := qualifiedIdentObject(&pkg.Info, call.Fun)
		if fnObj == nil || !isWireImport(fnObj.Pkg().Path()) {
			return nil, []error{notePosition(exprPos, errors.New("unknown pattern"))}
		}
		switch fnObj.Name() {
		case "NewSet":
			pset, errs := oc.processNewSet(pkg, call, varName)
			return pset, notePositionAll(exprPos, errs)
		case "Bind":
			b, err := processBind(oc.prog.Fset, &pkg.Info, call)
			if err != nil {
				return nil, []error{notePosition(exprPos, err)}
			}
			return b, nil
		case "Value":
			v, err := processValue(oc.prog.Fset, &pkg.Info, call)
			if err != nil {
				return nil, []error{notePosition(exprPos, err)}
			}
			return v, nil
		case "InterfaceValue":
			v, err := processInterfaceValue(oc.prog.Fset, &pkg.Info, call)
			if err != nil {
				return nil, []error{notePosition(exprPos, err)}
			}
			return v, nil
		default:
			return nil, []error{notePosition(exprPos, errors.New("unknown pattern"))}
		}
	}
	if tn := structArgType(&pkg.Info, expr); tn != nil {
		p, errs := processStructProvider(oc.prog.Fset, tn)
		if len(errs) > 0 {
			return nil, notePositionAll(exprPos, errs)
		}
		return p, nil
	}
	return nil, []error{notePosition(exprPos, errors.New("unknown pattern"))}
}

func (oc *objectCache) processNewSet(pkg *loader.PackageInfo, call *ast.CallExpr, varName string) (*ProviderSet, []error) {
	// Assumes that call.Fun is wire.NewSet or wire.Build.

	pset := &ProviderSet{
		Pos:     call.Pos(),
		PkgPath: pkg.Pkg.Path(),
		VarName: varName,
	}
	ec := new(errorCollector)
	for _, arg := range call.Args {
		item, errs := oc.processExpr(pkg, arg, "")
		if len(errs) > 0 {
			ec.add(errs...)
			continue
		}
		switch item := item.(type) {
		case *Provider:
			pset.Providers = append(pset.Providers, item)
		case *ProviderSet:
			pset.Imports = append(pset.Imports, item)
		case *IfaceBinding:
			pset.Bindings = append(pset.Bindings, item)
		case *Value:
			pset.Values = append(pset.Values, item)
		default:
			panic("unknown item type")
		}
	}
	if len(ec.errors) > 0 {
		return nil, ec.errors
	}
	var errs []error
	pset.providerMap, pset.srcMap, errs = buildProviderMap(oc.prog.Fset, oc.hasher, pset)
	if len(errs) > 0 {
		return nil, errs
	}
	if errs := verifyAcyclic(pset.providerMap, oc.hasher); len(errs) > 0 {
		return nil, errs
	}
	return pset, nil
}

// structArgType attempts to interpret an expression as a simple struct type.
// It assumes any parentheses have been stripped.
func structArgType(info *types.Info, expr ast.Expr) *types.TypeName {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	tn, ok := qualifiedIdentObject(info, lit.Type).(*types.TypeName)
	if !ok {
		return nil
	}
	if _, isStruct := tn.Type().Underlying().(*types.Struct); !isStruct {
		return nil
	}
	return tn
}

// qualifiedIdentObject finds the object for an identifier or a
// qualified identifier, or nil if the object could not be found.
func qualifiedIdentObject(info *types.Info, expr ast.Expr) types.Object {
	switch expr := expr.(type) {
	case *ast.Ident:
		return info.ObjectOf(expr)
	case *ast.SelectorExpr:
		pkgName, ok := expr.X.(*ast.Ident)
		if !ok {
			return nil
		}
		if _, ok := info.ObjectOf(pkgName).(*types.PkgName); !ok {
			return nil
		}
		return info.ObjectOf(expr.Sel)
	default:
		return nil
	}
}

// processFuncProvider creates a provider for a function declaration.
func processFuncProvider(fset *token.FileSet, fn *types.Func) (*Provider, []error) {
	sig := fn.Type().(*types.Signature)
	fpos := fn.Pos()
	providerSig, err := funcOutput(sig)
	if err != nil {
		return nil, []error{notePosition(fset.Position(fpos), fmt.Errorf("wrong signature for provider %s: %v", fn.Name(), err))}
	}
	params := sig.Params()
	provider := &Provider{
		ImportPath: fn.Pkg().Path(),
		Name:       fn.Name(),
		Pos:        fn.Pos(),
		Args:       make([]ProviderInput, params.Len()),
		Out:        []types.Type{providerSig.out},
		HasCleanup: providerSig.cleanup,
		HasErr:     providerSig.err,
	}
	for i := 0; i < params.Len(); i++ {
		provider.Args[i] = ProviderInput{
			Type: params.At(i).Type(),
		}
		for j := 0; j < i; j++ {
			if types.Identical(provider.Args[i].Type, provider.Args[j].Type) {
				return nil, []error{notePosition(fset.Position(fpos), fmt.Errorf("provider has multiple parameters of type %s", types.TypeString(provider.Args[j].Type, nil)))}
			}
		}
	}
	return provider, nil
}

func injectorFuncSignature(sig *types.Signature) ([]types.Type, outputSignature, error) {
	out, err := funcOutput(sig)
	if err != nil {
		return nil, outputSignature{}, err
	}
	params := sig.Params()
	given := make([]types.Type, params.Len())
	for i := 0; i < params.Len(); i++ {
		given[i] = params.At(i).Type()
	}
	return given, out, nil
}

type outputSignature struct {
	out     types.Type
	cleanup bool
	err     bool
}

// funcOutput validates an injector or provider function's return signature.
func funcOutput(sig *types.Signature) (outputSignature, error) {
	results := sig.Results()
	switch results.Len() {
	case 0:
		return outputSignature{}, errors.New("no return values")
	case 1:
		return outputSignature{out: results.At(0).Type()}, nil
	case 2:
		out := results.At(0).Type()
		switch t := results.At(1).Type(); {
		case types.Identical(t, errorType):
			return outputSignature{out: out, err: true}, nil
		case types.Identical(t, cleanupType):
			return outputSignature{out: out, cleanup: true}, nil
		default:
			return outputSignature{}, fmt.Errorf("second return type is %s; must be error or func()", types.TypeString(t, nil))
		}
	case 3:
		if t := results.At(1).Type(); !types.Identical(t, cleanupType) {
			return outputSignature{}, fmt.Errorf("second return type is %s; must be func()", types.TypeString(t, nil))
		}
		if t := results.At(2).Type(); !types.Identical(t, errorType) {
			return outputSignature{}, fmt.Errorf("third return type is %s; must be error", types.TypeString(t, nil))
		}
		return outputSignature{
			out:     results.At(0).Type(),
			cleanup: true,
			err:     true,
		}, nil
	default:
		return outputSignature{}, errors.New("too many return values")
	}
}

// processStructProvider creates a provider for a named struct type.
// It produces pointer and non-pointer variants via two values in Out.
func processStructProvider(fset *token.FileSet, typeName *types.TypeName) (*Provider, []error) {
	out := typeName.Type()
	st, ok := out.Underlying().(*types.Struct)
	if !ok {
		return nil, []error{fmt.Errorf("%v does not name a struct", typeName)}
	}

	pos := typeName.Pos()
	provider := &Provider{
		ImportPath: typeName.Pkg().Path(),
		Name:       typeName.Name(),
		Pos:        pos,
		Args:       make([]ProviderInput, st.NumFields()),
		Fields:     make([]string, st.NumFields()),
		IsStruct:   true,
		Out:        []types.Type{out, types.NewPointer(out)},
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		provider.Args[i] = ProviderInput{
			Type: f.Type(),
		}
		provider.Fields[i] = f.Name()
		for j := 0; j < i; j++ {
			if types.Identical(provider.Args[i].Type, provider.Args[j].Type) {
				return nil, []error{notePosition(fset.Position(pos), fmt.Errorf("provider struct has multiple fields of type %s", types.TypeString(provider.Args[j].Type, nil)))}
			}
		}
	}
	return provider, nil
}

// processBind creates an interface binding from a wire.Bind call.
func processBind(fset *token.FileSet, info *types.Info, call *ast.CallExpr) (*IfaceBinding, error) {
	// Assumes that call.Fun is wire.Bind.

	if len(call.Args) != 2 {
		return nil, notePosition(fset.Position(call.Pos()), errors.New("call to Bind takes exactly two arguments"))
	}
	// TODO(light): Verify that arguments are simple expressions.
	ifaceArgType := info.TypeOf(call.Args[0])
	ifacePtr, ok := ifaceArgType.(*types.Pointer)
	if !ok {
		return nil, notePosition(fset.Position(call.Pos()), fmt.Errorf("first argument to Bind must be a pointer to an interface type; found %s", types.TypeString(ifaceArgType, nil)))
	}
	iface := ifacePtr.Elem()
	methodSet, ok := iface.Underlying().(*types.Interface)
	if !ok {
		return nil, notePosition(fset.Position(call.Pos()), fmt.Errorf("first argument to Bind must be a pointer to an interface type; found %s", types.TypeString(ifaceArgType, nil)))
	}
	provided := info.TypeOf(call.Args[1])
	if types.Identical(iface, provided) {
		return nil, notePosition(fset.Position(call.Pos()), errors.New("cannot bind interface to itself"))
	}
	if !types.Implements(provided, methodSet) {
		return nil, notePosition(fset.Position(call.Pos()), fmt.Errorf("%s does not implement %s", types.TypeString(provided, nil), types.TypeString(iface, nil)))
	}
	return &IfaceBinding{
		Pos:      call.Pos(),
		Iface:    iface,
		Provided: provided,
	}, nil
}

// processValue creates a value from a wire.Value call.
func processValue(fset *token.FileSet, info *types.Info, call *ast.CallExpr) (*Value, error) {
	// Assumes that call.Fun is wire.Value.

	if len(call.Args) != 1 {
		return nil, notePosition(fset.Position(call.Pos()), errors.New("call to Value takes exactly one argument"))
	}
	ok := true
	ast.Inspect(call.Args[0], func(node ast.Node) bool {
		switch expr := node.(type) {
		case nil, *ast.ArrayType, *ast.BasicLit, *ast.BinaryExpr, *ast.ChanType, *ast.CompositeLit, *ast.FuncType, *ast.Ident, *ast.IndexExpr, *ast.InterfaceType, *ast.KeyValueExpr, *ast.MapType, *ast.ParenExpr, *ast.SelectorExpr, *ast.SliceExpr, *ast.StarExpr, *ast.StructType, *ast.TypeAssertExpr:
			// Good!
		case *ast.UnaryExpr:
			if expr.Op == token.ARROW {
				ok = false
				return false
			}
		case *ast.CallExpr:
			// Only acceptable if it's a type conversion.
			if _, isFunc := info.TypeOf(expr.Fun).(*types.Signature); isFunc {
				ok = false
				return false
			}
		default:
			ok = false
			return false
		}
		return true
	})
	if !ok {
		return nil, notePosition(fset.Position(call.Pos()), errors.New("argument to Value is too complex"))
	}
	// Result type can't be an interface type; use wire.InterfaceValue for that.
	argType := info.TypeOf(call.Args[0])
	if _, isInterfaceType := argType.Underlying().(*types.Interface); isInterfaceType {
		return nil, notePosition(fset.Position(call.Pos()), fmt.Errorf("argument to Value may not be an interface value (found %s); use InterfaceValue instead", types.TypeString(argType, nil)))
	}
	return &Value{
		Pos:  call.Args[0].Pos(),
		Out:  info.TypeOf(call.Args[0]),
		expr: call.Args[0],
		info: info,
	}, nil
}

// processInterfaceValue creates a value from a wire.InterfaceValue call.
func processInterfaceValue(fset *token.FileSet, info *types.Info, call *ast.CallExpr) (*Value, error) {
	// Assumes that call.Fun is wire.InterfaceValue.

	if len(call.Args) != 2 {
		return nil, notePosition(fset.Position(call.Pos()), errors.New("call to InterfaceValue takes exactly two arguments"))
	}
	ifaceArgType := info.TypeOf(call.Args[0])
	ifacePtr, ok := ifaceArgType.(*types.Pointer)
	if !ok {
		return nil, notePosition(fset.Position(call.Pos()), fmt.Errorf("first argument to InterfaceValue must be a pointer to an interface type; found %s", types.TypeString(ifaceArgType, nil)))
	}
	iface := ifacePtr.Elem()
	methodSet, ok := iface.Underlying().(*types.Interface)
	if !ok {
		return nil, notePosition(fset.Position(call.Pos()), fmt.Errorf("first argument to InterfaceValue must be a pointer to an interface type; found %s", types.TypeString(ifaceArgType, nil)))
	}
	provided := info.TypeOf(call.Args[1])
	if !types.Implements(provided, methodSet) {
		return nil, notePosition(fset.Position(call.Pos()), fmt.Errorf("%s does not implement %s", types.TypeString(provided, nil), types.TypeString(iface, nil)))
	}
	return &Value{
		Pos:  call.Args[1].Pos(),
		Out:  iface,
		expr: call.Args[1],
		info: info,
	}, nil
}

// findInjectorBuild returns the wire.Build call if fn is an injector template.
// It returns nil if the function is not an injector template.
func findInjectorBuild(info *types.Info, fn *ast.FuncDecl) (*ast.CallExpr, error) {
	if fn.Body == nil {
		return nil, nil
	}
	numStatements := 0
	invalid := false
	var wireBuildCall *ast.CallExpr
	for _, stmt := range fn.Body.List {
		switch stmt := stmt.(type) {
		case *ast.ExprStmt:
			numStatements++
			if numStatements > 1 {
				invalid = true
			}
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			if qualifiedIdentObject(info, call.Fun) == types.Universe.Lookup("panic") {
				if len(call.Args) != 1 {
					continue
				}
				call, ok = call.Args[0].(*ast.CallExpr)
				if !ok {
					continue
				}
			}
			buildObj := qualifiedIdentObject(info, call.Fun)
			if buildObj == nil || buildObj.Pkg() == nil || !isWireImport(buildObj.Pkg().Path()) || buildObj.Name() != "Build" {
				continue
			}
			wireBuildCall = call
		case *ast.EmptyStmt:
			// Do nothing.
		case *ast.ReturnStmt:
			// Allow the function to end in a return.
			if numStatements == 0 {
				return nil, nil
			}
		default:
			invalid = true
		}

	}
	if wireBuildCall == nil {
		return nil, nil
	}
	if invalid {
		return nil, errors.New("a call to wire.Build indicates that this function is an injector, but injectors must consist of only the wire.Build call and an optional return")
	}
	return wireBuildCall, nil
}

func isWireImport(path string) bool {
	// TODO(light): This is depending on details of the current loader.
	const vendorPart = "vendor/"
	if i := strings.LastIndex(path, vendorPart); i != -1 && (i == 0 || path[i-1] == '/') {
		path = path[i+len(vendorPart):]
	}
	return path == "github.com/google/go-cloud/wire"
}

func isProviderSetType(t types.Type) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Pkg() != nil && isWireImport(obj.Pkg().Path()) && obj.Name() == "ProviderSet"
}

// ProvidedType is a pointer to a Provider or a Value. The zero value is
// a nil pointer. It also holds the concrete type that the Provider or Value
// provided.
type ProvidedType struct {
	t types.Type
	p *Provider
	v *Value
}

// IsNil reports whether pv is the zero value.
func (pv ProvidedType) IsNil() bool {
	return pv.p == nil && pv.v == nil
}

// ConcreteType returns the concrete type that was provided.
func (pv ProvidedType) ConcreteType() types.Type {
	return pv.t
}

// IsProvider reports whether pv points to a Provider.
func (pv ProvidedType) IsProvider() bool {
	return pv.p != nil
}

// IsValue reports whether pv points to a Value.
func (pv ProvidedType) IsValue() bool {
	return pv.v != nil
}

// Provider returns pv as a Provider pointer. It panics if pv points to a
// Value.
func (pv ProvidedType) Provider() *Provider {
	if pv.v != nil {
		panic("Value pointer converted to a Provider")
	}
	return pv.p
}

// Value returns pv as a Value pointer. It panics if pv points to a
// Provider.
func (pv ProvidedType) Value() *Value {
	if pv.p != nil {
		panic("Provider pointer converted to a Value")
	}
	return pv.v
}
