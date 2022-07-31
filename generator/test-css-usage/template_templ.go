// Code generated by templ@(devel) DO NOT EDIT.

package testcssusage

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import "context"
import "io"
import "bytes"
import "strings"

func green() templ.CSSClass {
	var templCSSBuilder strings.Builder
	templCSSBuilder.WriteString(`color:#00ff00;`)
	templCSSID := templ.CSSID(`green`, templCSSBuilder.String())
	return templ.ComponentCSSClass{
		ID: templCSSID,
		Class: templ.SafeCSS(`.` + templCSSID + `{` + templCSSBuilder.String() + `}`),
	}
}

func className() templ.CSSClass {
	var templCSSBuilder strings.Builder
	templCSSBuilder.WriteString(`background-color:#ffffff;`)
	templCSSBuilder.WriteString(string(templ.SanitizeCSS(`color`, red)))
	templCSSID := templ.CSSID(`className`, templCSSBuilder.String())
	return templ.ComponentCSSClass{
		ID: templCSSID,
		Class: templ.SafeCSS(`.` + templCSSID + `{` + templCSSBuilder.String() + `}`),
	}
}

func Button(text string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) {
		templBuffer, templIsBuffer := w.(*bytes.Buffer)
		if !templIsBuffer {
			templBuffer = new(bytes.Buffer)
		}
		ctx = templ.InitializeRenderedItemsContext(ctx)
		var_1 := ctx
		ctx = templ.ClearChildren(var_1)
		// Element (standard)
		// Element CSS
		var var_2 templ.CSSClasses = templ.Classes(className(), templ.Class("&&&unsafe"), templ.SafeClass("safe"))
		err = templ.RenderCSSItems(ctx, templBuffer, var_2...)
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString("<button")
		if err != nil {
			return err
		}
		// Element Attributes
		_, err = templBuffer.WriteString(" class=")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString("\"")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString(templ.EscapeString(var_2.String()))
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString("\"")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString(" type=\"button\"")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString(">")
		if err != nil {
			return err
		}
		// StringExpression
		_, err = templBuffer.WriteString(templ.EscapeString(text))
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString("</button>")
		if err != nil {
			return err
		}
		if !templIsBuffer {
			_, err = io.Copy(w, templBuffer)
		}
		return err
	})
}

func ThreeButtons() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) {
		templBuffer, templIsBuffer := w.(*bytes.Buffer)
		if !templIsBuffer {
			templBuffer = new(bytes.Buffer)
		}
		ctx = templ.InitializeRenderedItemsContext(ctx)
		var_3 := ctx
		ctx = templ.ClearChildren(var_3)
		// CallTemplate
		err = Button("A").Render(ctx, templBuffer)
		if err != nil {
			return err
		}
		// CallTemplate
		err = Button("B").Render(ctx, templBuffer)
		if err != nil {
			return err
		}
		// Element (standard)
		// Element CSS
		var var_4 templ.CSSClasses = templ.Classes(green())
		err = templ.RenderCSSItems(ctx, templBuffer, var_4...)
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString("<button")
		if err != nil {
			return err
		}
		// Element Attributes
		_, err = templBuffer.WriteString(" class=")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString("\"")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString(templ.EscapeString(var_4.String()))
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString("\"")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString(" type=\"button\"")
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString(">")
		if err != nil {
			return err
		}
		// StringExpression
		_, err = templBuffer.WriteString(templ.EscapeString("Green"))
		if err != nil {
			return err
		}
		_, err = templBuffer.WriteString("</button>")
		if err != nil {
			return err
		}
		if !templIsBuffer {
			_, err = io.Copy(w, templBuffer)
		}
		return err
	})
}

