package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRoutingSaveRejectsDuplicateCells(t *testing.T) {
	h := newHarness(t)
	h.seed()
	before := h.mustDo(http.MethodGet, "/api/routing", nil)
	for _, profileID := range []uint{0, 7} {
		t.Run("profile "+itoa(profileID), func(t *testing.T) {
			var doc matrixDoc
			if err := json.Unmarshal(before, &doc); err != nil {
				t.Fatal(err)
			}
			cell := doc.Egresses[0]
			if profileID != 0 {
				cell.ProfileID = profileID
				doc.Egresses = append(doc.Egresses, cell)
			}
			cell.ClientHidden = true
			doc.Egresses = append(doc.Egresses, cell)
			doc.Policies[0].Remark = "must not be saved"
			resp, body := h.do(http.MethodPut, "/api/routing", doc)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "重复") || !strings.Contains(string(body), doc.Policies[cell.PolicyIndex].Name) {
				t.Fatalf("duplicate cell: status %d, body %s; want 400 naming the duplicate policy", resp.StatusCode, body)
			}
			if after := h.mustDo(http.MethodGet, "/api/routing", nil); string(after) != string(before) {
				t.Fatalf("rejected save changed the routing matrix: %s", after)
			}
		})
	}
}

func TestRoutingSaveRejectsDuplicatePolicyIDs(t *testing.T) {
	h := newHarness(t)
	h.seed()
	before := h.mustDo(http.MethodGet, "/api/routing", nil)
	var doc matrixDoc
	if err := json.Unmarshal(before, &doc); err != nil {
		t.Fatal(err)
	}
	doc.Policies[1].ID = doc.Policies[0].ID
	resp, body := h.do(http.MethodPut, "/api/routing", doc)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "重复") {
		t.Fatalf("duplicate policy ID: status %d, body %s; want 400 explaining the duplicate", resp.StatusCode, body)
	}
	if after := h.mustDo(http.MethodGet, "/api/routing", nil); string(after) != string(before) {
		t.Fatalf("rejected save changed the routing matrix: %s", after)
	}
}
