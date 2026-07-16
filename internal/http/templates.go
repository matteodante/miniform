package http

import "github.com/matteodante/miniform/internal/forms"

// FormTemplate remains an alias so existing HTTP rendering code keeps its vocabulary.
type FormTemplate = forms.FormTemplate

// GetFormTemplates returns the domain-owned template catalog.
func GetFormTemplates() []FormTemplate {
	return forms.GetFormTemplates()
}

// GetTemplateByID returns one domain-owned form template.
func GetTemplateByID(id string) *FormTemplate {
	return forms.GetTemplateByID(id)
}
