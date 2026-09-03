// Package provision keeps the remote 3x-ui panels converged on FireX's model:
// it discovers inbounds, pushes each user's client to exactly the panels their
// profile reaches, and folds per-panel counters into a global total.
package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/store"
)

// EmailSuffix namespaces FireX-managed clients on a shared panel so a manually
// created client is never mistaken for one of ours.
const EmailSuffix = "@firex"

// ClientGroup labels FireX-managed clients in the panel UI.
const ClientGroup = "firex"

type Manager struct {
	db *store.DB

	mu      sync.Mutex
	clients map[uint]*cachedClient

	// inFlight counts panel-facing passes in progress. FireX keeps no job
	// table to recover from, so instead of resuming interrupted work the
	// updater simply waits for this to drain before restarting.
	inFlight atomic.Int64
}

type cachedClient struct {
	client *panel.Client
	key    string
}

func NewManager(db *store.DB) *Manager {
	return &Manager{db: db, clients: map[uint]*cachedClient{}}
}

// busy marks a panel-facing pass as running; the returned func ends it. Calls
// nest, so an outer ReconcileAll stays busy across each ReconcileUser.
func (m *Manager) busy() func() {
	m.inFlight.Add(1)
	return func() { m.inFlight.Add(-1) }
}

// IsBusy reports whether a discovery, reconcile or traffic pass is running.
func (m *Manager) IsBusy() bool { return m.inFlight.Load() > 0 }

// ClientFor returns a cached HTTP client for the panel, rebuilding it when the
// panel's address or credentials changed.
func (m *Manager) ClientFor(p *model.Panel) *panel.Client {
	key := p.BaseURL + "\x00" + p.APIToken + "\x00" + strconv.FormatBool(p.SkipTLSVerify)
	m.mu.Lock()
	defer m.mu.Unlock()
	if cached, ok := m.clients[p.ID]; ok && cached.key == key {
		return cached.client
	}
	c := panel.New(p.BaseURL, p.APIToken, p.SkipTLSVerify)
	m.clients[p.ID] = &cachedClient{client: c, key: key}
	return c
}

func (m *Manager) Invalidate(panelID uint) {
	m.mu.Lock()
	delete(m.clients, panelID)
	m.mu.Unlock()
}

func NowMs() int64 { return time.Now().UnixMilli() }

// EmailFor is the panel-side identity of a FireX user.
func EmailFor(u *model.User) string { return u.Username + EmailSuffix }

// ---------------------------------------------------------------- discovery

// DiscoverPanel refreshes the panel's health and its inbound rows. Inbounds
// that vanished are flagged Missing rather than deleted so group membership and
// the admin's labels survive a panel outage.
func (m *Manager) DiscoverPanel(ctx context.Context, p *model.Panel) error {
	defer m.busy()()

	client := m.ClientFor(p)
	status, err := client.Status(ctx)
	if err != nil {
		m.markPanelOffline(p, err)
		return err
	}

	inbounds, err := client.Inbounds(ctx)
	if err != nil {
		m.markPanelOffline(p, err)
		return err
	}

	now := NowMs()
	seen := make([]int, 0, len(inbounds))
	for _, ib := range inbounds {
		seen = append(seen, ib.ID)
		var row model.Inbound
		err := m.db.Where("panel_id = ? AND remote_id = ?", p.ID, ib.ID).First(&row).Error
		if store.IsNotFound(err) {
			row = model.Inbound{
				PanelID:   p.ID,
				RemoteID:  ib.ID,
				Name:      ib.Remark,
				SortOrder: 100,
				UDP:       true,
				// A newly discovered inbound stays out of every subscription
				// until an admin reviews it and puts it in a node group.
				Enabled:   false,
				CreatedAt: now,
			}
		} else if err != nil {
			return fmt.Errorf("load inbound: %w", err)
		}
		row.InboundTag = ib.Tag
		row.Protocol = ib.Protocol
		row.Port = ib.Port
		row.RemoteRemark = ib.Remark
		row.RemoteEnabled = ib.Enable
		row.Missing = false
		row.LastSeenAt = now
		row.UpdatedAt = now
		if err := m.db.Save(&row).Error; err != nil {
			return fmt.Errorf("save inbound: %w", err)
		}
	}

	q := m.db.Model(&model.Inbound{}).Where("panel_id = ?", p.ID)
	if len(seen) > 0 {
		q = q.Where("remote_id NOT IN ?", seen)
	}
	if err := q.Updates(map[string]any{"missing": true, "updated_at": now}).Error; err != nil {
		return fmt.Errorf("flag missing inbounds: %w", err)
	}

	p.Status = model.PanelStatusOnline
	p.LastError = ""
	p.LastSeenAt = now
	p.XrayVersion = status.Xray.Version
	p.UpdatedAt = now
	return m.db.Model(p).Select("status", "last_error", "last_seen_at", "xray_version", "updated_at").Updates(p).Error
}

func (m *Manager) markPanelOffline(p *model.Panel, cause error) {
	p.Status = model.PanelStatusOffline
	p.LastError = cause.Error()
	p.UpdatedAt = NowMs()
	_ = m.db.Model(p).Select("status", "last_error", "updated_at").Updates(p).Error
}

// DiscoverAll refreshes every enabled panel, returning the first error while
// still processing the rest.
func (m *Manager) DiscoverAll(ctx context.Context) error {
	defer m.busy()()

	var panels []model.Panel
	if err := m.db.Where("enabled = ?", true).Find(&panels).Error; err != nil {
		return err
	}
	var firstErr error
	for i := range panels {
		if err := m.DiscoverPanel(ctx, &panels[i]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// -------------------------------------------------------------- reconcile

// desiredClient is what a user's client should look like on one panel.
type desiredClient struct {
	InboundIDs []int
	Client     panel.RemoteClient
}

func (d desiredClient) hash() string {
	buf, _ := json.Marshal(struct {
		I []int              `json:"i"`
		C panel.RemoteClient `json:"c"`
	}{d.InboundIDs, d.Client})
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:8])
}

// DesiredFor groups the inbounds a user may use by panel, and builds the client
// record each panel should hold. Panels absent from the map must not hold a
// client for this user at all.
func (m *Manager) DesiredFor(u *model.User) (map[uint]desiredClient, error) {
	inbounds, err := m.InboundsForUser(u)
	if err != nil {
		return nil, err
	}
	plan, err := m.planFor(u)
	if err != nil {
		return nil, err
	}

	byPanel := map[uint][]int{}
	for _, n := range inbounds {
		byPanel[n.PanelID] = append(byPanel[n.PanelID], n.RemoteID)
	}

	limitIP := 0
	if plan != nil {
		limitIP = plan.DeviceLimit
	}
	active := u.Active(NowMs())

	out := make(map[uint]desiredClient, len(byPanel))
	for panelID, inboundIDs := range byPanel {
		sort.Ints(inboundIDs)
		out[panelID] = desiredClient{
			InboundIDs: inboundIDs,
			Client: panel.RemoteClient{
				ID:       u.UUID,
				Password: u.UUID,
				Email:    EmailFor(u),
				LimitIP:  limitIP,
				// A per-panel cap equal to the global limit bounds the damage
				// between traffic polls; FireX still disables globally once the
				// aggregate across panels crosses the same line.
				TotalGB:    u.TrafficLimit,
				ExpiryTime: u.ExpiresAt,
				Enable:     active,
				Group:      ClientGroup,
				Comment:    "FireX:" + u.Username,
			},
		}
	}
	return out, nil
}

// InboundsForUser is everything the user's profile grants, filtered down to the
// inbounds that can actually carry traffic right now, in display order. The
// profile's node-group whitelist is the only thing consulted — rules and
// egresses never widen or narrow it.
func (m *Manager) InboundsForUser(u *model.User) ([]model.Inbound, error) {
	plan, err := m.planFor(u)
	if err != nil || plan == nil {
		return nil, err
	}
	return routing.InboundsForProfile(m.db, plan.ProfileID)
}

func (m *Manager) planFor(u *model.User) (*model.Plan, error) {
	if u.PlanID == 0 {
		return nil, nil
	}
	var plan model.Plan
	err := m.db.First(&plan, u.PlanID).Error
	if store.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// ReconcileUser converges every panel this user touches, including panels they
// were removed from. A failure on one panel is recorded and does not abort the
// others.
func (m *Manager) ReconcileUser(ctx context.Context, u *model.User) error {
	defer m.busy()()

	desired, err := m.DesiredFor(u)
	if err != nil {
		return err
	}

	var existing []model.UserPanel
	if err := m.db.Where("user_id = ?", u.ID).Find(&existing).Error; err != nil {
		return err
	}

	panelIDs := map[uint]bool{}
	for id := range desired {
		panelIDs[id] = true
	}
	for _, up := range existing {
		panelIDs[up.PanelID] = true
	}

	var firstErr error
	for panelID := range panelIDs {
		var p model.Panel
		if err := m.db.First(&p, panelID).Error; err != nil {
			if store.IsNotFound(err) {
				// The panel row is gone; drop our bookkeeping with it.
				m.db.Where("user_id = ? AND panel_id = ?", u.ID, panelID).Delete(&model.UserPanel{})
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		want, wanted := desired[panelID]
		if err := m.reconcileOnPanel(ctx, u, &p, want, wanted); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) reconcileOnPanel(ctx context.Context, u *model.User, p *model.Panel, want desiredClient, wanted bool) error {
	email := EmailFor(u)
	record := model.UserPanel{UserID: u.ID, PanelID: p.ID, Email: email}
	_ = m.db.Where("user_id = ? AND panel_id = ?", u.ID, p.ID).First(&record).Error
	record.UserID, record.PanelID, record.Email = u.ID, p.ID, email

	fail := func(err error) error {
		record.State = model.SyncStateFailed
		record.LastError = err.Error()
		record.UpdatedAt = NowMs()
		m.db.Save(&record)
		return err
	}

	// A disabled panel keeps its rows; we simply stop pushing to it.
	if !p.Enabled {
		return nil
	}

	client := m.ClientFor(p)
	inbounds, err := client.Inbounds(ctx)
	if err != nil {
		return fail(fmt.Errorf("panel %s: %w", p.Name, err))
	}
	membership := panel.InboundMembership(inbounds)
	actual, exists := membership[strings.ToLower(email)]

	if !wanted || len(want.InboundIDs) == 0 {
		if exists {
			if err := client.DeleteClient(ctx, email); err != nil {
				return fail(fmt.Errorf("panel %s: delete client: %w", p.Name, err))
			}
		}
		return m.db.Where("user_id = ? AND panel_id = ?", u.ID, p.ID).Delete(&model.UserPanel{}).Error
	}

	if !exists {
		if err := client.AddClient(ctx, want.Client, want.InboundIDs); err != nil {
			return fail(fmt.Errorf("panel %s: add client: %w", p.Name, err))
		}
	} else {
		attach, detach := diffInts(want.InboundIDs, actual)
		if len(attach) > 0 {
			if err := client.AttachClient(ctx, email, attach); err != nil {
				return fail(fmt.Errorf("panel %s: attach: %w", p.Name, err))
			}
		}
		if len(detach) > 0 {
			if err := client.DetachClient(ctx, email, detach); err != nil {
				return fail(fmt.Errorf("panel %s: detach: %w", p.Name, err))
			}
		}
		// Re-push the fields only when they actually drifted; a no-op update
		// still makes the panel restart-check its xray config.
		if record.DesiredHash != want.hash() || len(attach) > 0 {
			if err := client.UpdateClient(ctx, email, want.Client); err != nil {
				return fail(fmt.Errorf("panel %s: update client: %w", p.Name, err))
			}
		}
	}

	record.InboundIDs = joinInts(want.InboundIDs)
	record.DesiredHash = want.hash()
	record.State = model.SyncStateSynced
	record.LastError = ""
	record.UpdatedAt = NowMs()
	return m.db.Save(&record).Error
}

// ReconcileAll converges every user. Used by the periodic sync job and after
// bulk edits such as changing a plan's node set.
func (m *Manager) ReconcileAll(ctx context.Context) error {
	defer m.busy()()

	var users []model.User
	if err := m.db.Find(&users).Error; err != nil {
		return err
	}
	var firstErr error
	for i := range users {
		if err := m.ReconcileUser(ctx, &users[i]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ReconcileUsersOfPlan converges everyone on a plan whose inbound set changed.
func (m *Manager) ReconcileUsersOfPlan(ctx context.Context, planID uint) error {
	defer m.busy()()

	var users []model.User
	if err := m.db.Where("plan_id = ?", planID).Find(&users).Error; err != nil {
		return err
	}
	var firstErr error
	for i := range users {
		if err := m.ReconcileUser(ctx, &users[i]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ReconcileUsersOfPlans converges every user on any of these plans. A node group
// edit can touch several profiles at once, and each of those several plans.
func (m *Manager) ReconcileUsersOfPlans(ctx context.Context, planIDs []uint) error {
	seen := map[uint]bool{}
	var firstErr error
	for _, planID := range planIDs {
		if seen[planID] {
			continue
		}
		seen[planID] = true
		if err := m.ReconcileUsersOfPlan(ctx, planID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ------------------------------------------------------------------ traffic

// CollectTraffic folds each panel's per-client counters into the users' global
// totals and disables anyone who crossed their limit or expiry.
func (m *Manager) CollectTraffic(ctx context.Context) error {
	defer m.busy()()

	var panels []model.Panel
	if err := m.db.Where("enabled = ?", true).Find(&panels).Error; err != nil {
		return err
	}

	var firstErr error
	for i := range panels {
		p := &panels[i]
		client := m.ClientFor(p)
		inbounds, err := client.InboundsSlim(ctx)
		if err != nil {
			m.markPanelOffline(p, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		stats := panel.TrafficByEmail(inbounds)

		var records []model.UserPanel
		if err := m.db.Where("panel_id = ?", p.ID).Find(&records).Error; err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for j := range records {
			rec := &records[j]
			st, ok := stats[strings.ToLower(rec.Email)]
			if !ok {
				continue
			}
			dUp := counterDelta(rec.LastUp, st.Up)
			dDown := counterDelta(rec.LastDown, st.Down)
			rec.LastUp, rec.LastDown = st.Up, st.Down
			rec.UpdatedAt = NowMs()
			if err := m.db.Model(&model.UserPanel{}).
				Where("user_id = ? AND panel_id = ?", rec.UserID, rec.PanelID).
				Updates(map[string]any{
					"last_up":    rec.LastUp,
					"last_down":  rec.LastDown,
					"updated_at": rec.UpdatedAt,
				}).Error; err != nil && firstErr == nil {
				firstErr = err
			}
			if dUp == 0 && dDown == 0 {
				continue
			}
			// Increment in SQL so a concurrent admin edit of the user row
			// cannot clobber traffic that arrived between read and write.
			if err := m.db.Model(&model.User{}).Where("id = ?", rec.UserID).
				UpdateColumns(map[string]any{
					"upload":   gorm.Expr("upload + ?", dUp),
					"download": gorm.Expr("download + ?", dDown),
				}).Error; err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	if err := m.EnforceLimits(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// counterDelta turns two consecutive readings of a monotonic counter into a
// delta. A value that moved backwards means the panel reset the counter, so
// the new reading is itself the delta.
func counterDelta(last, current int64) int64 {
	if current < 0 {
		return 0
	}
	if current < last {
		return current
	}
	return current - last
}

// EnforceLimits flips the Depleted flag for users past their quota and pushes
// the resulting enable/disable to the panels.
func (m *Manager) EnforceLimits(ctx context.Context) error {
	defer m.busy()()

	var users []model.User
	if err := m.db.Find(&users).Error; err != nil {
		return err
	}
	now := NowMs()
	var firstErr error
	for i := range users {
		u := &users[i]
		depleted := u.TrafficLimit > 0 && u.Upload+u.Download >= u.TrafficLimit
		changed := false
		if depleted != u.Depleted {
			u.Depleted = depleted
			u.UpdatedAt = now
			if err := m.db.Model(u).Select("depleted", "updated_at").Updates(u).Error; err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			changed = true
		}
		// Expiry needs no stored flag, but the panels still have to be told;
		// compare against what we last pushed rather than re-pushing blindly.
		if !changed && !m.enableStateMatches(u, now) {
			changed = true
		}
		if changed {
			if err := m.ReconcileUser(ctx, u); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// enableStateMatches reports whether every panel record already reflects the
// user's current active state.
func (m *Manager) enableStateMatches(u *model.User, nowMs int64) bool {
	desired, err := m.DesiredFor(u)
	if err != nil {
		return true
	}
	var records []model.UserPanel
	if err := m.db.Where("user_id = ?", u.ID).Find(&records).Error; err != nil {
		return true
	}
	byPanel := map[uint]model.UserPanel{}
	for _, r := range records {
		byPanel[r.PanelID] = r
	}
	for panelID, want := range desired {
		rec, ok := byPanel[panelID]
		if !ok || rec.DesiredHash != want.hash() {
			return false
		}
	}
	return true
}

// ------------------------------------------------------------------ helpers

func diffInts(want, have []int) (missing, extra []int) {
	haveSet := make(map[int]bool, len(have))
	for _, v := range have {
		haveSet[v] = true
	}
	wantSet := make(map[int]bool, len(want))
	for _, v := range want {
		wantSet[v] = true
		if !haveSet[v] {
			missing = append(missing, v)
		}
	}
	for _, v := range have {
		if !wantSet[v] {
			extra = append(extra, v)
		}
	}
	return missing, extra
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}
