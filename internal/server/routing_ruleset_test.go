package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PFXDev/FireX/internal/clash"
	"github.com/PFXDev/FireX/internal/model"
)

func (h *harness) matrix() matrixDoc {
	h.t.Helper()
	var doc matrixDoc
	if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/routing", nil), &doc); err != nil {
		h.t.Fatal(err)
	}
	return doc
}

func (h *harness) ruleSetIndex(doc matrixDoc, name string) int {
	h.t.Helper()
	index := slices.IndexFunc(doc.RuleSets, func(set matrixRuleSet) bool { return set.Name == name })
	if index < 0 {
		h.t.Fatalf("no rule set %q in %+v", name, doc.RuleSets)
	}
	return index
}

func TestRuleSetServedThroughSubscription(t *testing.T) {
	h := newHarness(t)
	u := h.seed()

	provider := h.subscription(u.SubToken).RuleProviders["大流量下载"]
	if !strings.HasPrefix(provider.URL, h.server.URL+"/sub/"+u.SubToken+"/rules/") {
		t.Fatalf("provider url = %q, want it under the user's subscription path", provider.URL)
	}
	path := strings.TrimPrefix(provider.URL, h.server.URL)
	body := string(h.mustDo(http.MethodGet, path, nil))
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if !slices.Contains(lines, "GEOSITE,huggingface") || !slices.Contains(lines, "DOMAIN,persistent.oaistatic.com") {
		t.Errorf("served payload lacks stock hosts:\n%s", body)
	}
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			t.Errorf("served payload keeps blank or comment line %q", line)
		}
	}

	for _, bad := range []string{"/sub/nope/rules/1", "/sub/" + u.SubToken + "/rules/999", "/sub/" + u.SubToken + "/rules/x"} {
		if resp, _ := h.do(http.MethodGet, bad, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", bad, resp.StatusCode)
		}
	}
}

// Rules name a set the way members name a policy, so a rename in the editor
// has to reach every rule even when the client did not rewrite them.
func TestRuleSetRenameFollowsRules(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	doc := h.matrix()
	doc.RuleSets[h.ruleSetIndex(doc, "大流量下载")].Name = "下载源"
	h.mustDo(http.MethodPut, "/api/routing", doc)

	cfg := h.subscription(u.SubToken)
	if !slices.Contains(cfg.Rules, "RULE-SET,下载源,📥 大流量下载") {
		t.Errorf("rules were not rewritten to the new set name: %v", cfg.Rules)
	}
	if _, ok := cfg.RuleProviders["下载源"]; !ok || len(cfg.RuleProviders) != 1 {
		t.Errorf("rule-providers = %+v, want only the renamed set", cfg.RuleProviders)
	}
}

func TestRuleSetSaveRejectsBrokenSets(t *testing.T) {
	h := newHarness(t)
	h.seed()
	before := h.mustDo(http.MethodGet, "/api/routing", nil)

	for name, mutate := range map[string]func(doc *matrixDoc){
		"line with a target": func(doc *matrixDoc) {
			doc.RuleSets[0].Payload = "DOMAIN-SUFFIX,example.com,DIRECT"
		},
		"nested rule set": func(doc *matrixDoc) {
			doc.RuleSets[0].Payload = "RULE-SET,other"
		},
		"referenced set deleted": func(doc *matrixDoc) {
			doc.RuleSets = []matrixRuleSet{}
		},
		"duplicate names": func(doc *matrixDoc) {
			doc.RuleSets = append(doc.RuleSets, matrixRuleSet{Name: doc.RuleSets[0].Name})
		},
	} {
		t.Run(name, func(t *testing.T) {
			var doc matrixDoc
			if err := json.Unmarshal(before, &doc); err != nil {
				t.Fatal(err)
			}
			mutate(&doc)
			if resp, body := h.do(http.MethodPut, "/api/routing", doc); resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, body %s; want 400", resp.StatusCode, body)
			}
			if after := h.mustDo(http.MethodGet, "/api/routing", nil); string(after) != string(before) {
				t.Fatalf("rejected save changed the routing matrix")
			}
		})
	}
}

// A preset swap replaces the layout, not the lists an operator has curated.
func TestPresetKeepsEditedRuleSet(t *testing.T) {
	h := newHarness(t)
	h.seed()
	doc := h.matrix()
	doc.RuleSets[h.ruleSetIndex(doc, "大流量下载")].Payload = "DOMAIN-SUFFIX,example.com\n"
	h.mustDo(http.MethodPut, "/api/routing", doc)

	h.mustDo(http.MethodPost, "/api/routing/presets/lite", nil)
	h.mustDo(http.MethodPost, "/api/routing/presets/standard", nil)

	var set model.RuleSet
	if err := h.db.First(&set, "name = ?", "大流量下载").Error; err != nil {
		t.Fatal(err)
	}
	if set.Payload != "DOMAIN-SUFFIX,example.com\n" {
		t.Errorf("payload = %q, want the edited list kept", set.Payload)
	}
}

// RULE-SET may also name a provider the template declares; such a template
// cannot then drop it while a rule still points there.
func TestRuleSetMayNameTemplateProvider(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	withProvider := clash.DefaultTemplate + `rule-providers:
  custom:
    type: http
    behavior: domain
    url: https://example.com/custom.yaml
    interval: 86400
`
	h.mustDo(http.MethodPut, "/api/settings/clashTemplate", map[string]string{"template": withProvider})

	doc := h.matrix()
	doc.Policies[0].Rules = append(doc.Policies[0].Rules, matrixRule{Type: "RULE-SET", Value: "custom"})
	h.mustDo(http.MethodPut, "/api/routing", doc)

	cfg := h.subscription(u.SubToken)
	if p, ok := cfg.RuleProviders["custom"]; !ok || p.URL != "https://example.com/custom.yaml" {
		t.Errorf("template provider = %+v, want it kept beside FireX's own", p)
	}
	if _, ok := cfg.RuleProviders["大流量下载"]; !ok {
		t.Error("FireX rule set missing once the template declares providers")
	}

	if resp, body := h.do(http.MethodPut, "/api/settings/clashTemplate", map[string]string{"template": clash.DefaultTemplate}); resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "custom") {
		t.Errorf("dropping a referenced provider: status %d, body %s; want 400 naming it", resp.StatusCode, body)
	}
}
