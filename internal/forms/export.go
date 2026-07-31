package forms

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

const (
	MaxCSVExportEntries      = 10_000
	MaxCSVExportFields       = 500
	MaxCSVExportContentBytes = 8 * 1024 * 1024
)

type csvExportStats struct {
	EntryCount   int64 `gorm:"column:entry_count"`
	ContentBytes int64 `gorm:"column:content_bytes"`
}

var csvMetadataColumns = []string{
	"submission_id",
	"received_at",
	"endpoint_id",
	"endpoint_name",
	"endpoint_slug",
	"is_spam",
	"user_agent",
}

// ExportSubmissionsCSV writes a bounded spreadsheet-safe export of the filtered inbox.
func ExportSubmissionsCSV(db *gorm.DB, filter SubmissionFilter, output io.Writer) (int, error) {
	now := time.Now().UTC()
	statsQuery, err := submissionFilterQuery(db, filter, now)
	if err != nil {
		return 0, err
	}

	var stats csvExportStats
	if err := statsQuery.
		Select("COUNT(*) AS entry_count, COALESCE(SUM(LENGTH(CAST(data_json AS BLOB)) + COALESCE(LENGTH(CAST(user_agent AS BLOB)), 0)), 0) AS content_bytes").
		Scan(&stats).Error; err != nil {
		return 0, fmt.Errorf("measure submissions for CSV export: %w", err)
	}
	if stats.EntryCount > MaxCSVExportEntries {
		return 0, invalid("export", fmt.Sprintf(
			"Export exceeds %d entries; narrow the inbox filters and try again",
			MaxCSVExportEntries,
		))
	}
	if stats.ContentBytes > MaxCSVExportContentBytes {
		return 0, invalid("export", fmt.Sprintf(
			"Export exceeds %d MiB of submission content; narrow the inbox filters and try again",
			MaxCSVExportContentBytes/(1024*1024),
		))
	}

	query, err := submissionFilterQuery(db, filter, now)
	if err != nil {
		return 0, err
	}
	var submissions []Submission
	if err := query.
		Preload("Form").
		Order("created_at DESC, id DESC").
		Limit(MaxCSVExportEntries + 1).
		Find(&submissions).Error; err != nil {
		return 0, fmt.Errorf("list submissions for CSV export: %w", err)
	}
	if len(submissions) > MaxCSVExportEntries {
		return 0, invalid("export", fmt.Sprintf(
			"Export exceeds %d entries; narrow the inbox filters and try again",
			MaxCSVExportEntries,
		))
	}

	payloads := make([]map[string]any, len(submissions))
	fieldSet := make(map[string]struct{})
	var contentBytes int64
	for index := range submissions {
		contentBytes += int64(len(submissions[index].DataJSON) + len(submissions[index].UserAgent))
		if contentBytes > MaxCSVExportContentBytes {
			return 0, invalid("export", fmt.Sprintf(
				"Export exceeds %d MiB of submission content; narrow the inbox filters and try again",
				MaxCSVExportContentBytes/(1024*1024),
			))
		}
		decoder := json.NewDecoder(strings.NewReader(submissions[index].DataJSON))
		decoder.UseNumber()
		if err := decoder.Decode(&payloads[index]); err != nil {
			return 0, fmt.Errorf("decode submission %d for CSV export: %w", submissions[index].ID, err)
		}
		for name := range payloads[index] {
			fieldSet[name] = struct{}{}
			if len(fieldSet) > MaxCSVExportFields {
				return 0, invalid("export", fmt.Sprintf(
					"Export exceeds %d distinct fields; narrow the inbox filters and try again",
					MaxCSVExportFields,
				))
			}
		}
	}

	fieldNames := make([]string, 0, len(fieldSet))
	for name := range fieldSet {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	writer := csv.NewWriter(output)
	header := append([]string{}, csvMetadataColumns...)
	for _, name := range fieldNames {
		header = append(header, "field."+name)
	}
	if err := writer.Write(header); err != nil {
		return 0, fmt.Errorf("write CSV header: %w", err)
	}

	for index := range submissions {
		submission := &submissions[index]
		row := []string{
			strconv.FormatUint(uint64(submission.ID), 10),
			submission.CreatedAt.UTC().Format(time.RFC3339),
			strconv.FormatUint(uint64(submission.FormID), 10),
			safeCSVText(submission.Form.Name),
			safeCSVText(submission.Form.Slug),
			strconv.FormatBool(submission.IsSpam),
			safeCSVText(submission.UserAgent),
		}
		for _, name := range fieldNames {
			value, err := csvFieldValue(payloads[index][name])
			if err != nil {
				return 0, fmt.Errorf("encode submission %d field %q for CSV export: %w", submission.ID, name, err)
			}
			row = append(row, value)
		}
		if err := writer.Write(row); err != nil {
			return 0, fmt.Errorf("write submission %d to CSV: %w", submission.ID, err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return 0, fmt.Errorf("flush CSV export: %w", err)
	}
	return len(submissions), nil
}

func csvFieldValue(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return safeCSVText(typed), nil
	case json.Number:
		return typed.String(), nil
	case bool:
		return strconv.FormatBool(typed), nil
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
}

func safeCSVText(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed == "" || !strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return value
	}
	return "'" + value
}
