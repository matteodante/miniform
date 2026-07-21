package forms

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"slices"
	"strings"
	texttemplate "text/template"
	"time"
)

type RenderedEmail struct {
	Subject  string
	Format   string
	TextBody string
	HTMLBody string
}

type emailTemplateData struct {
	FormName    string
	SubmittedAt string
	Fields      map[string]string
	FieldList   []emailField
}

type emailField struct {
	Name  string
	Value string
}

func RenderEmail(delivery *EmailDelivery, submission *Submission) (*RenderedEmail, error) {
	if delivery == nil {
		return nil, fmt.Errorf("render email: notification is required")
	}
	if submission == nil || submission.Form == nil {
		return nil, fmt.Errorf("render email: submission and form are required")
	}

	format, err := NormalizeEmailFormat(delivery.Format)
	if err != nil {
		return nil, fmt.Errorf("render email: format is invalid")
	}
	fields, fieldList := emailTemplateFields(submission.DataJSON)
	data := emailTemplateData{
		FormName: submission.Form.Name, SubmittedAt: submission.CreatedAt.UTC().Format(time.RFC3339),
		Fields: fields, FieldList: fieldList,
	}

	subject, err := executeEmailTextTemplate("email subject", EffectiveEmailSubject(delivery), data)
	if err != nil {
		return nil, err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" || strings.ContainsAny(subject, "\r\n\x00") {
		return nil, fmt.Errorf("render email subject: result must be one non-empty line")
	}

	textBody, err := executeEmailTextTemplate("email text", EffectiveEmailText(delivery), data)
	if err != nil {
		return nil, err
	}
	rendered := &RenderedEmail{Subject: subject, Format: format, TextBody: textBody}
	if format != EmailFormatHTML {
		return rendered, nil
	}

	htmlTemplate, err := htmltemplate.New("email html").Option("missingkey=error").Parse(EffectiveEmailHTML(delivery))
	if err != nil {
		return nil, fmt.Errorf("parse email HTML template: %w", err)
	}
	var htmlBody bytes.Buffer
	if err := htmlTemplate.Execute(&htmlBody, data); err != nil {
		return nil, fmt.Errorf("render email HTML template: %w", err)
	}
	rendered.HTMLBody = htmlBody.String()
	return rendered, nil
}

func EmailTemplateFields(rawJSON string) map[string]string {
	fields, _ := emailTemplateFields(rawJSON)
	return fields
}

func ResolveEmailRecipients(delivery *EmailDelivery, fields map[string]string) ([]string, error) {
	if delivery == nil {
		return nil, fmt.Errorf("email notification missing")
	}
	if delivery.RecipientSource == EmailRecipientField {
		address, err := normalizeEmailAddress(fields[delivery.Recipient])
		if err != nil {
			return nil, fmt.Errorf("email recipient field %q missing or invalid", delivery.Recipient)
		}
		return []string{address}, nil
	}

	addresses, err := ParseEmailRecipients(delivery.Recipient)
	if err != nil {
		return nil, fmt.Errorf("email recipients missing or invalid")
	}
	recipients := make([]string, len(addresses))
	for i := range addresses {
		recipients[i] = FormatEmailRecipient(addresses[i])
	}
	return recipients, nil
}

func ResolveEmailReplyTo(delivery *EmailDelivery, fields map[string]string) (string, error) {
	if delivery == nil {
		return "", fmt.Errorf("email notification missing")
	}
	switch delivery.ReplyToSource {
	case "", EmailReplyToNone:
		return "", nil
	case EmailReplyToStatic:
		address, err := normalizeEmailAddress(delivery.ReplyTo)
		if err != nil {
			return "", fmt.Errorf("email Reply-To missing or invalid")
		}
		return address, nil
	case EmailReplyToField:
		address, err := normalizeEmailAddress(fields[delivery.ReplyTo])
		if err != nil {
			return "", fmt.Errorf("email Reply-To field %q missing or invalid", delivery.ReplyTo)
		}
		return address, nil
	default:
		return "", fmt.Errorf("email Reply-To source invalid")
	}
}

func executeEmailTextTemplate(name, source string, data emailTemplateData) (string, error) {
	template, err := texttemplate.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse %s template: %w", name, err)
	}
	var output bytes.Buffer
	if err := template.Execute(&output, data); err != nil {
		return "", fmt.Errorf("render %s template: %w", name, err)
	}
	return output.String(), nil
}

func emailTemplateFields(rawJSON string) (map[string]string, []emailField) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &fields); err != nil {
		fields = map[string]any{"raw": rawJSON}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	values := make(map[string]string, len(keys))
	fieldList := make([]emailField, 0, len(keys))
	for _, key := range keys {
		value := emailTemplateFieldValue(fields[key])
		values[key] = value
		fieldList = append(fieldList, emailField{Name: key, Value: value})
	}
	return values, fieldList
}

func emailTemplateFieldValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}
