// Code generated by templ@(devel) DO NOT EDIT.

package testrawelements

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import "context"
import "io"
import "bytes"

func StyleElement() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) {
		templBuffer, templIsBuffer := w.(*bytes.Buffer)
		if !templIsBuffer {
			templBuffer = new(bytes.Buffer)
		}
		ctx = templ.InitializeRenderedItemsContext(ctx)
		var_1 := ctx
		ctx = templ.ClearChildren(var_1)
// RawElement
		_, err = templBuffer.WriteString("<style>")
		if err != nil {
			return err
		}
// Text
var_2 := `<!-- Some stuff -->`
_, err = templBuffer.WriteString(var_2)
if err != nil {
	return err
}
		_, err = templBuffer.WriteString("</style>")
		if err != nil {
			return err
		}
		if !templIsBuffer {
			_, err = io.Copy(w, templBuffer)
		}
		return err
	})
}

// GoExpression
const StyleElementExpected = `<style><!-- Some stuff --></style>`

func ScriptElement() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) {
		templBuffer, templIsBuffer := w.(*bytes.Buffer)
		if !templIsBuffer {
			templBuffer = new(bytes.Buffer)
		}
		ctx = templ.InitializeRenderedItemsContext(ctx)
		var_3 := ctx
		ctx = templ.ClearChildren(var_3)
// RawElement
		_, err = templBuffer.WriteString("<script")
		if err != nil {
			return err
		}
		// Element Attributes
		_, err = templBuffer.WriteString(" type=\"text/javascript\"")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString(">")
		if err != nil {
			return err
		}
// Text
var_4 := `
    $("div").marquee();
    function test() {
          window.open("https://example.com")
    }
  `
_, err = templBuffer.WriteString(var_4)
if err != nil {
	return err
}
		_, err = templBuffer.WriteString("</script>")
		if err != nil {
			return err
		}
		if !templIsBuffer {
			_, err = io.Copy(w, templBuffer)
		}
		return err
	})
}

// GoExpression
const ScriptElementExpected = `<script type="text/javascript">
    $("div").marquee();
    function test() {
          window.open("https://example.com")
    }
  </script>`

