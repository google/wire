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

	"golang.org/x/tools/go/loader"
)

// A providerSet describes a set of providers.  The zero value is an empty
// providerSet.
type providerSet struct {
	providers []*providerInfo
	imports   []providerSetImport
}

type providerSetImport struct {
	providerSetRef
	pos token.Pos
}

// providerInfo records the signature of a provider function.
type providerInfo struct {
	importPath string
	funcName   string
	pos        token.Pos
	args       []types.Type
	out        types.Type
	hasErr     bool
}

// findProviderSets processes a package and extracts the provider sets declared in it.
func findProviderSets(fset *token.FileSet, pkg *types.Package, r *importResolver, typeInfo *types.Info, files []*ast.File) (map[string]*providerSet, error) {
	sets := make(map[string]*providerSet)
	var directives []directive
	for _, f := range files {
		fileScope := typeInfo.Scopes[f]
		for _, c := range f.Comments {
			directives = extractDirectives(directives[:0], c)
			for _, d := range directives {
				switch d.kind {
				case "provide", "use":
					// handled later
				case "import":
					if fileScope == nil {
						return nil, fmt.Errorf("%s: no scope found for file (likely a bug)", fset.File(f.Pos()).Name())
					}
					i := strings.IndexByte(d.line, ' ')
					// TODO(light): allow multiple imports in one line
					if i == -1 {
						return nil, fmt.Errorf("%s: invalid import: expected TARGET SETREF", fset.Position(d.pos))
					}
					name, spec := d.line[:i], d.line[i+1:]
					ref, err := parseProviderSetRef(r, spec, fileScope, pkg.Path(), d.pos)
					if err != nil {
						return nil, fmt.Errorf("%v: %v", fset.Position(d.pos), err)
					}
					if ref.importPath != pkg.Path() {
						imported := false
						for _, imp := range pkg.Imports() {
							if ref.importPath == imp.Path() {
								imported = true
								break
							}
						}
						if !imported {
							return nil, fmt.Errorf("%v: provider set %s imports %q which is not in the package's imports", fset.Position(d.pos), name, ref.importPath)
						}
					}
					if mod := sets[name]; mod != nil {
						found := false
						for _, other := range mod.imports {
							if ref == other.providerSetRef {
								found = true
								break
							}
						}
						if !found {
							mod.imports = append(mod.imports, providerSetImport{providerSetRef: ref, pos: d.pos})
						}
					} else {
						sets[name] = &providerSet{
							imports: []providerSetImport{{providerSetRef: ref, pos: d.pos}},
						}
					}
				default:
					return nil, fmt.Errorf("%v: unknown directive %s", fset.Position(d.pos), d.kind)
				}
			}
		}
		cmap := ast.NewCommentMap(fset, f, f.Comments)
		for _, decl := range f.Decls {
			directives = directives[:0]
			for _, cg := range cmap[decl] {
				directives = extractDirectives(directives, cg)
			}
			fn, isFunction := decl.(*ast.FuncDecl)
			var providerSetName string
			for _, d := range directives {
				if d.kind != "provide" {
					continue
				}
				if providerSetName != "" {
					return nil, fmt.Errorf("%v: multiple provide directives for %s", fset.Position(d.pos), fn.Name.Name)
				}
				if !isFunction {
					return nil, fmt.Errorf("%v: only functions can be marked as providers", fset.Position(d.pos))
				}
				providerSetName = fn.Name.Name
				if d.line != "" {
					// TODO(light): validate identifier
					providerSetName = d.line
				}
			}
			if providerSetName == "" {
				continue
			}
			fpos := fn.Pos()
			sig := typeInfo.ObjectOf(fn.Name).Type().(*types.Signature)
			r := sig.Results()
			var hasErr bool
			switch r.Len() {
			case 1:
				hasErr = false
			case 2:
				if t := r.At(1).Type(); !types.Identical(t, errorType) {
					return nil, fmt.Errorf("%v: wrong signature for provider %s: second return type must be error", fset.Position(fpos), fn.Name.Name)
				}
				hasErr = true
			default:
				return nil, fmt.Errorf("%v: wrong signature for provider %s: must have one return value and optional error", fset.Position(fpos), fn.Name.Name)
			}
			out := r.At(0).Type()
			p := sig.Params()
			provider := &providerInfo{
				importPath: pkg.Path(),
				funcName:   fn.Name.Name,
				pos:        fn.Pos(),
				args:       make([]types.Type, p.Len()),
				out:        out,
				hasErr:     hasErr,
			}
			for i := 0; i < p.Len(); i++ {
				provider.args[i] = p.At(i).Type()
				for j := 0; j < i; j++ {
					if types.Identical(provider.args[i], provider.args[j]) {
						return nil, fmt.Errorf("%v: provider has multiple parameters of type %s", fset.Position(fpos), types.TypeString(provider.args[j], nil))
					}
				}
			}
			if mod := sets[providerSetName]; mod != nil {
				for _, other := range mod.providers {
					if types.Identical(other.out, provider.out) {
						return nil, fmt.Errorf("%v: provider set %s has multiple providers for %s (previous declaration at %v)", fset.Position(fpos), providerSetName, types.TypeString(provider.out, nil), fset.Position(other.pos))
					}
				}
				mod.providers = append(mod.providers, provider)
			} else {
				sets[providerSetName] = &providerSet{
					providers: []*providerInfo{provider},
				}
			}
		}
	}
	return sets, nil
}

// providerSetCache is a lazily evaluated index of provider sets.
type providerSetCache struct {
	sets map[string]map[string]*providerSet
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

func (mc *providerSetCache) get(ref providerSetRef) (*providerSet, error) {
	if mods, cached := mc.sets[ref.importPath]; cached {
		mod := mods[ref.name]
		if mod == nil {
			return nil, fmt.Errorf("no such provider set %s in package %q", ref.name, ref.importPath)
		}
		return mod, nil
	}
	if mc.sets == nil {
		mc.sets = make(map[string]map[string]*providerSet)
	}
	pkg := mc.prog.Package(ref.importPath)
	mods, err := findProviderSets(mc.fset, pkg.Pkg, mc.r, &pkg.Info, pkg.Files)
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

// A providerSetRef is a parsed reference to a collection of providers.
type providerSetRef struct {
	importPath string
	name       string
}

func parseProviderSetRef(r *importResolver, ref string, s *types.Scope, pkg string, pos token.Pos) (providerSetRef, error) {
	// TODO(light): verify that provider set name is an identifier before returning

	i := strings.LastIndexByte(ref, '.')
	if i == -1 {
		return providerSetRef{importPath: pkg, name: ref}, nil
	}
	imp, name := ref[:i], ref[i+1:]
	if strings.HasPrefix(imp, `"`) {
		path, err := strconv.Unquote(imp)
		if err != nil {
			return providerSetRef{}, fmt.Errorf("parse provider set reference %q: bad import path", ref)
		}
		path, err = r.resolve(pos, path)
		if err != nil {
			return providerSetRef{}, fmt.Errorf("parse provider set reference %q: %v", ref, err)
		}
		return providerSetRef{importPath: path, name: name}, nil
	}
	_, obj := s.LookupParent(imp, pos)
	if obj == nil {
		return providerSetRef{}, fmt.Errorf("parse provider set reference %q: unknown identifier %s", ref, imp)
	}
	pn, ok := obj.(*types.PkgName)
	if !ok {
		return providerSetRef{}, fmt.Errorf("parse provider set reference %q: %s does not name a package", ref, imp)
	}
	return providerSetRef{importPath: pn.Imported().Path(), name: name}, nil
}

func (ref providerSetRef) String() string {
	return strconv.Quote(ref.importPath) + "." + ref.name
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

type directive struct {
	pos  token.Pos
	kind string
	line string
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
		if i := strings.IndexByte(line, '\n'); i != -1 {
			line, text = line[:i], line[i+1:]
		} else {
			text = ""
		}
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
