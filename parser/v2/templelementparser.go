package parser

import (
	"github.com/a-h/parse"
	"github.com/a-h/templ/parser/v2/goexpression"
)

type templElementExpressionParser struct{}

func (p templElementExpressionParser) Parse(pi *parse.Input) (n Node, matched bool, err error) {
	// Check the prefix first.
	if _, matched, err = parse.Rune('@').Parse(pi); err != nil || !matched {
		return nil, false, nil
	}

	// Parse the Go expression.
	r := &TemplElementExpression{}
	if r.Expression, err = parseGo("templ element", pi, goexpression.TemplExpression); err != nil {
		return r, true, err
	}

	// Once we've got a start expression, check to see if there's an open brace for children. {\n.
	var hasOpenBrace bool
	_, hasOpenBrace, err = openBraceWithOptionalPadding.Parse(pi)
	if err != nil {
		return
	}
	if !hasOpenBrace {
		return r, true, nil
	}

	// Once we've had the start of an element's children, we must conclude the block.

	// Node contents.
	np := newTemplateNodeParser(closeBraceWithOptionalPadding, "templ element closing brace")
	var nodes Nodes
	if nodes, matched, err = np.Parse(pi); err != nil || !matched {
		// Populate the nodes anyway, so that the LSP can use them.
		r.Children = nodes.Nodes
		err = parse.Error("@"+r.Expression.Value+": expected nodes, but none were found", pi.Position())
		return r, true, err
	}
	r.Children = nodes.Nodes

	// Read the required closing brace.
	if _, matched, err = closeBraceWithOptionalPadding.Parse(pi); err != nil || !matched {
		err = parse.Error("@"+r.Expression.Value+": missing end (expected '}')", pi.Position())
		return r, true, err
	}

	return r, true, nil
}

var templElementExpression templElementExpressionParser
