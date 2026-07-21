package http

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

func AdminFormEmailNew(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := requestedForm(ctx, db)
	if err != nil {
		return err
	}
	return renderEmailDeliveryEditor(ctx, db, form, nil, "")
}

func AdminFormEmailCreate(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := requestedForm(ctx, db)
	if err != nil {
		return err
	}
	params := emailDeliveryParams(ctx, form.ID, 0)
	_, err = forms.CreateEmailDelivery(ctx.Logger, db, params)
	if err == nil {
		return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", form.ID))
	}
	var validation *forms.ValidationError
	if errors.As(err, &validation) {
		draft := emailDeliveryDraft(params)
		return renderEmailDeliveryEditor(ctx, db, form, draft, validation.Message)
	}
	ctx.Logger.Error("create email notification", slog.Uint64("form_id", uint64(form.ID)), slog.Any("error", err))
	return fiber.ErrInternalServerError
}

func AdminFormEmailEdit(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := requestedForm(ctx, db)
	if err != nil {
		return err
	}
	delivery, err := requestedEmailDelivery(ctx, db, form.ID)
	if err != nil {
		return err
	}
	return renderEmailDeliveryEditor(ctx, db, form, delivery, "")
}

func AdminFormEmailPreview(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := requestedForm(ctx, db)
	if err != nil {
		return err
	}
	submissionID, err := strconv.ParseUint(strings.TrimSpace(ctx.FormValue("preview_submission_id")), 10, 32)
	if err != nil || submissionID == 0 {
		return emailPreviewError(ctx, "Select a submission to preview this email")
	}
	submission, err := forms.GetSubmissionByID(db, uint(submissionID))
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && submission.FormID != form.ID) {
		return emailPreviewError(ctx, "The selected submission does not belong to this endpoint")
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}

	delivery := emailDeliveryDraft(emailDeliveryParams(ctx, form.ID, 0))
	fields := forms.EmailTemplateFields(submission.DataJSON)
	recipients, err := forms.ResolveEmailRecipients(delivery, fields)
	if err != nil {
		return emailPreviewError(ctx, err.Error())
	}
	replyTo, err := forms.ResolveEmailReplyTo(delivery, fields)
	if err != nil {
		return emailPreviewError(ctx, err.Error())
	}
	if delivery.MailerProfileID == nil {
		return emailPreviewError(ctx, "Select an email route to preview the sender")
	}
	profile, err := integrations.GetMailerProfileByID(db, *delivery.MailerProfileID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return emailPreviewError(ctx, "The selected email route is unavailable")
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	from, err := integrations.MailerSender(profile)
	if err != nil {
		return emailPreviewError(ctx, err.Error())
	}
	from = forms.DisplayEmailAddresses(from)
	for i := range recipients {
		recipients[i] = forms.DisplayEmailAddresses(recipients[i])
	}
	replyTo = forms.DisplayEmailAddresses(replyTo)
	rendered, err := forms.RenderEmail(delivery, submission)
	if err != nil {
		return emailPreviewError(ctx, err.Error())
	}

	ctx.Set(fiber.HeaderCacheControl, "no-store")
	return ctx.JSON(fiber.Map{
		"ok": true, "submission_id": submission.ID, "from": from, "to": recipients, "reply_to": replyTo,
		"subject": rendered.Subject, "format": rendered.Format,
		"text": rendered.TextBody, "html": rendered.HTMLBody,
	})
}

func AdminFormEmailUpdate(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := requestedForm(ctx, db)
	if err != nil {
		return err
	}
	deliveryID, err := requestedEmailDeliveryID(ctx)
	if err != nil {
		return err
	}
	params := emailDeliveryParams(ctx, form.ID, deliveryID)
	_, err = forms.UpdateEmailDelivery(ctx.Logger, db, params)
	if err == nil {
		return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", form.ID))
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	var validation *forms.ValidationError
	if errors.As(err, &validation) {
		draft := emailDeliveryDraft(params)
		return renderEmailDeliveryEditor(ctx, db, form, draft, validation.Message)
	}
	ctx.Logger.Error("update email notification",
		slog.Uint64("form_id", uint64(form.ID)), slog.Uint64("email_delivery_id", uint64(deliveryID)), slog.Any("error", err))
	return fiber.ErrInternalServerError
}

func AdminFormEmailDelete(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := requestedForm(ctx, db)
	if err != nil {
		return err
	}
	deliveryID, err := requestedEmailDeliveryID(ctx)
	if err != nil {
		return err
	}
	if err := forms.DeleteEmailDelivery(ctx.Logger, db, form.ID, deliveryID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		ctx.Logger.Error("delete email notification",
			slog.Uint64("form_id", uint64(form.ID)), slog.Uint64("email_delivery_id", uint64(deliveryID)), slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", form.ID))
}

func renderEmailDeliveryEditor(ctx *cartridge.Context, db *gorm.DB, form *forms.Form, delivery *forms.EmailDelivery, message string) error {
	mailers, err := integrations.ListMailerProfiles(db)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if delivery == nil {
		delivery = &forms.EmailDelivery{
			Name: forms.DefaultEmailDeliveryName, RecipientSource: forms.EmailRecipientStatic,
			ReplyToSource: forms.EmailReplyToNone, SubjectTemplate: forms.DefaultEmailSubject,
			Format: forms.EmailFormatText, TextTemplate: forms.DefaultEmailText, HTMLTemplate: forms.DefaultEmailHTML,
		}
	}
	delivery = displayEmailDelivery(delivery)
	title := "Add email notification"
	if delivery.ID != 0 {
		title = "Edit email notification"
	}
	selectedMailerID := uint(0)
	if delivery.MailerProfileID != nil {
		selectedMailerID = *delivery.MailerProfileID
	}
	submissions, err := forms.GetSubmissions(db, form.ID, 25)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, title,
		"admin/forms/email/content", fiber.Map{
			"Error": message, "Form": form, "Delivery": delivery, "IsEdit": delivery.ID != 0,
			"SelectedMailerProfileID": selectedMailerID,
			"MailerProfiles":          mailers, "SubjectTemplate": forms.EffectiveEmailSubject(delivery),
			"TextTemplate": forms.EffectiveEmailText(delivery), "HTMLTemplate": forms.EffectiveEmailHTML(delivery),
			"PreviewSubmissions": submissions,
		})
}

func emailPreviewError(ctx *cartridge.Context, message string) error {
	ctx.Set(fiber.HeaderCacheControl, "no-store")
	return ctx.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"ok": false, "error": message})
}

func displayEmailDelivery(delivery *forms.EmailDelivery) *forms.EmailDelivery {
	if delivery == nil {
		return nil
	}
	display := *delivery
	display.Recipient = forms.DisplayEmailAddresses(display.Recipient)
	display.ReplyTo = forms.DisplayEmailAddresses(display.ReplyTo)
	return &display
}

func emailDeliveryParams(ctx *cartridge.Context, formID, id uint) forms.EmailDeliveryParams {
	return forms.EmailDeliveryParams{
		ID: id, FormID: formID, Name: ctx.FormValue("name"), Enabled: checkbox(ctx, "enabled"),
		MailerProfileID: optionalID(ctx.FormValue("mailer_profile_id")),
		RecipientSource: ctx.FormValue("recipient_source"), Recipient: ctx.FormValue("recipient"),
		ReplyToSource: ctx.FormValue("reply_to_source"), ReplyTo: ctx.FormValue("reply_to"),
		SubjectTemplate: ctx.FormValue("subject_template"), Format: ctx.FormValue("format"),
		TextTemplate: ctx.FormValue("text_template"), HTMLTemplate: ctx.FormValue("html_template"),
	}
}

func emailDeliveryDraft(params forms.EmailDeliveryParams) *forms.EmailDelivery {
	return &forms.EmailDelivery{
		ID: params.ID, FormID: params.FormID, Name: params.Name, Enabled: params.Enabled,
		MailerProfileID: params.MailerProfileID,
		RecipientSource: params.RecipientSource, Recipient: params.Recipient,
		ReplyToSource: params.ReplyToSource, ReplyTo: params.ReplyTo,
		SubjectTemplate: params.SubjectTemplate, Format: params.Format,
		TextTemplate: params.TextTemplate, HTMLTemplate: params.HTMLTemplate,
	}
}

func requestedEmailDelivery(ctx *cartridge.Context, db *gorm.DB, formID uint) (*forms.EmailDelivery, error) {
	id, err := requestedEmailDeliveryID(ctx)
	if err != nil {
		return nil, err
	}
	delivery, err := forms.GetEmailDelivery(db, formID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fiber.ErrNotFound
	}
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	return delivery, nil
}

func requestedEmailDeliveryID(ctx *cartridge.Context) (uint, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(ctx.Params("delivery_id")), 10, 32)
	if err != nil || id == 0 {
		return 0, fiber.ErrNotFound
	}
	return uint(id), nil
}
