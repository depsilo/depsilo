package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestAdminRoutesUseExplicitCapabilityGroups(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "router.go", nil, 0)
	if err != nil {
		t.Fatalf("parse router.go: %v", err)
	}
	httpMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	readCount := 0
	writeCount := 0
	seenRulesTest := false
	seenLogExport := false

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !httpMethods[selector.Sel.Name] || len(call.Args) == 0 {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		pathLiteral, ok := call.Args[0].(*ast.BasicLit)
		if !ok || pathLiteral.Kind != token.STRING {
			return true
		}
		path, err := strconv.Unquote(pathLiteral.Value)
		if err != nil {
			t.Fatalf("unquote route path %s: %v", pathLiteral.Value, err)
		}
		method := selector.Sel.Name
		switch receiver.Name {
		case "adminGroup", "proGroup":
			t.Errorf("%s %s bypasses explicit capability group via %s", method, path, receiver.Name)
		case "adminRead", "proRead":
			readCount++
			if method != "GET" && !(receiver.Name == "adminRead" && method == "POST" && path == "/rules/test") {
				t.Errorf("%s %s incorrectly registered as read capability", method, path)
			}
			seenRulesTest = seenRulesTest || (method == "POST" && path == "/rules/test")
			seenLogExport = seenLogExport || (method == "GET" && path == "/logs/export")
		case "adminWrite", "proWrite":
			writeCount++
			if method == "GET" {
				t.Errorf("GET %s incorrectly registered as write capability", path)
			}
		}
		return true
	})
	if readCount == 0 || writeCount == 0 {
		t.Fatalf("route counts read=%d write=%d", readCount, writeCount)
	}
	if !seenRulesTest {
		t.Fatal("POST /rules/test is not registered on adminRead")
	}
	if !seenLogExport {
		t.Fatal("GET /logs/export is not registered on adminRead")
	}
}
