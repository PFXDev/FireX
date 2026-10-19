package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/store"
	"github.com/PFXDev/FireX/internal/subscription"
)

// A snapshot is FireX's configuration as one document keyed by name rather
// than row id, so an agent can read it, edit it and send it back.
//
// Each top-level section is optional: a section the document leaves out is
// not touched, and a section it carries replaces what is stored — rows it does
// not list are deleted. Within a row an omitted field takes the value a new
// row would get, so a document means the same thing whatever the database
// held before. The whole document is applied and validated in one transaction.
//
// Users and plans are not part of it: they carry live state such as traffic
// and subscription tokens that a configuration push must never rewind.
// Applying never writes an inbound configuration to a panel either; a changed
// managed configuration becomes pending and is deployed through the inbound
// config endpoints, with their preview.

type snapshotDoc struct {
	Panels           []snapshotPanel     `json:"panels"`
	Inbounds         []snapshotInbound   `json:"inbounds"`
	InboundTemplates []snapshotTemplate  `json:"inboundTemplates"`
	NodeGroups       []snapshotNodeGroup `json:"nodeGroups"`
	Profiles         []snapshotProfile   `json:"profiles"`
	Routing          *snapshotRouting    `json:"routing"`
	// ClashTemplate is the mihomo base template; empty is the built-in default.
	ClashTemplate *string `json:"clashTemplate"`
}

type snapshotPanel struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	// APIToken is never exported. Empty keeps a stored panel's token and is
	// an error for a new panel.
	APIToken      string `json:"apiToken,omitempty"`
	SkipTLSVerify bool   `json:"skipTlsVerify"`
	Enabled       *bool  `json:"enabled"`
	Remark        string `json:"remark"`
}

// inboundRef names an inbound by its panel and its id on that panel, the only
// identity that survives between two FireX installs and a rediscovery.
type inboundRef struct {
	Panel    string `json:"panel"`
	RemoteID int    `json:"remoteId"`
}

func (r inboundRef) String() string { return fmt.Sprintf("%s#%d", r.Panel, r.RemoteID) }

type snapshotInbound struct {
	Panel    string `json:"panel"`
	RemoteID int    `json:"remoteId"`
	// What discovery last saw, so a reader can tell inbounds apart. Ignored
	// on apply: the panel owns these.
	Tag          string `json:"tag,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	Port         int    `json:"port,omitempty"`
	RemoteRemark string `json:"remoteRemark,omitempty"`
	Missing      bool   `json:"missing,omitempty"`

	Name      string `json:"name"`
	Emoji     string `json:"emoji"`
	SortOrder *int   `json:"sortOrder"`
	Enabled   *bool  `json:"enabled"`
	UDP       *bool  `json:"udp"`
	// Vision null keeps the stored setting, which is how an inbound from
	// before the setting existed stays in its preserve-each-flow mode.
	Vision        *bool  `json:"vision"`
	XHTTPTuning   *bool  `json:"xhttpTuning"`
	PublicAddress string `json:"publicAddress"`
	PublicPort    int    `json:"publicPort"`
	// ManagedConfig is the panel configuration FireX owns for this inbound;
	// null stops managing it. The panel is not written on apply.
	ManagedConfig map[string]any `json:"managedConfig"`
}

func (n snapshotInbound) ref() inboundRef {
	return inboundRef{Panel: strings.TrimSpace(n.Panel), RemoteID: n.RemoteID}
}

type snapshotTemplate struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}

type snapshotNodeGroup struct {
	Name       string            `json:"name"`
	Emoji      string            `json:"emoji"`
	Type       string            `json:"type"`
	TestURL    string            `json:"testUrl"`
	Interval   *int              `json:"interval"`
	Tolerance  *int              `json:"tolerance"`
	Multiplier *float64          `json:"multiplier"`
	SortOrder  *int              `json:"sortOrder"`
	Enabled    *bool             `json:"enabled"`
	Remark     string            `json:"remark"`
	Tags       map[string]string `json:"tags"`
	Inbounds   []inboundRef      `json:"inbounds"`
}

type snapshotProfile struct {
	Name       string   `json:"name"`
	AllGroups  bool     `json:"allGroups"`
	SortOrder  *int     `json:"sortOrder"`
	Enabled    *bool    `json:"enabled"`
	Remark     string   `json:"remark"`
	NodeGroups []string `json:"nodeGroups"`
}

type snapshotRouting struct {
	// Policies are in row order: both the rule precedence and the order the
	// groups appear in a client.
	Policies []snapshotPolicy `json:"policies"`
	Egresses []snapshotEgress `json:"egresses"`
	// RuleSets left out keep the stored sets, as in a matrix save.
	RuleSets []snapshotRuleSet `json:"ruleSets"`
}

type snapshotPolicy struct {
	Name    string       `json:"name"`
	Icon    string       `json:"icon"`
	IsFinal bool         `json:"isFinal"`
	Enabled *bool        `json:"enabled"`
	Remark  string       `json:"remark"`
	Rules   []matrixRule `json:"rules"`
}

type snapshotEgress struct {
	Policy string `json:"policy"`
	// Profile is empty for the default column.
	Profile      string         `json:"profile"`
	Type         string         `json:"type"`
	TestURL      string         `json:"testUrl"`
	Interval     int            `json:"interval"`
	Tolerance    int            `json:"tolerance"`
	Hidden       bool           `json:"hidden"`
	ClientHidden bool           `json:"clientHidden"`
	Members      []matrixMember `json:"members"`
}

type snapshotRuleSet struct {
	Name    string `json:"name"`
	Payload string `json:"payload"`
	Remark  string `json:"remark"`
}

// snapshotRevision fingerprints a document, so an apply built on a stale read
// is refused instead of silently reverting someone else's edit.
func snapshotRevision(doc snapshotDoc) string {
	encoded, _ := json.Marshal(doc)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:16])
}

// ------------------------------------------------------------------ export

func exportSnapshot(db *store.DB) (snapshotDoc, error) {
	doc := snapshotDoc{}

	var panels []model.Panel
	if err := db.Order("name ASC").Find(&panels).Error; err != nil {
		return doc, err
	}
	panelName := make(map[uint]string, len(panels))
	doc.Panels = make([]snapshotPanel, 0, len(panels))
	for _, p := range panels {
		panelName[p.ID] = p.Name
		doc.Panels = append(doc.Panels, snapshotPanel{
			Name: p.Name, BaseURL: p.BaseURL, SkipTLSVerify: p.SkipTLSVerify,
			Enabled: new(p.Enabled), Remark: p.Remark,
		})
	}

	var inbounds []model.Inbound
	if err := db.Find(&inbounds).Error; err != nil {
		return doc, err
	}
	sort.Slice(inbounds, func(i, j int) bool {
		a, b := panelName[inbounds[i].PanelID], panelName[inbounds[j].PanelID]
		if a != b {
			return a < b
		}
		return inbounds[i].RemoteID < inbounds[j].RemoteID
	})
	var configs []model.InboundConfig
	if err := db.Find(&configs).Error; err != nil {
		return doc, err
	}
	managed := make(map[uint]string, len(configs))
	for _, c := range configs {
		managed[c.InboundID] = c.Config
	}
	refs := make(map[uint]inboundRef, len(inbounds))
	doc.Inbounds = make([]snapshotInbound, 0, len(inbounds))
	for _, n := range inbounds {
		ref := inboundRef{Panel: panelName[n.PanelID], RemoteID: n.RemoteID}
		refs[n.ID] = ref
		row := snapshotInbound{
			Panel: ref.Panel, RemoteID: ref.RemoteID,
			Tag: n.InboundTag, Protocol: n.Protocol, Port: n.Port, RemoteRemark: n.RemoteRemark, Missing: n.Missing,
			Name: n.Name, Emoji: n.Emoji, SortOrder: new(n.SortOrder), Enabled: new(n.Enabled), UDP: new(n.UDP),
			Vision: n.Vision, XHTTPTuning: new(n.XHTTPTuning), PublicAddress: n.PublicAddress, PublicPort: n.PublicPort,
		}
		if raw, ok := managed[n.ID]; ok {
			if err := json.Unmarshal([]byte(raw), &row.ManagedConfig); err != nil {
				return doc, fmt.Errorf("入站 %s 的接管配置无法解析：%w", ref, err)
			}
		}
		doc.Inbounds = append(doc.Inbounds, row)
	}

	var templates []model.InboundTemplate
	if err := db.Order("name ASC, id ASC").Find(&templates).Error; err != nil {
		return doc, err
	}
	doc.InboundTemplates = make([]snapshotTemplate, 0, len(templates))
	for _, t := range templates {
		row := snapshotTemplate{Name: t.Name}
		if err := json.Unmarshal([]byte(t.Config), &row.Parameters); err != nil {
			return doc, fmt.Errorf("入站模板「%s」无法解析：%w", t.Name, err)
		}
		doc.InboundTemplates = append(doc.InboundTemplates, row)
	}

	var groups []model.NodeGroup
	if err := db.Order("sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		return doc, err
	}
	var tags []model.NodeGroupTag
	if err := db.Find(&tags).Error; err != nil {
		return doc, err
	}
	tagsByGroup := map[uint]map[string]string{}
	for _, t := range tags {
		if tagsByGroup[t.GroupID] == nil {
			tagsByGroup[t.GroupID] = map[string]string{}
		}
		tagsByGroup[t.GroupID][t.Key] = t.Value
	}
	var links []model.NodeGroupInbound
	if err := db.Find(&links).Error; err != nil {
		return doc, err
	}
	members := map[uint][]inboundRef{}
	for _, link := range links {
		if ref, ok := refs[link.InboundID]; ok {
			members[link.GroupID] = append(members[link.GroupID], ref)
		}
	}
	groupName := make(map[uint]string, len(groups))
	doc.NodeGroups = make([]snapshotNodeGroup, 0, len(groups))
	for _, g := range groups {
		groupName[g.ID] = g.Name
		list := jsonList(members[g.ID])
		sort.Slice(list, func(i, j int) bool {
			if list[i].Panel != list[j].Panel {
				return list[i].Panel < list[j].Panel
			}
			return list[i].RemoteID < list[j].RemoteID
		})
		groupTags := tagsByGroup[g.ID]
		if groupTags == nil {
			groupTags = map[string]string{}
		}
		doc.NodeGroups = append(doc.NodeGroups, snapshotNodeGroup{
			Name: g.Name, Emoji: g.Emoji, Type: g.Type, TestURL: g.TestURL,
			Interval: new(g.Interval), Tolerance: new(g.Tolerance), Multiplier: new(g.Multiplier),
			SortOrder: new(g.SortOrder), Enabled: new(g.Enabled), Remark: g.Remark,
			Tags: groupTags, Inbounds: list,
		})
	}

	var profiles []model.Profile
	if err := db.Order("sort_order ASC, id ASC").Find(&profiles).Error; err != nil {
		return doc, err
	}
	var grants []model.ProfileNodeGroup
	if err := db.Find(&grants).Error; err != nil {
		return doc, err
	}
	granted := map[uint]map[uint]bool{}
	for _, g := range grants {
		if granted[g.ProfileID] == nil {
			granted[g.ProfileID] = map[uint]bool{}
		}
		granted[g.ProfileID][g.GroupID] = true
	}
	profileName := make(map[uint]string, len(profiles))
	profileOrder := make(map[uint]int, len(profiles))
	doc.Profiles = make([]snapshotProfile, 0, len(profiles))
	for i, p := range profiles {
		profileName[p.ID], profileOrder[p.ID] = p.Name, i+1
		names := []string{}
		// Group order, so the list reads the way the groups render.
		for _, g := range groups {
			if granted[p.ID][g.ID] {
				names = append(names, g.Name)
			}
		}
		doc.Profiles = append(doc.Profiles, snapshotProfile{
			Name: p.Name, AllGroups: p.AllGroups, SortOrder: new(p.SortOrder),
			Enabled: new(p.Enabled), Remark: p.Remark, NodeGroups: names,
		})
	}

	routingDoc, err := exportRouting(db, profileName, profileOrder)
	if err != nil {
		return doc, err
	}
	doc.Routing = routingDoc
	doc.ClashTemplate = new(db.GetSetting(subscription.SettingKeyClashTemplate, ""))
	return doc, nil
}

func exportRouting(db *store.DB, profileName map[uint]string, profileOrder map[uint]int) (*snapshotRouting, error) {
	out := &snapshotRouting{}
	var policies []model.Policy
	if err := db.Order("sort_order ASC, id ASC").Find(&policies).Error; err != nil {
		return nil, err
	}
	var rules []model.Rule
	if err := db.Order("sort_order ASC, id ASC").Find(&rules).Error; err != nil {
		return nil, err
	}
	rulesByPolicy := map[uint][]matrixRule{}
	for _, r := range rules {
		rulesByPolicy[r.PolicyID] = append(rulesByPolicy[r.PolicyID], matrixRule{
			Type: r.Type, Value: r.Value, NoResolve: r.NoResolve, Disabled: r.Disabled,
		})
	}
	policyIndex := make(map[uint]int, len(policies))
	out.Policies = make([]snapshotPolicy, 0, len(policies))
	for i, p := range policies {
		policyIndex[p.ID] = i
		out.Policies = append(out.Policies, snapshotPolicy{
			Name: p.Name, Icon: p.Icon, IsFinal: p.IsFinal, Enabled: new(p.Enabled),
			Remark: p.Remark, Rules: jsonList(rulesByPolicy[p.ID]),
		})
	}

	var egresses []model.Egress
	if err := db.Find(&egresses).Error; err != nil {
		return nil, err
	}
	var members []model.EgressMember
	if err := db.Order("sort_order ASC, id ASC").Find(&members).Error; err != nil {
		return nil, err
	}
	byEgress := map[uint][]matrixMember{}
	for _, m := range members {
		byEgress[m.EgressID] = append(byEgress[m.EgressID], matrixMember{Kind: m.Kind, Ref: m.Ref})
	}
	// Row by row, the default column first and then the profiles in order.
	sort.Slice(egresses, func(i, j int) bool {
		a, b := egresses[i], egresses[j]
		if policyIndex[a.PolicyID] != policyIndex[b.PolicyID] {
			return policyIndex[a.PolicyID] < policyIndex[b.PolicyID]
		}
		return profileOrder[a.ProfileID] < profileOrder[b.ProfileID]
	})
	out.Egresses = make([]snapshotEgress, 0, len(egresses))
	for _, e := range egresses {
		index, ok := policyIndex[e.PolicyID]
		if !ok {
			continue
		}
		profile := ""
		if e.ProfileID != model.DefaultProfileID {
			if profile, ok = profileName[e.ProfileID]; !ok {
				continue
			}
		}
		out.Egresses = append(out.Egresses, snapshotEgress{
			Policy: policies[index].Name, Profile: profile, Type: e.Type, TestURL: e.TestURL,
			Interval: e.Interval, Tolerance: e.Tolerance, Hidden: e.Hidden, ClientHidden: e.ClientHidden,
			Members: jsonList(byEgress[e.ID]),
		})
	}

	var sets []model.RuleSet
	if err := db.Order("name ASC").Find(&sets).Error; err != nil {
		return nil, err
	}
	out.RuleSets = make([]snapshotRuleSet, 0, len(sets))
	for _, set := range sets {
		out.RuleSets = append(out.RuleSets, snapshotRuleSet{Name: set.Name, Payload: set.Payload, Remark: set.Remark})
	}
	return out, nil
}

// ------------------------------------------------------------------- apply

type snapshotRequest struct {
	snapshotDoc
	// Revision is the one the snapshot it was edited from carried. Required.
	Revision string `json:"revision"`
}

type removedPanel struct {
	panel  model.Panel
	emails []string
}

// snapshotEffects is what an apply still has to do once the transaction has
// committed: the panel work that cannot be rolled back.
type snapshotEffects struct {
	// discover holds panel ids; discoverNames names them for a preview, whose
	// new panels are rolled back and have no id left to look up.
	discover      []uint
	discoverNames []string
	invalidate    []uint
	removed       []removedPanel
	plans         []uint
	tokenChanged  []string
}

type snapshotResult struct {
	before, after snapshotDoc
	effects       snapshotEffects
}

var errSnapshotDryRun = errors.New("snapshot dry run")

func failSnapshot(c *gin.Context, err error) {
	var e *deploymentError
	if errors.As(err, &e) {
		failMsg(c, e.status, e.message)
		return
	}
	fail(c, http.StatusBadRequest, err)
}

// errSnapshotStale marks an apply whose revision no longer matches.
var errSnapshotStale = deployError(http.StatusConflict, "配置已被修改，请重新读取快照后再提交")

// runSnapshot applies the document inside one transaction and reports what
// changed. A dry run rolls everything back, so a preview is validated exactly
// as an apply would be. Dropping a panel deletes every FireX client on it, so
// it needs allowPanelRemoval; a dry run reports it instead.
func (s *Server) runSnapshot(req *snapshotRequest, dryRun, allowPanelRemoval bool) (*snapshotResult, error) {
	if req.Revision == "" {
		return nil, deployError(http.StatusBadRequest, "缺少 revision：先读取快照，再带着它返回的 revision 提交")
	}
	result := &snapshotResult{}
	err := s.db.Transaction(func(handle *gorm.DB) error {
		db := tx(handle)
		before, err := exportSnapshot(db)
		if err != nil {
			return err
		}
		if snapshotRevision(before) != req.Revision {
			return errSnapshotStale
		}
		result.before = before
		reachBefore, err := profileReach(db)
		if err != nil {
			return err
		}

		// A preview shows a panel removal rather than refusing it; the apply
		// is where it has to be confirmed.
		eff := &result.effects
		doc := &req.snapshotDoc
		if doc.Panels != nil {
			if err := applySnapshotPanels(handle, doc.Panels, allowPanelRemoval || dryRun, eff); err != nil {
				return err
			}
		}
		if doc.Inbounds != nil {
			if err := applySnapshotInbounds(handle, doc.Inbounds); err != nil {
				return err
			}
		}
		if doc.InboundTemplates != nil {
			if err := applySnapshotTemplates(handle, doc.InboundTemplates); err != nil {
				return err
			}
		}
		if doc.NodeGroups != nil {
			if err := applySnapshotNodeGroups(handle, doc.NodeGroups); err != nil {
				return err
			}
		}
		if doc.Profiles != nil {
			if err := applySnapshotProfiles(handle, doc.Profiles); err != nil {
				return err
			}
		}
		if doc.Routing != nil {
			if err := applySnapshotRouting(handle, doc.Routing); err != nil {
				return err
			}
			if err := db.SetSetting(store.SettingRoutingReview, ""); err != nil {
				return err
			}
		}
		if doc.ClashTemplate != nil {
			template := strings.TrimSpace(*doc.ClashTemplate)
			if err := checkClashTemplate(template); err != nil {
				return fmt.Errorf("Clash 模板：%w", err)
			}
			if err := db.SetSetting(subscription.SettingKeyClashTemplate, template); err != nil {
				return err
			}
		}
		if err := routing.Validate(db); err != nil {
			return err
		}

		reachAfter, err := profileReach(db)
		if err != nil {
			return err
		}
		for id, reach := range reachAfter {
			if reachBefore[id] != reach {
				eff.plans = append(eff.plans, routing.PlansUsingProfile(db, id)...)
			}
		}
		if result.after, err = exportSnapshot(db); err != nil {
			return err
		}
		if dryRun {
			return errSnapshotDryRun
		}
		return nil
	})
	if errors.Is(err, errSnapshotDryRun) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

// profileReach fingerprints what each profile provisions: the inbounds it
// reaches and the flow each carries. A profile whose fingerprint moved is the
// only kind whose users need pushing to the panels.
func profileReach(db *store.DB) (map[uint]string, error) {
	var ids []uint
	if err := db.Model(&model.Profile{}).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]string, len(ids))
	for _, id := range ids {
		inbounds, err := routing.InboundsForProfile(db, id)
		if err != nil {
			return nil, err
		}
		entries := make([]string, 0, len(inbounds))
		for _, n := range inbounds {
			vision := "keep"
			if n.Vision != nil {
				vision = fmt.Sprint(*n.Vision)
			}
			entries = append(entries, fmt.Sprintf("%d:%d:%s", n.PanelID, n.RemoteID, vision))
		}
		sort.Strings(entries)
		out[id] = strings.Join(entries, ",")
	}
	return out, nil
}

// createRow inserts a row exactly as given. GORM leaves a zero value out of an
// INSERT when its column has a default, and then reads the default back into
// the row, which would turn enabled: false into the column's true.
func createRow[T any](handle *gorm.DB, row *T) error {
	want := *row
	if err := handle.Create(row).Error; err != nil {
		return err
	}
	if err := handle.Model(row).Select("*").Omit("id", "created_at").Updates(&want).Error; err != nil {
		return err
	}
	// Reloads by the primary key Create just set.
	return handle.First(row).Error
}

func applySnapshotPanels(handle *gorm.DB, rows []snapshotPanel, allowRemoval bool, eff *snapshotEffects) error {
	var existing []model.Panel
	if err := handle.Find(&existing).Error; err != nil {
		return err
	}
	byName := make(map[string]*model.Panel, len(existing))
	for i := range existing {
		byName[existing[i].Name] = &existing[i]
	}
	listed := map[string]bool{}
	now := provision.NowMs()
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		baseURL := strings.TrimRight(strings.TrimSpace(row.BaseURL), "/")
		token := strings.TrimSpace(row.APIToken)
		if name == "" || baseURL == "" {
			return errBadRequest("每个面板都需要 name 和 baseUrl")
		}
		if listed[name] {
			return fmt.Errorf("面板「%s」重复", name)
		}
		listed[name] = true
		enabled := valueOr(row.Enabled, true)

		p, ok := byName[name]
		if !ok {
			if token == "" {
				return fmt.Errorf("新面板「%s」需要 apiToken", name)
			}
			created := model.Panel{
				Name: name, BaseURL: baseURL, APIToken: token, SkipTLSVerify: row.SkipTLSVerify,
				Enabled: enabled, Remark: row.Remark, Status: model.PanelStatusUnknown,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := createRow(handle, &created); err != nil {
				return err
			}
			if enabled {
				eff.discover = append(eff.discover, created.ID)
				eff.discoverNames = append(eff.discoverNames, name)
			}
			continue
		}
		tokenChanged := token != "" && token != p.APIToken
		connection := tokenChanged || p.BaseURL != baseURL || p.SkipTLSVerify != row.SkipTLSVerify
		if !connection && p.Enabled == enabled && p.Remark == row.Remark {
			continue
		}
		updates := map[string]any{
			"base_url": baseURL, "skip_tls_verify": row.SkipTLSVerify,
			"enabled": enabled, "remark": row.Remark, "updated_at": now,
		}
		if tokenChanged {
			updates["api_token"] = token
			eff.tokenChanged = append(eff.tokenChanged, name)
		}
		if err := handle.Model(&model.Panel{}).Where("id = ?", p.ID).Updates(updates).Error; err != nil {
			return err
		}
		eff.invalidate = append(eff.invalidate, p.ID)
		if connection && enabled {
			eff.discover = append(eff.discover, p.ID)
			eff.discoverNames = append(eff.discoverNames, name)
		}
	}

	var removed []string
	for _, p := range existing {
		if !listed[p.Name] {
			removed = append(removed, p.Name)
		}
	}
	if len(removed) > 0 && !allowRemoval {
		return deployError(http.StatusBadRequest, fmt.Sprintf(
			"快照会删除面板「%s」以及其上全部 FireX 客户端，合并前需要确认删除", strings.Join(removed, "」「")))
	}
	for _, p := range existing {
		if listed[p.Name] {
			continue
		}
		gone := removedPanel{panel: p}
		handle.Model(&model.UserPanel{}).Where("panel_id = ?", p.ID).Pluck("email", &gone.emails)
		if err := deletePanelRows(handle, p.ID); err != nil {
			return err
		}
		eff.removed = append(eff.removed, gone)
	}
	return nil
}

// inboundIndex resolves inbound references against the rows a transaction
// currently holds.
type inboundIndex struct {
	panels map[string]uint
	rows   map[uint]map[int]*model.Inbound
}

func loadInboundIndex(handle *gorm.DB) (*inboundIndex, []model.Inbound, error) {
	var panels []model.Panel
	if err := handle.Find(&panels).Error; err != nil {
		return nil, nil, err
	}
	var inbounds []model.Inbound
	if err := handle.Find(&inbounds).Error; err != nil {
		return nil, nil, err
	}
	index := &inboundIndex{panels: map[string]uint{}, rows: map[uint]map[int]*model.Inbound{}}
	for _, p := range panels {
		index.panels[p.Name] = p.ID
	}
	for i := range inbounds {
		n := &inbounds[i]
		if index.rows[n.PanelID] == nil {
			index.rows[n.PanelID] = map[int]*model.Inbound{}
		}
		index.rows[n.PanelID][n.RemoteID] = n
	}
	return index, inbounds, nil
}

func (x *inboundIndex) resolve(ref inboundRef) (*model.Inbound, error) {
	panelID, ok := x.panels[ref.Panel]
	if !ok {
		return nil, fmt.Errorf("入站 %s 引用了不存在的面板「%s」", ref, ref.Panel)
	}
	n, ok := x.rows[panelID][ref.RemoteID]
	if !ok {
		// A panel added by this same document has not been discovered yet.
		return nil, fmt.Errorf("面板「%s」上没有入站 #%d；新加的面板要先单独应用一次，等发现完入站再引用", ref.Panel, ref.RemoteID)
	}
	return n, nil
}

func applySnapshotInbounds(handle *gorm.DB, rows []snapshotInbound) error {
	index, inbounds, err := loadInboundIndex(handle)
	if err != nil {
		return err
	}
	var configs []model.InboundConfig
	if err := handle.Find(&configs).Error; err != nil {
		return err
	}
	managed := make(map[uint]*model.InboundConfig, len(configs))
	for i := range configs {
		managed[configs[i].InboundID] = &configs[i]
	}

	listed := map[uint]bool{}
	for _, row := range rows {
		ref := row.ref()
		inbound, err := index.resolve(ref)
		if err != nil {
			return err
		}
		if listed[inbound.ID] {
			return fmt.Errorf("入站 %s 重复", ref)
		}
		listed[inbound.ID] = true

		next := *inbound
		next.Name = strings.TrimSpace(row.Name)
		next.Emoji = strings.TrimSpace(row.Emoji)
		next.SortOrder = valueOr(row.SortOrder, 100)
		next.Enabled = valueOr(row.Enabled, false)
		next.UDP = valueOr(row.UDP, true)
		next.XHTTPTuning = valueOr(row.XHTTPTuning, true)
		if row.Vision != nil {
			// Only switching it on is checked, so a panel that later moved to
			// a transport without Vision does not block every other edit.
			if *row.Vision && (inbound.Vision == nil || !*inbound.Vision) {
				if reason := inboundVisionReason(*inbound); reason != "" {
					return fmt.Errorf("入站 %s 不能开启 Vision：%s", ref, reason)
				}
			}
			next.Vision = new(*row.Vision)
		}
		addr := strings.TrimSpace(row.PublicAddress)
		if !validPublicAddress(addr) {
			return fmt.Errorf("入站 %s 的 publicAddress 必须是主机名或 IP 地址", ref)
		}
		if addr == panel.ShareAddress(inbound.Config) {
			addr = ""
		}
		next.PublicAddress = addr
		if row.PublicPort < 0 || row.PublicPort > 65535 {
			return fmt.Errorf("入站 %s 的 publicPort 必须在 0 到 65535 之间", ref)
		}
		next.PublicPort = row.PublicPort
		if !reflect.DeepEqual(*inbound, next) {
			next.UpdatedAt = provision.NowMs()
			if err := handle.Save(&next).Error; err != nil {
				return err
			}
		}
		if err := applyManagedConfig(handle, next, managed[inbound.ID], row.ManagedConfig, ref); err != nil {
			return err
		}
	}

	for _, n := range inbounds {
		if listed[n.ID] {
			continue
		}
		if !n.Missing {
			var p model.Panel
			handle.First(&p, n.PanelID)
			return fmt.Errorf("入站 %s 不在快照里；面板上仍存在的入站不能删除，要删先在面板上删掉它", inboundRef{p.Name, n.RemoteID})
		}
		if err := deleteInboundRows(handle, n.ID); err != nil {
			return err
		}
	}
	return nil
}

// applyManagedConfig stores the configuration FireX owns for one inbound. A
// change bumps the revision and leaves it pending; deployment stays explicit.
func applyManagedConfig(handle *gorm.DB, inbound model.Inbound, stored *model.InboundConfig, params map[string]any, ref inboundRef) error {
	if params == nil {
		if stored == nil {
			return nil
		}
		return handle.Where("inbound_id = ?", inbound.ID).Delete(&model.InboundConfig{}).Error
	}
	raw, _ := json.Marshal(params)
	config, err := panel.EditableConfiguration(string(raw))
	if err == nil {
		err = panel.ValidateConfiguration(config)
	}
	if err != nil {
		return fmt.Errorf("入站 %s 的接管配置：%w", ref, err)
	}
	encoded, _ := json.Marshal(config)
	if stored == nil {
		row := model.InboundConfig{InboundID: inbound.ID, Config: string(encoded), Revision: 1}
		// Taking over what the panel already runs needs no deployment.
		if !inbound.Missing && panel.SameConfiguration(row.Config, inbound.Config) {
			row.Applied, row.AppliedRevision = row.Config, 1
		}
		return handle.Create(&row).Error
	}
	if panel.SameConfiguration(stored.Config, string(encoded)) {
		return nil
	}
	return handle.Model(&model.InboundConfig{}).Where("inbound_id = ?", inbound.ID).Updates(map[string]any{
		"config": string(encoded), "revision": stored.Revision + 1, "updated_at": provision.NowMs(),
	}).Error
}

func applySnapshotTemplates(handle *gorm.DB, rows []snapshotTemplate) error {
	var existing []model.InboundTemplate
	if err := handle.Order("id ASC").Find(&existing).Error; err != nil {
		return err
	}
	byName := map[string][]model.InboundTemplate{}
	for _, t := range existing {
		byName[t.Name] = append(byName[t.Name], t)
	}
	listed := map[string]bool{}
	keep := map[uint]bool{}
	for _, row := range rows {
		name, ok := templateName(row.Name)
		if !ok {
			return errBadRequest("入站模板名称不能为空且不能超过 200 字节")
		}
		if listed[name] {
			return fmt.Errorf("入站模板「%s」重复", name)
		}
		listed[name] = true
		raw, _ := json.Marshal(row.Parameters)
		config, err := templateParameters(string(raw))
		if err != nil {
			return fmt.Errorf("入站模板「%s」：%w", name, err)
		}
		encoded, _ := json.Marshal(config)

		matches := byName[name]
		switch len(matches) {
		case 0:
			if err := handle.Create(&model.InboundTemplate{Name: name, Config: string(encoded), Revision: 1}).Error; err != nil {
				return err
			}
		case 1:
			stored := matches[0]
			keep[stored.ID] = true
			if reflect.DeepEqual(templateMap(stored.Config), config) {
				continue
			}
			if err := handle.Model(&model.InboundTemplate{}).Where("id = ?", stored.ID).Updates(map[string]any{
				"config": string(encoded), "revision": stored.Revision + 1, "updated_at": provision.NowMs(),
			}).Error; err != nil {
				return err
			}
		default:
			// Templates are not unique by name in the UI, but a snapshot has
			// nothing else to tell them apart by.
			return fmt.Errorf("FireX 里有 %d 个同名入站模板「%s」，先在界面里改名", len(matches), name)
		}
	}
	for _, t := range existing {
		if !keep[t.ID] {
			if err := handle.Delete(&model.InboundTemplate{}, t.ID).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func templateMap(raw string) map[string]any {
	var out map[string]any
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func applySnapshotNodeGroups(handle *gorm.DB, rows []snapshotNodeGroup) error {
	index, _, err := loadInboundIndex(handle)
	if err != nil {
		return err
	}
	var existing []model.NodeGroup
	if err := handle.Find(&existing).Error; err != nil {
		return err
	}
	byName := make(map[string]model.NodeGroup, len(existing))
	for _, g := range existing {
		byName[g.Name] = g
	}
	listed := map[string]bool{}
	for _, row := range rows {
		req := nodeGroupRequest{
			Name: row.Name, Emoji: row.Emoji, Type: row.Type, TestURL: row.TestURL,
			Interval: new(valueOr(row.Interval, 300)), Tolerance: new(valueOr(row.Tolerance, 50)),
			Multiplier: new(valueOr(row.Multiplier, 1.0)), SortOrder: new(valueOr(row.SortOrder, 100)),
			Enabled: new(valueOr(row.Enabled, true)), Remark: row.Remark,
		}
		stored, exists := byName[strings.TrimSpace(row.Name)]
		g := stored
		if err := req.apply(&g); err != nil {
			return fmt.Errorf("节点组「%s」：%w", strings.TrimSpace(row.Name), err)
		}
		if listed[g.Name] {
			return fmt.Errorf("节点组「%s」重复", g.Name)
		}
		listed[g.Name] = true
		switch {
		case !exists:
			if err := createRow(handle, &g); err != nil {
				return err
			}
		case !reflect.DeepEqual(stored, g):
			if err := handle.Save(&g).Error; err != nil {
				return err
			}
		}

		keys := make([]string, 0, len(row.Tags))
		for key := range row.Tags {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		tags := make([]nodeGroupTag, 0, len(keys))
		for _, key := range keys {
			tags = append(tags, nodeGroupTag{Key: key, Value: row.Tags[key]})
		}
		if err := setGroupTags(handle, g.ID, tags); err != nil {
			return err
		}
		ids := make([]uint, 0, len(row.Inbounds))
		for _, ref := range row.Inbounds {
			ref.Panel = strings.TrimSpace(ref.Panel)
			n, err := index.resolve(ref)
			if err != nil {
				return fmt.Errorf("节点组「%s」：%w", g.Name, err)
			}
			ids = append(ids, n.ID)
		}
		if _, err := setGroupMembers(handle, g.ID, ids); err != nil {
			return err
		}
	}
	for _, g := range existing {
		if listed[g.Name] {
			continue
		}
		if _, err := deleteNodeGroupRows(handle, g); err != nil {
			return err
		}
	}
	return nil
}

func applySnapshotProfiles(handle *gorm.DB, rows []snapshotProfile) error {
	var groups []model.NodeGroup
	if err := handle.Find(&groups).Error; err != nil {
		return err
	}
	groupID := make(map[string]uint, len(groups))
	for _, g := range groups {
		groupID[g.Name] = g.ID
	}
	var existing []model.Profile
	if err := handle.Find(&existing).Error; err != nil {
		return err
	}
	byName := make(map[string]model.Profile, len(existing))
	for _, p := range existing {
		byName[p.Name] = p
	}
	listed := map[string]bool{}
	for _, row := range rows {
		req := profileRequest{
			Name: row.Name, AllGroups: new(row.AllGroups), SortOrder: new(valueOr(row.SortOrder, 100)),
			Enabled: new(valueOr(row.Enabled, true)), Remark: row.Remark,
		}
		stored, exists := byName[strings.TrimSpace(row.Name)]
		p := stored
		if err := req.apply(&p); err != nil {
			return err
		}
		if listed[p.Name] {
			return fmt.Errorf("分流方案「%s」重复", p.Name)
		}
		listed[p.Name] = true
		switch {
		case !exists:
			if err := createRow(handle, &p); err != nil {
				return err
			}
		case !reflect.DeepEqual(stored, p):
			if err := handle.Save(&p).Error; err != nil {
				return err
			}
		}
		ids := make([]uint, 0, len(row.NodeGroups))
		for _, name := range row.NodeGroups {
			id, ok := groupID[strings.TrimSpace(name)]
			if !ok {
				return fmt.Errorf("分流方案「%s」引用了不存在的节点组「%s」", p.Name, name)
			}
			ids = append(ids, id)
		}
		if err := setProfileGroups(handle, p.ID, ids); err != nil {
			return err
		}
	}
	for _, p := range existing {
		if listed[p.Name] {
			continue
		}
		// Plans are outside the snapshot, so one still bound here cannot be
		// moved by it.
		var plans []string
		handle.Model(&model.Plan{}).Where("profile_id = ?", p.ID).Order("name ASC").Pluck("name", &plans)
		if len(plans) > 0 {
			return fmt.Errorf("套餐「%s」还绑定着分流方案「%s」，先把套餐改到别的方案", strings.Join(plans, "」「"), p.Name)
		}
		if err := deleteProfileRows(handle, p.ID); err != nil {
			return err
		}
	}
	return nil
}

// applySnapshotRouting translates names back to the ids a matrix save speaks
// and hands it to the same writer the editor uses.
func applySnapshotRouting(handle *gorm.DB, r *snapshotRouting) error {
	var policies []model.Policy
	if err := handle.Find(&policies).Error; err != nil {
		return err
	}
	policyID := make(map[string]uint, len(policies))
	for _, p := range policies {
		policyID[p.Name] = p.ID
	}
	var profiles []model.Profile
	if err := handle.Find(&profiles).Error; err != nil {
		return err
	}
	profileID := make(map[string]uint, len(profiles))
	for _, p := range profiles {
		profileID[p.Name] = p.ID
	}

	doc := matrixDoc{
		Policies: make([]matrixPolicy, 0, len(r.Policies)),
		Egresses: make([]matrixEgress, 0, len(r.Egresses)),
	}
	indexByName := make(map[string]int, len(r.Policies))
	for i, p := range r.Policies {
		name := strings.TrimSpace(p.Name)
		if _, dup := indexByName[name]; dup {
			return fmt.Errorf("分流策略「%s」重复", name)
		}
		indexByName[name] = i
		doc.Policies = append(doc.Policies, matrixPolicy{
			ID: policyID[name], Name: name, Icon: p.Icon, IsFinal: p.IsFinal,
			Enabled: valueOr(p.Enabled, true), Remark: p.Remark, Rules: p.Rules,
		})
	}
	for _, e := range r.Egresses {
		index, ok := indexByName[strings.TrimSpace(e.Policy)]
		if !ok {
			return fmt.Errorf("出口引用了不存在的分流策略「%s」", e.Policy)
		}
		cell := matrixEgress{
			PolicyIndex: index, ProfileID: model.DefaultProfileID, Type: e.Type, TestURL: e.TestURL,
			Interval: e.Interval, Tolerance: e.Tolerance, Hidden: e.Hidden, ClientHidden: e.ClientHidden,
			Members: e.Members,
		}
		if name := strings.TrimSpace(e.Profile); name != "" {
			if cell.ProfileID, ok = profileID[name]; !ok {
				return fmt.Errorf("分流策略「%s」的出口引用了不存在的分流方案「%s」", e.Policy, name)
			}
		}
		doc.Egresses = append(doc.Egresses, cell)
	}
	if r.RuleSets != nil {
		var sets []model.RuleSet
		if err := handle.Find(&sets).Error; err != nil {
			return err
		}
		setID := make(map[string]uint, len(sets))
		for _, set := range sets {
			setID[set.Name] = set.ID
		}
		doc.RuleSets = make([]matrixRuleSet, 0, len(r.RuleSets))
		for _, set := range r.RuleSets {
			name := strings.TrimSpace(set.Name)
			doc.RuleSets = append(doc.RuleSets, matrixRuleSet{ID: setID[name], Name: name, Payload: set.Payload, Remark: set.Remark})
		}
	}
	return writeMatrix(handle, &doc)
}

// -------------------------------------------------------------------- diff

type snapshotChange struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	// Action is create, update or delete.
	Action string         `json:"action"`
	Fields []configChange `json:"fields,omitempty"`
}

type keyedRows struct {
	order []string
	rows  map[string]map[string]any
}

func keyRows[T any](rows []T, key func(int, T) string, extra func(int, map[string]any)) keyedRows {
	out := keyedRows{rows: make(map[string]map[string]any, len(rows))}
	for i, row := range rows {
		k := key(i, row)
		raw, _ := json.Marshal(row)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if extra != nil {
			extra(i, m)
		}
		out.order = append(out.order, k)
		out.rows[k] = m
	}
	return out
}

func diffRows(changes []snapshotChange, section string, before, after keyedRows) []snapshotChange {
	for _, k := range after.order {
		old, ok := before.rows[k]
		if !ok {
			// A reviewer needs to see what a new row holds, not just its name.
			fields := configChanges(map[string]any{}, after.rows[k], "")
			changes = append(changes, snapshotChange{Section: section, Key: k, Action: "create", Fields: fields})
			continue
		}
		if fields := configChanges(old, after.rows[k], ""); len(fields) > 0 {
			changes = append(changes, snapshotChange{Section: section, Key: k, Action: "update", Fields: fields})
		}
	}
	for _, k := range before.order {
		if _, ok := after.rows[k]; !ok {
			fields := configChanges(before.rows[k], map[string]any{}, "")
			changes = append(changes, snapshotChange{Section: section, Key: k, Action: "delete", Fields: fields})
		}
	}
	return changes
}

func diffSnapshots(before, after snapshotDoc, tokenChanged []string) []snapshotChange {
	changes := []snapshotChange{}
	panels := func(doc snapshotDoc) keyedRows {
		return keyRows(doc.Panels, func(_ int, p snapshotPanel) string { return p.Name }, nil)
	}
	changes = diffRows(changes, "panels", panels(before), panels(after))
	for _, name := range tokenChanged {
		token := configChange{Path: "apiToken"}
		found := false
		for i := range changes {
			if changes[i].Section == "panels" && changes[i].Key == name {
				changes[i].Fields, found = append(changes[i].Fields, token), true
			}
		}
		if !found {
			changes = append(changes, snapshotChange{Section: "panels", Key: name, Action: "update", Fields: []configChange{token}})
		}
	}

	inbounds := func(doc snapshotDoc) keyedRows {
		return keyRows(doc.Inbounds, func(_ int, n snapshotInbound) string { return n.ref().String() }, nil)
	}
	changes = diffRows(changes, "inbounds", inbounds(before), inbounds(after))
	templates := func(doc snapshotDoc) keyedRows {
		return keyRows(doc.InboundTemplates, func(_ int, t snapshotTemplate) string { return t.Name }, nil)
	}
	changes = diffRows(changes, "inboundTemplates", templates(before), templates(after))
	groups := func(doc snapshotDoc) keyedRows {
		return keyRows(doc.NodeGroups, func(_ int, g snapshotNodeGroup) string { return g.Name }, nil)
	}
	changes = diffRows(changes, "nodeGroups", groups(before), groups(after))
	profiles := func(doc snapshotDoc) keyedRows {
		return keyRows(doc.Profiles, func(_ int, p snapshotProfile) string { return p.Name }, nil)
	}
	changes = diffRows(changes, "profiles", profiles(before), profiles(after))

	// Row order is rule precedence, so a move is a change even when the row
	// itself is not.
	policies := func(doc snapshotDoc) keyedRows {
		return keyRows(doc.Routing.Policies, func(_ int, p snapshotPolicy) string { return p.Name },
			func(i int, m map[string]any) { m["position"] = i + 1 })
	}
	changes = diffRows(changes, "policies", policies(before), policies(after))
	egresses := func(doc snapshotDoc) keyedRows {
		return keyRows(doc.Routing.Egresses, func(_ int, e snapshotEgress) string {
			column := e.Profile
			if column == "" {
				column = "默认"
			}
			return e.Policy + " / " + column
		}, nil)
	}
	changes = diffRows(changes, "egresses", egresses(before), egresses(after))
	ruleSets := func(doc snapshotDoc) keyedRows {
		return keyRows(doc.Routing.RuleSets, func(_ int, r snapshotRuleSet) string { return r.Name }, nil)
	}
	changes = diffRows(changes, "ruleSets", ruleSets(before), ruleSets(after))

	// The template is one long text; that it changed is what matters.
	if *before.ClashTemplate != *after.ClashTemplate {
		changes = append(changes, snapshotChange{Section: "clashTemplate", Key: "clashTemplate", Action: "update"})
	}
	return changes
}

// ----------------------------------------------------------------- outcome

func uniquePlans(ids []uint) []uint {
	seen := map[uint]bool{}
	out := []uint{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// snapshotPreview is what applying a document would do, from a dry run.
type snapshotPreview struct {
	Changes []snapshotChange `json:"changes"`
	// Discover names the panels discovered after the commit.
	Discover []string `json:"discover"`
	// RemovedPanels lose every FireX client on them.
	RemovedPanels []string `json:"removedPanels"`
	// ReconcilePlans counts the plans whose users would be pushed.
	ReconcilePlans int `json:"reconcilePlans"`
}

func (r *snapshotResult) preview() snapshotPreview {
	removed := []string{}
	for _, gone := range r.effects.removed {
		removed = append(removed, gone.panel.Name)
	}
	return snapshotPreview{
		Changes:        diffSnapshots(r.before, r.after, r.effects.tokenChanged),
		Discover:       jsonList(r.effects.discoverNames),
		RemovedPanels:  removed,
		ReconcilePlans: len(uniquePlans(r.effects.plans)),
	}
}

type snapshotDiscovery struct {
	Panel string `json:"panel"`
	Error string `json:"error"`
}

type pendingDeployment struct {
	inboundRef
	// InboundID addresses the inbound config endpoints that deploy it.
	InboundID uint `json:"inboundId"`
	Revision  uint `json:"revision"`
}

// snapshotOutcome is what a committed apply did, panel work included.
type snapshotOutcome struct {
	// Revision is the configuration's revision after discovery.
	Revision              string              `json:"revision"`
	Changes               []snapshotChange    `json:"changes"`
	Discover              []snapshotDiscovery `json:"discover"`
	ReconciledPlans       int                 `json:"reconciledPlans"`
	SyncError             string              `json:"syncError"`
	RemoteCleanupFailures int                 `json:"remoteCleanupFailures"`
	// PendingDeployments are managed configurations waiting to be deployed
	// through the inbound config endpoints; applying never writes them.
	PendingDeployments []pendingDeployment `json:"pendingDeployments"`
}

// settleSnapshot does the panel work a committed apply left: drop the
// clients on removed panels, discover new or rewired ones, and push the users
// whose reach changed.
func (s *Server) settleSnapshot(result *snapshotResult) snapshotOutcome {
	eff := result.effects
	out := snapshotOutcome{
		Changes:            diffSnapshots(result.before, result.after, eff.tokenChanged),
		Discover:           []snapshotDiscovery{},
		PendingDeployments: []pendingDeployment{},
	}
	for _, id := range eff.invalidate {
		s.mgr.Invalidate(id)
	}
	for _, gone := range eff.removed {
		ctx, cancel := context.WithTimeout(context.Background(), discoverTimeout)
		out.RemoteCleanupFailures += s.deleteRemoteClients(ctx, &gone.panel, gone.emails)
		cancel()
		s.mgr.Invalidate(gone.panel.ID)
	}
	for _, id := range eff.discover {
		var p model.Panel
		if err := s.db.First(&p, id).Error; err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), discoverTimeout)
		err := s.mgr.DiscoverPanel(ctx, &p)
		cancel()
		out.Discover = append(out.Discover, snapshotDiscovery{Panel: p.Name, Error: errString(err)})
	}
	plans := uniquePlans(eff.plans)
	out.ReconciledPlans = len(plans)
	out.SyncError = errString(s.reconcilePlans(plans))
	s.subs.InvalidateAll()

	var configs []model.InboundConfig
	s.db.Where("applied_revision <> revision").Find(&configs)
	for _, cfg := range configs {
		var n model.Inbound
		var p model.Panel
		if s.db.First(&n, cfg.InboundID).Error != nil || s.db.First(&p, n.PanelID).Error != nil {
			continue
		}
		out.PendingDeployments = append(out.PendingDeployments, pendingDeployment{inboundRef{p.Name, n.RemoteID}, n.ID, cfg.Revision})
	}
	// Discovery may have added inbounds, so the revision to build on next is
	// read after it.
	if doc, err := exportSnapshot(s.db); err == nil {
		out.Revision = snapshotRevision(doc)
	}
	return out
}
