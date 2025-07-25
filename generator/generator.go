package generator

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	_ "embed"

	"github.com/a-h/templ/parser/v2"
)

type GenerateOpt func(g *generator) error

// WithVersion enables the version to be included in the generated code.
func WithVersion(v string) GenerateOpt {
	return func(g *generator) error {
		g.options.Version = v
		return nil
	}
}

// WithTimestamp enables the generated date to be included in the generated code.
func WithTimestamp(d time.Time) GenerateOpt {
	return func(g *generator) error {
		g.options.GeneratedDate = d.Format(time.RFC3339)
		return nil
	}
}

// WithFileName sets the filename of the templ file in template rendering error messages.
func WithFileName(name string) GenerateOpt {
	return func(g *generator) error {
		if filepath.IsAbs(name) {
			_, g.options.FileName = filepath.Split(name)
			return nil
		}
		g.options.FileName = name
		return nil
	}
}

// WithSkipCodeGeneratedComment skips the code generated comment at the top of the file.
// gopls disables edit related functionality for generated files, so the templ LSP may
// wish to skip generation of this comment so that gopls provides expected results.
func WithSkipCodeGeneratedComment() GenerateOpt {
	return func(g *generator) error {
		g.options.SkipCodeGeneratedComment = true
		return nil
	}
}

type GeneratorOutput struct {
	Options   GeneratorOptions  `json:"meta"`
	SourceMap *parser.SourceMap `json:"sourceMap"`
	Literals  []string          `json:"literals"`
}

type GeneratorOptions struct {
	// Version of templ.
	Version string
	// FileName to include in error messages if string expressions return an error.
	FileName string
	// SkipCodeGeneratedComment skips the code generated comment at the top of the file.
	SkipCodeGeneratedComment bool
	// GeneratedDate to include as a comment.
	GeneratedDate string
}

// HasGoChanged returns true if the Go code has changed between the previous and updated GeneratorOutput.
func HasGoChanged(previous, updated GeneratorOutput) bool {
	// If generator options have changed, we need to recompile.
	if previous.Options.Version != updated.Options.Version {
		return true
	}
	if previous.Options.FileName != updated.Options.FileName {
		return true
	}
	if previous.Options.SkipCodeGeneratedComment != updated.Options.SkipCodeGeneratedComment {
		return true
	}
	// We don't check the generated date as it's not used for determining if the file has changed.
	// If the number of literals has changed, we need to recompile.
	if len(previous.Literals) != len(updated.Literals) {
		return true
	}
	// If the Go code has changed, we need to recompile.
	if len(previous.SourceMap.Expressions) != len(updated.SourceMap.Expressions) {
		return true
	}
	for i, prev := range previous.SourceMap.Expressions {
		if prev != updated.SourceMap.Expressions[i] {
			return true
		}
	}
	return false
}

// HasTextChanged returns true if the text literals have changed between the previous and updated GeneratorOutput.
func HasTextChanged(previous, updated GeneratorOutput) bool {
	if len(previous.Literals) != len(updated.Literals) {
		return true
	}
	for i, prev := range previous.Literals {
		if prev != updated.Literals[i] {
			return true
		}
	}
	return false
}

// Generate generates Go code from the input template file to w, and returns a map of the location of Go expressions in the template
// to the location of the generated Go code in the output.
func Generate(template *parser.TemplateFile, w io.Writer, opts ...GenerateOpt) (op GeneratorOutput, err error) {
	g := &generator{
		tf:        template,
		w:         NewRangeWriter(w),
		sourceMap: parser.NewSourceMap(),
	}
	for _, opt := range opts {
		if err = opt(g); err != nil {
			return
		}
	}
	err = g.generate()
	if err != nil {
		return op, err
	}
	op.Options = g.options
	op.SourceMap = g.sourceMap
	op.Literals = g.w.Literals
	return op, nil
}

type generator struct {
	tf          *parser.TemplateFile
	w           *RangeWriter
	sourceMap   *parser.SourceMap
	variableID  int
	childrenVar string

	options GeneratorOptions
}

func (g *generator) generate() (err error) {
	if err = g.writeCodeGeneratedComment(); err != nil {
		return
	}
	if err = g.writeVersionComment(); err != nil {
		return
	}
	if err = g.writeGeneratedDateComment(); err != nil {
		return
	}
	if err = g.writeHeader(); err != nil {
		return
	}
	if err = g.writePackage(); err != nil {
		return
	}
	if err = g.writeImports(); err != nil {
		return
	}
	if err = g.writeTemplateNodes(); err != nil {
		return
	}
	if err = g.writeBlankAssignmentForRuntimeImport(); err != nil {
		return
	}
	return err
}

// See https://pkg.go.dev/cmd/go#hdr-Generate_Go_files_by_processing_source
// Automatically generated files have a comment in the header that instructs the LSP
// to stop operating.
func (g *generator) writeCodeGeneratedComment() (err error) {
	if g.options.SkipCodeGeneratedComment {
		// Write an empty comment so that the file is the same shape.
		_, err = g.w.Write("//\n\n")
		return err
	}
	_, err = g.w.Write("// Code generated by templ - DO NOT EDIT.\n\n")
	return err
}

func (g *generator) writeVersionComment() (err error) {
	if g.options.Version != "" {
		_, err = g.w.Write("// templ: version: " + g.options.Version + "\n")
	}
	return err
}

func (g *generator) writeGeneratedDateComment() (err error) {
	if g.options.GeneratedDate != "" {
		_, err = g.w.Write("// templ: generated: " + g.options.GeneratedDate + "\n")
	}
	return err
}

func (g *generator) writeHeader() (err error) {
	if len(g.tf.Header) == 0 {
		return nil
	}
	for _, n := range g.tf.Header {
		if err := g.writeGoExpression(n); err != nil {
			return err
		}
	}
	return err
}

func (g *generator) writePackage() error {
	var r parser.Range
	var err error
	// package ...
	if r, err = g.w.Write(g.tf.Package.Expression.Value + "\n\n"); err != nil {
		return err
	}
	g.sourceMap.Add(g.tf.Package.Expression, r)
	if _, err = g.w.Write("//lint:file-ignore SA4006 This context is only used if a nested component is present.\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeImports() error {
	var err error
	// Always import templ because it's the interface type of all templates.
	if _, err = g.w.Write("import \"github.com/a-h/templ\"\n"); err != nil {
		return err
	}
	if _, err = g.w.Write("import templruntime \"github.com/a-h/templ/runtime\"\n"); err != nil {
		return err
	}
	if _, err = g.w.Write("\n"); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeTemplateNodes() error {
	for i, n := range g.tf.Nodes {
		switch n := n.(type) {
		case *parser.TemplateFileGoExpression:
			if err := g.writeGoExpression(n); err != nil {
				return err
			}
		case *parser.HTMLTemplate:
			if err := g.writeTemplate(i, n); err != nil {
				return err
			}
		case *parser.CSSTemplate:
			if err := g.writeCSS(n); err != nil {
				return err
			}
		case *parser.ScriptTemplate:
			if err := g.writeScript(n); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown node type: %v", reflect.TypeOf(n))
		}
	}
	return nil
}

func (g *generator) writeCSS(n *parser.CSSTemplate) error {
	if n == nil {
		return errors.New("CSS template is nil")
	}
	var r parser.Range
	var tgtSymbolRange parser.Range
	var err error
	var indentLevel int

	// func
	if r, err = g.w.Write("func "); err != nil {
		return err
	}
	tgtSymbolRange.From = r.From
	if r, err = g.w.Write(n.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(n.Expression, r)
	// templ.CSSClass {
	if _, err = g.w.Write(" templ.CSSClass {\n"); err != nil {
		return err
	}
	{
		indentLevel++
		// templ_7745c5c3_CSSBuilder := templruntim.GetBuilder()
		if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_CSSBuilder := templruntime.GetBuilder()\n"); err != nil {
			return err
		}
		for _, p := range n.Properties {
			switch p := p.(type) {
			case *parser.ConstantCSSProperty:
				// Constant CSS property values are not sanitized.
				if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_CSSBuilder.WriteString("+createGoString(p.String(true))+")\n"); err != nil {
					return err
				}
			case *parser.ExpressionCSSProperty:
				// templ_7745c5c3_CSSBuilder.WriteString(templ.SanitizeCSS('name', p.Expression()))
				if _, err = g.w.WriteIndent(indentLevel, fmt.Sprintf("templ_7745c5c3_CSSBuilder.WriteString(string(templ.SanitizeCSS(`%s`, ", p.Name)); err != nil {
					return err
				}
				if r, err = g.w.Write(p.Value.Expression.Value); err != nil {
					return err
				}
				g.sourceMap.Add(p.Value.Expression, r)
				if _, err = g.w.Write(")))\n"); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown CSS property type: %v", reflect.TypeOf(p))
			}
		}
		if _, err = g.w.WriteIndent(indentLevel, fmt.Sprintf("templ_7745c5c3_CSSID := templ.CSSID(`%s`, templ_7745c5c3_CSSBuilder.String())\n", n.Name)); err != nil {
			return err
		}
		// return templ.CSS {
		if _, err = g.w.WriteIndent(indentLevel, "return templ.ComponentCSSClass{\n"); err != nil {
			return err
		}
		{
			indentLevel++
			// ID: templ_7745c5c3_CSSID,
			if _, err = g.w.WriteIndent(indentLevel, "ID: templ_7745c5c3_CSSID,\n"); err != nil {
				return err
			}
			// Class: templ.SafeCSS(".cssID{" + templ.CSSBuilder.String() + "}"),
			if _, err = g.w.WriteIndent(indentLevel, "Class: templ.SafeCSS(`.` + templ_7745c5c3_CSSID + `{` + templ_7745c5c3_CSSBuilder.String() + `}`),\n"); err != nil {
				return err
			}
			indentLevel--
		}
		if _, err = g.w.WriteIndent(indentLevel, "}\n"); err != nil {
			return err
		}
		indentLevel--
	}
	// }
	if r, err = g.w.WriteIndent(indentLevel, "}\n\n"); err != nil {
		return err
	}

	// Keep a track of symbol ranges for the LSP.
	tgtSymbolRange.To = r.To
	g.sourceMap.AddSymbolRange(n.Range, tgtSymbolRange)

	return nil
}

func (g *generator) writeGoExpression(n *parser.TemplateFileGoExpression) (err error) {
	if n == nil {
		return errors.New("go expression is nil")
	}
	var tgtSymbolRange parser.Range

	r, err := g.w.Write(n.Expression.Value)
	if err != nil {
		return err
	}
	tgtSymbolRange.From = r.From
	g.sourceMap.Add(n.Expression, r)
	v := n.Expression.Value
	lineSlice := strings.Split(v, "\n")
	lastLine := lineSlice[len(lineSlice)-1]
	if strings.HasPrefix(lastLine, "//") {
		if _, err = g.w.WriteIndent(0, "\n"); err != nil {
			return err
		}
		return err
	}
	if r, err = g.w.WriteIndent(0, "\n\n"); err != nil {
		return err
	}

	// Keep a track of symbol ranges for the LSP.
	tgtSymbolRange.To = r.To
	g.sourceMap.AddSymbolRange(n.Expression.Range, tgtSymbolRange)

	return err
}

func (g *generator) writeTemplBuffer(indentLevel int) (err error) {
	// templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
	if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)\n"); err != nil {
		return err
	}
	// if !templ_7745c5c3_IsBuffer {
	//	defer func() {
	//		templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
	//		if templ_7745c5c3_Err == nil {
	//			templ_7745c5c3_Err = templ_7745c5c3_BufErr
	//		}
	//	}()
	// }
	if _, err = g.w.WriteIndent(indentLevel, "if !templ_7745c5c3_IsBuffer {\n"); err != nil {
		return err
	}
	{
		indentLevel++
		if _, err = g.w.WriteIndent(indentLevel, "defer func() {\n"); err != nil {
			return err
		}
		{
			indentLevel++
			if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)\n"); err != nil {
				return err
			}
			if _, err = g.w.WriteIndent(indentLevel, "if templ_7745c5c3_Err == nil {\n"); err != nil {
				return err
			}
			{
				indentLevel++
				if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_Err = templ_7745c5c3_BufErr\n"); err != nil {
					return err
				}
				indentLevel--
			}
			if _, err = g.w.WriteIndent(indentLevel, "}\n"); err != nil {
				return err
			}
			indentLevel--
		}
		if _, err = g.w.WriteIndent(indentLevel, "}()\n"); err != nil {
			return err
		}
		indentLevel--
	}
	if _, err = g.w.WriteIndent(indentLevel, "}\n"); err != nil {
		return err
	}
	return
}

func (g *generator) writeTemplate(nodeIdx int, t *parser.HTMLTemplate) error {
	if t == nil {
		return errors.New("template is nil")
	}
	var r parser.Range
	var tgtSymbolRange parser.Range
	var err error
	var indentLevel int

	// func
	if r, err = g.w.Write("func "); err != nil {
		return err
	}
	tgtSymbolRange.From = r.From
	// (r *Receiver) Name(params []string)
	if r, err = g.w.Write(t.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(t.Expression, r)
	// templ.Component {
	if _, err = g.w.Write(" templ.Component {\n"); err != nil {
		return err
	}
	indentLevel++
	// return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
	if _, err = g.w.WriteIndent(indentLevel, "return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {\n"); err != nil {
		return err
	}
	{
		indentLevel++
		if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context\n"); err != nil {
			return err
		}
		if _, err = g.w.WriteIndent(indentLevel, "if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {\n"); err != nil {
			return err
		}
		{
			indentLevel++
			if _, err = g.w.WriteIndent(indentLevel, "return templ_7745c5c3_CtxErr"); err != nil {
				return err
			}
			indentLevel--
		}
		if _, err = g.w.WriteIndent(indentLevel, "}\n"); err != nil {
			return err
		}
		if err := g.writeTemplBuffer(indentLevel); err != nil {
			return err
		}
		// ctx = templ.InitializeContext(ctx)
		if _, err = g.w.WriteIndent(indentLevel, "ctx = templ.InitializeContext(ctx)\n"); err != nil {
			return err
		}
		g.childrenVar = g.createVariableName()
		// templ_7745c5c3_Var1 := templ.GetChildren(ctx)
		// if templ_7745c5c3_Var1 == nil {
		//  	templ_7745c5c3_Var1 = templ.NopComponent
		// }
		if _, err = g.w.WriteIndent(indentLevel, fmt.Sprintf("%s := templ.GetChildren(ctx)\n", g.childrenVar)); err != nil {
			return err
		}
		if _, err = g.w.WriteIndent(indentLevel, fmt.Sprintf("if %s == nil {\n", g.childrenVar)); err != nil {
			return err
		}
		{
			indentLevel++
			if _, err = g.w.WriteIndent(indentLevel, fmt.Sprintf("%s = templ.NopComponent\n", g.childrenVar)); err != nil {
				return err
			}
			indentLevel--
		}
		if _, err = g.w.WriteIndent(indentLevel, "}\n"); err != nil {
			return err
		}
		// ctx = templ.ClearChildren(children)
		if _, err = g.w.WriteIndent(indentLevel, "ctx = templ.ClearChildren(ctx)\n"); err != nil {
			return err
		}
		// Nodes.
		if err = g.writeNodes(indentLevel, stripWhitespace(t.Children), nil); err != nil {
			return err
		}
		// return nil
		if _, err = g.w.WriteIndent(indentLevel, "return nil\n"); err != nil {
			return err
		}
		indentLevel--
	}
	// })
	if _, err = g.w.WriteIndent(indentLevel, "})\n"); err != nil {
		return err
	}
	indentLevel--
	// }

	// Note: gofmt wants to remove a single empty line at the end of a file
	// so we have to make sure we don't output one if this is the last node.
	closingBrace := "}\n\n"
	if nodeIdx+1 >= len(g.tf.Nodes) {
		closingBrace = "}\n"
	}

	if r, err = g.w.WriteIndent(indentLevel, closingBrace); err != nil {
		return err
	}

	// Keep a track of symbol ranges for the LSP.
	tgtSymbolRange.To = r.To
	g.sourceMap.AddSymbolRange(t.Range, tgtSymbolRange)

	return nil
}

func stripWhitespace(input []parser.Node) (output []parser.Node) {
	for i, n := range input {
		if _, isWhiteSpace := n.(*parser.Whitespace); !isWhiteSpace {
			output = append(output, input[i])
		}
	}
	return output
}

func stripLeadingWhitespace(nodes []parser.Node) []parser.Node {
	for i, n := range nodes {
		if _, isWhiteSpace := n.(*parser.Whitespace); !isWhiteSpace {
			return nodes[i:]
		}
	}
	return []parser.Node{}
}

func stripTrailingWhitespace(nodes []parser.Node) []parser.Node {
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		if _, isWhiteSpace := n.(*parser.Whitespace); !isWhiteSpace {
			return nodes[0 : i+1]
		}
	}
	return []parser.Node{}
}

func stripLeadingAndTrailingWhitespace(nodes []parser.Node) []parser.Node {
	return stripTrailingWhitespace(stripLeadingWhitespace(nodes))
}

func (g *generator) writeNodes(indentLevel int, nodes []parser.Node, next parser.Node) error {
	for i, curr := range nodes {
		var nextNode parser.Node
		if i+1 < len(nodes) {
			nextNode = nodes[i+1]
		}
		if nextNode == nil {
			nextNode = next
		}
		if err := g.writeNode(indentLevel, curr, nextNode); err != nil {
			return err
		}
	}
	return nil
}

func (g *generator) writeNode(indentLevel int, current parser.Node, next parser.Node) (err error) {
	switch n := current.(type) {
	case *parser.DocType:
		err = g.writeDocType(indentLevel, n)
	case *parser.Element:
		err = g.writeElement(indentLevel, n)
	case *parser.HTMLComment:
		err = g.writeComment(indentLevel, n)
	case *parser.ChildrenExpression:
		err = g.writeChildrenExpression(indentLevel)
	case *parser.RawElement:
		err = g.writeRawElement(indentLevel, n)
	case *parser.ScriptElement:
		err = g.writeScriptElement(indentLevel, n)
	case *parser.ForExpression:
		err = g.writeForExpression(indentLevel, n, next)
	case *parser.CallTemplateExpression:
		err = g.writeCallTemplateExpression(indentLevel, n)
	case *parser.TemplElementExpression:
		err = g.writeTemplElementExpression(indentLevel, n)
	case *parser.IfExpression:
		err = g.writeIfExpression(indentLevel, n, next)
	case *parser.SwitchExpression:
		err = g.writeSwitchExpression(indentLevel, n, next)
	case *parser.StringExpression:
		err = g.writeStringExpression(indentLevel, n.Expression)
	case *parser.GoCode:
		err = g.writeGoCode(indentLevel, n.Expression)
	case *parser.Whitespace:
		err = g.writeWhitespace(indentLevel, n)
	case *parser.Text:
		err = g.writeText(indentLevel, n)
	case *parser.GoComment:
		// Do not render Go comments in the output HTML.
		return
	default:
		return fmt.Errorf("unhandled type: %v", reflect.TypeOf(n))
	}
	// Write trailing whitespace, if there is a next node that might need the space.
	// If the next node is inline or text, we might need it.
	// If the current node is a block element, we don't need it.
	needed := (isInlineOrText(current) && isInlineOrText(next))
	if ws, ok := current.(parser.WhitespaceTrailer); ok && needed {
		if err := g.writeWhitespaceTrailer(indentLevel, ws.Trailing()); err != nil {
			return err
		}
	}
	return
}

func isInlineOrText(next parser.Node) bool {
	// While these are formatted as blocks when they're written in the HTML template.
	// They're inline - i.e. there's no whitespace rendered around them at runtime for minification.
	if next == nil {
		return false
	}
	switch n := next.(type) {
	case *parser.IfExpression:
		return true
	case *parser.SwitchExpression:
		return true
	case *parser.ForExpression:
		return true
	case *parser.Element:
		return !n.IsBlockElement()
	case *parser.Text:
		return true
	case *parser.StringExpression:
		return true
	}
	return false
}

func (g *generator) writeWhitespaceTrailer(indentLevel int, n parser.TrailingSpace) (err error) {
	if n == parser.SpaceNone {
		return nil
	}
	// Normalize whitespace for minified output. In HTML, a single space is equivalent to
	// any number of spaces, tabs, or newlines.
	if n == parser.SpaceVertical {
		n = parser.SpaceHorizontal
	}
	if _, err = g.w.WriteStringLiteral(indentLevel, string(n)); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeDocType(indentLevel int, n *parser.DocType) (err error) {
	if _, err = g.w.WriteStringLiteral(indentLevel, fmt.Sprintf("<!doctype %s>", escapeQuotes(n.Value))); err != nil {
		return err
	}
	return nil
}

func escapeQuotes(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}

func (g *generator) writeIfExpression(indentLevel int, n *parser.IfExpression, nextNode parser.Node) (err error) {
	var r parser.Range
	// if
	if _, err = g.w.WriteIndent(indentLevel, `if `); err != nil {
		return err
	}
	// x == y {
	if r, err = g.w.Write(n.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(n.Expression, r)
	// {
	if _, err = g.w.Write(` {` + "\n"); err != nil {
		return err
	}
	{
		indentLevel++
		if err = g.writeNodes(indentLevel, stripLeadingAndTrailingWhitespace(n.Then), nextNode); err != nil {
			return err
		}
		indentLevel--
	}
	for _, elseIf := range n.ElseIfs {
		// } else if {
		if _, err = g.w.WriteIndent(indentLevel, `} else if `); err != nil {
			return err
		}
		// x == y {
		if r, err = g.w.Write(elseIf.Expression.Value); err != nil {
			return err
		}
		g.sourceMap.Add(elseIf.Expression, r)
		// {
		if _, err = g.w.Write(` {` + "\n"); err != nil {
			return err
		}
		{
			indentLevel++
			if err = g.writeNodes(indentLevel, stripLeadingAndTrailingWhitespace(elseIf.Then), nextNode); err != nil {
				return err
			}
			indentLevel--
		}
	}
	if len(n.Else) > 0 {
		// } else {
		if _, err = g.w.WriteIndent(indentLevel, `} else {`+"\n"); err != nil {
			return err
		}
		{
			indentLevel++
			if err = g.writeNodes(indentLevel, stripLeadingAndTrailingWhitespace(n.Else), nextNode); err != nil {
				return err
			}
			indentLevel--
		}
	}
	// }
	if _, err = g.w.WriteIndent(indentLevel, `}`+"\n"); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeSwitchExpression(indentLevel int, n *parser.SwitchExpression, next parser.Node) (err error) {
	var r parser.Range
	// switch
	if _, err = g.w.WriteIndent(indentLevel, `switch `); err != nil {
		return err
	}
	// val
	if r, err = g.w.Write(n.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(n.Expression, r)
	// {
	if _, err = g.w.Write(` {` + "\n"); err != nil {
		return err
	}

	if len(n.Cases) > 0 {
		for _, c := range n.Cases {
			// case x:
			// default:
			if r, err = g.w.WriteIndent(indentLevel, c.Expression.Value); err != nil {
				return err
			}
			g.sourceMap.Add(c.Expression, r)
			indentLevel++
			if err = g.writeNodes(indentLevel, stripLeadingAndTrailingWhitespace(c.Children), next); err != nil {
				return err
			}
			indentLevel--
		}
	}
	// }
	if _, err = g.w.WriteIndent(indentLevel, `}`+"\n"); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeChildrenExpression(indentLevel int) (err error) {
	if _, err = g.w.WriteIndent(indentLevel, fmt.Sprintf("templ_7745c5c3_Err = %s.Render(ctx, templ_7745c5c3_Buffer)\n", g.childrenVar)); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeTemplElementExpression(indentLevel int, n *parser.TemplElementExpression) (err error) {
	if len(n.Children) == 0 {
		return g.writeSelfClosingTemplElementExpression(indentLevel, n)
	}
	return g.writeBlockTemplElementExpression(indentLevel, n)
}

func (g *generator) writeBlockTemplElementExpression(indentLevel int, n *parser.TemplElementExpression) (err error) {
	var r parser.Range
	childrenName := g.createVariableName()
	if _, err = g.w.WriteIndent(indentLevel, childrenName+" := templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {\n"); err != nil {
		return err
	}
	indentLevel++
	if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context\n"); err != nil {
		return err
	}
	if err := g.writeTemplBuffer(indentLevel); err != nil {
		return err
	}
	// ctx = templ.InitializeContext(ctx)
	if _, err = g.w.WriteIndent(indentLevel, "ctx = templ.InitializeContext(ctx)\n"); err != nil {
		return err
	}
	if err = g.writeNodes(indentLevel, stripLeadingAndTrailingWhitespace(n.Children), nil); err != nil {
		return err
	}
	// return nil
	if _, err = g.w.WriteIndent(indentLevel, "return nil\n"); err != nil {
		return err
	}
	indentLevel--
	if _, err = g.w.WriteIndent(indentLevel, "})\n"); err != nil {
		return err
	}
	if _, err = g.w.WriteIndent(indentLevel, `templ_7745c5c3_Err = `); err != nil {
		return err
	}
	if r, err = g.w.Write(n.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(n.Expression, r)
	// .Render(templ.WithChildren(ctx, children), templ_7745c5c3_Buffer)
	if _, err = g.w.Write(".Render(templ.WithChildren(ctx, " + childrenName + "), templ_7745c5c3_Buffer)\n"); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeSelfClosingTemplElementExpression(indentLevel int, n *parser.TemplElementExpression) (err error) {
	if _, err = g.w.WriteIndent(indentLevel, `templ_7745c5c3_Err = `); err != nil {
		return err
	}
	// Template expression.
	var r parser.Range
	if r, err = g.w.Write(n.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(n.Expression, r)
	// .Render(ctx, templ_7745c5c3_Buffer)
	if _, err = g.w.Write(".Render(ctx, templ_7745c5c3_Buffer)\n"); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeCallTemplateExpression(indentLevel int, n *parser.CallTemplateExpression) (err error) {
	if _, err = g.w.WriteIndent(indentLevel, `templ_7745c5c3_Err = `); err != nil {
		return err
	}
	// Template expression.
	var r parser.Range
	if r, err = g.w.Write(n.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(n.Expression, r)
	// .Render(ctx, templ_7745c5c3_Buffer)
	if _, err = g.w.Write(".Render(ctx, templ_7745c5c3_Buffer)\n"); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeForExpression(indentLevel int, n *parser.ForExpression, next parser.Node) (err error) {
	var r parser.Range
	// for
	if _, err = g.w.WriteIndent(indentLevel, `for `); err != nil {
		return err
	}
	// i, v := range p.Stuff
	if r, err = g.w.Write(n.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(n.Expression, r)
	// {
	if _, err = g.w.Write(` {` + "\n"); err != nil {
		return err
	}
	// Children.
	indentLevel++
	if err = g.writeNodes(indentLevel, stripLeadingAndTrailingWhitespace(n.Children), next); err != nil {
		return err
	}
	indentLevel--
	// }
	if _, err = g.w.WriteIndent(indentLevel, `}`+"\n"); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeErrorHandler(indentLevel int) (err error) {
	_, err = g.w.WriteIndent(indentLevel, "if templ_7745c5c3_Err != nil {\n")
	if err != nil {
		return err
	}
	indentLevel++
	_, err = g.w.WriteIndent(indentLevel, "return templ_7745c5c3_Err\n")
	if err != nil {
		return err
	}
	indentLevel--
	_, err = g.w.WriteIndent(indentLevel, "}\n")
	if err != nil {
		return err
	}
	return err
}

func (g *generator) writeExpressionErrorHandler(indentLevel int, expression parser.Expression) (err error) {
	_, err = g.w.WriteIndent(indentLevel, "if templ_7745c5c3_Err != nil {\n")
	if err != nil {
		return err
	}
	indentLevel++
	line := int(expression.Range.To.Line + 1)
	col := int(expression.Range.To.Col)
	_, err = g.w.WriteIndent(indentLevel, "return	templ.Error{Err: templ_7745c5c3_Err, FileName: "+createGoString(g.options.FileName)+", Line: "+strconv.Itoa(line)+", Col: "+strconv.Itoa(col)+"}\n")
	if err != nil {
		return err
	}
	indentLevel--
	_, err = g.w.WriteIndent(indentLevel, "}\n")
	if err != nil {
		return err
	}
	return err
}

func (g *generator) writeElement(indentLevel int, n *parser.Element) (err error) {
	if len(n.Attributes) == 0 {
		// <div>
		if _, err = g.w.WriteStringLiteral(indentLevel, fmt.Sprintf(`<%s>`, html.EscapeString(n.Name))); err != nil {
			return err
		}
	} else {
		attrs := parser.CopyAttributes(n.Attributes)
		// <style type="text/css"></style>
		if err = g.writeElementCSS(indentLevel, attrs); err != nil {
			return err
		}
		// <script></script>
		if err = g.writeElementScript(indentLevel, attrs); err != nil {
			return err
		}
		// <div
		if _, err = g.w.WriteStringLiteral(indentLevel, fmt.Sprintf(`<%s`, html.EscapeString(n.Name))); err != nil {
			return err
		}
		if err = g.writeElementAttributes(indentLevel, n.Name, attrs); err != nil {
			return err
		}
		// >
		if _, err = g.w.WriteStringLiteral(indentLevel, `>`); err != nil {
			return err
		}
	}
	// Skip children and close tag for void elements.
	if n.IsVoidElement() && len(n.Children) == 0 {
		return nil
	}
	// Children.
	if err = g.writeNodes(indentLevel, stripWhitespace(n.Children), nil); err != nil {
		return err
	}
	// </div>
	if _, err = g.w.WriteStringLiteral(indentLevel, fmt.Sprintf(`</%s>`, html.EscapeString(n.Name))); err != nil {
		return err
	}
	return err
}

func (g *generator) writeAttributeCSS(indentLevel int, attr *parser.ExpressionAttribute) (result *parser.ExpressionAttribute, ok bool, err error) {
	var r parser.Range
	name := html.EscapeString(attr.Key.String())
	if name != "class" {
		ok = false
		return
	}
	// Create a class name for the style.
	// The expression can either be expecting a templ.Classes call, or an expression that returns
	// var templ_7745c5c3_CSSClasses = []any{
	classesName := g.createVariableName()
	if _, err = g.w.WriteIndent(indentLevel, "var "+classesName+" = []any{"); err != nil {
		return
	}
	// p.Name()
	if r, err = g.w.Write(attr.Expression.Value); err != nil {
		return
	}
	g.sourceMap.Add(attr.Expression, r)
	// }\n
	if _, err = g.w.Write("}\n"); err != nil {
		return
	}
	// Render the CSS before the element if required.
	// templ_7745c5c3_Err = templ.RenderCSSItems(ctx, templ_7745c5c3_Buffer, templ_7745c5c3_CSSClasses...)
	if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_Err = templ.RenderCSSItems(ctx, templ_7745c5c3_Buffer, "+classesName+"...)\n"); err != nil {
		return
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return
	}
	// Rewrite the ExpressionAttribute to point at the new variable.
	newAttr := &parser.ExpressionAttribute{
		Key: attr.Key,
		Expression: parser.Expression{
			Value: "templ.CSSClasses(" + classesName + ").String()",
		},
	}
	return newAttr, true, nil
}

func (g *generator) writeAttributesCSS(indentLevel int, attrs []parser.Attribute) (err error) {
	for i, attr := range attrs {
		if attr, ok := attr.(*parser.ExpressionAttribute); ok {
			attr, ok, err = g.writeAttributeCSS(indentLevel, attr)
			if err != nil {
				return err
			}
			if ok {
				attrs[i] = attr
			}
		}
		if cattr, ok := attr.(*parser.ConditionalAttribute); ok {
			err = g.writeAttributesCSS(indentLevel, cattr.Then)
			if err != nil {
				return err
			}
			err = g.writeAttributesCSS(indentLevel, cattr.Else)
			if err != nil {
				return err
			}
			attrs[i] = cattr
		}
	}
	return nil
}

func (g *generator) writeElementCSS(indentLevel int, attrs []parser.Attribute) (err error) {
	return g.writeAttributesCSS(indentLevel, attrs)
}

func isScriptAttribute(name string) bool {
	for _, prefix := range []string{"on", "hx-on:"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (g *generator) writeElementScript(indentLevel int, attrs []parser.Attribute) (err error) {
	var scriptExpressions []string
	for _, attr := range attrs {
		scriptExpressions = append(scriptExpressions, getAttributeScripts(attr)...)
	}
	if len(scriptExpressions) == 0 {
		return
	}
	// Render the scripts before the element if required.
	// templ_7745c5c3_Err = templ.RenderScriptItems(ctx, templ_7745c5c3_Buffer, a, b, c)
	if _, err = g.w.WriteIndent(indentLevel, "templ_7745c5c3_Err = templ.RenderScriptItems(ctx, templ_7745c5c3_Buffer, "+strings.Join(scriptExpressions, ", ")+")\n"); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return err
}

func getAttributeScripts(attr parser.Attribute) (scripts []string) {
	if attr, ok := attr.(*parser.ConditionalAttribute); ok {
		for _, attr := range attr.Then {
			scripts = append(scripts, getAttributeScripts(attr)...)
		}
		for _, attr := range attr.Else {
			scripts = append(scripts, getAttributeScripts(attr)...)
		}
	}
	if attr, ok := attr.(*parser.ExpressionAttribute); ok {
		name := html.EscapeString(attr.Key.String())
		if isScriptAttribute(name) {
			scripts = append(scripts, attr.Expression.Value)
		}
	}
	return scripts
}

func (g *generator) writeAttributeKey(indentLevel int, attr parser.AttributeKey) (err error) {
	if attr, ok := attr.(parser.ConstantAttributeKey); ok {
		name := html.EscapeString(attr.Name)
		if _, err = g.w.WriteStringLiteral(indentLevel, fmt.Sprintf(` %s`, name)); err != nil {
			return err
		}
		return nil
	}
	if attr, ok := attr.(parser.ExpressionAttributeKey); ok {
		var r parser.Range
		vn := g.createVariableName()
		// var vn string
		if _, err = g.w.WriteIndent(indentLevel, "var "+vn+" string\n"); err != nil {
			return err
		}
		// vn, templ_7745c5c3_Err = templ.JoinStringErrs(
		if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err = templ.JoinStringErrs("); err != nil {
			return err
		}
		// p.Name()
		if r, err = g.w.Write(attr.Expression.Value); err != nil {
			return err
		}
		g.sourceMap.Add(attr.Expression, r)
		// )
		if _, err = g.w.Write(")\n"); err != nil {
			return err
		}
		// Attribute expression error handler.
		err = g.writeExpressionErrorHandler(indentLevel, attr.Expression)
		if err != nil {
			return err
		}

		// _, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(vn)
		if _, err = g.w.WriteIndent(indentLevel, "_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(` `+"+vn+"))\n"); err != nil {
			return err
		}
		return g.writeErrorHandler(indentLevel)
	}
	return fmt.Errorf("unknown attribute key type %T", attr)
}

func (g *generator) writeBoolConstantAttribute(indentLevel int, attr *parser.BoolConstantAttribute) (err error) {
	return g.writeAttributeKey(indentLevel, attr.Key)
}

func (g *generator) writeConstantAttribute(indentLevel int, attr *parser.ConstantAttribute) (err error) {
	if err = g.writeAttributeKey(indentLevel, attr.Key); err != nil {
		return err
	}
	quote := `"`
	if attr.SingleQuote {
		quote = "'"
	}
	value := escapeQuotes("=" + quote + attr.Value + quote)
	if _, err = g.w.WriteStringLiteral(indentLevel, value); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeBoolExpressionAttribute(indentLevel int, attr *parser.BoolExpressionAttribute) (err error) {
	// if
	if _, err = g.w.WriteIndent(indentLevel, `if `); err != nil {
		return err
	}
	// x == y
	var r parser.Range
	if r, err = g.w.Write(attr.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(attr.Expression, r)
	// {
	if _, err = g.w.Write(` {` + "\n"); err != nil {
		return err
	}
	{
		indentLevel++
		if err = g.writeAttributeKey(indentLevel, attr.Key); err != nil {
			return err
		}
		indentLevel--
	}
	// }
	if _, err = g.w.WriteIndent(indentLevel, `}`+"\n"); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeExpressionAttributeValueURL(indentLevel int, attr *parser.ExpressionAttribute) (err error) {
	vn := g.createVariableName()
	// var vn templ.SafeURL
	if _, err = g.w.WriteIndent(indentLevel, "var "+vn+" templ.SafeURL\n"); err != nil {
		return err
	}
	// vn, templ_7745c5c3_Err = templ.JoinURLErrs(
	if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err = templ.JoinURLErrs("); err != nil {
		return err
	}
	// p.Name()
	var r parser.Range
	if r, err = g.w.Write(attr.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(attr.Expression, r)
	// )
	if _, err = g.w.Write(")\n"); err != nil {
		return err
	}
	// Attribute expression error handler.
	err = g.writeExpressionErrorHandler(indentLevel, attr.Expression)
	if err != nil {
		return err
	}
	// _, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(vn)
	if _, err = g.w.WriteIndent(indentLevel, "_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString("+vn+"))\n"); err != nil {
		return err
	}
	return g.writeErrorHandler(indentLevel)
}

func (g *generator) writeExpressionAttributeValueScript(indentLevel int, attr *parser.ExpressionAttribute) (err error) {
	// It's a JavaScript handler, and requires special handling, because we expect a JavaScript expression.
	vn := g.createVariableName()
	// var vn templ.ComponentScript =
	if _, err = g.w.WriteIndent(indentLevel, "var "+vn+" templ.ComponentScript = "); err != nil {
		return err
	}
	// p.Name()
	var r parser.Range
	if r, err = g.w.Write(attr.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(attr.Expression, r)
	if _, err = g.w.Write("\n"); err != nil {
		return err
	}
	if _, err = g.w.WriteIndent(indentLevel, "_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("+vn+".Call)\n"); err != nil {
		return err
	}
	return g.writeErrorHandler(indentLevel)
}

func (g *generator) writeExpressionAttributeValueDefault(indentLevel int, attr *parser.ExpressionAttribute) (err error) {
	var r parser.Range
	vn := g.createVariableName()
	// var vn string
	if _, err = g.w.WriteIndent(indentLevel, "var "+vn+" string\n"); err != nil {
		return err
	}
	// vn, templ_7745c5c3_Err = templ.JoinStringErrs(
	if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err = templ.JoinStringErrs("); err != nil {
		return err
	}
	// p.Name()
	if r, err = g.w.Write(attr.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(attr.Expression, r)
	// )
	if _, err = g.w.Write(")\n"); err != nil {
		return err
	}
	// Attribute expression error handler.
	err = g.writeExpressionErrorHandler(indentLevel, attr.Expression)
	if err != nil {
		return err
	}

	// _, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(vn)
	if _, err = g.w.WriteIndent(indentLevel, "_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString("+vn+"))\n"); err != nil {
		return err
	}
	return g.writeErrorHandler(indentLevel)
}

func (g *generator) writeExpressionAttributeValueStyle(indentLevel int, attr *parser.ExpressionAttribute) (err error) {
	var r parser.Range
	vn := g.createVariableName()
	// var vn string
	if _, err = g.w.WriteIndent(indentLevel, "var "+vn+" string\n"); err != nil {
		return err
	}
	// vn, templ_7745c5c3_Err = templruntime.SanitizeStyleAttributeValues(
	if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err = templruntime.SanitizeStyleAttributeValues("); err != nil {
		return err
	}
	// value
	if r, err = g.w.Write(attr.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(attr.Expression, r)
	// )
	if _, err = g.w.Write(")\n"); err != nil {
		return err
	}
	// Attribute expression error handler.
	err = g.writeExpressionErrorHandler(indentLevel, attr.Expression)
	if err != nil {
		return err
	}

	// _, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(vn))
	if _, err = g.w.WriteIndent(indentLevel, "_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString("+vn+"))\n"); err != nil {
		return err
	}
	return g.writeErrorHandler(indentLevel)
}

func (g *generator) writeExpressionAttribute(indentLevel int, elementName string, attr *parser.ExpressionAttribute) (err error) {
	if err = g.writeAttributeKey(indentLevel, attr.Key); err != nil {
		return err
	}
	// ="
	if _, err = g.w.WriteStringLiteral(indentLevel, `=\"`); err != nil {
		return err
	}
	attrKey := html.EscapeString(attr.Key.String())
	// Value.
	if isExpressionAttributeValueURL(elementName, attrKey) {
		if err := g.writeExpressionAttributeValueURL(indentLevel, attr); err != nil {
			return err
		}
	} else if isScriptAttribute(attrKey) {
		if err := g.writeExpressionAttributeValueScript(indentLevel, attr); err != nil {
			return err
		}
	} else if attrKey == "style" {
		if err := g.writeExpressionAttributeValueStyle(indentLevel, attr); err != nil {
			return err
		}
	} else {
		if err := g.writeExpressionAttributeValueDefault(indentLevel, attr); err != nil {
			return err
		}
	}
	// Close quote.
	if _, err = g.w.WriteStringLiteral(indentLevel, `\"`); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeSpreadAttributes(indentLevel int, attr *parser.SpreadAttributes) (err error) {
	// templ.RenderAttributes(ctx, w, spreadAttrs)
	if _, err = g.w.WriteIndent(indentLevel, `templ_7745c5c3_Err = templ.RenderAttributes(ctx, templ_7745c5c3_Buffer, `); err != nil {
		return err
	}
	// spreadAttrs
	var r parser.Range
	if r, err = g.w.Write(attr.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(attr.Expression, r)
	// )
	if _, err = g.w.Write(")\n"); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeConditionalAttribute(indentLevel int, elementName string, attr *parser.ConditionalAttribute) (err error) {
	// if
	if _, err = g.w.WriteIndent(indentLevel, `if `); err != nil {
		return err
	}
	// x == y
	var r parser.Range
	if r, err = g.w.Write(attr.Expression.Value); err != nil {
		return err
	}
	g.sourceMap.Add(attr.Expression, r)
	// {
	if _, err = g.w.Write(` {` + "\n"); err != nil {
		return err
	}
	{
		indentLevel++
		if err = g.writeElementAttributes(indentLevel, elementName, attr.Then); err != nil {
			return err
		}
		indentLevel--
	}
	if len(attr.Else) > 0 {
		// } else {
		if _, err = g.w.WriteIndent(indentLevel, `} else {`+"\n"); err != nil {
			return err
		}
		{
			indentLevel++
			if err = g.writeElementAttributes(indentLevel, elementName, attr.Else); err != nil {
				return err
			}
			indentLevel--
		}
	}
	// }
	if _, err = g.w.WriteIndent(indentLevel, `}`+"\n"); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeElementAttributes(indentLevel int, name string, attrs []parser.Attribute) (err error) {
	for _, attr := range attrs {
		switch attr := attr.(type) {
		case *parser.BoolConstantAttribute:
			err = g.writeBoolConstantAttribute(indentLevel, attr)
		case *parser.ConstantAttribute:
			err = g.writeConstantAttribute(indentLevel, attr)
		case *parser.BoolExpressionAttribute:
			err = g.writeBoolExpressionAttribute(indentLevel, attr)
		case *parser.ExpressionAttribute:
			err = g.writeExpressionAttribute(indentLevel, name, attr)
		case *parser.SpreadAttributes:
			err = g.writeSpreadAttributes(indentLevel, attr)
		case *parser.ConditionalAttribute:
			err = g.writeConditionalAttribute(indentLevel, name, attr)
		default:
			err = fmt.Errorf("unknown attribute type %T", attr)
		}
	}
	return
}

func (g *generator) writeRawElement(indentLevel int, n *parser.RawElement) (err error) {
	if len(n.Attributes) == 0 {
		// <div>
		if _, err = g.w.WriteStringLiteral(indentLevel, fmt.Sprintf(`<%s>`, html.EscapeString(n.Name))); err != nil {
			return err
		}
	} else {
		// <script></script>
		if err = g.writeElementScript(indentLevel, n.Attributes); err != nil {
			return err
		}
		// <div
		if _, err = g.w.WriteStringLiteral(indentLevel, fmt.Sprintf(`<%s`, html.EscapeString(n.Name))); err != nil {
			return err
		}
		if err = g.writeElementAttributes(indentLevel, n.Name, n.Attributes); err != nil {
			return err
		}
		// >
		if _, err = g.w.WriteStringLiteral(indentLevel, `>`); err != nil {
			return err
		}
	}
	// Contents.
	if err = g.writeText(indentLevel, &parser.Text{Value: n.Contents}); err != nil {
		return err
	}
	// </div>
	if _, err = g.w.WriteStringLiteral(indentLevel, fmt.Sprintf(`</%s>`, html.EscapeString(n.Name))); err != nil {
		return err
	}
	return err
}

func (g *generator) writeScriptElement(indentLevel int, n *parser.ScriptElement) (err error) {
	if len(n.Attributes) == 0 {
		// <div>
		if _, err = g.w.WriteStringLiteral(indentLevel, `<script>`); err != nil {
			return err
		}
	} else {
		// <script></script>
		if err = g.writeElementScript(indentLevel, n.Attributes); err != nil {
			return err
		}
		// <div
		if _, err = g.w.WriteStringLiteral(indentLevel, "<script"); err != nil {
			return err
		}
		if err = g.writeElementAttributes(indentLevel, "script", n.Attributes); err != nil {
			return err
		}
		// >
		if _, err = g.w.WriteStringLiteral(indentLevel, `>`); err != nil {
			return err
		}
	}
	// Contents.
	for _, c := range n.Contents {
		if err = g.writeScriptContents(indentLevel, c); err != nil {
			return err
		}
	}
	// </div>
	if _, err = g.w.WriteStringLiteral(indentLevel, "</script>"); err != nil {
		return err
	}
	return err
}

func (g *generator) writeScriptContents(indentLevel int, c parser.ScriptContents) (err error) {
	if c.Value != nil {
		if *c.Value == "" {
			return nil
		}
		// This is a JS expression and can be written directly to the output.
		return g.writeText(indentLevel, &parser.Text{Value: *c.Value})
	}
	if c.GoCode != nil {
		// This is a Go code block. The code needs to be evaluated, and the result written to the output.
		// The variable is JSON encoded to ensure that it is safe to use within a script tag.
		var r parser.Range
		vn := g.createVariableName()
		// Here, we need to get the result, which might be any type. We can use templ.ScriptContent to get the result.
		// vn, templ_7745c5c3_Err := templruntime.ScriptContent(
		fnCall := "templruntime.ScriptContentOutsideStringLiteral"
		if c.InsideStringLiteral {
			fnCall = "templruntime.ScriptContentInsideStringLiteral"
		}
		if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err := "+fnCall+"("); err != nil {
			return err
		}
		// p.Name()
		if r, err = g.w.Write(c.GoCode.Expression.Value); err != nil {
			return err
		}
		g.sourceMap.Add(c.GoCode.Expression, r)
		// )
		if _, err = g.w.Write(")\n"); err != nil {
			return err
		}

		// Expression error handler.
		err = g.writeExpressionErrorHandler(indentLevel, c.GoCode.Expression)
		if err != nil {
			return err
		}

		// _, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(jvn)
		if _, err = g.w.WriteIndent(indentLevel, "_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString("+vn+")\n"); err != nil {
			return err
		}
		if err = g.writeErrorHandler(indentLevel); err != nil {
			return err
		}

		// Write any trailing space.
		if c.GoCode.TrailingSpace != "" {
			if err = g.writeText(indentLevel, &parser.Text{Value: string(c.GoCode.TrailingSpace)}); err != nil {
				return err
			}
		}

		return nil
	}
	return errors.New("unknown script content")
}

func (g *generator) writeComment(indentLevel int, c *parser.HTMLComment) (err error) {
	// <!--
	if _, err = g.w.WriteStringLiteral(indentLevel, "<!--"); err != nil {
		return err
	}
	// Contents.
	if err = g.writeText(indentLevel, &parser.Text{Value: c.Contents}); err != nil {
		return err
	}
	// -->
	if _, err = g.w.WriteStringLiteral(indentLevel, "-->"); err != nil {
		return err
	}
	return err
}

func (g *generator) createVariableName() string {
	g.variableID++
	return "templ_7745c5c3_Var" + strconv.Itoa(g.variableID)
}

func (g *generator) writeGoCode(indentLevel int, e parser.Expression) (err error) {
	if strings.TrimSpace(e.Value) == "" {
		return
	}
	var r parser.Range
	if r, err = g.w.WriteIndent(indentLevel, e.Value+"\n"); err != nil {
		return err
	}
	g.sourceMap.Add(e, r)
	return nil
}

func (g *generator) writeStringExpression(indentLevel int, e parser.Expression) (err error) {
	if strings.TrimSpace(e.Value) == "" {
		return
	}
	var r parser.Range
	vn := g.createVariableName()
	// var vn string
	if _, err = g.w.WriteIndent(indentLevel, "var "+vn+" string\n"); err != nil {
		return err
	}
	// vn, templ_7745c5c3_Err = templ.JoinStringErrs(
	if _, err = g.w.WriteIndent(indentLevel, vn+", templ_7745c5c3_Err = templ.JoinStringErrs("); err != nil {
		return err
	}
	// p.Name()
	if r, err = g.w.Write(e.Value); err != nil {
		return err
	}
	g.sourceMap.Add(e, r)
	// )
	if _, err = g.w.Write(")\n"); err != nil {
		return err
	}

	// String expression error handler.
	err = g.writeExpressionErrorHandler(indentLevel, e)
	if err != nil {
		return err
	}

	// _, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(vn)
	if _, err = g.w.WriteIndent(indentLevel, "_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString("+vn+"))\n"); err != nil {
		return err
	}
	if err = g.writeErrorHandler(indentLevel); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeWhitespace(indentLevel int, n *parser.Whitespace) (err error) {
	if len(n.Value) == 0 {
		return
	}
	// _, err = templ_7745c5c3_Buffer.WriteString(` `)
	if _, err = g.w.WriteStringLiteral(indentLevel, " "); err != nil {
		return err
	}
	return nil
}

func (g *generator) writeText(indentLevel int, n *parser.Text) (err error) {
	_, err = g.w.WriteStringLiteral(indentLevel, escapeQuotes(n.Value))
	return err
}

func createGoString(s string) string {
	var sb strings.Builder
	sb.WriteRune('`')
	sects := strings.Split(s, "`")
	for i, sect := range sects {
		sb.WriteString(sect)
		if len(sects) > i+1 {
			sb.WriteString("` + \"`\" + `")
		}
	}
	sb.WriteRune('`')
	return sb.String()
}

func (g *generator) writeScript(t *parser.ScriptTemplate) error {
	if t == nil {
		return errors.New("script template is nil")
	}
	var r parser.Range
	var tgtSymbolRange parser.Range
	var err error
	var indentLevel int

	// func
	if r, err = g.w.Write("func "); err != nil {
		return err
	}
	tgtSymbolRange.From = r.From
	if r, err = g.w.Write(t.Name.Value); err != nil {
		return err
	}
	g.sourceMap.Add(t.Name, r)
	// (
	if _, err = g.w.Write("("); err != nil {
		return err
	}
	// Write parameters.
	if r, err = g.w.Write(t.Parameters.Value); err != nil {
		return err
	}
	g.sourceMap.Add(t.Parameters, r)
	// ) templ.ComponentScript {
	if _, err = g.w.Write(") templ.ComponentScript {\n"); err != nil {
		return err
	}
	indentLevel++
	// return templ.ComponentScript{
	if _, err = g.w.WriteIndent(indentLevel, "return templ.ComponentScript{\n"); err != nil {
		return err
	}
	{
		indentLevel++
		fn := functionName(t.Name.Value, t.Value)
		goFn := createGoString(fn)
		// Name: "scriptName",
		if _, err = g.w.WriteIndent(indentLevel, "Name: "+goFn+",\n"); err != nil {
			return err
		}
		// Function: `function scriptName(a, b, c){` + `constantScriptValue` + `}`,
		prefix := "function " + fn + "(" + stripTypes(t.Parameters.Value) + "){"
		body := strings.TrimLeftFunc(t.Value, unicode.IsSpace)
		suffix := "}"
		if _, err = g.w.WriteIndent(indentLevel, "Function: "+createGoString(prefix+body+suffix)+",\n"); err != nil {
			return err
		}
		// Call: templ.SafeScript(scriptName, a, b, c)
		if _, err = g.w.WriteIndent(indentLevel, "Call: templ.SafeScript("+goFn+", "+stripTypes(t.Parameters.Value)+"),\n"); err != nil {
			return err
		}
		// CallInline: templ.SafeScriptInline(scriptName, a, b, c)
		if _, err = g.w.WriteIndent(indentLevel, "CallInline: templ.SafeScriptInline("+goFn+", "+stripTypes(t.Parameters.Value)+"),\n"); err != nil {
			return err
		}
		indentLevel--
	}
	// }
	if _, err = g.w.WriteIndent(indentLevel, "}\n"); err != nil {
		return err
	}
	indentLevel--
	// }
	if r, err = g.w.WriteIndent(indentLevel, "}\n\n"); err != nil {
		return err
	}

	// Keep track of the symbol range for the LSP.
	tgtSymbolRange.To = r.To
	g.sourceMap.AddSymbolRange(t.Range, tgtSymbolRange)

	return nil
}

// writeBlankAssignmentForRuntimeImport writes out a blank identifier assignment.
// This ensures that even if the github.com/a-h/templ/runtime package is not used in the generated code,
// the Go compiler will not complain about the unused import.
func (g *generator) writeBlankAssignmentForRuntimeImport() error {
	var err error
	if _, err = g.w.Write("var _ = templruntime.GeneratedTemplate"); err != nil {
		return err
	}
	return nil
}

func functionName(name string, body string) string {
	h := sha256.New()
	h.Write([]byte(body))
	hp := hex.EncodeToString(h.Sum(nil))[0:4]
	return "__templ_" + name + "_" + hp
}

func stripTypes(parameters string) string {
	variableNames := []string{}
	params := strings.Split(parameters, ",")
	for _, param := range params {
		p := strings.Split(strings.TrimSpace(param), " ")
		variableNames = append(variableNames, strings.TrimSpace(p[0]))
	}
	return strings.Join(variableNames, ", ")
}

func isExpressionAttributeValueURL(elementName, attrName string) bool {
	switch elementName {
	case "a", "link":
		return attrName == "href"
	case "form":
		return attrName == "action"
	case "object":
		return attrName == "data"
	}
	return false
}
