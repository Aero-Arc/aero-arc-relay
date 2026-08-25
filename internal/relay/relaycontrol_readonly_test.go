package relay

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

// This test enforces that RelayControl RPC handlers remain strictly read-only.
//
// It intentionally checks source for disallowed operations so future changes can't
// accidentally introduce state mutation (command creep) in v0.1.
func TestRelayControl_IsReadOnly(t *testing.T) {
	// Locate apiserver.go on disk.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	apiserverPath := filepath.Join(filepath.Dir(thisFile), "apiserver.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, apiserverPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", apiserverPath, err)
	}

	targetFns := map[string]bool{
		"ListActiveDrones": true,
		"GetDroneStatus":   true,
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || !targetFns[fn.Name.Name] || fn.Body == nil {
			continue
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			// Disallow taking a write lock in RelayControl handlers.
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if sel.Sel != nil && sel.Sel.Name == "Lock" {
						t.Fatalf("%s must not call Lock() (RelayControl is read-only)", fn.Name.Name)
					}
				}
				// Disallow delete(s.grpcSessions, ...) patterns.
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "delete" && len(call.Args) > 0 {
					if isSelector(call.Args[0], "s", "grpcSessions") {
						t.Fatalf("%s must not delete from s.grpcSessions (RelayControl is read-only)", fn.Name.Name)
					}
				}
			}

			// Disallow map writes like: s.grpcSessions[key] = ...
			if as, ok := n.(*ast.AssignStmt); ok {
				for _, lhs := range as.Lhs {
					if idx, ok := lhs.(*ast.IndexExpr); ok {
						if isSelector(idx.X, "s", "grpcSessions") {
							t.Fatalf("%s must not mutate s.grpcSessions (RelayControl is read-only)", fn.Name.Name)
						}
					}
				}
			}

			return true
		})
	}
}

func isSelector(expr ast.Expr, ident, sel string) bool {
	selExpr, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := selExpr.X.(*ast.Ident)
	if !ok || x.Name != ident {
		return false
	}
	return selExpr.Sel != nil && selExpr.Sel.Name == sel
}
