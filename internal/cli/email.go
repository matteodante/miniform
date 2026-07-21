package cli

import (
	"strings"

	"github.com/matteodante/miniform/internal/forms"
)

func (r *Runner) runEmail(args []string) (any, error) {
	action, actionArgs, err := requireAction("email", args)
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		return r.emailList(actionArgs)
	case "get":
		return r.emailGet(actionArgs)
	case "create":
		return r.emailCreate(actionArgs)
	case "update":
		return r.emailUpdate(actionArgs)
	case "delete":
		return r.emailDelete(actionArgs)
	default:
		return nil, usageError("unknown email action: " + action)
	}
}

func (r *Runner) emailList(args []string) (any, error) {
	set := newFlagSet("email.list")
	formID := set.Uint("form-id", 0, "form id")
	if err := r.parseFlags(set, "email.list", args); err != nil {
		return nil, err
	}
	if err := requireUint(*formID, "form-id"); err != nil {
		return nil, err
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	deliveries, err := forms.ListEmailDeliveries(r.DB, *formID)
	if err != nil {
		return nil, err
	}
	views := make([]emailDeliveryView, 0, len(deliveries))
	for i := range deliveries {
		views = append(views, newEmailDeliveryView(&deliveries[i], false))
	}
	return views, nil
}

func (r *Runner) emailGet(args []string) (any, error) {
	set := newFlagSet("email.get")
	id := set.Uint("id", 0, "email notification id")
	if err := r.parseFlags(set, "email.get", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	delivery, err := forms.GetEmailDeliveryByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	return newEmailDeliveryView(delivery, true), nil
}

func (r *Runner) emailCreate(args []string) (any, error) {
	set := newFlagSet("email.create")
	formID := set.Uint("form-id", 0, "form id")
	name := set.String("name", forms.DefaultEmailDeliveryName, "notification name")
	enabled := set.Bool("enabled", false, "enable notification")
	mailerID := set.Uint("mailer-profile-id", 0, "mailer profile id")
	recipientSource := set.String("recipient-source", forms.EmailRecipientStatic, "static or field")
	recipient := set.String("recipient", "", "address list or submission field name")
	replyToSource := set.String("reply-to-source", forms.EmailReplyToNone, "none, static, or field")
	replyTo := set.String("reply-to", "", "address or submission field name")
	subject := set.String("subject-template", forms.DefaultEmailSubject, "Go text/template subject")
	format := set.String("format", forms.EmailFormatText, "text or html")
	textFile := set.String("text-template-file", "", "path containing text template")
	htmlFile := set.String("html-template-file", "", "path containing HTML template")
	if err := r.parseFlags(set, "email.create", args); err != nil {
		return nil, err
	}
	if err := requireUint(*formID, "form-id"); err != nil {
		return nil, err
	}
	textBody, err := readContentFile(*textFile)
	if err != nil {
		return nil, validationError(err.Error())
	}
	htmlBody, err := readContentFile(*htmlFile)
	if err != nil {
		return nil, validationError(err.Error())
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	delivery, err := forms.CreateEmailDelivery(r.Logger, r.DB, forms.EmailDeliveryParams{
		FormID: *formID, Name: *name, Enabled: *enabled, MailerProfileID: optionalUint(*mailerID),
		RecipientSource: *recipientSource, Recipient: *recipient,
		ReplyToSource: *replyToSource, ReplyTo: *replyTo,
		SubjectTemplate: *subject, Format: *format, TextTemplate: textBody, HTMLTemplate: htmlBody,
	})
	if err != nil {
		return nil, err
	}
	return newEmailDeliveryView(delivery, true), nil
}

func (r *Runner) emailUpdate(args []string) (any, error) {
	set := newFlagSet("email.update")
	id := set.Uint("id", 0, "email notification id")
	name := set.String("name", "", "notification name")
	enabled := set.Bool("enabled", false, "enable notification")
	mailerID := set.Uint("mailer-profile-id", 0, "mailer profile id")
	clearMailer := set.Bool("clear-mailer-profile", false, "remove mailer profile assignment")
	recipientSource := set.String("recipient-source", "", "static or field")
	recipient := set.String("recipient", "", "address list or submission field name")
	replyToSource := set.String("reply-to-source", "", "none, static, or field")
	replyTo := set.String("reply-to", "", "address or submission field name")
	subject := set.String("subject-template", "", "Go text/template subject")
	format := set.String("format", "", "text or html")
	textFile := set.String("text-template-file", "", "path containing text template")
	htmlFile := set.String("html-template-file", "", "path containing HTML template")
	if err := r.parseFlags(set, "email.update", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if *clearMailer && flagWasSet(set, "mailer-profile-id") {
		return nil, usageError("--clear-mailer-profile conflicts with --mailer-profile-id")
	}
	textBody, err := readContentFile(*textFile)
	if err != nil {
		return nil, validationError(err.Error())
	}
	htmlBody, err := readContentFile(*htmlFile)
	if err != nil {
		return nil, validationError(err.Error())
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	current, err := forms.GetEmailDeliveryByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	params := emailParamsFromDelivery(current)
	if flagWasSet(set, "name") {
		params.Name = *name
	}
	if flagWasSet(set, "enabled") {
		params.Enabled = *enabled
	}
	if flagWasSet(set, "mailer-profile-id") {
		params.MailerProfileID = optionalUint(*mailerID)
	}
	if *clearMailer {
		params.MailerProfileID = nil
	}
	if flagWasSet(set, "recipient-source") {
		params.RecipientSource = *recipientSource
	}
	if flagWasSet(set, "recipient") {
		params.Recipient = *recipient
	}
	if flagWasSet(set, "reply-to-source") {
		params.ReplyToSource = *replyToSource
	}
	if flagWasSet(set, "reply-to") {
		params.ReplyTo = *replyTo
	}
	if flagWasSet(set, "subject-template") {
		params.SubjectTemplate = *subject
	}
	if flagWasSet(set, "format") {
		params.Format = *format
	}
	if strings.TrimSpace(*textFile) != "" {
		params.TextTemplate = textBody
	}
	if strings.TrimSpace(*htmlFile) != "" {
		params.HTMLTemplate = htmlBody
	}
	updated, err := forms.UpdateEmailDelivery(r.Logger, r.DB, params)
	if err != nil {
		return nil, err
	}
	return newEmailDeliveryView(updated, true), nil
}

func (r *Runner) emailDelete(args []string) (any, error) {
	set := newFlagSet("email.delete")
	id := set.Uint("id", 0, "email notification id")
	yes := set.Bool("yes", false, "confirm destructive operation")
	if err := r.parseFlags(set, "email.delete", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if !*yes {
		return nil, usageError("email delete requires --yes")
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	delivery, err := forms.GetEmailDeliveryByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	if err := forms.DeleteEmailDelivery(r.Logger, r.DB, delivery.FormID, delivery.ID); err != nil {
		return nil, err
	}
	return map[string]any{"id": delivery.ID, "form_id": delivery.FormID, "deleted": true}, nil
}

func emailParamsFromDelivery(delivery *forms.EmailDelivery) forms.EmailDeliveryParams {
	return forms.EmailDeliveryParams{
		ID: delivery.ID, FormID: delivery.FormID, Name: delivery.Name, Enabled: delivery.Enabled,
		MailerProfileID: delivery.MailerProfileID,
		RecipientSource: delivery.RecipientSource, Recipient: delivery.Recipient,
		ReplyToSource: delivery.ReplyToSource, ReplyTo: delivery.ReplyTo,
		SubjectTemplate: delivery.SubjectTemplate, Format: delivery.Format,
		TextTemplate: delivery.TextTemplate, HTMLTemplate: delivery.HTMLTemplate,
	}
}
