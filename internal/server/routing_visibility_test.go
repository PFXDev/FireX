package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PFXDev/FireX/internal/model"
)

// Client visibility must survive the editor round trip and inheritance without
// changing routing, including references to hidden policies and the MATCH rule.
func TestRoutingClientHiddenPreservesRouting(t *testing.T) {
	h := newHarness(t)
	u := h.seed()
	var profile model.Profile
	if err := h.db.First(&profile, "name = ?", "标准分流").Error; err != nil {
		t.Fatal(err)
	}

	readMatrix := func() matrixDoc {
		t.Helper()
		var doc matrixDoc
		if err := json.Unmarshal(h.mustDo(http.MethodGet, "/api/routing", nil), &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	doc := readMatrix()
	policyNames := map[string]bool{}
	for _, p := range doc.Policies {
		policy := model.Policy{Name: p.Name, Icon: p.Icon}
		policyNames[policy.DisplayName()] = true
	}

	type renderedConfig struct {
		Groups []struct {
			Name    string   `yaml:"name"`
			Type    string   `yaml:"type"`
			Hidden  *bool    `yaml:"hidden"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
		Rules []string `yaml:"rules"`
	}
	readConfig := func(path string, preview bool) renderedConfig {
		t.Helper()
		raw := h.mustDo(http.MethodGet, path, nil)
		if preview {
			var result struct {
				YAML string `json:"yaml"`
			}
			if err := json.Unmarshal(raw, &result); err != nil || result.YAML == "" {
				t.Fatalf("invalid preview: %s (%v)", raw, err)
			}
			raw = []byte(result.YAML)
		}
		var cfg renderedConfig
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	baseline := readConfig("/sub/"+u.SubToken+"?target=mihomo", false)
	if len(baseline.Groups) <= len(policyNames) || len(baseline.Rules) == 0 {
		t.Fatal("fixture must render policies, node groups and rules")
	}

	for _, tc := range []struct {
		name          string
		defaultHidden bool
		override      bool
		profileHidden bool
	}{
		{name: "existing defaults stay visible"},
		{name: "inherit hidden default", defaultHidden: true, profileHidden: true},
		{name: "override to visible", defaultHidden: true, override: true},
		{name: "override to hidden", override: true, profileHidden: true},
		{name: "turn hiding off again", override: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := matrixDoc{Policies: doc.Policies}
			for _, e := range doc.Egresses {
				if e.ProfileID != model.DefaultProfileID {
					continue
				}
				e.ClientHidden = tc.defaultHidden
				next.Egresses = append(next.Egresses, e)
				if tc.override {
					e.ProfileID = profile.ID
					e.ClientHidden = tc.profileHidden
					next.Egresses = append(next.Egresses, e)
				}
			}
			h.mustDo(http.MethodPut, "/api/routing", next)
			saved := readMatrix()
			if len(saved.Egresses) != len(next.Egresses) {
				t.Fatalf("saved %d egresses, want %d", len(saved.Egresses), len(next.Egresses))
			}
			for _, e := range saved.Egresses {
				want := tc.defaultHidden
				if e.ProfileID == profile.ID {
					want = tc.profileHidden
				}
				if e.Hidden || e.ClientHidden != want {
					t.Errorf("saved visibility for policy %d / profile %d: hidden=%v, clientHidden=%v; want false / %v",
						e.PolicyIndex, e.ProfileID, e.Hidden, e.ClientHidden, want)
				}
			}

			for _, output := range []struct {
				path    string
				preview bool
				hidden  bool
			}{
				{"/api/routing/preview?profileId=0", true, tc.defaultHidden},
				{"/api/routing/preview?profileId=" + itoa(profile.ID), true, tc.profileHidden},
				{"/sub/" + u.SubToken + "?target=mihomo", false, tc.profileHidden},
			} {
				cfg := readConfig(output.path, output.preview)
				for i, g := range cfg.Groups {
					wantHidden := policyNames[g.Name] && output.hidden
					if wantHidden && (g.Hidden == nil || !*g.Hidden) {
						t.Errorf("%s: group %q must emit hidden: true", output.path, g.Name)
					}
					if !wantHidden && g.Hidden != nil {
						t.Errorf("%s: visible group %q must omit hidden", output.path, g.Name)
					}
					cfg.Groups[i].Hidden = nil
				}
				if !reflect.DeepEqual(cfg, baseline) {
					t.Errorf("%s: changing client visibility altered groups, members or rules\ngot: %#v\nwant: %#v", output.path, cfg, baseline)
				}
			}
		})
	}
}
