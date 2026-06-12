package analyzer

import (
	"strings"
	"unicode"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// SymbolKind represents the kind of a symbol.
type SymbolKind string

const (
	KindFunc   SymbolKind = "func"
	KindMethod SymbolKind = "method"
	KindType   SymbolKind = "type"
	KindConst  SymbolKind = "const"
	KindVar    SymbolKind = "var"
)

// SymbolDef represents a symbol definition.
type SymbolDef struct {
	Name      string
	Kind      SymbolKind
	Package   string // Go package name
	File      string // file path
	Line      int    // 1-indexed line number
	Exported  bool   // starts with uppercase
	Receiver  string // for methods: receiver type name
	StartByte uint   // for deletion: start of the declaration
	EndByte   uint   // for deletion: end of the declaration
	Ignored   bool   // has // overkill:ignore comment
}

// SymbolRef represents a reference to a symbol.
type SymbolRef struct {
	Name    string
	File    string
	Line    int
	Package string // Go package name of the referencing file
}

// CollectSymbols walks the AST and extracts definitions and references.
// If isTestFile is true, definitions are not collected (but references are).
func CollectSymbols(tree *sitter.Tree, source []byte, filePath string, pkgName string, isTestFile bool) ([]SymbolDef, []SymbolRef) {
	var defs []SymbolDef
	var refs []SymbolRef

	root := tree.RootNode()

	// Walk all top-level children for definitions.
	for i := range root.ChildCount() {
		child := root.Child(i)
		if child == nil {
			continue
		}

		switch child.Kind() {
		case "function_declaration":
			if !isTestFile {
				if def := extractFuncDef(child, source, filePath, pkgName); def != nil {
					def.Ignored = hasIgnoreComment(child, source)
					defs = append(defs, *def)
				}
			}
		case "method_declaration":
			if !isTestFile {
				if def := extractMethodDef(child, source, filePath, pkgName); def != nil {
					def.Ignored = hasIgnoreComment(child, source)
					defs = append(defs, *def)
				}
			}
		case "type_declaration":
			if !isTestFile {
				defs = append(defs, extractTypeDefs(child, source, filePath, pkgName)...)
			}
		case "const_declaration":
			if !isTestFile {
				defs = append(defs, extractConstVarDefs(child, source, filePath, pkgName, KindConst)...)
			}
		case "var_declaration":
			if !isTestFile {
				defs = append(defs, extractConstVarDefs(child, source, filePath, pkgName, KindVar)...)
			}
		}
	}

	// Walk entire tree for references (identifiers).
	collectRefs(root, source, filePath, pkgName, &refs)

	return defs, refs
}

// extractFuncDef extracts a function definition from a function_declaration node.
func extractFuncDef(node *sitter.Node, source []byte, filePath, pkgName string) *SymbolDef {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nodeText(nameNode, source)
	return &SymbolDef{
		Name:      name,
		Kind:      KindFunc,
		Package:   pkgName,
		File:      filePath,
		Line:      int(node.StartPosition().Row) + 1,
		Exported:  isExported(name),
		StartByte: uint(node.StartByte()),
		EndByte:   uint(node.EndByte()),
	}
}

// extractMethodDef extracts a method definition from a method_declaration node.
func extractMethodDef(node *sitter.Node, source []byte, filePath, pkgName string) *SymbolDef {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nodeText(nameNode, source)

	// Extract receiver type name.
	receiver := ""
	paramNode := node.ChildByFieldName("receiver")
	if paramNode != nil {
		receiver = extractReceiverType(paramNode, source)
	}

	return &SymbolDef{
		Name:      name,
		Kind:      KindMethod,
		Package:   pkgName,
		File:      filePath,
		Line:      int(node.StartPosition().Row) + 1,
		Exported:  isExported(name),
		Receiver:  receiver,
		StartByte: uint(node.StartByte()),
		EndByte:   uint(node.EndByte()),
	}
}

// extractReceiverType extracts the receiver type name from a parameter_list node.
func extractReceiverType(paramList *sitter.Node, source []byte) string {
	// parameter_list > parameter_declaration > type
	for i := range paramList.ChildCount() {
		child := paramList.Child(i)
		if child == nil || child.Kind() != "parameter_declaration" {
			continue
		}
		// The type can be a pointer_type or a type_identifier.
		typeNode := child.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		return extractBaseType(typeNode, source)
	}
	return ""
}

// extractBaseType gets the base type name, stripping pointer (*).
func extractBaseType(node *sitter.Node, source []byte) string {
	if node.Kind() == "pointer_type" {
		// pointer_type has a child which is the base type.
		for i := range node.ChildCount() {
			child := node.Child(i)
			if child != nil && child.Kind() != "*" {
				return nodeText(child, source)
			}
		}
	}
	return nodeText(node, source)
}

// extractTypeDefs extracts type definitions from a type_declaration node.
func extractTypeDefs(node *sitter.Node, source []byte, filePath, pkgName string) []SymbolDef {
	var defs []SymbolDef
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || child.Kind() != "type_spec" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := nodeText(nameNode, source)
		def := SymbolDef{
			Name:      name,
			Kind:      KindType,
			Package:   pkgName,
			File:      filePath,
			Line:      int(child.StartPosition().Row) + 1,
			Exported:  isExported(name),
			StartByte: uint(node.StartByte()),
			EndByte:   uint(node.EndByte()),
			Ignored:   hasIgnoreComment(node, source),
		}
		defs = append(defs, def)
	}
	return defs
}

// extractConstVarDefs extracts const/var definitions from a const_declaration or var_declaration node.
func extractConstVarDefs(node *sitter.Node, source []byte, filePath, pkgName string, kind SymbolKind) []SymbolDef {
	var defs []SymbolDef
	specKind := "const_spec"
	if kind == KindVar {
		specKind = "var_spec"
	}
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || child.Kind() != specKind {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := nodeText(nameNode, source)
		def := SymbolDef{
			Name:      name,
			Kind:      kind,
			Package:   pkgName,
			File:      filePath,
			Line:      int(child.StartPosition().Row) + 1,
			Exported:  isExported(name),
			StartByte: uint(node.StartByte()),
			EndByte:   uint(node.EndByte()),
			Ignored:   hasIgnoreComment(node, source),
		}
		defs = append(defs, def)
	}
	return defs
}

// collectRefs recursively collects identifier references from the AST.
func collectRefs(node *sitter.Node, source []byte, filePath, pkgName string, refs *[]SymbolRef) {
	if node == nil {
		return
	}

	kind := node.Kind()

	// Collect identifiers that are references (not definitions).
	if kind == "identifier" || kind == "type_identifier" || kind == "field_identifier" {
		// Skip if this is a definition name (handled by parent extraction).
		parent := node.Parent()
		if parent != nil && isDefNameNode(node, parent) {
			// This is a definition, not a reference.
		} else {
			name := nodeText(node, source)
			if name != "" && name != "_" {
				*refs = append(*refs, SymbolRef{
					Name:    name,
					File:    filePath,
					Line:    int(node.StartPosition().Row) + 1,
					Package: pkgName,
				})
			}
		}
	}

	// Recurse into children.
	for i := range node.ChildCount() {
		child := node.Child(i)
		collectRefs(child, source, filePath, pkgName, refs)
	}
}

// isDefNameNode checks if the node is the "name" field of a definition node.
func isDefNameNode(node, parent *sitter.Node) bool {
	switch parent.Kind() {
	case "function_declaration", "method_declaration":
		nameNode := parent.ChildByFieldName("name")
		return nameNode != nil && nameNode.StartByte() == node.StartByte()
	case "type_spec", "const_spec", "var_spec":
		nameNode := parent.ChildByFieldName("name")
		return nameNode != nil && nameNode.StartByte() == node.StartByte()
	case "package_clause":
		return true // package name is not a reference
	}
	return false
}

// hasIgnoreComment checks if the node has a preceding comment containing "overkill:ignore".
func hasIgnoreComment(node *sitter.Node, source []byte) bool {
	// Look at the previous sibling for a comment node.
	prev := node.PrevSibling()
	if prev != nil && prev.Kind() == "comment" {
		text := nodeText(prev, source)
		if strings.Contains(text, "overkill:ignore") {
			return true
		}
	}
	return false
}

// nodeText returns the text content of a tree-sitter node.
func nodeText(node *sitter.Node, source []byte) string {
	start := node.StartByte()
	end := node.EndByte()
	if start >= uint(len(source)) || end > uint(len(source)) {
		return ""
	}
	return string(source[start:end])
}

// isExported returns true if the name starts with an uppercase letter.
func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return unicode.IsUpper(rune(name[0]))
}
