package goose

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/loader"
)

// A ProviderSet describes a set of providers.  The zero value is an empty
// ProviderSet.
type ProviderSet struct {
	Providers []*Provider
	Bindings  []IfaceBinding
	Imports   []ProviderSetImport
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

// A ProviderSetImport adds providers from one provider set into another.
type ProviderSetImport struct {
	ProviderSetID
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
	conf := newLoaderConfig(bctx, wd, false)
	for _, p := range pkgs {
		conf.Import(p)
	}
	prog, err := conf.Load()
	if err != nil {
		return nil, fmt.Errorf("load: %v", err)
	}
	r := newImportResolver(conf, prog.Fset)
	var next []string
	initial := make(map[string]struct{})
	for _, pkgInfo := range prog.InitialPackages() {
		path := pkgInfo.Pkg.Path()
		next = append(next, path)
		initial[path] = struct{}{}
	}
	visited := make(map[string]struct{})
	info := &Info{
		Fset: prog.Fset,
		Sets: make(map[ProviderSetID]*ProviderSet),
		All:  make(map[ProviderSetID]*ProviderSet),
	}
	for len(next) > 0 {
		curr := next[len(next)-1]
		next = next[:len(next)-1]
		if _, ok := visited[curr]; ok {
			continue
		}
		visited[curr] = struct{}{}
		pkgInfo := prog.Package(curr)
		sets, err := findProviderSets(findContext{
			fset:     prog.Fset,
			pkg:      pkgInfo.Pkg,
			typeInfo: &pkgInfo.Info,
			r:        r,
		}, pkgInfo.Files)
		if err != nil {
			return nil, fmt.Errorf("load: %v", err)
		}
		path := pkgInfo.Pkg.Path()
		for name, set := range sets {
			info.All[ProviderSetID{path, name}] = set
			for _, imp := range set.Imports {
				next = append(next, imp.ImportPath)
			}
		}
		if _, ok := initial[path]; ok {
			for name, set := range sets {
				info.Sets[ProviderSetID{path, name}] = set
			}
		}
	}
	return info, nil
}

// Info holds the result of Load.
type Info struct {
	Fset *token.FileSet

	// Sets contains all the provider sets in the initial packages.
	Sets map[ProviderSetID]*ProviderSet

	// All contains all the provider sets transitively depended on by the
	// initial packages' provider sets.
	All map[ProviderSetID]*ProviderSet
}

// A ProviderSetID identifies a provider set.
type ProviderSetID struct {
	ImportPath string
	Name       string
}

// String returns the ID as ""path/to/pkg".Foo".
func (id ProviderSetID) String() string {
	return id.symref().String()
}

func (id ProviderSetID) symref() symref {
	return symref{importPath: id.ImportPath, name: id.Name}
}

type findContext struct {
	fset     *token.FileSet
	pkg      *types.Package
	typeInfo *types.Info
	r        *importResolver
}

// findProviderSets processes a package and extracts the provider sets declared in it.
func findProviderSets(fctx findContext, files []*ast.File) (map[string]*ProviderSet, error) {
	sets := make(map[string]*ProviderSet)
	for _, f := range files {
		fileScope := fctx.typeInfo.Scopes[f]
		if fileScope == nil {
			return nil, fmt.Errorf("%s: no scope found for file (likely a bug)", fctx.fset.File(f.Pos()).Name())
		}
		for _, dg := range parseFile(fctx.fset, f) {
			if dg.decl != nil {
				if err := processDeclDirectives(fctx, sets, fileScope, dg); err != nil {
					return nil, err
				}
			} else {
				for _, d := range dg.dirs {
					if err := processUnassociatedDirective(fctx, sets, fileScope, d); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return sets, nil
}

// processUnassociatedDirective handles any directive that was not associated with a top-level declaration.
func processUnassociatedDirective(fctx findContext, sets map[string]*ProviderSet, scope *types.Scope, d directive) error {
	switch d.kind {
	case "provide":
		return fmt.Errorf("%v: only functions can be marked as providers", fctx.fset.Position(d.pos))
	case "use":
		// Ignore, picked up by injector flow.
	case "bind":
		args := d.args()
		if len(args) != 3 {
			return fmt.Errorf("%v: invalid binding: expected TARGET IFACE TYPE", fctx.fset.Position(d.pos))
		}
		ifaceRef, err := parseSymbolRef(fctx.r, args[1], scope, fctx.pkg.Path(), d.pos)
		if err != nil {
			return fmt.Errorf("%v: %v", fctx.fset.Position(d.pos), err)
		}
		ifaceObj, err := ifaceRef.resolveObject(fctx.pkg)
		if err != nil {
			return fmt.Errorf("%v: %v", fctx.fset.Position(d.pos), err)
		}
		ifaceDecl, ok := ifaceObj.(*types.TypeName)
		if !ok {
			return fmt.Errorf("%v: %v does not name a type", fctx.fset.Position(d.pos), ifaceRef)
		}
		iface := ifaceDecl.Type()
		methodSet, ok := iface.Underlying().(*types.Interface)
		if !ok {
			return fmt.Errorf("%v: %v does not name an interface type", fctx.fset.Position(d.pos), ifaceRef)
		}

		providedRef, err := parseSymbolRef(fctx.r, strings.TrimPrefix(args[2], "*"), scope, fctx.pkg.Path(), d.pos)
		if err != nil {
			return fmt.Errorf("%v: %v", fctx.fset.Position(d.pos), err)
		}
		providedObj, err := providedRef.resolveObject(fctx.pkg)
		if err != nil {
			return fmt.Errorf("%v: %v", fctx.fset.Position(d.pos), err)
		}
		providedDecl, ok := providedObj.(*types.TypeName)
		if !ok {
			return fmt.Errorf("%v: %v does not name a type", fctx.fset.Position(d.pos), providedRef)
		}
		provided := providedDecl.Type()
		if types.Identical(provided, iface) {
			return fmt.Errorf("%v: cannot bind interface to itself", fctx.fset.Position(d.pos))
		}
		if strings.HasPrefix(args[2], "*") {
			provided = types.NewPointer(provided)
		}
		if !types.Implements(provided, methodSet) {
			return fmt.Errorf("%v: %s does not implement %s", fctx.fset.Position(d.pos), types.TypeString(provided, nil), types.TypeString(iface, nil))
		}

		name := args[0]
		if pset := sets[name]; pset != nil {
			pset.Bindings = append(pset.Bindings, IfaceBinding{
				Iface:    iface,
				Provided: provided,
			})
		} else {
			sets[name] = &ProviderSet{
				Bindings: []IfaceBinding{{
					Iface:    iface,
					Provided: provided,
				}},
			}
		}
	case "import":
		args := d.args()
		if len(args) < 2 {
			return fmt.Errorf("%v: invalid import: expected TARGET SETREF", fctx.fset.Position(d.pos))
		}
		name := args[0]
		for _, spec := range args[1:] {
			ref, err := parseSymbolRef(fctx.r, spec, scope, fctx.pkg.Path(), d.pos)
			if err != nil {
				return fmt.Errorf("%v: %v", fctx.fset.Position(d.pos), err)
			}
			if findImport(fctx.pkg, ref.importPath) == nil {
				return fmt.Errorf("%v: provider set %s imports %q which is not in the package's imports", fctx.fset.Position(d.pos), name, ref.importPath)
			}
			if mod := sets[name]; mod != nil {
				found := false
				for _, other := range mod.Imports {
					if ref == other.symref() {
						found = true
						break
					}
				}
				if !found {
					mod.Imports = append(mod.Imports, ProviderSetImport{
						ProviderSetID: ProviderSetID{
							ImportPath: ref.importPath,
							Name:       ref.name,
						},
						Pos: d.pos,
					})
				}
			} else {
				sets[name] = &ProviderSet{
					Imports: []ProviderSetImport{{
						ProviderSetID: ProviderSetID{
							ImportPath: ref.importPath,
							Name:       ref.name,
						},
						Pos: d.pos,
					}},
				}
			}
		}
	default:
		return fmt.Errorf("%v: unknown directive %s", fctx.fset.Position(d.pos), d.kind)
	}
	return nil
}

// processDeclDirectives processes the directives associated with a top-level declaration.
func processDeclDirectives(fctx findContext, sets map[string]*ProviderSet, scope *types.Scope, dg directiveGroup) error {
	p, err := dg.single(fctx.fset, "provide")
	if err != nil {
		return err
	}
	if !p.isValid() {
		return nil
	}
	var providerSetName string
	if args := p.args(); len(args) == 1 {
		// TODO(light): validate identifier
		providerSetName = args[0]
	} else if len(args) > 1 {
		return fmt.Errorf("%v: goose:provide takes at most one argument", fctx.fset.Position(p.pos))
	}
	switch decl := dg.decl.(type) {
	case *ast.FuncDecl:
		fn := fctx.typeInfo.ObjectOf(decl.Name).(*types.Func)
		provider, err := processFuncProvider(fctx, fn)
		if err != nil {
			return err
		}
		if providerSetName == "" {
			providerSetName = fn.Name()
		}
		if mod := sets[providerSetName]; mod != nil {
			for _, other := range mod.Providers {
				if types.Identical(other.Out, provider.Out) {
					return fmt.Errorf("%v: provider set %s has multiple providers for %s (previous declaration at %v)", fctx.fset.Position(fn.Pos()), providerSetName, types.TypeString(provider.Out, nil), fctx.fset.Position(other.Pos))
				}
			}
			mod.Providers = append(mod.Providers, provider)
		} else {
			sets[providerSetName] = &ProviderSet{
				Providers: []*Provider{provider},
			}
		}
	case *ast.GenDecl:
		if decl.Tok != token.TYPE {
			return fmt.Errorf("%v: only functions and structs can be marked as providers", fctx.fset.Position(p.pos))
		}
		if len(decl.Specs) != 1 {
			// TODO(light): tighten directive extraction to associate with particular specs.
			return fmt.Errorf("%v: only functions and structs can be marked as providers", fctx.fset.Position(p.pos))
		}
		typeName := fctx.typeInfo.ObjectOf(decl.Specs[0].(*ast.TypeSpec).Name).(*types.TypeName)
		if _, ok := typeName.Type().(*types.Named).Underlying().(*types.Struct); !ok {
			return fmt.Errorf("%v: only functions and structs can be marked as providers", fctx.fset.Position(p.pos))
		}
		provider, err := processStructProvider(fctx, typeName)
		if err != nil {
			return err
		}
		if providerSetName == "" {
			providerSetName = typeName.Name()
		}
		ptrProvider := new(Provider)
		*ptrProvider = *provider
		ptrProvider.Out = types.NewPointer(provider.Out)
		if mod := sets[providerSetName]; mod != nil {
			for _, other := range mod.Providers {
				if types.Identical(other.Out, provider.Out) {
					return fmt.Errorf("%v: provider set %s has multiple providers for %s (previous declaration at %v)", fctx.fset.Position(typeName.Pos()), providerSetName, types.TypeString(provider.Out, nil), fctx.fset.Position(other.Pos))
				}
				if types.Identical(other.Out, ptrProvider.Out) {
					return fmt.Errorf("%v: provider set %s has multiple providers for %s (previous declaration at %v)", fctx.fset.Position(typeName.Pos()), providerSetName, types.TypeString(ptrProvider.Out, nil), fctx.fset.Position(other.Pos))
				}
			}
			mod.Providers = append(mod.Providers, provider, ptrProvider)
		} else {
			sets[providerSetName] = &ProviderSet{
				Providers: []*Provider{provider, ptrProvider},
			}
		}
	default:
		return fmt.Errorf("%v: only functions and structs can be marked as providers", fctx.fset.Position(p.pos))
	}
	return nil
}

func processFuncProvider(fctx findContext, fn *types.Func) (*Provider, error) {
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
			return nil, fmt.Errorf("%v: wrong signature for provider %s: second return type must be error or func()", fctx.fset.Position(fpos), fn.Name())
		}
	case 3:
		if t := r.At(1).Type(); !types.Identical(t, cleanupType) {
			return nil, fmt.Errorf("%v: wrong signature for provider %s: second return type must be func()", fctx.fset.Position(fpos), fn.Name())
		}
		if t := r.At(2).Type(); !types.Identical(t, errorType) {
			return nil, fmt.Errorf("%v: wrong signature for provider %s: third return type must be error", fctx.fset.Position(fpos), fn.Name())
		}
		hasCleanup, hasErr = true, true
	default:
		return nil, fmt.Errorf("%v: wrong signature for provider %s: must have one return value and optional error", fctx.fset.Position(fpos), fn.Name())
	}
	out := r.At(0).Type()
	params := sig.Params()
	provider := &Provider{
		ImportPath: fctx.pkg.Path(),
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
				return nil, fmt.Errorf("%v: provider has multiple parameters of type %s", fctx.fset.Position(fpos), types.TypeString(provider.Args[j].Type, nil))
			}
		}
	}
	return provider, nil
}

func processStructProvider(fctx findContext, typeName *types.TypeName) (*Provider, error) {
	out := typeName.Type()
	st := out.Underlying().(*types.Struct)

	pos := typeName.Pos()
	provider := &Provider{
		ImportPath: fctx.pkg.Path(),
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
				return nil, fmt.Errorf("%v: provider struct has multiple fields of type %s", fctx.fset.Position(pos), types.TypeString(provider.Args[j].Type, nil))
			}
		}
	}
	return provider, nil
}

// providerSetCache is a lazily evaluated index of provider sets.
type providerSetCache struct {
	sets map[string]map[string]*ProviderSet
	fset *token.FileSet
	prog *loader.Program
	r    *importResolver
}

func newProviderSetCache(prog *loader.Program, r *importResolver) *providerSetCache {
	return &providerSetCache{
		fset: prog.Fset,
		prog: prog,
		r:    r,
	}
}

func (mc *providerSetCache) get(ref symref) (*ProviderSet, error) {
	if mods, cached := mc.sets[ref.importPath]; cached {
		mod := mods[ref.name]
		if mod == nil {
			return nil, fmt.Errorf("no such provider set %s in package %q", ref.name, ref.importPath)
		}
		return mod, nil
	}
	if mc.sets == nil {
		mc.sets = make(map[string]map[string]*ProviderSet)
	}
	pkg := mc.prog.Package(ref.importPath)
	mods, err := findProviderSets(findContext{
		fset:     mc.fset,
		pkg:      pkg.Pkg,
		typeInfo: &pkg.Info,
		r:        mc.r,
	}, pkg.Files)
	if err != nil {
		mc.sets[ref.importPath] = nil
		return nil, err
	}
	mc.sets[ref.importPath] = mods
	mod := mods[ref.name]
	if mod == nil {
		return nil, fmt.Errorf("no such provider set %s in package %q", ref.name, ref.importPath)
	}
	return mod, nil
}

// A symref is a parsed reference to a symbol (either a provider set or a Go object).
type symref struct {
	importPath string
	name       string
}

func parseSymbolRef(r *importResolver, ref string, s *types.Scope, pkg string, pos token.Pos) (symref, error) {
	// TODO(light): verify that provider set name is an identifier before returning

	i := strings.LastIndexByte(ref, '.')
	if i == -1 {
		return symref{importPath: pkg, name: ref}, nil
	}
	imp, name := ref[:i], ref[i+1:]
	if strings.HasPrefix(imp, `"`) {
		path, err := strconv.Unquote(imp)
		if err != nil {
			return symref{}, fmt.Errorf("parse symbol reference %q: bad import path", ref)
		}
		path, err = r.resolve(pos, path)
		if err != nil {
			return symref{}, fmt.Errorf("parse symbol reference %q: %v", ref, err)
		}
		return symref{importPath: path, name: name}, nil
	}
	_, obj := s.LookupParent(imp, pos)
	if obj == nil {
		return symref{}, fmt.Errorf("parse symbol reference %q: unknown identifier %s", ref, imp)
	}
	pn, ok := obj.(*types.PkgName)
	if !ok {
		return symref{}, fmt.Errorf("parse symbol reference %q: %s does not name a package", ref, imp)
	}
	return symref{importPath: pn.Imported().Path(), name: name}, nil
}

func (ref symref) String() string {
	return strconv.Quote(ref.importPath) + "." + ref.name
}

func (ref symref) resolveObject(pkg *types.Package) (types.Object, error) {
	imp := findImport(pkg, ref.importPath)
	if imp == nil {
		return nil, fmt.Errorf("resolve Go reference %v: package not directly imported", ref)
	}
	obj := imp.Scope().Lookup(ref.name)
	if obj == nil {
		return nil, fmt.Errorf("resolve Go reference %v: %s not found in package", ref, ref.name)
	}
	return obj, nil
}

type importResolver struct {
	fset        *token.FileSet
	bctx        *build.Context
	findPackage func(bctx *build.Context, importPath, fromDir string, mode build.ImportMode) (*build.Package, error)
}

func newImportResolver(c *loader.Config, fset *token.FileSet) *importResolver {
	r := &importResolver{
		fset:        fset,
		bctx:        c.Build,
		findPackage: c.FindPackage,
	}
	if r.bctx == nil {
		r.bctx = &build.Default
	}
	if r.findPackage == nil {
		r.findPackage = (*build.Context).Import
	}
	return r
}

func (r *importResolver) resolve(pos token.Pos, path string) (string, error) {
	dir := filepath.Dir(r.fset.File(pos).Name())
	pkg, err := r.findPackage(r.bctx, path, dir, build.FindOnly)
	if err != nil {
		return "", err
	}
	return pkg.ImportPath, nil
}

func findImport(pkg *types.Package, path string) *types.Package {
	if pkg.Path() == path {
		return pkg
	}
	for _, imp := range pkg.Imports() {
		if imp.Path() == path {
			return imp
		}
	}
	return nil
}

// A directive is a parsed goose comment.
type directive struct {
	pos  token.Pos
	kind string
	line string
}

// A directiveGroup is a set of directives associated with a particular
// declaration.
type directiveGroup struct {
	decl ast.Decl
	dirs []directive
}

// parseFile extracts the directives from a file, grouped by declaration.
func parseFile(fset *token.FileSet, f *ast.File) []directiveGroup {
	cmap := ast.NewCommentMap(fset, f, f.Comments)
	// Reserve first group for directives that don't associate with a
	// declaration, like import.
	groups := make([]directiveGroup, 1, len(f.Decls)+1)
	// Walk declarations and add to groups.
	for _, decl := range f.Decls {
		grp := directiveGroup{decl: decl}
		ast.Inspect(decl, func(node ast.Node) bool {
			if g := cmap[node]; len(g) > 0 {
				for _, cg := range g {
					start := len(grp.dirs)
					grp.dirs = extractDirectives(grp.dirs, cg)

					// Move directives that don't associate into the unassociated group.
					n := 0
					for i := start; i < len(grp.dirs); i++ {
						if k := grp.dirs[i].kind; k == "provide" || k == "use" {
							grp.dirs[start+n] = grp.dirs[i]
							n++
						} else {
							groups[0].dirs = append(groups[0].dirs, grp.dirs[i])
						}
					}
					grp.dirs = grp.dirs[:start+n]
				}
				delete(cmap, node)
			}
			return true
		})
		if len(grp.dirs) > 0 {
			groups = append(groups, grp)
		}
	}
	// Place remaining directives into the unassociated group.
	unassoc := &groups[0]
	for _, g := range cmap {
		for _, cg := range g {
			unassoc.dirs = extractDirectives(unassoc.dirs, cg)
		}
	}
	if len(unassoc.dirs) == 0 {
		return groups[1:]
	}
	return groups
}

func extractDirectives(d []directive, cg *ast.CommentGroup) []directive {
	const prefix = "goose:"
	text := cg.Text()
	for len(text) > 0 {
		text = strings.TrimLeft(text, " \t\r\n")
		if !strings.HasPrefix(text, prefix) {
			break
		}
		line := text[len(prefix):]
		// Text() is always newline terminated.
		i := strings.IndexByte(line, '\n')
		line, text = line[:i], line[i+1:]
		if i := strings.IndexByte(line, ' '); i != -1 {
			d = append(d, directive{
				kind: line[:i],
				line: strings.TrimSpace(line[i+1:]),
				pos:  cg.Pos(), // TODO(light): more precise position
			})
		} else {
			d = append(d, directive{
				kind: line,
				pos:  cg.Pos(), // TODO(light): more precise position
			})
		}
	}
	return d
}

// single finds at most one directive that matches the given kind.
func (dg directiveGroup) single(fset *token.FileSet, kind string) (directive, error) {
	var found directive
	ok := false
	for _, d := range dg.dirs {
		if d.kind != kind {
			continue
		}
		if ok {
			switch decl := dg.decl.(type) {
			case *ast.FuncDecl:
				return directive{}, fmt.Errorf("%v: multiple %s directives for %s", fset.Position(d.pos), kind, decl.Name.Name)
			case *ast.GenDecl:
				if decl.Tok == token.TYPE && len(decl.Specs) == 1 {
					name := decl.Specs[0].(*ast.TypeSpec).Name.Name
					return directive{}, fmt.Errorf("%v: multiple %s directives for %s", fset.Position(d.pos), kind, name)
				}
				return directive{}, fmt.Errorf("%v: multiple %s directives", fset.Position(d.pos), kind)
			default:
				return directive{}, fmt.Errorf("%v: multiple %s directives", fset.Position(d.pos), kind)
			}
		}
		found, ok = d, true
	}
	return found, nil
}

func (d directive) isValid() bool {
	return d.kind != ""
}

// args splits the directive line into tokens.
func (d directive) args() []string {
	var args []string
	start := -1
	state := 0 // 0 = boundary, 1 = in token, 2 = in quote, 3 = quote backslash
	for i, r := range d.line {
		switch state {
		case 0:
			// Argument boundary
			switch {
			case r == '"':
				start = i
				state = 2
			case !unicode.IsSpace(r):
				start = i
				state = 1
			}
		case 1:
			// In token
			switch {
			case unicode.IsSpace(r):
				args = append(args, d.line[start:i])
				start = -1
				state = 0
			case r == '"':
				state = 2
			}
		case 2:
			// In quotes
			switch {
			case r == '"':
				state = 1
			case r == '\\':
				state = 3
			}
		case 3:
			// Quote backslash. Consumes one character and jumps back into "in quote" state.
			state = 2
		default:
			panic("unreachable")
		}
	}
	if start != -1 {
		args = append(args, d.line[start:])
	}
	return args
}

// isInjectFile reports whether a given file is an injection template.
func isInjectFile(f *ast.File) bool {
	// TODO(light): better determination
	for _, cg := range f.Comments {
		text := cg.Text()
		if strings.HasPrefix(text, "+build") && strings.Contains(text, "gooseinject") {
			return true
		}
	}
	return false
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
