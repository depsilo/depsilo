package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestAPIRoutesStayUnderVersionedGroups enforces the compatibility policy in
// docs/compatibility.md: every Depsilo-owned API route lives under /api/v1.
// Route registrations on the engine with an /api path must be exactly the
// versioned group, and registrations inside that group must be relative so a
// path cannot accidentally escape the prefix.
func TestAPIRoutesStayUnderVersionedGroups(t *testing.T) {
	source, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	violations, err := apiVersionViolations(source)
	if err != nil {
		t.Fatalf("parse router.go: %v", err)
	}
	for _, violation := range violations {
		t.Errorf("%s", violation)
	}
}

// TestAPIVersionContractCatchesViolations proves the contract check is not
// vacuous: an unversioned engine route, a repeated prefix inside the group,
// and a missing group are all reported.
func TestAPIVersionContractCatchesViolations(t *testing.T) {
	violations, err := apiVersionViolations([]byte(`package api

func build(r *gin.Engine) {
	apiV1 := r.Group("/api/v1")
	r.GET("/api/legacy", nil)
	apiV1.GET("/api/v1/legacy", nil)
}
`))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(violations, "\n")
	for _, want := range []string{
		`/api/legacy bypasses the versioned API group`,
		`/api/v1/legacy repeats the API prefix inside the versioned group`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("violations = %v, want %q", violations, want)
		}
	}

	missing, err := apiVersionViolations([]byte("package api\n\nfunc build(r *gin.Engine) {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || !strings.Contains(missing[0], "no longer creates the /api/v1 group") {
		t.Fatalf("missing-group violations = %v", missing)
	}
}

func apiVersionViolations(source []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "router.go", source, 0)
	if err != nil {
		return nil, err
	}

	httpMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	apiGroups := map[string]bool{
		"apiV1": true, "authGroup": true, "adminGroup": true,
		"adminRead": true, "adminWrite": true, "proRead": true, "proWrite": true,
		"packageHistoryRead": true,
	}
	sawVersionedGroup := false
	var violations []string

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) == 0 {
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
			violations = append(violations, "unquotable route path "+pathLiteral.Value)
			return true
		}

		switch {
		case selector.Sel.Name == "Group":
			if receiver.Name == "r" && strings.HasPrefix(path, "/api") {
				if path != "/api/v1" {
					violations = append(violations, "API group "+path+" must be the versioned /api/v1 prefix")
				}
				sawVersionedGroup = true
			}
		case httpMethods[selector.Sel.Name] && receiver.Name == "r":
			if strings.HasPrefix(path, "/api/") {
				violations = append(violations, selector.Sel.Name+" "+path+" bypasses the versioned API group")
			}
		case httpMethods[selector.Sel.Name] && apiGroups[receiver.Name]:
			if strings.HasPrefix(path, "/api") {
				violations = append(violations, selector.Sel.Name+" "+path+" repeats the API prefix inside the versioned group")
			}
		}
		return true
	})

	if !sawVersionedGroup {
		violations = append(violations, "router.go no longer creates the /api/v1 group")
	}
	return violations, nil
}
