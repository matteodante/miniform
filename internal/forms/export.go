package forms

import (
	"bytes"
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
	EntryCount   int64
	ContentBytes int64
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

	stats, err := measureCSVExport(statsQuery)
	if err != nil {
		return 0, err
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
		if err := collectCSVFieldNames(submissions[index].DataJSON, fieldSet); err != nil {
			return 0, fmt.Errorf("decode submission %d for CSV export: %w", submissions[index].ID, err)
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
		payload, err := decodeCSVPayload(submission.DataJSON)
		if err != nil {
			return 0, fmt.Errorf("decode submission %d for CSV export: %w", submission.ID, err)
		}
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
			value, err := csvFieldValue(payload[name])
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

func measureCSVExport(query *gorm.DB) (csvExportStats, error) {
	rows, err := query.
		Select("LENGTH(CAST(data_json AS BLOB)), COALESCE(LENGTH(CAST(user_agent AS BLOB)), 0)").
		Limit(MaxCSVExportEntries + 1).
		Rows()
	if err != nil {
		return csvExportStats{}, fmt.Errorf("measure submissions for CSV export: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var stats csvExportStats
	for rows.Next() {
		var dataBytes int64
		var userAgentBytes int64
		if err := rows.Scan(&dataBytes, &userAgentBytes); err != nil {
			return csvExportStats{}, fmt.Errorf("measure submission for CSV export: %w", err)
		}
		stats.EntryCount++
		stats.ContentBytes += dataBytes + userAgentBytes
	}
	if err := rows.Err(); err != nil {
		return csvExportStats{}, fmt.Errorf("measure submissions for CSV export: %w", err)
	}
	return stats, nil
}

func collectCSVFieldNames(encoded string, fieldSet map[string]struct{}) error {
	return visitCSVPayload(encoded, func(name string, _ json.RawMessage) error {
		fieldSet[name] = struct{}{}
		if len(fieldSet) > MaxCSVExportFields {
			return invalid("export", fmt.Sprintf(
				"Export exceeds %d distinct fields; narrow the inbox filters and try again",
				MaxCSVExportFields,
			))
		}
		return nil
	})
}

func decodeCSVPayload(encoded string) (map[string]json.RawMessage, error) {
	payload := make(map[string]json.RawMessage)
	err := visitCSVPayload(encoded, func(name string, value json.RawMessage) error {
		payload[name] = value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func visitCSVPayload(encoded string, visit func(string, json.RawMessage) error) error {
	decoder := json.NewDecoder(strings.NewReader(encoded))
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("submission payload is not a JSON object")
	}

	for decoder.More() {
		fieldToken, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := fieldToken.(string)
		if !ok {
			return fmt.Errorf("submission field name is not a string")
		}

		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if err := visit(name, value); err != nil {
			return err
		}
	}

	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return fmt.Errorf("submission payload has an invalid closing delimiter")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("submission payload contains multiple JSON values")
		}
		return err
	}
	return nil
}

func csvFieldValue(value json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] != '"' {
		return string(trimmed), nil
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err != nil {
		return "", err
	}
	return safeCSVText(text), nil
}

func safeCSVText(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed == "" || !strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return value
	}
	return "'" + value
}
