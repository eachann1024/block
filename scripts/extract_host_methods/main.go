package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var hostMethodRe = regexp.MustCompile(`^host\.[a-z0-9_]+(?:\.[a-z0-9_]+)?$`)

type extractor struct {
	consts            map[string]ast.Expr
	vars              map[string]ast.Expr
	funcs             map[string][]*ast.FuncDecl
	hostConfigTargets map[string]string
	visited           map[*ast.FuncDecl]bool
	methods           map[string]bool
}

func main() {
	dirFlag := flag.String("dir", filepath.Join("internal", "plugins"), "directory containing plugin host dispatchers")
	flag.Parse()

	methods, err := extractFromDir(*dirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "extract_host_methods: %v\n", err)
		os.Exit(1)
	}
	for _, m := range methods {
		fmt.Println(m)
	}
}

func extractFromDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	ex := &extractor{
		consts:            make(map[string]ast.Expr),
		vars:              make(map[string]ast.Expr),
		funcs:             make(map[string][]*ast.FuncDecl),
		hostConfigTargets: make(map[string]string),
		visited:           make(map[*ast.FuncDecl]bool),
		methods:           make(map[string]bool),
	}
	for _, f := range files {
		ex.collectDecls(f)
	}
	for _, fn := range ex.funcs["dispatchHostMethod"] {
		ex.walkFunc(fn, nil)
	}

	out := make([]string, 0, len(ex.methods))
	for m := range ex.methods {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

func collectConstSpecs(d *ast.GenDecl, dst map[string]ast.Expr) {
	if d.Tok != token.CONST {
		return
	}
	var prev []ast.Expr
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		vals := vs.Values
		if len(vals) == 0 {
			vals = prev
		} else {
			prev = vals
		}
		for i, name := range vs.Names {
			if i < len(vals) && name.Name != "_" {
				dst[name.Name] = vals[i]
			}
		}
	}
}

func (ex *extractor) collectDecls(f *ast.File) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil && d.Body != nil {
				ex.funcs[d.Name.Name] = append(ex.funcs[d.Name.Name], d)
			}
		case *ast.GenDecl:
			if d.Tok == token.CONST {
				collectConstSpecs(d, ex.consts)
			} else if d.Tok == token.VAR {
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i < len(vs.Values) && name.Name != "_" {
							ex.vars[name.Name] = vs.Values[i]
						}
					}
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if id, ok := cl.Type.(*ast.Ident); ok && id.Name == "HostConfig" {
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				field, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				if target := callTargetName(kv.Value); target != "" {
					ex.hostConfigTargets[field.Name] = target
				}
			}
		}
		return true
	})
}

func callTargetName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}

func (ex *extractor) evalString(expr ast.Expr, locals map[string]ast.Expr, visiting map[string]bool) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.ParenExpr:
		return ex.evalString(e.X, locals, visiting)
	case *ast.Ident:
		if visiting[e.Name] {
			return "", false
		}
		target, ok := locals[e.Name]
		if !ok {
			target, ok = ex.consts[e.Name]
		}
		if !ok {
			return "", false
		}
		visiting[e.Name] = true
		val, ok := ex.evalString(target, locals, visiting)
		delete(visiting, e.Name)
		return val, ok
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, okL := ex.evalString(e.X, locals, visiting)
		if !okL {
			return "", false
		}
		right, okR := ex.evalString(e.Y, locals, visiting)
		if !okR {
			return "", false
		}
		return left + right, true
	default:
		return "", false
	}
}

func (ex *extractor) addIfMethod(expr ast.Expr, locals map[string]ast.Expr) {
	val, ok := ex.evalString(expr, locals, make(map[string]bool))
	if ok && hostMethodRe.MatchString(val) {
		ex.methods[val] = true
	}
}

func initialMethodVars(fn *ast.FuncDecl, paramIndices []int) map[string]bool {
	vars := make(map[string]bool)
	if fn.Type == nil || fn.Type.Params == nil {
		return vars
	}
	if len(paramIndices) > 0 {
		pos := 0
		want := make(map[int]bool, len(paramIndices))
		for _, idx := range paramIndices {
			want[idx] = true
		}
		for _, field := range fn.Type.Params.List {
			if len(field.Names) == 0 {
				pos++
				continue
			}
			for _, name := range field.Names {
				if want[pos] {
					vars[name.Name] = true
				}
				pos++
			}
		}
	}
	if len(vars) == 0 {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if name.Name == "method" {
					vars[name.Name] = true
				}
			}
		}
	}
	return vars
}

func exprUsesMethodVar(expr ast.Expr, methodVars map[string]bool) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return methodVars[e.Name]
	case *ast.ParenExpr:
		return exprUsesMethodVar(e.X, methodVars)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			if exprUsesMethodVar(arg, methodVars) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (ex *extractor) walkFunc(fn *ast.FuncDecl, paramIndices []int) {
	if fn == nil || fn.Body == nil || ex.visited[fn] {
		return
	}
	ex.visited[fn] = true

	locals := make(map[string]ast.Expr)
	methodVars := initialMethodVars(fn, paramIndices)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			collectConstSpecs(node, locals)
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && i < len(node.Rhs) {
					if exprUsesMethodVar(node.Rhs[i], methodVars) {
						methodVars[id.Name] = true
					}
				}
			}
		}
		return true
	})

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SwitchStmt:
			if node.Tag != nil && exprUsesMethodVar(node.Tag, methodVars) && node.Body != nil {
				for _, stmt := range node.Body.List {
					cc, ok := stmt.(*ast.CaseClause)
					if !ok {
						continue
					}
					for _, expr := range cc.List {
						ex.addIfMethod(expr, locals)
					}
				}
			}
		case *ast.BinaryExpr:
			if node.Op == token.EQL {
				if exprUsesMethodVar(node.X, methodVars) {
					ex.addIfMethod(node.Y, locals)
				} else if exprUsesMethodVar(node.Y, methodVars) {
					ex.addIfMethod(node.X, locals)
				}
			}
		case *ast.IndexExpr:
			if exprUsesMethodVar(node.Index, methodVars) {
				if id, ok := node.X.(*ast.Ident); ok {
					if cl, ok := ex.vars[id.Name].(*ast.CompositeLit); ok {
						for _, elt := range cl.Elts {
							if kv, ok := elt.(*ast.KeyValueExpr); ok {
								ex.addIfMethod(kv.Key, locals)
							}
						}
					}
				}
			}
		case *ast.CallExpr:
			var passed []int
			for idx, arg := range node.Args {
				if exprUsesMethodVar(arg, methodVars) {
					passed = append(passed, idx)
				}
			}
			if len(passed) == 0 {
				return true
			}
			target := callTargetName(node.Fun)
			if mapped, ok := ex.hostConfigTargets[target]; ok {
				target = mapped
			}
			for _, callee := range ex.funcs[target] {
				ex.walkFunc(callee, passed)
			}
		}
		return true
	})
}
