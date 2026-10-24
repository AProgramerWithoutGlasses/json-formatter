package app

import (
	"strings"
	"testing"
)

func TestFormatPreservesData(t *testing.T) {
	input := `{"id":900719925474099312345,"n":1.2300e+12,"a":1,"a":2,"中文":"<script> & \\ ","ok":true}`
	for _, indent := range []string{"2", "4", "tab"} {
		got, err := Format(FormatRequest{Input: input, Indent: indent, Mode: "format"})
		if err != nil {
			t.Fatal(err)
		}
		compact, err := Format(FormatRequest{Input: got.Output, Indent: indent, Mode: "compact"})
		if err != nil || compact.Output != input {
			t.Fatalf("round trip changed data: %s, %v", compact.Output, err)
		}
	}
}

func TestInvalidJSON(t *testing.T) {
	for _, input := range []string{"", `{"a":}`, `{"a":1,}`, `{} {}`, `// comment`, "[1,2", `NaN`} {
		_, err := Format(FormatRequest{Input: input, Mode: "format"})
		if err == nil || err.Line < 1 || err.Column < 1 {
			t.Errorf("expected positioned error for %q: %v", input, err)
		}
	}
	for _, mode := range []string{"format", "compact"} {
		_, err := Format(FormatRequest{Input: "{\n  \"中文\": ?\n}", Mode: mode})
		if err == nil || err.Line != 2 || err.Column != 9 {
			t.Fatalf("wrong unicode position in %s: %+v", mode, err)
		}
	}
}

func TestLimitsAndOptions(t *testing.T) {
	for _, req := range []FormatRequest{
		{Input: strings.Repeat(" ", MaxInput+1), Mode: "format"},
		{Input: "{}", Indent: "8", Mode: "format"},
		{Input: "{}", Mode: "unknown"},
		{Input: string([]byte{0xff}), Mode: "format"},
	} {
		if _, err := Format(req); err == nil {
			t.Fatalf("expected rejection: %+v", req.Mode)
		}
	}
	for _, input := range []string{`null`, `true`, `123`, `"hello"`, `[]`, `{}`} {
		if _, err := Format(FormatRequest{Input: input, Mode: "format"}); err != nil {
			t.Fatal(err)
		}
	}
}
