package contract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// TestContractRules keeps the package within the rules in doc.go, which make
// the generated TypeScript match the JSON encoding exactly.
func TestContractRules(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if strings.Contains(strings.Split(path, "/")[0], ".") || strings.HasPrefix(path, "depsilo/") {
				t.Errorf("%s imports %s; contract types depend only on the standard library", name, path)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			typeSpec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structType.Fields.List {
				checkField(t, name, typeSpec.Name.Name, field)
			}
			return true
		})
	}
}

func checkField(t *testing.T, file, typeName string, field *ast.Field) {
	t.Helper()
	fieldName := typeName
	if len(field.Names) > 0 {
		fieldName += "." + field.Names[0].Name
	}
	if len(field.Names) > 0 && !field.Names[0].IsExported() {
		t.Errorf("%s: %s is unexported; contract fields are all part of the JSON body", file, fieldName)
		return
	}
	var tag reflect.StructTag
	if field.Tag != nil {
		raw, _ := strconv.Unquote(field.Tag.Value)
		tag = reflect.StructTag(raw)
	}
	jsonTag, hasJSON := tag.Lookup("json")
	if len(field.Names) > 0 && !hasJSON {
		t.Errorf("%s: %s has no json tag", file, fieldName)
	}
	if _, pointer := field.Type.(*ast.StarExpr); !pointer {
		return
	}
	omitEmpty := strings.Contains(jsonTag, ",omitempty")
	tsType, tsOptions, _ := strings.Cut(tag.Get("tstype"), ",")
	nullable := strings.HasSuffix(strings.TrimSpace(tsType), "| null") && tsOptions == "required"
	switch {
	case omitEmpty && tsType != "":
		t.Errorf("%s: %s is omitempty and also declares a tstype", file, fieldName)
	case !omitEmpty && !nullable:
		t.Errorf("%s: pointer %s needs json omitempty or tstype:\"T | null,required\"", file, fieldName)
	}
}
