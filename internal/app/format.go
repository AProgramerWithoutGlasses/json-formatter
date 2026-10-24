package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxInput = 1 << 20

type FormatRequest struct {
	Input  string `json:"input"`
	Indent string `json:"indent"`
	Mode   string `json:"mode"`
	Save   bool   `json:"save"`
	Repair bool   `json:"repair"`
}

type FormatResult struct {
	Output  string      `json:"output"`
	Bytes   int         `json:"bytes"`
	Lines   int         `json:"lines"`
	Saved   bool        `json:"saved"`
	Warning string      `json:"warning,omitempty"`
	Repair  *RepairInfo `json:"repair,omitempty"`
}

type APIError struct {
	Error  string `json:"error"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

// Format changes whitespace only: large integers, duplicate keys and key order survive.
func Format(req FormatRequest) (FormatResult, *APIError) {
	result, apiErr := formatStrict(req)
	if apiErr == nil || !req.Repair || apiErr.Line == 0 {
		return result, apiErr
	}
	repaired, marks, removed, err := repairJSON(req.Input)
	if err != nil {
		return result, apiErr
	}
	req.Input = repaired
	result, errResult := formatStrict(req)
	if errResult != nil {
		return FormatResult{}, apiErr
	}
	result.Repair = repairInfo(repaired, result.Output, marks, removed)
	return result, nil
}

func formatStrict(req FormatRequest) (FormatResult, *APIError) {
	if len(req.Input) > MaxInput {
		return FormatResult{}, &APIError{Error: "输入不能超过 1 MB"}
	}
	if !utf8.ValidString(req.Input) {
		return FormatResult{}, &APIError{Error: "请输入 UTF-8 编码的 JSON"}
	}
	if req.Indent == "" {
		req.Indent = "2"
	}
	indents := map[string]string{"2": "  ", "4": "    ", "tab": "\t"}
	indent, ok := indents[req.Indent]
	if !ok {
		return FormatResult{}, &APIError{Error: "缩进仅支持 2、4 或 tab"}
	}
	if req.Mode != "format" && req.Mode != "compact" {
		return FormatResult{}, &APIError{Error: "未知的整理模式"}
	}
	var output bytes.Buffer
	var err error
	if req.Mode == "compact" {
		err = json.Compact(&output, []byte(req.Input))
	} else {
		err = json.Indent(&output, []byte(req.Input), "", indent)
	}
	if err != nil {
		// Compact does not populate SyntaxError.Offset in Go 1.25.
		// Validate into RawMessage only on failure to obtain a byte-accurate location.
		var raw json.RawMessage
		if validationErr := json.Unmarshal([]byte(req.Input), &raw); validationErr != nil {
			err = validationErr
		}
		offset := len(req.Input)
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) && syntax.Offset > 0 {
			offset = min(int(syntax.Offset)-1, len(req.Input))
		}
		prefix := req.Input[:offset]
		line := strings.Count(prefix, "\n") + 1
		column := utf8.RuneCountInString(prefix[strings.LastIndex(prefix, "\n")+1:]) + 1
		return FormatResult{}, &APIError{Error: "JSON 语法有误：" + err.Error(), Line: line, Column: column}
	}
	result := strings.TrimSpace(output.String())
	return FormatResult{Output: result, Bytes: len(result), Lines: strings.Count(result, "\n") + 1}, nil
}
