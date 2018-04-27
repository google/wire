package goose

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/loader"
)

// A ProviderSet describes a set of providers.  The zero value is an empty
// ProviderSet.
type ProviderSet struct {
	// Pos is the position of the call to goose.NewSet or goose.Use that
	// created the set.
	Pos token.Pos
	// PkgPath is the import path of the package that declared this set.
	PkgPath string
	// Name is the variable name of the set, if it came from a package
	// variable.
	Name string

	Providers []*Provider
	Bindings  []*IfaceBinding
	Imports   []*ProviderSet
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

// objectCache is a lazily evaluated mapping of objects to goose structures.
type objectCache struct {
	prog    *loader.Program
	objects map[objRef]interface{} // *Provider or *ProviderSet
}

type objRef struct {
	importPath string
	name       string
}

func newObjectCache(prog *loader.Program) *objectCache {
	return &objectCache{
		prog:    prog,
		objects: make(map[objRef]interface{}),
	}
}

// get converts a Go object into a goose structure. It may return a
// *Provider, a structProviderPair, an *IfaceBinding, or a *ProviderSet.
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

// processExpr converts an expression into a goose structure. It may
// return a *Provider, a structProviderPair, an *IfaceBinding, or a
// *ProviderSet.
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
		if fnObj == nil || !isGooseImport(fnObj.Pkg().Path()) {
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
	// Assumes that call.Fun is goose.NewSet or goose.Use.

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
		default:
			panic("unknown item type")
		}
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
	r := sig.Results()
	var hasCleanup, hasErr bool
	switch r.Len() {
	case 1:
		hasCleanup, hasErr = false, false
	case 2:
		switch t := r.At(1).Type(); {
		case types.Identical(t, errorType):
			hasCleanup, hasErr = false, true
		case types.Identical(t, cleanupType):
			hasCleanup, hasErr = true, false
		default:
			return nil, fmt.Errorf("%v: wrong signature for provider %s: second return type must be error or func()", fset.Position(fpos), fn.Name())
		}
	case 3:
		if t := r.At(1).Type(); !types.Identical(t, cleanupType) {
			return nil, fmt.Errorf("%v: wrong signature for provider %s: second return type must be func()", fset.Position(fpos), fn.Name())
		}
		if t := r.At(2).Type(); !types.Identical(t, errorType) {
			return nil, fmt.Errorf("%v: wrong signature for provider %s: third return type must be error", fset.Position(fpos), fn.Name())
		}
		hasCleanup, hasErr = true, true
	default:
		return nil, fmt.Errorf("%v: wrong signature for provider %s: must have one return value and optional error", fset.Position(fpos), fn.Name())
	}
	out := r.At(0).Type()
	params := sig.Params()
	provider := &Provider{
		ImportPath: fn.Pkg().Path(),
		Name:       fn.Name(),
		Pos:        fn.Pos(),
		Args:       make([]ProviderInput, params.Len()),
		Out:        out,
		HasCleanup: hasCleanup,
		HasErr:     hasErr,
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

// processBind creates an interface binding from a goose.Bind call.
func processBind(fset *token.FileSet, info *types.Info, call *ast.CallExpr) (*IfaceBinding, error) {
	// Assumes that call.Fun is goose.Bind.

	if len(call.Args) != 2 {
		return nil, fmt.Errorf("%v: call to Bind takes exactly two arguments", fset.Position(call.Pos()))
	}
	// TODO(light): Verify that arguments are simple expressions.
	iface := info.TypeOf(call.Args[0])
	methodSet, ok := iface.Underlying().(*types.Interface)
	if !ok {
		return nil, fmt.Errorf("%v: first argument to bind must be of interface type; found %s", fset.Position(call.Pos()), types.TypeString(iface, nil))
	}
	provided := info.TypeOf(call.Args[1])
	if types.Identical(iface, provided) {
		return nil, fmt.Errorf("%v: cannot bind interface to itself", fset.Position(call.Pos()))
	}
	if !types.Implements(provided, methodSet) {
		return nil, fmt.Errorf("%v: %s does not implement %s", fset.Position(call.Pos()), types.TypeString(provided, nil), types.TypeString(iface, nil))
	}
	return &IfaceBinding{
		Pos:      call.Pos(),
		Iface:    iface,
		Provided: provided,
	}, nil
}

// isInjector checks whether a given function declaration is an
// injector template, returning the goose.Use call. It returns nil if
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
		default:
			return nil
		}
	}
	panicCall, ok := only.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	panicIdent, ok := panicCall.Fun.(*ast.Ident)
	if !ok {
		return nil
	}
	if info.ObjectOf(panicIdent) != types.Universe.Lookup("panic") {
		return nil
	}
	if len(panicCall.Args) != 1 {
		return nil
	}
	useCall, ok := panicCall.Args[0].(*ast.CallExpr)
	if !ok {
		return nil
	}
	useObj := qualifiedIdentObject(info, useCall.Fun)
	if !isGooseImport(useObj.Pkg().Path()) || useObj.Name() != "Use" {
		return nil
	}
	return useCall
}

func isGooseImport(path string) bool {
	// TODO(light): This is depending on details of the current loader.
	const vendorPart = "vendor/"
	if i := strings.LastIndex(path, vendorPart); i != -1 && (i == 0 || path[i-1] == '/') {
		path = path[i+len(vendorPart):]
	}
	return path == "codename/goose"
}

// paramIndex returns the index of the parameter with the given name, or
// -1 if no such parameter exists.
func paramIndex(params *types.Tuple, name string) int {
	for i := 0; i < params.Len(); i++ {
		if params.At(i).Name() == name {
			return i
		}
	}
	return -1
}
