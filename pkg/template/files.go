package template

import (
	"embed"
	"html/template"
)

//go:embed *
var files embed.FS

type Template string

const (
	TemplateSuccessPage Template = "success.gohtml"
)

func GetTemplate(t Template) *template.Template {
	return template.Must(template.New(string(t)).
		ParseFS(files, string(t)))
}
