package forms

import (
	"fmt"
	htmlstd "html"
	"strings"
)

const formActionPlaceholder = "{{FORM_ACTION}}"

type FormTemplate struct {
	ID          string
	Name        string
	Description string
	Slug        string
	Icon        string
	Color       string
	HTML        string
}

type templateDefinition struct {
	ID, Name, Description, Slug, Icon, Color string
	Eyebrow, Title, Introduction, Submit     string
	Fields                                   []templateField
}

type templateField struct {
	Name, Label, Kind, Placeholder, Helper string
	Options                                []string
	Required                               bool
}

func GetFormTemplates() []FormTemplate {
	templates := make([]FormTemplate, 0, len(templateCatalog))
	for _, definition := range templateCatalog {
		templates = append(templates, FormTemplate{
			ID: definition.ID, Name: definition.Name, Description: definition.Description,
			Slug: definition.Slug, Icon: definition.Icon, Color: definition.Color,
			HTML: renderTemplate(definition),
		})
	}
	return templates
}

func GetTemplateByID(id string) *FormTemplate {
	for _, template := range GetFormTemplates() {
		if template.ID == id {
			return &template
		}
	}
	return nil
}

func (template *FormTemplate) RenderHTML(action string) string {
	if template == nil || strings.TrimSpace(template.HTML) == "" {
		return ""
	}
	if strings.TrimSpace(action) == "" {
		action = "/forms/your-form/submit?token=YOUR_FORM_TOKEN"
	}
	return strings.ReplaceAll(template.HTML, formActionPlaceholder, htmlstd.EscapeString(action))
}

func renderTemplate(definition templateDefinition) string {
	var output strings.Builder
	fmt.Fprintf(&output, `<section class="mf-template" data-template="%s">
  <header class="mf-template__header">
    <p class="mf-template__eyebrow">%s</p>
    <h2>%s</h2>
    <p>%s</p>
  </header>
  <form class="mf-template__form" action="%s" method="POST">
`, escape(definition.ID), escape(definition.Eyebrow), escape(definition.Title), escape(definition.Introduction), formActionPlaceholder)
	for _, field := range definition.Fields {
		renderField(&output, field)
	}
	fmt.Fprintf(&output, "    <button type=\"submit\">%s</button>\n  </form>\n</section>\n%s", escape(definition.Submit), templateStyles)
	return output.String()
}

func renderField(output *strings.Builder, field templateField) {
	required := ""
	if field.Required {
		required = " required"
	}
	if field.Kind == "checkbox" {
		fmt.Fprintf(output, "    <label class=\"mf-template__check\"><input type=\"checkbox\" name=\"%s\"%s> <span>%s</span></label>\n", escape(field.Name), required, escape(field.Label))
		return
	}

	fmt.Fprintf(output, "    <label class=\"mf-template__field\"><span>%s</span>\n      ", escape(field.Label))
	switch field.Kind {
	case "textarea":
		fmt.Fprintf(output, "<textarea name=\"%s\" placeholder=\"%s\"%s></textarea>", escape(field.Name), escape(field.Placeholder), required)
	case "select":
		fmt.Fprintf(output, "<select name=\"%s\"%s>", escape(field.Name), required)
		for _, option := range field.Options {
			if option == "" {
				output.WriteString(`<option value="">Select</option>`)
				continue
			}
			fmt.Fprintf(output, "<option>%s</option>", escape(option))
		}
		output.WriteString("</select>")
	default:
		fmt.Fprintf(output, "<input type=\"%s\" name=\"%s\" placeholder=\"%s\"%s>", escape(field.Kind), escape(field.Name), escape(field.Placeholder), required)
	}
	if field.Helper != "" {
		fmt.Fprintf(output, "\n      <small>%s</small>", escape(field.Helper))
	}
	output.WriteString("\n    </label>\n")
}

func escape(value string) string {
	return htmlstd.EscapeString(value)
}

var templateCatalog = []templateDefinition{
	{
		ID: "contact", Name: "Contact Form", Description: "Simple contact form with name, email, and message fields",
		Slug: "contact", Icon: "💬", Color: "blue", Eyebrow: "Contact", Title: "Contact our team",
		Introduction: "Tell us how we can help and someone will reply within one business day.", Submit: "Send message",
		Fields: []templateField{
			{Name: "name", Label: "Full name", Kind: "text", Placeholder: "Alex Rivers", Required: true},
			{Name: "company", Label: "Company", Kind: "text", Placeholder: "Acme Inc."},
			{Name: "email", Label: "Email address", Kind: "email", Placeholder: "you@example.com", Required: true},
			{Name: "topic", Label: "Topic", Kind: "select", Options: []string{"", "Product question", "Billing", "Partnership", "Something else"}, Required: true},
			{Name: "message", Label: "How can we help?", Kind: "textarea", Placeholder: "Share details about your request", Required: true},
			{Name: "consent", Label: "I agree to be contacted about this request.", Kind: "checkbox", Required: true},
		},
	},
	{
		ID: "feedback", Name: "Feedback Form", Description: "Collect user feedback and feature requests",
		Slug: "feedback", Icon: "💡", Color: "purple", Eyebrow: "Feedback", Title: "Share your feedback",
		Introduction: "Help us build the roadmap. Tell us what’s working and what could be better.", Submit: "Send feedback",
		Fields: []templateField{
			{Name: "name", Label: "Name", Kind: "text", Placeholder: "Taylor", Required: true},
			{Name: "email", Label: "Email", Kind: "email", Placeholder: "you@example.com"},
			{Name: "satisfaction", Label: "How satisfied are you?", Kind: "select", Options: []string{"", "Very satisfied", "Satisfied", "Neutral", "Unsatisfied"}, Required: true},
			{Name: "feature", Label: "Feature or area", Kind: "text", Placeholder: "Exports, notifications, ..."},
			{Name: "comments", Label: "Comments", Kind: "textarea", Placeholder: "What should we improve?", Required: true},
		},
	},
	{
		ID: "bug-report", Name: "Bug Report", Description: "Help users report bugs and technical issues",
		Slug: "bug-report", Icon: "🐛", Color: "red", Eyebrow: "Bug report", Title: "Report an issue",
		Introduction: "Found something off? Share the details and we’ll investigate within a few hours.", Submit: "Submit bug",
		Fields: []templateField{
			{Name: "reporter", Label: "Name", Kind: "text", Placeholder: "Jordan", Required: true},
			{Name: "email", Label: "Email", Kind: "email", Placeholder: "you@example.com", Required: true},
			{Name: "severity", Label: "Severity", Kind: "select", Options: []string{"", "Low", "Medium", "High", "Critical"}, Required: true},
			{Name: "area", Label: "Area of the product", Kind: "text", Placeholder: "Account settings"},
			{Name: "steps", Label: "Steps to reproduce", Kind: "textarea", Placeholder: "1. Go to..., 2. Click...", Helper: "Include as much detail as possible.", Required: true},
			{Name: "expected", Label: "Expected vs. actual behavior", Kind: "textarea", Placeholder: "Expected X but saw Y"},
		},
	},
	{
		ID: "newsletter", Name: "Newsletter Signup", Description: "Collect email addresses for your newsletter",
		Slug: "newsletter", Icon: "📧", Color: "green", Eyebrow: "Newsletter", Title: "Join the newsletter",
		Introduction: "Receive product updates, launch notes, and best practices twice a month.", Submit: "Subscribe",
		Fields: []templateField{
			{Name: "email", Label: "Email address", Kind: "email", Placeholder: "you@company.com", Required: true},
			{Name: "first_name", Label: "First name", Kind: "text", Placeholder: "Jamie", Required: true},
			{Name: "interest", Label: "What would you like to hear about?", Kind: "select", Options: []string{"Product updates", "Growth stories", "Weekly tips"}},
			{Name: "consent", Label: "I agree to receive occasional product emails.", Kind: "checkbox", Required: true},
		},
	},
	{
		ID: "waitlist", Name: "Waitlist", Description: "Build a waitlist for your product launch",
		Slug: "waitlist", Icon: "⏳", Color: "yellow", Eyebrow: "Waitlist", Title: "Join the early access list",
		Introduction: "We’re releasing limited invites. Tell us a bit about your team and we’ll keep you posted.", Submit: "Request invite",
		Fields: []templateField{
			{Name: "name", Label: "Full name", Kind: "text", Placeholder: "Morgan Lee", Required: true},
			{Name: "company", Label: "Company", Kind: "text", Placeholder: "Northwind"},
			{Name: "email", Label: "Work email", Kind: "email", Placeholder: "you@company.com", Required: true},
			{Name: "team_size", Label: "Team size", Kind: "select", Options: []string{"", "1-5 people", "6-25 people", "26-100 people", "100+ people"}},
			{Name: "use_case", Label: "What will you use us for?", Kind: "textarea", Placeholder: "Share how your team would use the product", Required: true},
		},
	},
	{
		ID: "blank", Name: "Blank Form", Description: "Start from scratch with an empty form",
		Icon: "📝", Color: "gray", Eyebrow: "Simple form", Title: "Let’s collect data",
		Introduction: "Use this lightweight template as a starting point.", Submit: "Submit",
		Fields: []templateField{
			{Name: "field_one", Label: "Field label", Kind: "text", Placeholder: "Text input"},
			{Name: "field_two", Label: "Message", Kind: "textarea", Placeholder: "Textarea input"},
		},
	},
}

const templateStyles = `<style>
  .mf-template { box-sizing: border-box; max-width: 38rem; margin: 1.5rem auto; padding: clamp(1.25rem, 4vw, 2.5rem); border: 1px solid #d8d2c7; background: #fffdf8; color: #20231f; font-family: ui-sans-serif, system-ui, sans-serif; }
  .mf-template *, .mf-template *::before, .mf-template *::after { box-sizing: inherit; }
  .mf-template__header { margin-bottom: 1.75rem; }
  .mf-template__eyebrow { margin: 0 0 .65rem; color: #48604d; font: 700 .72rem/1.2 ui-monospace, monospace; letter-spacing: .12em; text-transform: uppercase; }
  .mf-template h2 { margin: 0; font: 700 clamp(1.65rem, 5vw, 2.25rem)/1.05 Georgia, serif; }
  .mf-template__header > p:last-child { margin: .8rem 0 0; color: #666d64; line-height: 1.55; }
  .mf-template__form { display: grid; gap: 1rem; }
  .mf-template__field { display: grid; gap: .4rem; color: #30352f; font-size: .9rem; font-weight: 650; }
  .mf-template input, .mf-template select, .mf-template textarea { width: 100%; border: 1px solid #c9c2b6; border-radius: .3rem; background: #f7f3eb; padding: .8rem .9rem; color: inherit; font: inherit; }
  .mf-template textarea { min-height: 7.5rem; resize: vertical; }
  .mf-template input:focus, .mf-template select:focus, .mf-template textarea:focus { outline: 3px solid #cad9cc; border-color: #48604d; background: #fff; }
  .mf-template small { color: #737a71; font-weight: 400; }
  .mf-template__check { display: flex; gap: .65rem; align-items: flex-start; color: #4f554e; font-size: .9rem; }
  .mf-template__check input { width: 1rem; margin-top: .15rem; }
  .mf-template button { border: 0; border-radius: .3rem; background: #334c39; padding: .9rem 1.1rem; color: #fff; font: 700 .9rem/1 ui-sans-serif, system-ui, sans-serif; cursor: pointer; }
  .mf-template button:hover { background: #25392a; }
</style>`
