package analyzer

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// ParseFile parses a Go source file and returns the tree-sitter tree.
// Even syntactically invalid files will produce a partial tree (tree-sitter is error-tolerant).
func ParseFile(source []byte) (*sitter.Tree, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	lang := sitter.NewLanguage(golang.Language())
	if err := parser.SetLanguage(lang); err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	return tree, nil
}
