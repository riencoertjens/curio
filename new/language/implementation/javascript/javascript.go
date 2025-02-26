package javascript

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/ssoroka/slice"
	"golang.org/x/exp/slices"

	"github.com/bearer/bearer/new/language/implementation"
	"github.com/bearer/bearer/new/language/tree"
	"github.com/bearer/bearer/pkg/util/regex"

	patternquerytypes "github.com/bearer/bearer/new/language/patternquery/types"
	sitter "github.com/smacker/go-tree-sitter"
)

var (
	variableLookupParents = []string{
		"pair",
		"arguments",
		"binary_expression",
		"template_substitution",
		"array",
	}

	anonymousPatternNodeParentTypes = []string{}
	patternMatchNodeContainerTypes  = []string{}

	// $<name:type> or $<name:type1|type2> or $<name>
	patternQueryVariableRegex = regexp.MustCompile(`\$<(?P<name>[^>:!\.]+)(?::(?P<types>[^>]+))?>`)
	allowedPatternQueryTypes  = []string{"identifier", "property_identifier", "_", "member_expression"}

	matchNodeRegex = regexp.MustCompile(`\$<!>`)

	ellipsisRegex = regexp.MustCompile(`\$<\.\.\.>`)
)

type javascriptImplementation struct{}

func Get() implementation.Implementation {
	return &javascriptImplementation{}
}

func (implementation *javascriptImplementation) SitterLanguage() *sitter.Language {
	return javascript.GetLanguage()
}

func (*javascriptImplementation) AnalyzeFlow(rootNode *tree.Node) error {
	scope := implementation.NewScope(nil)

	return rootNode.Walk(func(node *tree.Node, visitChildren func() error) error {
		switch node.Type() {
		// () => {}
		// function getName() {}
		case "function", "arrow_function", "method_definition":
			previousScope := scope
			scope = implementation.NewScope(previousScope)
			err := visitChildren()
			scope = previousScope
			return err
		// user = ...
		case "assignment_expression":
			left := node.ChildByFieldName("left")
			right := node.ChildByFieldName("right")

			if left.Type() == "identifier" {
				err := visitChildren()

				scope.Assign(left.Content(), node)
				node.UnifyWith(right)

				return err
			}
		// const user = ...
		// var user = ...
		// let user = ...
		case "variable_declarator":
			name := node.ChildByFieldName("name")
			value := node.ChildByFieldName("value")

			if name.Type() == "identifier" {
				err := visitChildren()

				scope.Assign(name.Content(), node)
				node.UnifyWith(value)

				return err
			}
		case "identifier":
			parent := node.Parent()
			if parent == nil {
				break
			}

			if slice.Contains(variableLookupParents, parent.Type()) ||
				(parent.Type() == "assignment_expression" && node.Equal(parent.ChildByFieldName("right"))) ||
				(parent.Type() == "new_expression" && node.Equal(parent.ChildByFieldName("constructor"))) ||
				(parent.Type() == "variable_declarator" && node.Equal(parent.ChildByFieldName("value"))) ||
				(parent.Type() == "member_expression" && node.Equal(parent.ChildByFieldName("object"))) ||
				(parent.Type() == "call_expression" && node.Equal(parent.ChildByFieldName("function"))) ||
				(parent.Type() == "subscript_expression" && node.Equal(parent.ChildByFieldName("object"))) {
				if scopedNode := scope.Lookup(node.Content()); scopedNode != nil {
					node.UnifyWith(scopedNode)
				}
			}

			if parent.Type() == "formal_parameters" {
				scope.Assign(node.Content(), node)
			}
		case "property_identifier":
			parent := node.Parent()
			if parent != nil && slice.Contains(variableLookupParents, parent.Type()) {
				if scopedNode := scope.Lookup(node.Content()); scopedNode != nil {
					node.UnifyWith(scopedNode)
				}
			}
		}

		return visitChildren()
	})
}

// TODO: See if anything needs to be added here
func (implementation *javascriptImplementation) ExtractPatternVariables(input string) (string, []patternquerytypes.Variable, error) {
	nameIndex := patternQueryVariableRegex.SubexpIndex("name")
	typesIndex := patternQueryVariableRegex.SubexpIndex("types")
	i := 0

	var params []patternquerytypes.Variable

	replaced, err := regex.ReplaceAllWithSubmatches(patternQueryVariableRegex, input, func(submatches []string) (string, error) {
		nodeTypes := strings.Split(submatches[typesIndex], "|")
		if nodeTypes[0] == "" {
			nodeTypes = []string{"_"}
		}

		for _, nodeType := range nodeTypes {
			if !slices.Contains(allowedPatternQueryTypes, nodeType) {
				return "", fmt.Errorf("invalid node type '%s' in pattern query", nodeType)
			}
		}

		dummyValue := produceDummyValue(i, nodeTypes[0])

		params = append(params, patternquerytypes.Variable{
			Name:       submatches[nameIndex],
			NodeTypes:  nodeTypes,
			DummyValue: dummyValue,
		})

		i += 1

		return dummyValue, nil
	})

	if err != nil {
		return "", nil, err
	}

	return replaced, params, nil
}

// TODO: See if anything needs to be added here
func (implementation *javascriptImplementation) ExtractPatternMatchNode(input string) (string, int, error) {
	inputBytes := []byte(input)
	matches := matchNodeRegex.FindAllIndex(inputBytes, -1)

	if len(matches) == 0 {
		return input, 0, nil
	}

	if len(matches) > 1 {
		return "", 0, errors.New("pattern must only contain a single match node")
	}

	match := matches[0]
	return string(inputBytes[0:match[0]]) + string(inputBytes[match[1]:]), match[0], nil
}

func produceDummyValue(i int, nodeType string) string {
	return "CurioVar" + fmt.Sprint(i)
}

// TODO: See if anything needs to be added here
func (implementation *javascriptImplementation) AnonymousPatternNodeParentTypes() []string {
	return anonymousPatternNodeParentTypes
}

// TODO: See if anything needs to be added here
func (implementation *javascriptImplementation) FindPatternMatchNode(input []byte) [][]int {
	return matchNodeRegex.FindAllIndex(input, -1)
}

// TODO: See if anything needs to be added here
func (implementation *javascriptImplementation) FindPatternUnanchoredPoints(input []byte) [][]int {
	return ellipsisRegex.FindAllIndex(input, -1)
}

// TODO: See if anything needs to be added here
func (implementation *javascriptImplementation) IsTerminalDetectionNode(node *tree.Node) bool {
	return false
}

func (implementation *javascriptImplementation) PatternMatchNodeContainerTypes() []string {
	return patternMatchNodeContainerTypes
}

func (*javascriptImplementation) PatternLeafContentTypes() []string {
	return []string{
		// identifiers
		"identifier", "property_identifier", "shorthand_property_identifier",
		// datatypes/literals
		"template_string", "string", "number", "null", "true", "false",
	}
}

func (implementation *javascriptImplementation) PatternIsAnchored(node *tree.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return true
	}

	// Class body class_body
	// arrow functions statement_block
	// function statement_block
	// method statement_blocks
	unAnchored := []string{"statement_blocks", "class_body", "pair"}

	return !slices.Contains(unAnchored, node.Type())
}

func (implementation *javascriptImplementation) IsRootOfRuleQuery(node *tree.Node) bool {
	return !(node.Type() == "expression_statement")
}

func (implementation *javascriptImplementation) PatternNodeTypes(node *tree.Node) []string {
	return []string{node.Type()}
}

func (implementation *javascriptImplementation) TranslatePatternContent(fromNodeType, toNodeType, content string) string {
	return content
}
