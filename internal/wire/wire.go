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

// Package wire provides compile-time dependency injection logic as a
// Go library.
package wire

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/printer"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/loader"
)

// Generate performs dependency injection for a single package,
// returning the gofmt'd Go source code.
func Generate(bctx *build.Context, wd string, pkg string) ([]byte, []error) {
	mainPkg, err := bctx.Import(pkg, wd, build.FindOnly)
	if err != nil {
		return nil, []error{fmt.Errorf("load: %v", err)}
	}
	// TODO(light): Stop errors from printing to stderr.
	conf := &loader.Config{
		Build: bctx,
		Cwd:   wd,
		TypeCheckFuncBodies: func(path string) bool {
			return path == mainPkg.ImportPath
		},
		FindPackage: func(bctx *build.Context, importPath, fromDir string, mode build.ImportMode) (*build.Package, error) {
			// Optimistically try to load in the package with normal build tags.
			pkg, err := bctx.Import(importPath, fromDir, mode)

			// If this is the generated package, then load it in with the
			// wireinject build tag to pick up the injector template. Since
			// the *build.Context is shared between calls to FindPackage, this
			// uses a copy.
			if pkg != nil && pkg.ImportPath == mainPkg.ImportPath {
				bctx2 := new(build.Context)
				*bctx2 = *bctx
				n := len(bctx2.BuildTags)
				bctx2.BuildTags = append(bctx2.BuildTags[:n:n], "wireinject")
				pkg, err = bctx2.Import(importPath, fromDir, mode)
			}
			return pkg, err
		},
	}
	conf.Import(pkg)

	prog, err := conf.Load()
	if err != nil {
		return nil, []error{fmt.Errorf("load: %v", err)}
	}
	if len(prog.InitialPackages()) != 1 {
		// This is more of a violated precondition than anything else.
		return nil, []error{fmt.Errorf("load: got %d packages", len(prog.InitialPackages()))}
	}
	pkgInfo := prog.InitialPackages()[0]
	g := newGen(prog, pkgInfo.Pkg.Path())
	injectorFiles, errs := generateInjectors(g, pkgInfo)
	if len(errs) > 0 {
		return nil, errs
	}
	copyNonInjectorDecls(g, injectorFiles, &pkgInfo.Info)
	goSrc := g.frame()
	fmtSrc, err := format.Source(goSrc)
	if err != nil {
		// This is likely a bug from a poorly generated source file.
		// Return an error and the unformatted source.
		return goSrc, []error{err}
	}
	return fmtSrc, nil
}

// generateInjectors generates the injectors for a given package.
func generateInjectors(g *gen, pkgInfo *loader.PackageInfo) (injectorFiles []*ast.File, _ []error) {
	oc := newObjectCache(g.prog)
	injectorFiles = make([]*ast.File, 0, len(pkgInfo.Files))
	for _, f := range pkgInfo.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			buildCall := isInjector(&pkgInfo.Info, fn)
			if buildCall == nil {
				continue
			}
			if len(injectorFiles) == 0 || injectorFiles[len(injectorFiles)-1] != f {
				// This is the first injector generated for this file.
				// Write a file header.
				name := filepath.Base(g.prog.Fset.File(f.Pos()).Name())
				g.p("// Injectors from %s:\n\n", name)
				injectorFiles = append(injectorFiles, f)
			}
			set, errs := oc.processNewSet(pkgInfo, buildCall)
			if len(errs) > 0 {
				position := g.prog.Fset.Position(fn.Pos())
				errs = append([]error(nil), errs...)
				for i := range errs {
					errs[i] = fmt.Errorf("%v: %v", position, errs[i])
				}
				return nil, errs
			}
			sig := pkgInfo.ObjectOf(fn.Name).Type().(*types.Signature)
			if errs := g.inject(fn.Name.Name, sig, set); len(errs) > 0 {
				position := g.prog.Fset.Position(fn.Pos())
				errs = append([]error(nil), errs...)
				for i := range errs {
					errs[i] = fmt.Errorf("%v: %v", position, errs[i])
				}
				return nil, errs
			}
		}
	}
	return injectorFiles, nil
}

// copyNonInjectorDecls copies any non-injector declarations from the
// given files into the generated output.
func copyNonInjectorDecls(g *gen, files []*ast.File, info *types.Info) {
	for _, f := range files {
		name := filepath.Base(g.prog.Fset.File(f.Pos()).Name())
		first := true
		for _, decl := range f.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if isInjector(info, decl) != nil {
					continue
				}
			case *ast.GenDecl:
				if decl.Tok == token.IMPORT {
					continue
				}
			default:
				continue
			}
			if first {
				g.p("// %s:\n\n", name)
				first = false
			}
			// TODO(light): Add line number at top of each declaration.
			g.writeAST(info, decl)
			g.p("\n\n")
		}
	}
}

// gen is the file-wide generator state.
type gen struct {
	currPackage string
	buf         bytes.Buffer
	imports     map[string]string
	values      map[ast.Expr]string
	prog        *loader.Program // for positions and determining package names
}

func newGen(prog *loader.Program, pkg string) *gen {
	return &gen{
		currPackage: pkg,
		imports:     make(map[string]string),
		values:      make(map[ast.Expr]string),
		prog:        prog,
	}
}

// frame bakes the built up source body into an unformatted Go source file.
func (g *gen) frame() []byte {
	if g.buf.Len() == 0 {
		return nil
	}
	var buf bytes.Buffer
	buf.WriteString("// Code generated by gowire. DO NOT EDIT.\n\n")
	buf.WriteString("//go:generate gowire\n")
	buf.WriteString("//+build !wireinject\n\n")
	buf.WriteString("package ")
	buf.WriteString(g.prog.Package(g.currPackage).Pkg.Name())
	buf.WriteString("\n\n")
	if len(g.imports) > 0 {
		buf.WriteString("import (\n")
		imps := make([]string, 0, len(g.imports))
		for path := range g.imports {
			imps = append(imps, path)
		}
		sort.Strings(imps)
		for _, path := range imps {
			// TODO(light): Omit the local package identifier if it matches
			// the package name.
			fmt.Fprintf(&buf, "\t%s %q\n", g.imports[path], path)
		}
		buf.WriteString(")\n\n")
	}
	buf.Write(g.buf.Bytes())
	return buf.Bytes()
}

// inject emits the code for an injector.
func (g *gen) inject(name string, sig *types.Signature, set *ProviderSet) []error {
	injectSig, err := funcOutput(sig)
	if err != nil {
		return []error{fmt.Errorf("inject %s: %v", name, err)}
	}
	params := sig.Params()
	given := make([]types.Type, params.Len())
	for i := 0; i < params.Len(); i++ {
		given[i] = params.At(i).Type()
	}
	calls, errs := solve(g.prog.Fset, injectSig.out, given, set)
	if len(errs) > 0 {
		return errs
	}
	type pendingVar struct {
		name     string
		expr     ast.Expr
		typeInfo *types.Info
	}
	var pendingVars []pendingVar
	for i := range calls {
		c := &calls[i]
		if c.hasCleanup && !injectSig.cleanup {
			return []error{fmt.Errorf("inject %s: provider for %s returns cleanup but injection does not return cleanup function", name, types.TypeString(c.out, nil))}
		}
		if c.hasErr && !injectSig.err {
			return []error{fmt.Errorf("inject %s: provider for %s returns error but injection not allowed to fail", name, types.TypeString(c.out, nil))}
		}
		if c.kind == valueExpr {
			if err := accessibleFrom(c.valueTypeInfo, c.valueExpr, g.currPackage); err != nil {
				// TODO(light): Display line number of value expression.
				ts := types.TypeString(c.out, nil)
				return []error{fmt.Errorf("inject %s: value %s can't be used: %v", name, ts, err)}
			}
			if g.values[c.valueExpr] == "" {
				t := c.valueTypeInfo.TypeOf(c.valueExpr)
				name := disambiguate("_wire"+export(typeVariableName(t))+"Value", g.nameInFileScope)
				g.values[c.valueExpr] = name
				pendingVars = append(pendingVars, pendingVar{
					name:     name,
					expr:     c.valueExpr,
					typeInfo: c.valueTypeInfo,
				})
			}
		}
	}

	// Perform one pass to collect all imports, followed by the real pass.
	injectPass(name, params, injectSig, calls, &injectorGen{
		g:       g,
		errVar:  disambiguate("err", g.nameInFileScope),
		discard: true,
	})
	injectPass(name, params, injectSig, calls, &injectorGen{
		g:       g,
		errVar:  disambiguate("err", g.nameInFileScope),
		discard: false,
	})
	if len(pendingVars) > 0 {
		g.p("var (\n")
		for _, pv := range pendingVars {
			g.p("\t%s = ", pv.name)
			g.writeAST(pv.typeInfo, pv.expr)
			g.p("\n")
		}
		g.p(")\n\n")
	}
	return nil
}

// rewritePkgRefs rewrites any package references in an AST into references for the
// generated package.
func (g *gen) rewritePkgRefs(info *types.Info, node ast.Node) ast.Node {
	start, end := node.Pos(), node.End()
	node = copyAST(node)
	// First, rewrite all package names. This lets us know all the
	// potentially colliding identifiers.
	node = astutil.Apply(node, func(c *astutil.Cursor) bool {
		switch node := c.Node().(type) {
		case *ast.Ident:
			// This is an unqualified identifier (qualified identifiers are peeled off below).
			obj := info.ObjectOf(node)
			if obj == nil {
				return false
			}
			if pkg := obj.Pkg(); pkg != nil && obj.Parent() == pkg.Scope() && pkg.Path() != g.currPackage {
				// An identifier from either a dot import or read from a different package.
				newPkgID := g.qualifyImport(pkg.Path())
				c.Replace(&ast.SelectorExpr{
					X:   ast.NewIdent(newPkgID),
					Sel: ast.NewIdent(node.Name),
				})
				return false
			}
			return true
		case *ast.SelectorExpr:
			pkgIdent, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			pkgName, ok := info.ObjectOf(pkgIdent).(*types.PkgName)
			if !ok {
				return true
			}
			// This is a qualified identifier. Rewrite and avoid visiting subexpressions.
			newPkgID := g.qualifyImport(pkgName.Imported().Path())
			c.Replace(&ast.SelectorExpr{
				X:   ast.NewIdent(newPkgID),
				Sel: ast.NewIdent(node.Sel.Name),
			})
			return false
		default:
			return true
		}
	}, nil)
	// Now that we have all the identifiers, rename any variables declared
	// in this scope to not collide.
	newNames := make(map[types.Object]string)
	inNewNames := func(n string) bool {
		for _, other := range newNames {
			if other == n {
				return true
			}
		}
		return false
	}
	var scopeStack []*types.Scope
	pkgScope := g.prog.Package(g.currPackage).Pkg.Scope()
	node = astutil.Apply(node, func(c *astutil.Cursor) bool {
		if scope := info.Scopes[c.Node()]; scope != nil {
			scopeStack = append(scopeStack, scope)
		}
		id, ok := c.Node().(*ast.Ident)
		if !ok {
			return true
		}
		obj := info.ObjectOf(id)
		if obj == nil {
			// We rewrote this identifier earlier, so it does not need
			// further rewriting.
			return true
		}
		if n, ok := newNames[obj]; ok {
			// We picked a new name for this symbol. Rewrite it.
			c.Replace(ast.NewIdent(n))
			return false
		}
		if par := obj.Parent(); par == nil || par == pkgScope {
			// Don't rename methods, field names, or top-level identifiers.
			return true
		}

		// Rename any symbols defined within rewritePkgRefs's node that conflict
		// with any symbols in the generated file.
		objName := obj.Name()
		if pos := obj.Pos(); pos < start || end <= pos || !(g.nameInFileScope(objName) || inNewNames(objName)) {
			return true
		}
		newName := disambiguate(objName, func(n string) bool {
			if g.nameInFileScope(n) || inNewNames(n) {
				return true
			}
			if len(scopeStack) > 0 {
				// Avoid picking a name that conflicts with other names in the
				// current scope.
				_, obj := scopeStack[len(scopeStack)-1].LookupParent(n, 0)
				if obj != nil {
					return true
				}
			}
			return false
		})
		newNames[obj] = newName
		c.Replace(ast.NewIdent(newName))
		return false
	}, func(c *astutil.Cursor) bool {
		if info.Scopes[c.Node()] != nil {
			// Should be top of stack; pop it.
			scopeStack = scopeStack[:len(scopeStack)-1]
		}
		return true
	})
	return node
}

// writeAST prints an AST node into the generated output, rewriting any
// package references it encounters.
func (g *gen) writeAST(info *types.Info, node ast.Node) {
	node = g.rewritePkgRefs(info, node)
	if err := printer.Fprint(&g.buf, g.prog.Fset, node); err != nil {
		panic(err)
	}
}

func (g *gen) qualifiedID(path, sym string) string {
	name := g.qualifyImport(path)
	if name == "" {
		return sym
	}
	return name + "." + sym
}

func (g *gen) qualifyImport(path string) string {
	if path == g.currPackage {
		return ""
	}
	// TODO(light): This is depending on details of the current loader.
	const vendorPart = "vendor/"
	unvendored := path
	if i := strings.LastIndex(path, vendorPart); i != -1 && (i == 0 || path[i-1] == '/') {
		unvendored = path[i+len(vendorPart):]
	}
	if name := g.imports[unvendored]; name != "" {
		return name
	}
	// TODO(light): Use parts of import path to disambiguate.
	name := disambiguate(g.prog.Package(path).Pkg.Name(), func(n string) bool {
		// Don't let an import take the "err" name. That's annoying.
		return n == "err" || g.nameInFileScope(n)
	})
	g.imports[unvendored] = name
	return name
}

func (g *gen) nameInFileScope(name string) bool {
	for _, other := range g.imports {
		if other == name {
			return true
		}
	}
	for _, other := range g.values {
		if other == name {
			return true
		}
	}
	_, obj := g.prog.Package(g.currPackage).Pkg.Scope().LookupParent(name, 0)
	return obj != nil
}

func (g *gen) qualifyPkg(pkg *types.Package) string {
	return g.qualifyImport(pkg.Path())
}

func (g *gen) p(format string, args ...interface{}) {
	fmt.Fprintf(&g.buf, format, args...)
}

// injectorGen is the per-injector pass generator state.
type injectorGen struct {
	g *gen

	paramNames   []string
	localNames   []string
	cleanupNames []string
	errVar       string

	// discard causes ig.p and ig.writeAST to no-op. Useful to run
	// generation for side-effects like filling in g.imports.
	discard bool
}

// injectPass generates an injector given the output from analysis.
func injectPass(name string, params *types.Tuple, injectSig outputSignature, calls []call, ig *injectorGen) {
	ig.p("func %s(", name)
	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			ig.p(", ")
		}
		pi := params.At(i)
		a := pi.Name()
		if a == "" || a == "_" {
			a = unexport(typeVariableName(pi.Type()))
			if a == "" {
				a = "arg"
			}
		}
		ig.paramNames = append(ig.paramNames, disambiguate(a, ig.nameInInjector))
		ig.p("%s %s", ig.paramNames[i], types.TypeString(pi.Type(), ig.g.qualifyPkg))
	}
	outTypeString := types.TypeString(injectSig.out, ig.g.qualifyPkg)
	if injectSig.cleanup && injectSig.err {
		ig.p(") (%s, func(), error) {\n", outTypeString)
	} else if injectSig.cleanup {
		ig.p(") (%s, func()) {\n", outTypeString)
	} else if injectSig.err {
		ig.p(") (%s, error) {\n", outTypeString)
	} else {
		ig.p(") %s {\n", outTypeString)
	}
	for i := range calls {
		c := &calls[i]
		lname := unexport(typeVariableName(c.out))
		if lname == "" {
			lname = "v"
		}
		lname = disambiguate(lname, ig.nameInInjector)
		ig.localNames = append(ig.localNames, lname)
		switch c.kind {
		case structProvider:
			ig.structProviderCall(lname, c)
		case funcProviderCall:
			ig.funcProviderCall(lname, c, injectSig)
		case valueExpr:
			ig.valueExpr(lname, c)
		default:
			panic("unknown kind")
		}
	}
	if len(calls) == 0 {
		for i := 0; i < params.Len(); i++ {
			if types.Identical(injectSig.out, params.At(i).Type()) {
				ig.p("\treturn %s", ig.paramNames[i])
				break
			}
		}
	} else {
		ig.p("\treturn %s", ig.localNames[len(calls)-1])
	}
	if injectSig.cleanup {
		ig.p(", func() {\n")
		for i := len(ig.cleanupNames) - 1; i >= 0; i-- {
			ig.p("\t\t%s()\n", ig.cleanupNames[i])
		}
		ig.p("\t}")
	}
	if injectSig.err {
		ig.p(", nil")
	}
	ig.p("\n}\n\n")
}

func (ig *injectorGen) funcProviderCall(lname string, c *call, injectSig outputSignature) {
	ig.p("\t%s", lname)
	prevCleanup := len(ig.cleanupNames)
	if c.hasCleanup {
		cname := disambiguate("cleanup", ig.nameInInjector)
		ig.cleanupNames = append(ig.cleanupNames, cname)
		ig.p(", %s", cname)
	}
	if c.hasErr {
		ig.p(", %s", ig.errVar)
	}
	ig.p(" := ")
	ig.p("%s(", ig.g.qualifiedID(c.importPath, c.name))
	for i, a := range c.args {
		if i > 0 {
			ig.p(", ")
		}
		if a < len(ig.paramNames) {
			ig.p("%s", ig.paramNames[a])
		} else {
			ig.p("%s", ig.localNames[a-len(ig.paramNames)])
		}
	}
	ig.p(")\n")
	if c.hasErr {
		ig.p("\tif %s != nil {\n", ig.errVar)
		for i := prevCleanup - 1; i >= 0; i-- {
			ig.p("\t\t%s()\n", ig.cleanupNames[i])
		}
		ig.p("\t\treturn %s", zeroValue(injectSig.out, ig.g.qualifyPkg))
		if injectSig.cleanup {
			ig.p(", nil")
		}
		// TODO(light): Give information about failing provider.
		ig.p(", err\n")
		ig.p("\t}\n")
	}
}

func (ig *injectorGen) structProviderCall(lname string, c *call) {
	ig.p("\t%s", lname)
	ig.p(" := ")
	if _, ok := c.out.(*types.Pointer); ok {
		ig.p("&")
	}
	ig.p("%s{\n", ig.g.qualifiedID(c.importPath, c.name))
	for i, a := range c.args {
		ig.p("\t\t%s: ", c.fieldNames[i])
		if a < len(ig.paramNames) {
			ig.p("%s", ig.paramNames[a])
		} else {
			ig.p("%s", ig.localNames[a-len(ig.paramNames)])
		}
		ig.p(",\n")
	}
	ig.p("\t}\n")
}

func (ig *injectorGen) valueExpr(lname string, c *call) {
	ig.p("\t%s := %s\n", lname, ig.g.values[c.valueExpr])
}

// nameInInjector reports whether name collides with any other identifier
// in the current injector.
func (ig *injectorGen) nameInInjector(name string) bool {
	if name == ig.errVar {
		return true
	}
	for _, a := range ig.paramNames {
		if a == name {
			return true
		}
	}
	for _, l := range ig.localNames {
		if l == name {
			return true
		}
	}
	for _, l := range ig.cleanupNames {
		if l == name {
			return true
		}
	}
	return ig.g.nameInFileScope(name)
}

func (ig *injectorGen) p(format string, args ...interface{}) {
	if ig.discard {
		return
	}
	ig.g.p(format, args...)
}

func (ig *injectorGen) writeAST(info *types.Info, node ast.Node) {
	node = ig.g.rewritePkgRefs(info, node)
	if ig.discard {
		return
	}
	if err := printer.Fprint(&ig.g.buf, ig.g.prog.Fset, node); err != nil {
		panic(err)
	}
}

// zeroValue returns the shortest expression that evaluates to the zero
// value for the given type.
func zeroValue(t types.Type, qf types.Qualifier) string {
	switch u := t.Underlying().(type) {
	case *types.Array, *types.Struct:
		return types.TypeString(t, qf) + "{}"
	case *types.Basic:
		info := u.Info()
		switch {
		case info&types.IsBoolean != 0:
			return "false"
		case info&(types.IsInteger|types.IsFloat|types.IsComplex) != 0:
			return "0"
		case info&types.IsString != 0:
			return `""`
		default:
			panic("unreachable")
		}
	case *types.Chan, *types.Interface, *types.Map, *types.Pointer, *types.Signature, *types.Slice:
		return "nil"
	default:
		panic("unreachable")
	}
}

// typeVariableName invents a variable name derived from the type name
// or returns the empty string if one could not be found. There are no
// guarantees about whether the name is exported or unexported: call
// export() or unexport() to convert.
func typeVariableName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	switch t := t.(type) {
	case *types.Basic:
		return t.Name()
	case *types.Named:
		// TODO(light): Include package name when appropriate.
		return t.Obj().Name()
	}
	return ""
}

// unexport converts a name that is potentially exported to an unexported name.
func unexport(name string) string {
	if name == "" {
		return ""
	}
	r, sz := utf8.DecodeRuneInString(name)
	if !unicode.IsUpper(r) {
		// foo -> foo
		return name
	}
	r2, sz2 := utf8.DecodeRuneInString(name[sz:])
	if !unicode.IsUpper(r2) {
		// Foo -> foo
		return string(unicode.ToLower(r)) + name[sz:]
	}
	// UPPERWord -> upperWord
	sbuf := new(strings.Builder)
	sbuf.WriteRune(unicode.ToLower(r))
	i := sz
	r, sz = r2, sz2
	for unicode.IsUpper(r) && sz > 0 {
		r2, sz2 := utf8.DecodeRuneInString(name[i+sz:])
		if sz2 > 0 && unicode.IsLower(r2) {
			break
		}
		i += sz
		sbuf.WriteRune(unicode.ToLower(r))
		r, sz = r2, sz2
	}
	sbuf.WriteString(name[i:])
	return sbuf.String()
}

// export converts a name that is potentially unexported to an exported name.
func export(name string) string {
	if name == "" {
		return ""
	}
	r, sz := utf8.DecodeRuneInString(name)
	if unicode.IsUpper(r) {
		// Foo -> Foo
		return name
	}
	// fooBar -> FooBar
	sbuf := new(strings.Builder)
	sbuf.WriteRune(unicode.ToUpper(r))
	sbuf.WriteString(name[sz:])
	return sbuf.String()
}

// disambiguate picks a unique name, preferring name if it is already unique.
func disambiguate(name string, collides func(string) bool) string {
	if !collides(name) {
		return name
	}
	buf := []byte(name)
	if len(buf) > 0 && buf[len(buf)-1] >= '0' && buf[len(buf)-1] <= '9' {
		buf = append(buf, '_')
	}
	base := len(buf)
	for n := 2; ; n++ {
		buf = strconv.AppendInt(buf[:base], int64(n), 10)
		sbuf := string(buf)
		if !collides(sbuf) {
			return sbuf
		}
	}
}

// accessibleFrom reports whether node can be copied to wantPkg without
// violating Go visibility rules.
func accessibleFrom(info *types.Info, node ast.Node, wantPkg string) error {
	var unexportError error
	ast.Inspect(node, func(node ast.Node) bool {
		if unexportError != nil {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		obj := info.ObjectOf(ident)
		if _, ok := obj.(*types.PkgName); ok {
			// Local package names are fine, since we can just reimport them.
			return true
		}
		if pkg := obj.Pkg(); pkg != nil && !ast.IsExported(ident.Name) && pkg.Path() != wantPkg {
			unexportError = fmt.Errorf("uses unexported identifier %s", obj.Name())
			return false
		}
		return true
	})
	return unexportError
}

var (
	errorType   = types.Universe.Lookup("error").Type()
	cleanupType = types.NewSignature(nil, nil, nil, false)
)
