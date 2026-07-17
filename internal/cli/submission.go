package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/matteodante/miniform/internal/forms"
)

type submissionView struct {
	ID            uint               `json:"id"`
	FormID        uint               `json:"form_id"`
	FormName      string             `json:"form_name,omitempty"`
	Data          any                `json:"data"`
	UserAgent     string             `json:"user_agent,omitempty"`
	IsSpam        bool               `json:"is_spam"`
	WebhookEvents []webhookEventView `json:"webhook_events,omitempty"`
	EmailEvents   []emailEventView   `json:"email_events,omitempty"`
	Files         []fileView         `json:"files,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

type submissionPageView struct {
	Items      []submissionView `json:"items"`
	Page       int              `json:"page"`
	PerPage    int              `json:"per_page"`
	TotalCount int64            `json:"total_count"`
	TotalPages int              `json:"total_pages"`
}

type fileView struct {
	ID           uint      `json:"id"`
	SubmissionID uint      `json:"submission_id"`
	FieldName    string    `json:"field_name"`
	Filename     string    `json:"filename"`
	ContentType  string    `json:"content_type,omitempty"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
}

func (r *Runner) runSubmission(args []string) (any, error) {
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	action, actionArgs, err := requireAction("submission", args)
	if err != nil {
		return nil, err
	}
	switch action {
	case "create":
		return r.submissionCreate(actionArgs)
	case "list":
		return r.submissionList(actionArgs)
	case "get":
		return r.submissionGet(actionArgs)
	case "delete":
		return r.submissionDelete(actionArgs)
	case "file-list":
		return r.submissionFileList(actionArgs)
	case "file-copy":
		return r.submissionFileCopy(actionArgs)
	default:
		return nil, usageError("unknown submission action: " + action)
	}
}

func (r *Runner) submissionCreate(args []string) (any, error) {
	set := newFlagSet("submission.create")
	formID := set.Uint("form-id", 0, "target form id")
	slug := set.String("slug", "", "target form slug")
	dataFile := set.String("data-file", "", "path containing a JSON object, or - for stdin")
	userAgent := set.String("user-agent", "miniform-cli", "recorded user agent")
	var fileSpecs stringSliceValue
	set.Var(&fileSpecs, "file", "upload in FIELD=PATH format; repeatable")
	if err := r.parseFlags(set, "submission.create", args); err != nil {
		return nil, err
	}
	if (*formID == 0) == (strings.TrimSpace(*slug) == "") {
		return nil, usageError("set exactly one of --form-id or --slug")
	}
	if err := requireString(*dataFile, "data-file"); err != nil {
		return nil, err
	}

	var form *forms.Form
	var err error
	if *formID > 0 {
		form, err = forms.GetByID(r.DB, *formID)
	} else {
		form, err = forms.GetBySlug(r.DB, strings.TrimSpace(*slug))
	}
	if err != nil {
		return nil, err
	}
	payloadText, err := readFileValue(*dataFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	payload := make(map[string]any)
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		return nil, validationError("data file must contain one JSON object")
	}
	if r.Config != nil && r.Config.MaxInputFields > 0 && len(payload) > r.Config.MaxInputFields {
		return nil, validationError("submission payload exceeds max input fields")
	}

	uploads, err := openCLIUploads(fileSpecs)
	if err != nil {
		return nil, err
	}
	defer forms.CloseFiles(uploads)
	dataDir := ""
	if r.Config != nil {
		dataDir = r.Config.DataDirectory
	}
	submission, err := forms.CreateSubmissionWithFiles(r.Logger, r.DB, form, payload, *userAgent, dataDir, uploads)
	if err != nil {
		return nil, err
	}
	loaded, err := forms.GetSubmissionByID(r.DB, submission.ID)
	if err != nil {
		return nil, err
	}
	return newSubmissionView(loaded), nil
}

func (r *Runner) submissionList(args []string) (any, error) {
	set := newFlagSet("submission.list")
	formID := set.Uint("form-id", 0, "filter by form id")
	rangeValue := set.String("range", "all", "all, 7d, 30d, or 90d")
	query := set.String("query", "", "search within JSON payload")
	spam := set.String("spam", "", "filter spam status: true or false")
	page := set.Int("page", 1, "page number")
	perPage := set.Int("per-page", 20, "items per page, maximum 200")
	if err := r.parseFlags(set, "submission.list", args); err != nil {
		return nil, err
	}
	spamFilter, err := parseOptionalBool(*spam)
	if err != nil {
		return nil, err
	}

	result, err := forms.ListSubmissions(r.DB, forms.SubmissionFilter{
		FormID:  *formID,
		Range:   *rangeValue,
		Query:   *query,
		Page:    *page,
		PerPage: *perPage,
		IsSpam:  spamFilter,
	})
	if err != nil {
		return nil, err
	}
	items := make([]submissionView, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, newSubmissionView(&result.Items[i]))
	}
	return submissionPageView{
		Items:      items,
		Page:       result.Page,
		PerPage:    result.PerPage,
		TotalCount: result.TotalCount,
		TotalPages: result.TotalPages,
	}, nil
}

func (r *Runner) submissionGet(args []string) (any, error) {
	set := newFlagSet("submission.get")
	id := set.Uint("id", 0, "submission id")
	if err := r.parseFlags(set, "submission.get", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	submission, err := forms.GetSubmissionByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	return newSubmissionView(submission), nil
}

func (r *Runner) submissionDelete(args []string) (any, error) {
	set := newFlagSet("submission.delete")
	id := set.Uint("id", 0, "submission id")
	yes := set.Bool("yes", false, "confirm destructive operation")
	if err := r.parseFlags(set, "submission.delete", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if !*yes {
		return nil, usageError("submission delete requires --yes")
	}
	dataDir := ""
	if r.Config != nil {
		dataDir = r.Config.DataDirectory
	}
	if err := forms.DeleteSubmission(r.Logger, r.DB, dataDir, *id); err != nil {
		return nil, err
	}
	return map[string]any{"id": *id, "deleted": true}, nil
}

func (r *Runner) submissionFileList(args []string) (any, error) {
	set := newFlagSet("submission.file-list")
	id := set.Uint("id", 0, "submission id")
	if err := r.parseFlags(set, "submission.file-list", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	submission, err := forms.GetSubmissionByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	files := make([]fileView, 0, len(submission.Files))
	for _, file := range submission.Files {
		files = append(files, newFileView(file))
	}
	return files, nil
}

func (r *Runner) submissionFileCopy(args []string) (any, error) {
	set := newFlagSet("submission.file-copy")
	id := set.Uint("id", 0, "submission id")
	fileID := set.Uint("file-id", 0, "submission file id")
	output := set.String("output", "", "destination path, or - for stdout")
	force := set.Bool("force", false, "overwrite destination file")
	if err := r.parseFlags(set, "submission.file-copy", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if err := requireUint(*fileID, "file-id"); err != nil {
		return nil, err
	}
	if err := requireString(*output, "output"); err != nil {
		return nil, err
	}
	if *output == "-" && r.JSON {
		return nil, usageError("--output - cannot be combined with --json")
	}
	if r.Config == nil {
		return nil, internalError("resolve upload path", fmt.Errorf("configuration dependency is unavailable"))
	}

	file, err := forms.GetSubmissionFile(r.DB, *id, *fileID)
	if err != nil {
		return nil, err
	}
	source, err := forms.OpenSubmissionFile(r.Config.DataDirectory, file)
	if err != nil {
		return nil, internalError("open submission file", err)
	}
	defer func() { _ = source.Close() }()

	if *output == "-" {
		if _, err := io.Copy(r.Stdout, source); err != nil {
			return nil, internalError("stream submission file", err)
		}
		return nil, nil
	}
	if err := copySubmissionFile(source, *output, *force); err != nil {
		return nil, err
	}
	absoluteOutput, _ := filepath.Abs(*output)
	return map[string]any{
		"submission_id": *id,
		"file_id":       *fileID,
		"output":        absoluteOutput,
		"size":          file.Size,
	}, nil
}

func copySubmissionFile(source io.Reader, destination string, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	// #nosec G304 -- The destination is the operator's explicit --output path.
	target, err := os.OpenFile(destination, flags, 0o600)
	if os.IsExist(err) {
		return conflictError("destination already exists; pass --force to overwrite it")
	}
	if err != nil {
		return internalError("create destination file", err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return internalError("copy submission file", copyErr)
	}
	if closeErr != nil {
		return internalError("close destination file", closeErr)
	}
	return nil
}

func openCLIUploads(specs []string) ([]*forms.UploadedFile, error) {
	if len(specs) > forms.MaxTotalFiles {
		return nil, validationError("too many files")
	}
	fieldCounts := make(map[string]int)
	uploads := make([]*forms.UploadedFile, 0, len(specs))
	for _, spec := range specs {
		field, path, ok := strings.Cut(spec, "=")
		field = strings.TrimSpace(field)
		path = strings.TrimSpace(path)
		if !ok || field == "" || path == "" {
			forms.CloseFiles(uploads)
			return nil, validationError("file must use FIELD=PATH format")
		}
		fieldCounts[field]++
		if fieldCounts[field] > forms.MaxFilesPerField {
			forms.CloseFiles(uploads)
			return nil, validationError("too many files for field " + field)
		}

		// #nosec G304 -- Each upload path is explicitly supplied as FIELD=PATH.
		file, err := os.Open(path)
		if err != nil {
			forms.CloseFiles(uploads)
			return nil, validationError("open upload file: " + err.Error())
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			forms.CloseFiles(uploads)
			return nil, validationError("inspect upload file: " + err.Error())
		}
		filename := filepath.Base(path)
		header := &multipart.FileHeader{Filename: filename, Size: info.Size()}
		if err := forms.ValidateFile(header); err != nil {
			_ = file.Close()
			forms.CloseFiles(uploads)
			return nil, validationError(err.Error())
		}
		uploads = append(uploads, &forms.UploadedFile{
			FieldName:   field,
			Filename:    filename,
			ContentType: mime.TypeByExtension(filepath.Ext(filename)),
			Size:        info.Size(),
			Data:        file,
		})
	}
	return uploads, nil
}

func newSubmissionView(submission *forms.Submission) submissionView {
	formName := ""
	if submission.Form != nil {
		formName = submission.Form.Name
	}
	view := submissionView{
		ID:        submission.ID,
		FormID:    submission.FormID,
		FormName:  formName,
		Data:      decodeSubmissionData(submission.DataJSON),
		UserAgent: submission.UserAgent,
		IsSpam:    submission.IsSpam,
		CreatedAt: submission.CreatedAt,
		UpdatedAt: submission.UpdatedAt,
	}
	for i := range submission.WebhookEvents {
		event := newWebhookEventView(&submission.WebhookEvents[i])
		event.FormID = submission.FormID
		event.FormName = formName
		view.WebhookEvents = append(view.WebhookEvents, event)
	}
	for i := range submission.EmailEvents {
		event := newEmailEventView(&submission.EmailEvents[i])
		event.FormID = submission.FormID
		event.FormName = formName
		view.EmailEvents = append(view.EmailEvents, event)
	}
	for _, file := range submission.Files {
		view.Files = append(view.Files, newFileView(file))
	}
	return view
}

func decodeSubmissionData(raw string) any {
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		return value
	}
	return strings.TrimSpace(raw)
}

func newFileView(file *forms.SubmissionFile) fileView {
	return fileView{
		ID:           file.ID,
		SubmissionID: file.SubmissionID,
		FieldName:    file.FieldName,
		Filename:     file.Filename,
		ContentType:  file.ContentType,
		Size:         file.Size,
		CreatedAt:    file.CreatedAt,
	}
}
