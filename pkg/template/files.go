package template

import (
	"embed"
	"html/template"

	"github.com/Masterminds/sprig/v3"
)

//go:embed *
var files embed.FS

type Template string

const (
	TemplateSuccessPage Template = "success.gohtml"
)

func GetTemplate(t Template) *template.Template {
	return template.Must(template.New(string(t)).
		Funcs(sprig.HtmlFuncMap()).
		ParseFS(files, string(t)))
}
