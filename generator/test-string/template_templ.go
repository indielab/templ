// Code generated by templ@(devel) DO NOT EDIT.

package teststring

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import "context"
import "io"
import "bytes"

func render(s string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) (err error) {
		templBuffer, templIsBuffer := w.(*bytes.Buffer)
		if !templIsBuffer {
			templBuffer = new(bytes.Buffer)
		}
		ctx = templ.InitializeRenderedItemsContext(ctx)
		var_1 := ctx
		ctx = templ.ClearChildren(var_1)
		// StringExpression
		_, err = templBuffer.WriteString(templ.EscapeString(s))
		if err != nil {
			return err
		}
		if !templIsBuffer {
			_, err = io.Copy(w, templBuffer)
		}
		return err
	})
}

