package analyzer

import (
	"testing"
)

func TestCollectSymbols_ExportedFunc(t *testing.T) {
	source := []byte(`package mypkg

func Hello() string {
	return "hello"
}

func unexported() {}
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer tree.Close()

	defs, _ := CollectSymbols(tree, source, "test.go", "mypkg", false)

	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}

	hello := defs[0]
	if hello.Name != "Hello" {
		t.Errorf("expected name 'Hello', got %q", hello.Name)
	}
	if hello.Kind != KindFunc {
		t.Errorf("expected kind 'func', got %q", hello.Kind)
	}
	if !hello.Exported {
		t.Error("expected Hello to be exported")
	}
	if hello.Package != "mypkg" {
		t.Errorf("expected package 'mypkg', got %q", hello.Package)
	}

	unexported := defs[1]
	if unexported.Name != "unexported" {
		t.Errorf("expected name 'unexported', got %q", unexported.Name)
	}
	if unexported.Exported {
		t.Error("expected unexported to not be exported")
	}
}

func TestCollectSymbols_Method(t *testing.T) {
	source := []byte(`package mypkg

type Server struct{}

func (s *Server) Start() error {
	return nil
}
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer tree.Close()

	defs, _ := CollectSymbols(tree, source, "test.go", "mypkg", false)

	// Expect: type Server + method Start
	var methodDef *SymbolDef
	for i := range defs {
		if defs[i].Kind == KindMethod {
			methodDef = &defs[i]
			break
		}
	}
	if methodDef == nil {
		t.Fatal("expected a method definition")
	}
	if methodDef.Name != "Start" {
		t.Errorf("expected method name 'Start', got %q", methodDef.Name)
	}
	if methodDef.Receiver != "Server" {
		t.Errorf("expected receiver 'Server', got %q", methodDef.Receiver)
	}
}

func TestCollectSymbols_References(t *testing.T) {
	source := []byte(`package mypkg

func Hello() string {
	return greet("world")
}

func greet(name string) string {
	return "hello " + name
}
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer tree.Close()

	_, refs := CollectSymbols(tree, source, "test.go", "mypkg", false)

	// Check that "greet" appears as a reference.
	found := false
	for _, ref := range refs {
		if ref.Name == "greet" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'greet' to be found as a reference")
	}
}

func TestCollectSymbols_TestFile(t *testing.T) {
	source := []byte(`package mypkg

func TestSomething(t *testing.T) {
	Hello()
}
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer tree.Close()

	defs, refs := CollectSymbols(tree, source, "test_test.go", "mypkg", true)

	// Test files should not produce definitions.
	if len(defs) != 0 {
		t.Errorf("expected 0 definitions from test file, got %d", len(defs))
	}

	// But references should still be collected.
	found := false
	for _, ref := range refs {
		if ref.Name == "Hello" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Hello' to be found as a reference in test file")
	}
}

func TestCollectSymbols_IgnoreComment(t *testing.T) {
	source := []byte(`package mypkg

// overkill:ignore
func Deprecated() {}

func Active() {}
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer tree.Close()

	defs, _ := CollectSymbols(tree, source, "test.go", "mypkg", false)

	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}

	if !defs[0].Ignored {
		t.Error("expected Deprecated to be ignored")
	}
	if defs[1].Ignored {
		t.Error("expected Active to not be ignored")
	}
}

func TestCollectSymbols_ConstAndVar(t *testing.T) {
	source := []byte(`package mypkg

const MaxSize = 100

var DefaultName = "test"
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer tree.Close()

	defs, _ := CollectSymbols(tree, source, "test.go", "mypkg", false)

	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}

	if defs[0].Kind != KindConst || defs[0].Name != "MaxSize" {
		t.Errorf("expected const MaxSize, got %s %s", defs[0].Kind, defs[0].Name)
	}
	if defs[1].Kind != KindVar || defs[1].Name != "DefaultName" {
		t.Errorf("expected var DefaultName, got %s %s", defs[1].Kind, defs[1].Name)
	}
}
