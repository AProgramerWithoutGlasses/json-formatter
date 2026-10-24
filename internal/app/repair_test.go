package app

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestRepairJSON(t *testing.T) {
	cases := []struct {
		name, input, want, added string
		removed                  int
	}{
		{"missing comma", `{"a":1 "b":2}`, `{"a":1,"b":2}`, `,`, 0},
		{"missing colon", `{"a" 1}`, `{"a":1}`, `:`, 0},
		{"missing closing brackets", `{"a":[1,2`, `{"a":[1,2]}`, `]}`, 0},
		{"extra comma", `{"a":1,}`, `{"a":1}`, ``, 1},
		{"duplicate comma", `[1,,2,]`, `[1,2]`, ``, 2},
		{"extra colon", `{"a"::1}`, `{"a":1}`, ``, 1},
		{"bare key", `{name:1}`, `{"name":1}`, `""`, 0},
		{"missing opening quote on key", `{name":1}`, `{"name":1}`, `"`, 0},
		{"missing closing quote on key", `{"name:1}`, `{"name":1}`, `"`, 0},
		{"missing closing quote on value", `{"a":"hello}`, `{"a":"hello"}`, `"`, 0},
		{"single quotes", `{'a':'hello'}`, `{"a":"hello"}`, `""""`, 4},
		{"single with double quote", `{'a':'say "hi"'}`, `{"a":"say \"hi\""}`, `"""\\"`, 4},
		{"single escaped apostrophe", `{'a':'it\'s'}`, `{"a":"it's"}`, `""""`, 5},
		{"mismatch", `{"a":[1,2}`, `{"a":[1,2]}`, `]`, 0},
		{"replace bracket", `[1,2}`, `[1,2]`, `]`, 1},
		{"extra ending", `{"a":1}}`, `{"a":1}`, ``, 1},
		{"combined and unicode", `{name:"中文😀" "n":900719925474099312345,"a":1,"a":2,`, `{"name":"中文😀","n":900719925474099312345,"a":1,"a":2}`, `"",}`, 1},
		{"colon inside key", `{"x:y":"a,b]" "escaped":"a\"b"}`, `{"x:y":"a,b]","escaped":"a\"b"}`, `,`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"format", "compact"} {
				result, err := Format(FormatRequest{Input: tc.input, Mode: mode, Indent: "2", Repair: true})
				if err != nil {
					t.Fatalf("%s: %+v", mode, err)
				}
				compact, e := Format(FormatRequest{Input: result.Output, Mode: "compact"})
				if e != nil || compact.Output != tc.want {
					t.Fatalf("want %s, got %s (%+v)", tc.want, compact.Output, e)
				}
				if result.Repair == nil {
					t.Fatal("missing repair info")
				}
				if result.Repair.Removed != tc.removed {
					t.Fatalf("removed: %+v", result.Repair)
				}
				units := utf16.Encode([]rune(result.Output))
				marked := ""
				for _, r := range result.Repair.Ranges {
					if r.Start < 0 || r.End > len(units) || r.Start >= r.End {
						t.Fatalf("bad range %+v", r)
					}
					marked += string(utf16.Decode(units[r.Start:r.End]))
				}
				if marked != tc.added {
					t.Fatalf("highlight want %q got %q", tc.added, marked)
				}
				if result.Repair.Added != len(tc.added) {
					t.Fatalf("added: %+v", result.Repair)
				}
			}
		})
	}
}

func TestRepairRejectsAmbiguousData(t *testing.T) {
	for _, input := range []string{`{"a":}`, `{"a":?}`, `{"a":undefined}`, `{"a":NaN}`, `{"a":01}`, `{"a":1e}`, `{"a":hello world}`, `{} {}`, `{"a":"bad\q"}`, `{"a":"ends\`, strings.Repeat("[", 129) + "1", "[" + strings.Repeat("1 ", 300) + "]"} {
		if _, err := Format(FormatRequest{Input: input, Mode: "format", Repair: true}); err == nil {
			t.Errorf("must reject %q", input)
		}
	}
}

func TestRepairOptInAndValidUntouched(t *testing.T) {
	if _, err := Format(FormatRequest{Input: `{"a":1,}`, Mode: "format"}); err == nil {
		t.Fatal("repair should be opt-in in API")
	}
	input := `{"x:y":"😀 \\ \" ,}]","n":900719925474099312345,"a":1,"a":2}`
	r, err := Format(FormatRequest{Input: input, Mode: "compact", Repair: true})
	if err != nil || r.Output != input || r.Repair != nil {
		t.Fatalf("changed valid data: %+v, %+v", r, err)
	}
}

func FuzzRepair(f *testing.F) {
	for _, s := range []string{`{"a":1,}`, `[1 2`, `{"a":?}`, `{'😀':'x'}`, `{"a":1}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 10000 {
			return
		}
		r, err := Format(FormatRequest{Input: s, Mode: "format", Repair: true})
		if err == nil && !json.Valid([]byte(r.Output)) {
			t.Fatalf("invalid output %q", r.Output)
		}
	})
}
