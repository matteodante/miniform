package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"github.com/valyala/fasthttp"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
)

type submissionPreview struct {
	forms.Submission
	DataJSONPreview string
}

func SubmissionList(ctx *cartridge.Context) error {
	page, _ := strconv.Atoi(ctx.Query("page", "1"))
	formID, _ := strconv.ParseUint(ctx.Query("form_id"), 10, 32)
	rangeFilter := strings.TrimSpace(ctx.Query("range"))
	search := strings.TrimSpace(ctx.Query("q"))
	result, err := forms.ListSubmissions(ctx.DB(), forms.SubmissionFilter{
		FormID: uint(formID), Range: rangeFilter, Query: search, Page: page, PerPage: 20,
	})
	if err != nil {
		var validation *forms.ValidationError
		if errors.As(err, &validation) {
			return ctx.Status(fiber.StatusBadRequest).SendString(validation.Message)
		}
		return fiber.ErrInternalServerError
	}
	summary, err := forms.GetInboxSummary(ctx.DB(), time.Now())
	if err != nil {
		return fiber.ErrInternalServerError
	}

	previews := make([]submissionPreview, len(result.Items))
	for i := range result.Items {
		previews[i] = submissionPreview{Submission: result.Items[i], DataJSONPreview: previewJSON(result.Items[i].DataJSON)}
	}
	return renderPage(ctx, "Inbox", "admin/submissions/index/content", fiber.Map{
		"Submissions": previews, "Endpoints": summary.Forms,
		"EndpointCount": summary.FormCount, "EntriesLast24h": summary.EntriesLast24,
		"Page": result.Page, "NextPage": result.Page + 1, "PrevPage": result.Page - 1,
		"TotalPages": result.TotalPages, "TotalCount": result.TotalCount,
		"HasNext": result.Page < result.TotalPages, "HasPrev": result.Page > 1,
		"FormID": ctx.Query("form_id"), "Range": rangeFilter, "Search": search,
	})
}

func AdminSubmissionShow(ctx *cartridge.Context) error {
	id, err := requestedID(ctx)
	if err != nil {
		return err
	}
	submission, err := forms.GetSubmissionByID(ctx.DB(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, "Inbox entry", "admin/submissions/show/content", fiber.Map{
		"Submission": submission, "JSON": prettyJSON(submission.DataJSON), "HasFiles": len(submission.Files) != 0,
	})
}

func AdminSubmissionFileDownload(ctx *cartridge.Context, cfg *config.Config) error {
	submissionID, err := requestedID(ctx)
	if err != nil {
		return err
	}
	fileID, err := paramID(ctx, "file_id")
	if err != nil {
		return err
	}
	file, err := forms.GetSubmissionFile(ctx.DB(), submissionID, fileID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	source, err := forms.OpenSubmissionFile(cfg.DataDirectory, file)
	if errors.Is(err, fs.ErrNotExist) {
		return fiber.ErrNotFound
	}
	if err != nil {
		ctx.Logger.Error("open submission file", slog.Uint64("file_id", uint64(file.ID)), slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	info, err := source.Stat()
	if err != nil {
		_ = source.Close()
		ctx.Logger.Error("stat submission file", slog.Uint64("file_id", uint64(file.ID)), slog.Any("error", err))
		return fiber.ErrInternalServerError
	}

	contentType := file.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(file.Filename))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	ctx.Set(fiber.HeaderContentDisposition, mime.FormatMediaType("attachment", map[string]string{"filename": file.Filename}))
	ctx.Set(fiber.HeaderContentType, contentType)
	return sendDownload(ctx, source, info)
}

type downloadRange struct {
	io.Reader
	io.Closer
}

func sendDownload(ctx *cartridge.Context, source *os.File, info os.FileInfo) error {
	size := int(info.Size())
	if int64(size) != info.Size() {
		_ = source.Close()
		ctx.Logger.Error("submission file is too large to stream", slog.Int64("size", info.Size()))
		return fiber.ErrInternalServerError
	}

	ctx.Set(fiber.HeaderAcceptRanges, "bytes")
	ctx.Set(fiber.HeaderLastModified, info.ModTime().UTC().Format(http.TimeFormat))
	if !ctx.Context().IfModifiedSince(info.ModTime()) {
		_ = source.Close()
		ctx.Context().NotModified()
		return nil
	}
	if ctx.Get(fiber.HeaderRange) == "" {
		return ctx.SendStream(source, size)
	}

	start, end, err := fasthttp.ParseByteRange([]byte(ctx.Get(fiber.HeaderRange)), size)
	if err != nil {
		_ = source.Close()
		ctx.Set(fiber.HeaderContentRange, fmt.Sprintf("bytes */%d", size))
		return ctx.SendStatus(fiber.StatusRequestedRangeNotSatisfiable)
	}
	if _, err := source.Seek(int64(start), io.SeekStart); err != nil {
		_ = source.Close()
		ctx.Logger.Error("seek submission file", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	length := end - start + 1
	ctx.Status(fiber.StatusPartialContent)
	ctx.Set(fiber.HeaderContentRange, fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	return ctx.SendStream(downloadRange{Reader: io.LimitReader(source, int64(length)), Closer: source}, length)
}

func previewJSON(raw string) string {
	text := strings.ReplaceAll(raw, "\n", " ")
	if utf8.RuneCountInString(text) <= 100 {
		return text
	}
	runes := []rune(text)
	return string(runes[:100]) + "..."
}

func prettyJSON(raw string) string {
	var output bytes.Buffer
	if json.Indent(&output, []byte(raw), "", "  ") == nil {
		return output.String()
	}
	return raw
}

func paramID(ctx *cartridge.Context, name string) (uint, error) {
	id, err := strconv.ParseUint(ctx.Params(name), 10, 32)
	if err != nil || id == 0 {
		return 0, fiber.ErrNotFound
	}
	return uint(id), nil
}
