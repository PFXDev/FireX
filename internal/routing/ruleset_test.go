package routing

import (
	"slices"
	"strings"
	"testing"
)

func TestRuleSetLines(t *testing.T) {
	got, err := RuleSetLines("# comment\r\n\n domain-suffix , hf.co \nGEOIP,google,NO-RESOLVE\nDOMAIN-REGEX,^doc-[^.]+-docs\\.googleusercontent\\.com$\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DOMAIN-SUFFIX,hf.co", "GEOIP,google,no-resolve", `DOMAIN-REGEX,^doc-[^.]+-docs\.googleusercontent\.com$`}
	if !slices.Equal(got, want) {
		t.Errorf("RuleSetLines() = %q, want %q", got, want)
	}

	for payload, wantErr := range map[string]string{
		"DOMAIN,example.com,DIRECT":      "第 1 行",
		"\nRULE-SET,other":               "第 2 行",
		"MATCH":                          "MATCH",
		"DOMAIN":                         "缺少匹配内容",
		"DOMAIN-SUFFIX,a.com,no-resolve": "no-resolve",
		"FOO,bar":                        "未知的匹配类型",
	} {
		if _, err := RuleSetLines(payload); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("RuleSetLines(%q) error = %v, want it to mention %q", payload, err, wantErr)
		}
	}
}
