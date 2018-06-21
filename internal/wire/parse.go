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
	"go/build"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/types/typeutil"
)

// A ProviderSet describes a set of providers.  The zero value is an empty
// ProviderSet.
type ProviderSet struct {
	// Pos is the position of the call to wire.NewSet or wire.Build that
	// created the set.
	Pos token.Pos
	// PkgPath is the import path of the package that declared this set.
	PkgPath string
	// Name is the variable name of the set, if it came from a package
	// variable.
	Name string

	Providers []*Provider
	Bindings  []*IfaceBinding
	Values    []*Value
	Imports   []*ProviderSet

	// providerMap maps from provided type to a *Provider or *Value.
	// It includes all of the imported types.
	providerMap *typeutil.Map
}

// Outputs returns a new slice containing the set of possible types the
// provider set can produce. The order is unspecified.
func (set *ProviderSet) Outputs() []types.Type {
	return set.providerMap.Keys()
}

// For returns the provider or value for the given type, or the zero
// ProviderOrValue.
func (set *ProviderSet) For(t types.Type) ProviderOrValue {
	switch x := set.providerMap.At(t).(type) {
	case nil:
		return ProviderOrValue{}
	case *Provider:
		return ProviderOrValue{p: x}
	case *Value:
		return ProviderOrValue{v: x}
	default:
		panic("invalid value in typeMap")
	}
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

	// Out is the type this provider produces.
	Out types.Type

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
// the provider sets' transitive dependencies.
func Load(bctx *build.Context, wd string, pkgs []string) (*Info, error) {
	// TODO(light): Stop errors from printing to stderr.
	conf := &loader.Config{
		Build:               bctx,
		Cwd:                 wd,
		TypeCheckFuncBodies: func(string) bool { return false },
	}
	for _, p := range pkgs {
		conf.Import(p)
	}
	prog, err := conf.Load()
	if err != nil {
		return nil, fmt.Errorf("load: %v", err)
	}
	info := &Info{
		Fset: prog.Fset,
		Sets: make(map[ProviderSetID]*ProviderSet),
	}
	oc := newObjectCache(prog)
	for _, pkgInfo := range prog.InitialPackages() {
		scope := pkgInfo.Pkg.Scope()
		for _, name := range scope.Names() {
			item, err := oc.get(scope.Lookup(name))
			if err != nil {
				continue
			}
			pset, ok := item.(*ProviderSet)
			if !ok {
				continue
			}
			// pset.Name may not equal name, since it could be an alias to
			// another provider set.
			id := ProviderSetID{ImportPath: pset.PkgPath, VarName: name}
			info.Sets[id] = pset
		}
	}
	return info, nil
}

// Info holds the result of Load.
type Info struct {
	Fset *token.FileSet

	// Sets contains all the provider sets in the initial packages.
	Sets map[ProviderSetID]*ProviderSet
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

// objectCache is a lazily evaluated mapping of objects to Wire structures.
type objectCache struct {
	prog    *loader.Program
	objects map[objRef]interface{} // *Provider, *ProviderSet, *IfaceBinding, or *Value
	hasher  typeutil.Hasher
}

type objRef struct {
	importPath string
	name       string
}

func newObjectCache(prog *loader.Program) *objectCache {
	return &objectCache{
		prog:    prog,
		objects: make(map[objRef]interface{}),
		hasher:  typeutil.MakeHasher(),
	}
}

// get converts a Go object into a Wire structure. It may return a
// *Provider, a structProviderPair, an *IfaceBinding, a *ProviderSet,
// or a *Value.
func (oc *objectCache) get(obj types.Object) (interface{}, error) {
	ref := objRef{
		importPath: obj.Pkg().Path(),
		name:       obj.Name(),
	}
	if val, cached := oc.objects[ref]; cached {
		if val == nil {
			return nil, fmt.Errorf("%v is not a provider or a provider set", obj)
		}
		return val, nil
	}
	switch obj := obj.(type) {
	case *types.Var:
		spec := oc.varDecl(obj)
		if len(spec.Values) == 0 {
			return nil, fmt.Errorf("%v is not a provider or a provider set", obj)
		}
		var i int
		for i = range spec.Names {
			if spec.Names[i].Name == obj.Name() {
				break
			}
		}
		return oc.processExpr(oc.prog.Package(obj.Pkg().Path()), spec.Values[i])
	case *types.Func:
		p, err := processFuncProvider(oc.prog.Fset, obj)
		if err != nil {
			oc.objects[ref] = nil
			return nil, err
		}
		oc.objects[ref] = p
		return p, nil
	default:
		oc.objects[ref] = nil
		return nil, fmt.Errorf("%v is not a provider or a provider set", obj)
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
// return a *Provider, a structProviderPair, an *IfaceBinding, a
// *ProviderSet, or a *Value.
func (oc *objectCache) processExpr(pkg *loader.PackageInfo, expr ast.Expr) (interface{}, error) {
	exprPos := oc.prog.Fset.Position(expr.Pos())
	expr = astutil.Unparen(expr)
	if obj := qualifiedIdentObject(&pkg.Info, expr); obj != nil {
		item, err := oc.get(obj)
		if err != nil {
			return nil, fmt.Errorf("%v: %v", exprPos, err)
		}
		return item, nil
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		fnObj := qualifiedIdentObject(&pkg.Info, call.Fun)
		if fnObj == nil || !isWireImport(fnObj.Pkg().Path()) {
			return nil, fmt.Errorf("%v: unknown pattern", exprPos)
		}
		switch fnObj.Name() {
		case "NewSet":
			pset, err := oc.processNewSet(pkg, call)
			if err != nil {
				return nil, fmt.Errorf("%v: %v", exprPos, err)
			}
			return pset, nil
		case "Bind":
			b, err := processBind(oc.prog.Fset, &pkg.Info, call)
			if err != nil {
				return nil, fmt.Errorf("%v: %v", exprPos, err)
			}
			return b, nil
		case "Value":
			v, err := processValue(oc.prog.Fset, &pkg.Info, call)
			if err != nil {
				return nil, fmt.Errorf("%v: %v", exprPos, err)
			}
			return v, nil
		default:
			return nil, fmt.Errorf("%v: unknown pattern", exprPos)
		}
	}
	if tn := structArgType(&pkg.Info, expr); tn != nil {
		p, err := processStructProvider(oc.prog.Fset, tn)
		if err != nil {
			return nil, fmt.Errorf("%v: %v", exprPos, err)
		}
		ptrp := new(Provider)
		*ptrp = *p
		ptrp.Out = types.NewPointer(p.Out)
		return structProviderPair{p, ptrp}, nil
	}
	return nil, fmt.Errorf("%v: unknown pattern", exprPos)
}

type structProviderPair struct {
	provider    *Provider
	ptrProvider *Provider
}

func (oc *objectCache) processNewSet(pkg *loader.PackageInfo, call *ast.CallExpr) (*ProviderSet, error) {
	// Assumes that call.Fun is wire.NewSet or wire.Build.

	pset := &ProviderSet{
		Pos:     call.Pos(),
		PkgPath: pkg.Pkg.Path(),
	}
	for _, arg := range call.Args {
		item, err := oc.processExpr(pkg, arg)
		if err != nil {
			return nil, err
		}
		switch item := item.(type) {
		case *Provider:
			pset.Providers = append(pset.Providers, item)
		case *ProviderSet:
			pset.Imports = append(pset.Imports, item)
		case *IfaceBinding:
			pset.Bindings = append(pset.Bindings, item)
		case structProviderPair:
			pset.Providers = append(pset.Providers, item.provider, item.ptrProvider)
		case *Value:
			pset.Values = append(pset.Values, item)
		default:
			panic("unknown item type")
		}
	}
	var err error
	pset.providerMap, err = buildProviderMap(oc.prog.Fset, oc.hasher, pset)
	if err != nil {
		return nil, err
	}
	if err := verifyAcyclic(pset.providerMap, oc.hasher); err != nil {
		return nil, err
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
func processFuncProvider(fset *token.FileSet, fn *types.Func) (*Provider, error) {
	sig := fn.Type().(*types.Signature)
	fpos := fn.Pos()
	providerSig, err := funcOutput(sig)
	if err != nil {
		return nil, fmt.Errorf("%v: wrong signature for provider %s: %v", fset.Position(fpos), fn.Name(), err)
	}
	params := sig.Params()
	provider := &Provider{
		ImportPath: fn.Pkg().Path(),
		Name:       fn.Name(),
		Pos:        fn.Pos(),
		Args:       make([]ProviderInput, params.Len()),
		Out:        providerSig.out,
		HasCleanup: providerSig.cleanup,
		HasErr:     providerSig.err,
	}
	for i := 0; i < params.Len(); i++ {
		provider.Args[i] = ProviderInput{
			Type: params.At(i).Type(),
		}
		for j := 0; j < i; j++ {
			if types.Identical(provider.Args[i].Type, provider.Args[j].Type) {
				return nil, fmt.Errorf("%v: provider has multiple parameters of type %s", fset.Position(fpos), types.TypeString(provider.Args[j].Type, nil))
			}
		}
	}
	return provider, nil
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
// It only produces the non-pointer variant.
func processStructProvider(fset *token.FileSet, typeName *types.TypeName) (*Provider, error) {
	out := typeName.Type()
	st, ok := out.Underlying().(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("%v does not name a struct", typeName)
	}

	pos := typeName.Pos()
	provider := &Provider{
		ImportPath: typeName.Pkg().Path(),
		Name:       typeName.Name(),
		Pos:        pos,
		Args:       make([]ProviderInput, st.NumFields()),
		Fields:     make([]string, st.NumFields()),
		IsStruct:   true,
		Out:        out,
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		provider.Args[i] = ProviderInput{
			Type: f.Type(),
		}
		provider.Fields[i] = f.Name()
		for j := 0; j < i; j++ {
			if types.Identical(provider.Args[i].Type, provider.Args[j].Type) {
				return nil, fmt.Errorf("%v: provider struct has multiple fields of type %s", fset.Position(pos), types.TypeString(provider.Args[j].Type, nil))
			}
		}
	}
	return provider, nil
}

// processBind creates an interface binding from a wire.Bind call.
func processBind(fset *token.FileSet, info *types.Info, call *ast.CallExpr) (*IfaceBinding, error) {
	// Assumes that call.Fun is wire.Bind.

	if len(call.Args) != 2 {
		return nil, fmt.Errorf("%v: call to Bind takes exactly two arguments", fset.Position(call.Pos()))
	}
	// TODO(light): Verify that arguments are simple expressions.
	ifaceArgType := info.TypeOf(call.Args[0])
	ifacePtr, ok := ifaceArgType.(*types.Pointer)
	if !ok {
		return nil, fmt.Errorf("%v: first argument to bind must be a pointer to an interface type; found %s", fset.Position(call.Pos()), types.TypeString(ifaceArgType, nil))
	}
	methodSet, ok := ifacePtr.Elem().Underlying().(*types.Interface)
	if !ok {
		return nil, fmt.Errorf("%v: first argument to bind must be a pointer to an interface type; found %s", fset.Position(call.Pos()), types.TypeString(ifaceArgType, nil))
	}
	provided := info.TypeOf(call.Args[1])
	if types.Identical(ifacePtr.Elem(), provided) {
		return nil, fmt.Errorf("%v: cannot bind interface to itself", fset.Position(call.Pos()))
	}
	if !types.Implements(provided, methodSet) {
		return nil, fmt.Errorf("%v: %s does not implement %s", fset.Position(call.Pos()), types.TypeString(provided, nil), types.TypeString(ifaceArgType, nil))
	}
	return &IfaceBinding{
		Pos:      call.Pos(),
		Iface:    ifacePtr.Elem(),
		Provided: provided,
	}, nil
}

// processValue creates a value from a wire.Value call.
func processValue(fset *token.FileSet, info *types.Info, call *ast.CallExpr) (*Value, error) {
	// Assumes that call.Fun is wire.Value.

	if len(call.Args) != 1 {
		return nil, fmt.Errorf("%v: call to Value takes exactly one argument", fset.Position(call.Pos()))
	}
	ok := true
	ast.Inspect(call.Args[0], func(node ast.Node) bool {
		switch node.(type) {
		case nil, *ast.ArrayType, *ast.BasicLit, *ast.BinaryExpr, *ast.ChanType, *ast.CompositeLit, *ast.FuncType, *ast.Ident, *ast.IndexExpr, *ast.InterfaceType, *ast.KeyValueExpr, *ast.MapType, *ast.ParenExpr, *ast.SelectorExpr, *ast.SliceExpr, *ast.StarExpr, *ast.StructType, *ast.TypeAssertExpr:
			// Good!
		case *ast.UnaryExpr:
			expr := node.(*ast.UnaryExpr)
			if expr.Op == token.ARROW {
				ok = false
				return false
			}
		case *ast.CallExpr:
			// Only acceptable if it's a type conversion.
			call := node.(*ast.CallExpr)
			if _, isFunc := info.TypeOf(call.Fun).(*types.Signature); isFunc {
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
		return nil, fmt.Errorf("%v: argument to Value is too complex", fset.Position(call.Pos()))
	}
	return &Value{
		Pos:  call.Args[0].Pos(),
		Out:  info.TypeOf(call.Args[0]),
		expr: call.Args[0],
		info: info,
	}, nil
}

// isInjector checks whether a given function declaration is an
// injector template, returning the wire.Build call. It returns nil if
// the function is not an injector template.
func isInjector(info *types.Info, fn *ast.FuncDecl) *ast.CallExpr {
	if fn.Body == nil {
		return nil
	}
	var only *ast.ExprStmt
	for _, stmt := range fn.Body.List {
		switch stmt := stmt.(type) {
		case *ast.ExprStmt:
			if only != nil {
				return nil
			}
			only = stmt
		case *ast.EmptyStmt:
			// Do nothing.
		case *ast.ReturnStmt:
			// Allow the function to end in a return.
			if only == nil {
				return nil
			}
		default:
			return nil
		}
	}
	if only == nil {
		return nil
	}
	call, ok := only.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if qualifiedIdentObject(info, call.Fun) == types.Universe.Lookup("panic") {
		if len(call.Args) != 1 {
			return nil
		}
		call, ok = call.Args[0].(*ast.CallExpr)
		if !ok {
			return nil
		}
	}
	buildObj := qualifiedIdentObject(info, call.Fun)
	if !isWireImport(buildObj.Pkg().Path()) || buildObj.Name() != "Build" {
		return nil
	}
	return call
}

func isWireImport(path string) bool {
	// TODO(light): This is depending on details of the current loader.
	const vendorPart = "vendor/"
	if i := strings.LastIndex(path, vendorPart); i != -1 && (i == 0 || path[i-1] == '/') {
		path = path[i+len(vendorPart):]
	}
	return path == "github.com/google/go-x-cloud/wire"
}

// ProviderOrValue is a pointer to a Provider or a Value. The zero value is
// a nil pointer.
type ProviderOrValue struct {
	p *Provider
	v *Value
}

// IsNil reports whether pv is the zero value.
func (pv ProviderOrValue) IsNil() bool {
	return pv.p == nil && pv.v == nil
}

// IsProvider reports whether pv points to a Provider.
func (pv ProviderOrValue) IsProvider() bool {
	return pv.p != nil
}

// IsValue reports whether pv points to a Value.
func (pv ProviderOrValue) IsValue() bool {
	return pv.v != nil
}

// Provider returns pv as a Provider pointer. It panics if pv points to a
// Value.
func (pv ProviderOrValue) Provider() *Provider {
	if pv.v != nil {
		panic("Value pointer converted to a Provider")
	}
	return pv.p
}

// Provider returns pv as a Value pointer. It panics if pv points to a
// Provider.
func (pv ProviderOrValue) Value() *Value {
	if pv.p != nil {
		panic("Provider pointer converted to a Value")
	}
	return pv.v
}
