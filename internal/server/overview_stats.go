package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/routing"
	"github.com/PFXDev/FireX/internal/store"
)

const (
	panelNearQuotaRatio    = 0.80
	userNearDepletionRatio = 0.85
	userExpiringSoonWindow = 7 * 24 * time.Hour
	topUsersLimit          = 8
)

type overviewPanelHealth struct {
	ID               uint   `json:"id"`
	Name             string `json:"name"`
	Enabled          bool   `json:"enabled"`
	Status           string `json:"status"`
	LastError        string `json:"lastError"`
	LastSeenAt       int64  `json:"lastSeenAt"`
	XrayVersion      string `json:"xrayVersion"`
	InboundCount     int64  `json:"inboundCount"`
	EnabledInbounds  int64  `json:"enabledInbounds"`
	MissingInbounds  int64  `json:"missingInbounds"`
	UserCount        int64  `json:"userCount"`
	TrafficLimit     int64  `json:"trafficLimit"`
	Upload           int64  `json:"upload"`
	Download         int64  `json:"download"`
	TotalUpload      int64  `json:"totalUpload"`
	TotalDownload    int64  `json:"totalDownload"`
	OverQuota        bool   `json:"overQuota"`
	NextTrafficReset int64  `json:"nextTrafficReset"`
}

type overviewCounts struct {
	Panels                      int64 `json:"panels"`
	EnabledPanels               int64 `json:"enabledPanels"`
	DisabledPanels              int64 `json:"disabledPanels"`
	OnlinePanels                int64 `json:"onlinePanels"`
	OfflinePanels               int64 `json:"offlinePanels"`
	OverQuotaPanels             int64 `json:"overQuotaPanels"`
	NearQuotaPanels             int64 `json:"nearQuotaPanels"`
	Inbounds                    int64 `json:"inbounds"`
	EnabledInbounds             int64 `json:"enabledInbounds"`
	MissingInbounds             int64 `json:"missingInbounds"`
	DriftedInbounds             int64 `json:"driftedInbounds"`
	PendingInbounds             int64 `json:"pendingInbounds"`
	UngroupedInbounds           int64 `json:"ungroupedInbounds"`
	NodeGroups                  int64 `json:"nodeGroups"`
	Profiles                    int64 `json:"profiles"`
	Plans                       int64 `json:"plans"`
	Users                       int64 `json:"users"`
	ActiveUsers                 int64 `json:"activeUsers"`
	ExternalSubscriptions       int64 `json:"externalSubscriptions"`
	FailedExternalSubscriptions int64 `json:"failedExternalSubscriptions"`
}

type overviewUserBreakdown struct {
	Active        int64 `json:"active"`
	Disabled      int64 `json:"disabled"`
	Expired       int64 `json:"expired"`
	Depleted      int64 `json:"depleted"`
	ExpiringSoon  int64 `json:"expiringSoon"`
	NearDepletion int64 `json:"nearDepletion"`
	PendingSyncs  int64 `json:"pendingSyncs"`
	SubActive24h  int64 `json:"subActive24h"`
	SubActive7d   int64 `json:"subActive7d"`
	SubFetched    int64 `json:"subFetched"`
}

type overviewClientShare struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type overviewAttention struct {
	RoutingNeedsReview   bool   `json:"routingNeedsReview"`
	PendingAgentSessions int64  `json:"pendingAgentSessions"`
	UpdateAvailable      string `json:"updateAvailable"`
	UpdateReady          bool   `json:"updateReady"`
}

type overviewTodayTraffic struct {
	TodayUpload               int64 `json:"todayUpload"`
	TodayDownload             int64 `json:"todayDownload"`
	YesterdayUpload           int64 `json:"yesterdayUpload"`
	YesterdayDownload         int64 `json:"yesterdayDownload"`
	YesterdaySameTimeUpload   int64 `json:"yesterdaySameTimeUpload"`
	YesterdaySameTimeDownload int64 `json:"yesterdaySameTimeDownload"`
}

type trafficSlice struct {
	ID            uint   `json:"id"`
	Name          string `json:"name"`
	Upload        int64  `json:"upload"`
	Download      int64  `json:"download"`
	TotalUpload   int64  `json:"totalUpload"`
	TotalDownload int64  `json:"totalDownload"`
	TrafficLimit  int64  `json:"trafficLimit,omitempty"`
	OverQuota     bool   `json:"overQuota,omitempty"`
}

type userTrafficSlice struct {
	ID            uint   `json:"id"`
	Username      string `json:"username"`
	PlanName      string `json:"planName"`
	Upload        int64  `json:"upload"`
	Download      int64  `json:"download"`
	TotalUpload   int64  `json:"totalUpload"`
	TotalDownload int64  `json:"totalDownload"`
	TrafficLimit  int64  `json:"trafficLimit"`
	Active        bool   `json:"active"`
}

type overviewTopUsers struct {
	Period []userTrafficSlice `json:"period"`
	Total  []userTrafficSlice `json:"total"`
}

func (s *Server) handleOverview(c *gin.Context) {
	nowTime := s.mgr.Now()
	now := nowTime.UnixMilli()
	loc := s.mgr.Loc()

	var counts overviewCounts
	s.db.Model(&model.NodeGroup{}).Count(&counts.NodeGroups)
	s.db.Model(&model.Profile{}).Count(&counts.Profiles)
	s.db.Model(&model.ExternalSubscription{}).Count(&counts.ExternalSubscriptions)
	s.db.Model(&model.ExternalSubscription{}).
		Where("enabled = ? AND last_error <> ''", true).
		Count(&counts.FailedExternalSubscriptions)

	var plans []model.Plan
	s.db.Order("sort_order ASC, id ASC").Find(&plans)
	counts.Plans = int64(len(plans))
	planNames := make(map[uint]string, len(plans))
	for _, p := range plans {
		planNames[p.ID] = p.Name
	}

	var inbounds []model.Inbound
	s.db.Find(&inbounds)
	counts.Inbounds = int64(len(inbounds))

	var managedConfigs []model.InboundConfig
	s.db.Find(&managedConfigs)
	managedByInbound := make(map[uint]*model.InboundConfig, len(managedConfigs))
	for i := range managedConfigs {
		managedByInbound[managedConfigs[i].InboundID] = &managedConfigs[i]
	}

	var groupedIDs []uint
	s.db.Model(&model.NodeGroupInbound{}).Distinct("inbound_id").Pluck("inbound_id", &groupedIDs)
	groupedSet := make(map[uint]bool, len(groupedIDs))
	for _, id := range groupedIDs {
		groupedSet[id] = true
	}

	type panelInboundCounts struct {
		total   int64
		enabled int64
		missing int64
	}
	inboundsByPanel := make(map[uint]*panelInboundCounts)
	for _, ib := range inbounds {
		entry := inboundsByPanel[ib.PanelID]
		if entry == nil {
			entry = &panelInboundCounts{}
			inboundsByPanel[ib.PanelID] = entry
		}
		entry.total++
		if ib.Missing {
			counts.MissingInbounds++
			entry.missing++
		} else if ib.Enabled {
			counts.EnabledInbounds++
			entry.enabled++
			if !groupedSet[ib.ID] {
				counts.UngroupedInbounds++
			}
		}
		if cfg := managedByInbound[ib.ID]; cfg != nil {
			switch configState(ib, cfg) {
			case configDrifted:
				counts.DriftedInbounds++
			case configPending:
				counts.PendingInbounds++
			}
		}
	}

	var users []model.User
	s.db.Find(&users)
	counts.Users = int64(len(users))

	var (
		periodUp, periodDown       int64
		userTotalUp, userTotalDown int64
		breakdown                  overviewUserBreakdown
	)
	expiringCutoff := now + int64(userExpiringSoonWindow/time.Millisecond)
	cutoff24h := now - int64(24*time.Hour/time.Millisecond)
	cutoff7d := now - int64(7*24*time.Hour/time.Millisecond)

	clientCounts := make(map[string]int)
	planTraffic := make(map[uint]*trafficSlice, len(plans)+1)
	for _, p := range plans {
		planTraffic[p.ID] = &trafficSlice{ID: p.ID, Name: p.Name}
	}

	userSlices := make([]userTrafficSlice, 0, len(users))
	for _, u := range users {
		periodUp += u.Upload
		periodDown += u.Download
		userTotalUp += u.TotalUpload
		userTotalDown += u.TotalDownload

		active := u.Active(now)
		switch {
		case !u.Enabled:
			breakdown.Disabled++
		case u.ExpiresAt > 0 && u.ExpiresAt <= now:
			breakdown.Expired++
		case u.Depleted || (u.TrafficLimit > 0 && u.Upload+u.Download >= u.TrafficLimit):
			breakdown.Depleted++
		default:
			breakdown.Active++
			counts.ActiveUsers++
			if u.ExpiresAt > now && u.ExpiresAt <= expiringCutoff {
				breakdown.ExpiringSoon++
			}
			if u.TrafficLimit > 0 && float64(u.Upload+u.Download)/float64(u.TrafficLimit) >= userNearDepletionRatio {
				breakdown.NearDepletion++
			}
		}

		if u.LastSubAt > 0 {
			breakdown.SubFetched++
			if u.LastSubAt >= cutoff24h {
				breakdown.SubActive24h++
			}
			if u.LastSubAt >= cutoff7d {
				breakdown.SubActive7d++
			}
		}
		if strings.TrimSpace(u.LastSubUA) != "" {
			family := classifySubscriptionClient(u.LastSubUA)
			clientCounts[family]++
		}

		ps := planTraffic[u.PlanID]
		if ps == nil {
			name := planNames[u.PlanID]
			if name == "" {
				name = "未分配套餐"
			}
			ps = &trafficSlice{ID: u.PlanID, Name: name}
			planTraffic[u.PlanID] = ps
		}
		ps.Upload += u.Upload
		ps.Download += u.Download
		ps.TotalUpload += u.TotalUpload
		ps.TotalDownload += u.TotalDownload

		userSlices = append(userSlices, userTrafficSlice{
			ID:            u.ID,
			Username:      u.Username,
			PlanName:      planNames[u.PlanID],
			Upload:        u.Upload,
			Download:      u.Download,
			TotalUpload:   u.TotalUpload,
			TotalDownload: u.TotalDownload,
			TrafficLimit:  u.TrafficLimit,
			Active:        active,
		})
	}

	byPlan := make([]trafficSlice, 0, len(planTraffic))
	for _, p := range plans {
		if ps := planTraffic[p.ID]; ps != nil {
			byPlan = append(byPlan, *ps)
		}
	}
	if unassigned := planTraffic[0]; unassigned != nil && (unassigned.Upload+unassigned.Download > 0 || unassigned.TotalUpload+unassigned.TotalDownload > 0) {
		byPlan = append(byPlan, *unassigned)
	}

	topUsers := buildTopUsers(userSlices, topUsersLimit)
	clients := buildClientShares(clientCounts)

	var userCountRows []struct {
		PanelID uint
		Count   int64
	}
	s.db.Model(&model.UserPanel{}).Select("panel_id, COUNT(*) AS count").Group("panel_id").Scan(&userCountRows)
	usersByPanel := make(map[uint]int64, len(userCountRows))
	for _, row := range userCountRows {
		usersByPanel[row.PanelID] = row.Count
	}

	var failed, pending int64
	s.db.Model(&model.UserPanel{}).Where("state = ?", model.SyncStateFailed).Count(&failed)
	s.db.Model(&model.UserPanel{}).Where("state = ?", model.SyncStatePending).Count(&pending)
	breakdown.PendingSyncs = pending

	var panels []model.Panel
	s.db.Order("id ASC").Find(&panels)
	counts.Panels = int64(len(panels))
	health := make([]overviewPanelHealth, 0, len(panels))
	var panelPeriodUp, panelPeriodDown int64
	var panelTotalUp, panelTotalDown int64
	for _, p := range panels {
		overQuota := p.OverQuota()
		if p.Enabled {
			counts.EnabledPanels++
			if p.Status == model.PanelStatusOnline {
				counts.OnlinePanels++
			} else {
				counts.OfflinePanels++
			}
			if overQuota {
				counts.OverQuotaPanels++
			} else if p.TrafficLimit > 0 && float64(p.Upload+p.Download)/float64(p.TrafficLimit) >= panelNearQuotaRatio {
				counts.NearQuotaPanels++
			}
		} else {
			counts.DisabledPanels++
		}

		ibCounts := inboundsByPanel[p.ID]
		var totalIb, enabledIb, missingIb int64
		if ibCounts != nil {
			totalIb, enabledIb, missingIb = ibCounts.total, ibCounts.enabled, ibCounts.missing
		}
		health = append(health, overviewPanelHealth{
			ID:               p.ID,
			Name:             p.Name,
			Enabled:          p.Enabled,
			Status:           p.Status,
			LastError:        p.LastError,
			LastSeenAt:       p.LastSeenAt,
			XrayVersion:      p.XrayVersion,
			InboundCount:     totalIb,
			EnabledInbounds:  enabledIb,
			MissingInbounds:  missingIb,
			UserCount:        usersByPanel[p.ID],
			TrafficLimit:     p.TrafficLimit,
			Upload:           p.Upload,
			Download:         p.Download,
			TotalUpload:      p.TotalUpload,
			TotalDownload:    p.TotalDownload,
			OverQuota:        overQuota,
			NextTrafficReset: s.mgr.NextPanelTrafficReset(&p),
		})
		panelPeriodUp += p.Upload
		panelPeriodDown += p.Download
		panelTotalUp += p.TotalUpload
		panelTotalDown += p.TotalDownload
	}

	var global model.TrafficTotal
	_ = s.db.First(&global, 1).Error
	totalUp := max(global.Upload, userTotalUp, panelTotalUp)
	totalDown := max(global.Download, userTotalDown, panelTotalDown)

	byPanel, byGroup := s.trafficDistribution(panels)
	todayTraffic := s.computeTodayTraffic(nowTime, loc)

	admin := currentAdmin(c)
	var attention overviewAttention
	if canViewConfig(admin) {
		attention.RoutingNeedsReview = s.db.GetSetting(store.SettingRoutingReview, "") != ""
	}
	if isSuperAdmin(admin) {
		s.db.Model(&model.AgentSession{}).
			Where("state = ? AND expires_at > ?", model.AgentSubmitted, now).
			Count(&attention.PendingAgentSessions)
		if s.upd != nil {
			st := s.upd.Status()
			if st.State == "ready" || (st.LatestVersion != "" && st.CurrentVersion != "" && st.LatestVersion != st.CurrentVersion) {
				attention.UpdateAvailable = st.LatestVersion
				attention.UpdateReady = st.State == "ready"
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"counts":              counts,
		"upload":              periodUp,
		"download":            periodDown,
		"panelPeriodUpload":   panelPeriodUp,
		"panelPeriodDownload": panelPeriodDown,
		"totalUpload":         totalUp,
		"totalDownload":       totalDown,
		"failedSyncs":         failed,
		"panels":              health,
		"trafficByPanel":      byPanel,
		"trafficByGroup":      byGroup,
		"trafficByPlan":       byPlan,
		"topUsers":            topUsers,
		"userBreakdown":       breakdown,
		"clients":             clients,
		"todayTraffic":        todayTraffic,
		"attention":           attention,
	})
}

func buildTopUsers(users []userTrafficSlice, limit int) overviewTopUsers {
	period := make([]userTrafficSlice, 0, len(users))
	total := make([]userTrafficSlice, 0, len(users))
	for _, u := range users {
		if u.Upload+u.Download > 0 {
			period = append(period, u)
		}
		if u.TotalUpload+u.TotalDownload > 0 {
			total = append(total, u)
		}
	}
	sort.Slice(period, func(i, j int) bool {
		di := period[i].Upload + period[i].Download
		dj := period[j].Upload + period[j].Download
		if di != dj {
			return di > dj
		}
		return period[i].ID < period[j].ID
	})
	sort.Slice(total, func(i, j int) bool {
		di := total[i].TotalUpload + total[i].TotalDownload
		dj := total[j].TotalUpload + total[j].TotalDownload
		if di != dj {
			return di > dj
		}
		return total[i].ID < total[j].ID
	})
	if len(period) > limit {
		period = period[:limit]
	}
	if len(total) > limit {
		total = total[:limit]
	}
	return overviewTopUsers{Period: period, Total: total}
}

func buildClientShares(counts map[string]int) []overviewClientShare {
	out := make([]overviewClientShare, 0, len(counts))
	for name, count := range counts {
		if count > 0 {
			out = append(out, overviewClientShare{Name: name, Count: count})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Name == "其他" {
			return false
		}
		if out[j].Name == "其他" {
			return true
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// classifySubscriptionClient maps a raw subscription User-Agent into a concise
// client family label for the overview chart.
func classifySubscriptionClient(ua string) string {
	lower := strings.ToLower(strings.TrimSpace(ua))
	switch {
	case lower == "":
		return "未知"
	case strings.Contains(lower, "flclash"):
		return "FlClash"
	case strings.Contains(lower, "clash-verge"), strings.Contains(lower, "clash verge"):
		return "Clash Verge"
	case strings.Contains(lower, "mihomo"), strings.Contains(lower, "clash.meta"), strings.Contains(lower, "clashmeta"):
		return "Mihomo"
	case strings.Contains(lower, "stash"):
		return "Stash"
	case strings.Contains(lower, "clash"):
		return "Clash"
	case strings.Contains(lower, "shadowrocket"):
		return "Shadowrocket"
	case strings.Contains(lower, "surge"):
		return "Surge"
	case strings.Contains(lower, "quantumult"):
		return "Quantumult X"
	case strings.Contains(lower, "loon"):
		return "Loon"
	case strings.Contains(lower, "karing"):
		return "Karing"
	case strings.Contains(lower, "hiddify"):
		return "Hiddify"
	case strings.Contains(lower, "sing-box"), strings.Contains(lower, "nekobox"), strings.Contains(lower, "nekoray"), strings.Contains(lower, "husi"), strings.HasPrefix(lower, "sf"):
		return "sing-box"
	case strings.Contains(lower, "v2ray"), strings.Contains(lower, "xray"):
		return "v2ray / Xray"
	case strings.Contains(lower, "curl/"), strings.Contains(lower, "wget/"):
		return "CLI"
	case strings.Contains(lower, "mozilla/"):
		return "浏览器"
	default:
		return "其他"
	}
}

func (s *Server) computeTodayTraffic(now time.Time, loc *time.Location) overviewTodayTraffic {
	localNow := now.In(loc)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	yesterdaySameHourEnd := time.Date(
		yesterdayStart.Year(), yesterdayStart.Month(), yesterdayStart.Day(),
		localNow.Hour(), 0, 0, 0, loc,
	)

	todayStartMs := todayStart.UnixMilli()
	yesterdayStartMs := yesterdayStart.UnixMilli()
	yesterdaySameHourMs := yesterdaySameHourEnd.UnixMilli()

	var rows []model.TrafficBucket
	if err := s.db.Where("bucket_ts >= ?", yesterdayStartMs).Find(&rows).Error; err != nil {
		return overviewTodayTraffic{}
	}

	var out overviewTodayTraffic
	for _, r := range rows {
		if r.BucketTs >= todayStartMs {
			out.TodayUpload += r.Upload
			out.TodayDownload += r.Download
		} else if r.BucketTs >= yesterdayStartMs {
			out.YesterdayUpload += r.Upload
			out.YesterdayDownload += r.Download
			if r.BucketTs <= yesterdaySameHourMs {
				out.YesterdaySameTimeUpload += r.Upload
				out.YesterdaySameTimeDownload += r.Download
			}
		}
	}
	return out
}

// trafficDistribution splits FireX traffic by panel, from the panels' period
// and all-time counters, and by node group, as the sum of the panels each
// group spans. A panel serves at most one group, so the groups do not overlap;
// traffic on panels in no group is reported under group id 0.
func (s *Server) trafficDistribution(panels []model.Panel) ([]trafficSlice, []trafficSlice) {
	byPanel := make([]trafficSlice, 0, len(panels))
	panelByID := map[uint]model.Panel{}
	for _, p := range panels {
		panelByID[p.ID] = p
		byPanel = append(byPanel, trafficSlice{
			ID:            p.ID,
			Name:          p.Name,
			Upload:        p.Upload,
			Download:      p.Download,
			TotalUpload:   p.TotalUpload,
			TotalDownload: p.TotalDownload,
			TrafficLimit:  p.TrafficLimit,
			OverQuota:     p.OverQuota(),
		})
	}

	var groups []model.NodeGroup
	s.db.Order("sort_order ASC, id ASC").Find(&groups)
	ids := make([]uint, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	spans, _ := routing.GroupPanels(s.db, ids)
	counted := map[uint]bool{}
	byGroup := make([]trafficSlice, 0, len(groups)+1)
	for _, g := range groups {
		slice := trafficSlice{ID: g.ID, Name: g.DisplayName()}
		for _, panelID := range spans[g.ID] {
			if counted[panelID] {
				continue
			}
			counted[panelID] = true
			p := panelByID[panelID]
			slice.Upload += p.Upload
			slice.Download += p.Download
			slice.TotalUpload += p.TotalUpload
			slice.TotalDownload += p.TotalDownload
		}
		byGroup = append(byGroup, slice)
	}
	rest := trafficSlice{Name: "未归组"}
	for _, p := range panels {
		if !counted[p.ID] {
			rest.Upload += p.Upload
			rest.Download += p.Download
			rest.TotalUpload += p.TotalUpload
			rest.TotalDownload += p.TotalDownload
		}
	}
	if rest.Upload+rest.Download > 0 || rest.TotalUpload+rest.TotalDownload > 0 {
		byGroup = append(byGroup, rest)
	}
	return byPanel, byGroup
}

type trafficHistoryPoint struct {
	Ts       int64 `json:"ts"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}

func (s *Server) handleOverviewTrafficHistory(c *gin.Context) {
	rangeParam := strings.TrimSpace(c.DefaultQuery("range", "24h"))
	var (
		granularity string
		slotCount   int
	)
	switch rangeParam {
	case "24h":
		granularity = "hour"
		slotCount = 24
	case "7d":
		granularity = "day"
		slotCount = 7
	case "30d":
		granularity = "day"
		slotCount = 30
	default:
		failMsg(c, http.StatusBadRequest, "range must be one of 24h, 7d, 30d")
		return
	}

	var panelID uint
	if raw := strings.TrimSpace(c.Query("panelId")); raw != "" && raw != "0" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			failMsg(c, http.StatusBadRequest, "invalid panelId")
			return
		}
		panelID = uint(id)
	}
	var groupID uint
	if raw := strings.TrimSpace(c.Query("groupId")); raw != "" && raw != "0" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			failMsg(c, http.StatusBadRequest, "invalid groupId")
			return
		}
		groupID = uint(id)
	}

	loc := s.mgr.Loc()
	localNow := s.mgr.Now().In(loc)

	slots := make([]trafficHistoryPoint, slotCount)
	slotIndex := make(map[int64]int, slotCount)
	var startMs int64

	if granularity == "hour" {
		endHour := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), localNow.Hour(), 0, 0, 0, loc)
		startHour := endHour.Add(-time.Duration(slotCount-1) * time.Hour)
		startMs = startHour.UnixMilli()
		for i := 0; i < slotCount; i++ {
			ts := startHour.Add(time.Duration(i) * time.Hour).UnixMilli()
			slots[i] = trafficHistoryPoint{Ts: ts}
			slotIndex[ts] = i
		}
	} else {
		today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
		startDay := today.AddDate(0, 0, -(slotCount - 1))
		startMs = startDay.UnixMilli()
		for i := 0; i < slotCount; i++ {
			ts := startDay.AddDate(0, 0, i).UnixMilli()
			slots[i] = trafficHistoryPoint{Ts: ts}
			slotIndex[ts] = i
		}
	}

	var filterPanelIDs []uint
	emptyScope := false
	if panelID > 0 {
		filterPanelIDs = []uint{panelID}
	} else if groupID > 0 {
		spans, err := routing.GroupPanels(s.db, []uint{groupID})
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		filterPanelIDs = spans[groupID]
		if len(filterPanelIDs) == 0 {
			emptyScope = true
		}
	}

	if !emptyScope {
		var rows []model.TrafficBucket
		q := s.db.Where("bucket_ts >= ?", startMs)
		if len(filterPanelIDs) > 0 {
			q = q.Where("panel_id IN ?", filterPanelIDs)
		}
		if err := q.Find(&rows).Error; err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		for _, r := range rows {
			var key int64
			if granularity == "hour" {
				t := time.UnixMilli(r.BucketTs).In(loc)
				key = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc).UnixMilli()
			} else {
				t := time.UnixMilli(r.BucketTs).In(loc)
				key = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).UnixMilli()
			}
			if idx, ok := slotIndex[key]; ok {
				slots[idx].Upload += r.Upload
				slots[idx].Download += r.Download
			}
		}
	}

	var totalUp, totalDown, peakTotal, peakTs int64
	for _, slot := range slots {
		totalUp += slot.Upload
		totalDown += slot.Download
		sum := slot.Upload + slot.Download
		if sum > peakTotal {
			peakTotal = sum
			peakTs = slot.Ts
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"range":         rangeParam,
		"granularity":   granularity,
		"totalUpload":   totalUp,
		"totalDownload": totalDown,
		"peakBucketTs":  peakTs,
		"peakTotal":     peakTotal,
		"buckets":       slots,
	})
}
