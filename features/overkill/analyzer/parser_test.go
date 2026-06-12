package analyzer

import (
	"testing"
)

func TestParseFile_SimpleFunction(t *testing.T) {
	source := []byte(`package main

func Hello() string {
	return "hello"
}
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("expected non-nil root node")
	}
	if root.Kind() != "source_file" {
		t.Errorf("expected root kind 'source_file', got %q", root.Kind())
	}
	if root.ChildCount() == 0 {
		t.Fatal("expected at least one child node")
	}
}

func TestParseFile_TypeDeclaration(t *testing.T) {
	source := []byte(`package main

type MyStruct struct {
	Name string
	Age  int
}
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.ChildCount() < 2 {
		t.Fatalf("expected at least 2 children (package_clause + type_declaration), got %d", root.ChildCount())
	}
}

func TestParseFile_InvalidSyntax(t *testing.T) {
	source := []byte(`package main

func Broken( {
	this is invalid
}
`)
	tree, err := ParseFile(source)
	if err != nil {
		t.Fatalf("ParseFile should not fail on invalid syntax: %v", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("expected non-nil root node even for invalid syntax")
	}
	// tree-sitter produces a tree with ERROR nodes for invalid syntax.
	if !root.HasError() {
		t.Error("expected tree to have errors for invalid syntax")
	}
}
