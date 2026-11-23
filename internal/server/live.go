package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/PFXDev/FireX/internal/model"
	"github.com/PFXDev/FireX/internal/panel"
	"github.com/PFXDev/FireX/internal/provision"
	"github.com/PFXDev/FireX/internal/store"
)

const (
	defaultLiveInterval         = 2 * time.Second
	defaultLivePollTimeout      = 2500 * time.Millisecond
	defaultLiveIdleGrace        = 2 * time.Second
	defaultLiveHeartbeat        = 15 * time.Second
	defaultLiveHistoryCap       = 60
	defaultLiveHistoryRetention = 2 * time.Minute
	liveBackoffThreshold        = 2
	liveBackoffSkipTicks        = 4
	highDiskThresholdPercent    = 85.0
)

type LivePanelStat struct {
	ID            uint     `json:"id"`
	Name          string   `json:"name"`
	Online        bool     `json:"online"`
	Error         string   `json:"error,omitempty"`
	RttMs         int64    `json:"rttMs"`
	CPU           *float64 `json:"cpu"`
	CPUCores      int      `json:"cpuCores"`
	MemUsed       uint64   `json:"memUsed"`
	MemTotal      uint64   `json:"memTotal"`
	MemPercent    float64  `json:"memPercent"`
	DiskUsed      uint64   `json:"diskUsed"`
	DiskTotal     uint64   `json:"diskTotal"`
	DiskPercent   float64  `json:"diskPercent"`
	Load1         float64  `json:"load1"`
	Load5         float64  `json:"load5"`
	Load15        float64  `json:"load15"`
	LoadIndex     *float64 `json:"loadIndex"`
	NetUp         uint64   `json:"netUp"`
	NetDown       uint64   `json:"netDown"`
	TCPCount      int      `json:"tcpCount"`
	UDPCount      int      `json:"udpCount"`
	OnlineUsers   *int     `json:"onlineUsers"`
	OnlineClients *int     `json:"onlineClients"`
	XrayState     string   `json:"xrayState"`
	XrayError     string   `json:"xrayError,omitempty"`
	XrayVersion   string   `json:"xrayVersion"`
	PanelVersion  string   `json:"panelVersion,omitempty"`
	Uptime        uint64   `json:"uptime"`
	SampledAt     int64    `json:"sampledAt"`
}

type LiveOnlineUser struct {
	Username string   `json:"username"`
	Panels   []string `json:"panels"`
}

type LiveMetricExtreme struct {
	Value     float64 `json:"value"`
	PanelID   uint    `json:"panelId"`
	PanelName string  `json:"panelName"`
}

type LiveMetricStats struct {
	Samples int               `json:"samples"`
	Avg     float64           `json:"avg"`
	Min     LiveMetricExtreme `json:"min"`
	Max     LiveMetricExtreme `json:"max"`
}

type LiveTrendPoint struct {
	T           int64    `json:"t"`
	NetUp       uint64   `json:"netUp"`
	NetDown     uint64   `json:"netDown"`
	CPUAvg      *float64 `json:"cpuAvg"`
	MemAvg      float64  `json:"memAvg"`
	LoadAvg     *float64 `json:"loadAvg"`
	SystemLoad  *float64 `json:"systemLoad"`
	Connections int      `json:"connections"`
	OnlineUsers *int     `json:"onlineUsers"`
}

type LiveSummary struct {
	TotalPanels        int             `json:"totalPanels"`
	OnlinePanels       int             `json:"onlinePanels"`
	ActivePanels       int             `json:"activePanels"`
	XrayAbnormalPanels int             `json:"xrayAbnormalPanels"`
	HighDiskPanels     int             `json:"highDiskPanels"`
	TotalNetUp         uint64          `json:"totalNetUp"`
	TotalNetDown       uint64          `json:"totalNetDown"`
	TotalTCP           int             `json:"totalTcp"`
	TotalUDP           int             `json:"totalUdp"`
	TotalConnections   int             `json:"totalConnections"`
	OnlineUsers        int             `json:"onlineUsers"`
	UserSamplePanels   int             `json:"userSamplePanels"`
	OnlineUserSessions int             `json:"onlineUserSessions"`
	OnlineClients      int             `json:"onlineClients"`
	TotalCPUCores      int             `json:"totalCpuCores"`
	TotalMemUsed       uint64          `json:"totalMemUsed"`
	TotalMemTotal      uint64          `json:"totalMemTotal"`
	FleetMemPercent    float64         `json:"fleetMemPercent"`
	FleetLoad1         float64         `json:"fleetLoad1"`
	FleetLoad5         float64         `json:"fleetLoad5"`
	FleetLoad15        float64         `json:"fleetLoad15"`
	FleetLoadPerCore   float64         `json:"fleetLoadPerCore"`
	SystemLoadIndex    float64         `json:"systemLoadIndex"`
	SystemLoadLevel    string          `json:"systemLoadLevel"`
	CPU                LiveMetricStats `json:"cpu"`
	Mem                LiveMetricStats `json:"mem"`
	LoadIndex          LiveMetricStats `json:"loadIndex"`
	Rtt                LiveMetricStats `json:"rtt"`
	Disk               LiveMetricStats `json:"disk"`
}

type LiveSnapshot struct {
	Timestamp   int64            `json:"timestamp"`
	IntervalMs  int64            `json:"intervalMs"`
	Summary     LiveSummary      `json:"summary"`
	Panels      []LivePanelStat  `json:"panels"`
	OnlineUsers []LiveOnlineUser `json:"onlineUsers,omitempty"`
	Point       LiveTrendPoint   `json:"point"`
	History     []LiveTrendPoint `json:"history,omitempty"`
}

type panelFailState struct {
	failures      int
	skipRemaining int
	lastError     string
	lastSampledAt int64
}

type liveSubscriber struct {
	ch           chan LiveSnapshot
	interval     time.Duration // 0 means use hub default interval
	lastSentAt   time.Time
	needsHistory bool
}

// LiveHub coordinates strictly on-demand panel telemetry polling for active
// SSE viewers. Zero subscribers means zero background goroutines and zero
// requests to remote 3x-ui panels. Recent trend history is retained briefly
// across page switches so returning to the overview does not reset sparklines.
type LiveHub struct {
	db  *store.DB
	mgr *provision.Manager

	interval         time.Duration
	pollTimeout      time.Duration
	idleGrace        time.Duration
	heartbeat        time.Duration
	historyCap       int
	historyRetention time.Duration

	mu              sync.Mutex
	subs            map[chan LiveSnapshot]*liveSubscriber
	closed          bool
	stopCh          chan struct{}
	epoch           uint64
	activeInterval  time.Duration
	intervalChanged chan struct{}
	collectCancel   context.CancelFunc
	idleTimer       *time.Timer
	last            *LiveSnapshot
	history         []LiveTrendPoint
	failState       map[uint]*panelFailState
}

func newLiveHub(db *store.DB, mgr *provision.Manager) *LiveHub {
	return &LiveHub{
		db:               db,
		mgr:              mgr,
		interval:         defaultLiveInterval,
		pollTimeout:      defaultLivePollTimeout,
		idleGrace:        defaultLiveIdleGrace,
		heartbeat:        defaultLiveHeartbeat,
		historyCap:       defaultLiveHistoryCap,
		historyRetention: defaultLiveHistoryRetention,
		subs:             make(map[chan LiveSnapshot]*liveSubscriber),
		stopCh:           make(chan struct{}),
		intervalChanged:  make(chan struct{}, 1),
		failState:        make(map[uint]*panelFailState),
	}
}

// Done is closed when the hub shuts down so active SSE handlers exit cleanly.
func (h *LiveHub) Done() <-chan struct{} { return h.stopCh }

// Subscribe registers an SSE viewer using the hub's default sampling interval.
func (h *LiveHub) Subscribe() (chan LiveSnapshot, *LiveSnapshot, bool) {
	return h.SubscribeWithInterval(0)
}

// SubscribeWithInterval registers an SSE viewer with an optional preferred
// sampling interval (0 follows the hub default), starting the shared collector
// on 0 -> 1 and returning any recent catch-up snapshot with buffered history.
func (h *LiveHub) SubscribeWithInterval(requested time.Duration) (chan LiveSnapshot, *LiveSnapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, false
	}

	if h.idleTimer != nil {
		h.idleTimer.Stop()
		h.idleTimer = nil
	}

	h.pruneStaleHistoryLocked(time.Now().UnixMilli())

	ch := make(chan LiveSnapshot, 4)
	sub := &liveSubscriber{
		ch:           ch,
		interval:     requested,
		needsHistory: len(h.history) > 0,
	}
	h.subs[ch] = sub

	subInterval := h.subIntervalLocked(sub)
	var initial *LiveSnapshot
	if h.last != nil {
		snap := *h.last
		snap.IntervalMs = subInterval.Milliseconds()
		if len(h.history) > 0 {
			snap.History = append([]LiveTrendPoint(nil), h.history...)
		}
		initial = &snap
		sub.needsHistory = false
		sub.lastSentAt = time.Now()
	}

	effective := h.effectiveIntervalLocked()
	if h.collectCancel == nil {
		h.epoch++
		epoch := h.epoch
		ctx, cancel := context.WithCancel(context.Background())
		h.collectCancel = cancel
		h.activeInterval = effective
		go h.run(ctx, epoch, effective)
	} else if effective != h.activeInterval {
		h.activeInterval = effective
		h.signalIntervalChangedLocked()
	}

	return ch, initial, true
}

// Unsubscribe removes a viewer and stops the collector after idleGrace once no
// viewers remain.
func (h *LiveHub) Unsubscribe(ch chan LiveSnapshot) {
	if ch == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.subs[ch]; !exists {
		return
	}
	delete(h.subs, ch)

	if len(h.subs) == 0 && h.collectCancel != nil && !h.closed {
		expectedEpoch := h.epoch
		if h.idleGrace <= 0 {
			h.stopCollectorLocked(false)
			return
		}
		if h.idleTimer != nil {
			h.idleTimer.Stop()
		}
		h.idleTimer = time.AfterFunc(h.idleGrace, func() {
			h.stopIdle(expectedEpoch)
		})
		return
	}

	if len(h.subs) > 0 && h.collectCancel != nil && !h.closed {
		effective := h.effectiveIntervalLocked()
		if effective != h.activeInterval {
			h.activeInterval = effective
			h.signalIntervalChangedLocked()
		}
	}
}

func (h *LiveHub) subIntervalLocked(sub *liveSubscriber) time.Duration {
	if sub.interval > 0 {
		return sub.interval
	}
	return h.interval
}

func (h *LiveHub) effectiveIntervalLocked() time.Duration {
	var minInterval time.Duration
	for _, sub := range h.subs {
		d := h.subIntervalLocked(sub)
		if minInterval == 0 || d < minInterval {
			minInterval = d
		}
	}
	if minInterval <= 0 {
		return h.interval
	}
	return minInterval
}

func (h *LiveHub) signalIntervalChangedLocked() {
	select {
	case h.intervalChanged <- struct{}{}:
	default:
	}
}

func (h *LiveHub) pruneStaleHistoryLocked(nowMs int64) {
	if h.historyRetention <= 0 || len(h.history) == 0 {
		return
	}
	cutoff := nowMs - h.historyRetention.Milliseconds()
	keep := 0
	for keep < len(h.history) && h.history[keep].T < cutoff {
		keep++
	}
	if keep == 0 {
		return
	}
	if keep >= len(h.history) {
		h.history = nil
		return
	}
	h.history = append([]LiveTrendPoint(nil), h.history[keep:]...)
}

func (h *LiveHub) stopIdle(expectedEpoch uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.subs) > 0 || h.epoch != expectedEpoch {
		return
	}
	h.stopCollectorLocked(false)
}

func (h *LiveHub) stopCollectorLocked(clearHistory bool) {
	if h.idleTimer != nil {
		h.idleTimer.Stop()
		h.idleTimer = nil
	}
	if h.collectCancel != nil {
		h.collectCancel()
		h.collectCancel = nil
	}
	h.epoch++
	h.activeInterval = 0
	h.last = nil
	if clearHistory {
		h.history = nil
	}
	h.failState = make(map[uint]*panelFailState)
}

// Close permanently stops the hub and unblocks all active SSE handlers.
func (h *LiveHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.stopCh)
	h.stopCollectorLocked(true)
}

func (h *LiveHub) run(ctx context.Context, epoch uint64, interval time.Duration) {
	h.collectOnce(ctx, epoch)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.intervalChanged:
			h.mu.Lock()
			next := h.activeInterval
			h.mu.Unlock()
			if next > 0 && next != interval {
				interval = next
				ticker.Reset(interval)
			}
		case <-ticker.C:
			h.collectOnce(ctx, epoch)
		}
	}
}

type onlineUserEntry struct {
	key      string
	username string
}

type panelPollResult struct {
	stat         LivePanelStat
	users        []onlineUserEntry
	clientEmails []string
	probed       bool
	failed       bool
}

func (h *LiveHub) collectOnce(ctx context.Context, epoch uint64) {
	if ctx.Err() != nil {
		return
	}

	var panels []model.Panel
	if err := h.db.Where("enabled = ?", true).Order("id ASC").Find(&panels).Error; err != nil {
		return
	}

	h.mu.Lock()
	if h.closed || h.epoch != epoch || ctx.Err() != nil {
		h.mu.Unlock()
		return
	}
	pollTimeout := h.pollTimeout
	intervalMs := h.activeInterval.Milliseconds()
	if intervalMs <= 0 {
		intervalMs = h.interval.Milliseconds()
	}
	historyCap := h.historyCap
	skipMap := make(map[uint]*panelFailState, len(panels))
	for _, p := range panels {
		if st, ok := h.failState[p.ID]; ok {
			copied := *st
			skipMap[p.ID] = &copied
		}
	}
	h.mu.Unlock()

	results := make([]panelPollResult, len(panels))
	var wg sync.WaitGroup
	for i := range panels {
		p := panels[i]
		prevFail := skipMap[p.ID]
		if prevFail != nil && prevFail.skipRemaining > 0 {
			results[i] = panelPollResult{
				stat: LivePanelStat{
					ID:        p.ID,
					Name:      p.Name,
					Online:    false,
					Error:     prevFail.lastError,
					SampledAt: prevFail.lastSampledAt,
				},
			}
			continue
		}

		wg.Add(1)
		go func(idx int, p model.Panel) {
			defer wg.Done()
			results[idx] = h.pollPanel(ctx, &p, pollTimeout)
		}(i, p)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return
	}

	nowTime := time.Now()
	now := nowTime.UnixMilli()
	stats := make([]LivePanelStat, len(panels))
	uniqueUsers := make(map[string]*LiveOnlineUser)
	uniqueClients := make(map[string]struct{})
	for i, res := range results {
		stats[i] = res.stat
		if res.stat.Online {
			for _, u := range res.users {
				entry, ok := uniqueUsers[u.key]
				if !ok {
					entry = &LiveOnlineUser{Username: u.username}
					uniqueUsers[u.key] = entry
				}
				entry.Panels = append(entry.Panels, res.stat.Name)
			}
			for _, em := range res.clientEmails {
				uniqueClients[em] = struct{}{}
			}
		}
	}

	onlineUserList := make([]LiveOnlineUser, 0, len(uniqueUsers))
	for _, entry := range uniqueUsers {
		sort.Strings(entry.Panels)
		onlineUserList = append(onlineUserList, *entry)
	}
	sort.Slice(onlineUserList, func(i, j int) bool {
		if len(onlineUserList[i].Panels) != len(onlineUserList[j].Panels) {
			return len(onlineUserList[i].Panels) > len(onlineUserList[j].Panels)
		}
		return strings.ToLower(onlineUserList[i].Username) < strings.ToLower(onlineUserList[j].Username)
	})

	summary := buildLiveSummary(stats, len(uniqueUsers), len(uniqueClients))
	point := LiveTrendPoint{
		T:           now,
		NetUp:       summary.TotalNetUp,
		NetDown:     summary.TotalNetDown,
		MemAvg:      summary.Mem.Avg,
		Connections: summary.TotalConnections,
	}

	if summary.CPU.Samples > 0 {
		point.CPUAvg = &summary.CPU.Avg
		point.LoadAvg = &summary.LoadIndex.Avg
		point.SystemLoad = &summary.SystemLoadIndex
	}
	if summary.UserSamplePanels > 0 && summary.UserSamplePanels == summary.OnlinePanels {
		point.OnlineUsers = &summary.OnlineUsers
	}

	snap := LiveSnapshot{
		Timestamp:   now,
		IntervalMs:  intervalMs,
		Summary:     summary,
		Panels:      stats,
		OnlineUsers: onlineUserList,
		Point:       point,
	}

	h.mu.Lock()
	if h.closed || h.epoch != epoch || ctx.Err() != nil {
		h.mu.Unlock()
		return
	}

	// Update per-panel backoff state and prune panels that were disabled/deleted.
	activeIDs := make(map[uint]struct{}, len(panels))
	for i, p := range panels {
		activeIDs[p.ID] = struct{}{}
		res := results[i]
		if !res.probed {
			if st, ok := h.failState[p.ID]; ok && st.skipRemaining > 0 {
				st.skipRemaining--
			}
			continue
		}
		if res.failed {
			st, ok := h.failState[p.ID]
			if !ok {
				st = &panelFailState{}
				h.failState[p.ID] = st
			}
			st.failures++
			st.lastError = res.stat.Error
			st.lastSampledAt = res.stat.SampledAt
			if st.failures >= liveBackoffThreshold {
				st.skipRemaining = liveBackoffSkipTicks
			}
		} else {
			delete(h.failState, p.ID)
		}
	}
	for id := range h.failState {
		if _, ok := activeIDs[id]; !ok {
			delete(h.failState, id)
		}
	}

	h.history = append(h.history, point)
	if historyCap > 0 && len(h.history) > historyCap {
		h.history = append([]LiveTrendPoint(nil), h.history[len(h.history)-historyCap:]...)
	}

	// Published panel slices are immutable, including the non-nil empty slice.
	h.last = &snap

	for _, sub := range h.subs {
		subInterval := h.subIntervalLocked(sub)
		if !sub.lastSentAt.IsZero() && sub.interval > 0 {
			// Allow slight ticker jitter so a 5s subscriber on a 2s collector
			// does not slip an entire extra cycle.
			if nowTime.Sub(sub.lastSentAt) < subInterval-250*time.Millisecond {
				continue
			}
		}
		frame := snap
		frame.IntervalMs = subInterval.Milliseconds()
		if sub.needsHistory && len(h.history) > 0 {
			frame.History = append([]LiveTrendPoint(nil), h.history...)
			sub.needsHistory = false
		}
		sub.lastSentAt = nowTime
		select {
		case sub.ch <- frame:
		default:
			// Drop one stale buffered frame so slow consumers catch up to the latest tick.
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- frame:
			default:
			}
		}
	}
	h.mu.Unlock()
}

func (h *LiveHub) pollPanel(ctx context.Context, p *model.Panel, timeout time.Duration) panelPollResult {
	pCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := h.mgr.ClientFor(p)

	var (
		status    *panel.Status
		statusErr error
		rttMs     int64
		onlines   []string
		onlineErr error
		wg        sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		start := time.Now()
		status, statusErr = client.StatusLive(pCtx)
		rttMs = max(int64(0), time.Since(start).Milliseconds())
	}()
	go func() {
		defer wg.Done()
		onlines, onlineErr = client.OnlineClients(pCtx)
	}()
	wg.Wait()

	sampledAt := time.Now().UnixMilli()
	if statusErr != nil {
		return panelPollResult{
			stat: LivePanelStat{
				ID:        p.ID,
				Name:      p.Name,
				Online:    false,
				Error:     statusErr.Error(),
				SampledAt: sampledAt,
			},
			probed: true,
			failed: true,
		}
	}

	cores := status.LogicalPro
	if cores <= 0 {
		cores = status.CpuCores
	}
	if cores <= 0 {
		cores = 1
	}

	cpuPct := round1(clampFloat(status.Cpu, 0, 100))
	var memPct float64
	if status.Mem.Total > 0 {
		memPct = round1(clampFloat(float64(status.Mem.Current)/float64(status.Mem.Total)*100, 0, 100))
	}
	var diskPct float64
	if status.Disk.Total > 0 {
		diskPct = round1(clampFloat(float64(status.Disk.Current)/float64(status.Disk.Total)*100, 0, 100))
	}

	var load1, load5, load15 float64
	if len(status.Loads) > 0 {
		load1 = round2(max(0.0, status.Loads[0]))
	}
	if len(status.Loads) > 1 {
		load5 = round2(max(0.0, status.Loads[1]))
	}
	if len(status.Loads) > 2 {
		load15 = round2(max(0.0, status.Loads[2]))
	}

	var loadIdx float64
	if len(status.Loads) > 0 {
		loadNorm := clampFloat(load1/float64(cores)*100, 0, 100)
		loadIdx = round1(0.45*cpuPct + 0.30*memPct + 0.25*loadNorm)
	} else {
		loadIdx = round1(0.60*cpuPct + 0.40*memPct)
	}

	var cpu, loadIndex *float64
	if !slices.Contains(status.UnavailableMetrics, "cpu") {
		cpu, loadIndex = &cpuPct, &loadIdx
	}

	var userCount, clientCount *int
	var users []onlineUserEntry
	var clientEmails []string
	if onlineErr == nil {
		seen := make(map[string]struct{}, len(onlines))
		suffix := strings.ToLower(provision.EmailSuffix)
		suffixLen := len(provision.EmailSuffix)
		for _, raw := range onlines {
			trimmed := strings.TrimSpace(raw)
			norm := strings.ToLower(trimmed)
			if norm == "" {
				continue
			}
			if _, ok := seen[norm]; ok {
				continue
			}
			seen[norm] = struct{}{}
			clientEmails = append(clientEmails, norm)
			if strings.HasSuffix(norm, suffix) {
				username := trimmed[:len(trimmed)-suffixLen]
				if username == "" {
					username = trimmed
				}
				users = append(users, onlineUserEntry{key: norm, username: username})
			}
		}
		uCount, cCount := len(users), len(clientEmails)
		userCount, clientCount = &uCount, &cCount
	}

	return panelPollResult{
		stat: LivePanelStat{
			ID:            p.ID,
			Name:          p.Name,
			Online:        true,
			RttMs:         rttMs,
			CPU:           cpu,
			CPUCores:      cores,
			MemUsed:       status.Mem.Current,
			MemTotal:      status.Mem.Total,
			MemPercent:    memPct,
			DiskUsed:      status.Disk.Current,
			DiskTotal:     status.Disk.Total,
			DiskPercent:   diskPct,
			Load1:         load1,
			Load5:         load5,
			Load15:        load15,
			LoadIndex:     loadIndex,
			NetUp:         status.NetIO.Up,
			NetDown:       status.NetIO.Down,
			TCPCount:      max(0, status.TcpCount),
			UDPCount:      max(0, status.UdpCount),
			OnlineUsers:   userCount,
			OnlineClients: clientCount,
			XrayState:     status.Xray.State,
			XrayError:     status.Xray.ErrorMsg,
			XrayVersion:   status.Xray.Version,
			PanelVersion:  status.PanelVersion,
			Uptime:        status.Uptime,
			SampledAt:     sampledAt,
		},
		users:        users,
		clientEmails: clientEmails,
		probed:       true,
	}
}

func buildLiveSummary(stats []LivePanelStat, uniqueUsers, uniqueClients int) LiveSummary {
	summary := LiveSummary{
		TotalPanels:   len(stats),
		OnlineUsers:   uniqueUsers,
		OnlineClients: uniqueClients,
	}

	for _, st := range stats {
		if !st.Online {
			continue
		}
		summary.OnlinePanels++
		// Active means actual traffic throughput or connected proxy clients,
		// not merely host TCP sockets (which FireX's own probe always opens).
		if st.NetUp+st.NetDown > 0 || (st.OnlineClients != nil && *st.OnlineClients > 0) {
			summary.ActivePanels++
		}
		if st.XrayState != "" && !strings.EqualFold(st.XrayState, "running") {
			summary.XrayAbnormalPanels++
		}
		summary.TotalNetUp += st.NetUp
		summary.TotalNetDown += st.NetDown
		summary.TotalTCP += st.TCPCount
		summary.TotalUDP += st.UDPCount
		if st.OnlineUsers != nil {
			summary.OnlineUserSessions += *st.OnlineUsers
			summary.UserSamplePanels++
		}
		summary.TotalCPUCores += st.CPUCores
		summary.TotalMemUsed += st.MemUsed
		summary.TotalMemTotal += st.MemTotal
		summary.FleetLoad1 += st.Load1
		summary.FleetLoad5 += st.Load5
		summary.FleetLoad15 += st.Load15

		summary.Mem.add(st.MemPercent, st)
		summary.Rtt.add(float64(st.RttMs), st)
		if st.DiskTotal > 0 {
			summary.Disk.add(st.DiskPercent, st)
			if st.DiskPercent >= highDiskThresholdPercent {
				summary.HighDiskPanels++
			}
		}
		if st.CPU != nil {
			summary.CPU.add(*st.CPU, st)
			summary.LoadIndex.add(*st.LoadIndex, st)
		}
	}

	summary.TotalConnections = summary.TotalTCP + summary.TotalUDP
	if summary.TotalMemTotal > 0 {
		summary.FleetMemPercent = round1(clampFloat(float64(summary.TotalMemUsed)/float64(summary.TotalMemTotal)*100, 0, 100))
	}
	summary.FleetLoad1 = round2(summary.FleetLoad1)
	summary.FleetLoad5 = round2(summary.FleetLoad5)
	summary.FleetLoad15 = round2(summary.FleetLoad15)
	if summary.TotalCPUCores > 0 {
		summary.FleetLoadPerCore = round2(summary.FleetLoad1 / float64(summary.TotalCPUCores))
	}

	for _, metric := range []*LiveMetricStats{&summary.CPU, &summary.Mem, &summary.LoadIndex, &summary.Rtt, &summary.Disk} {
		if metric.Samples > 0 {
			metric.Avg = round1(metric.Avg / float64(metric.Samples))
		}
	}
	if summary.LoadIndex.Samples == 0 {
		summary.SystemLoadLevel = "unknown"
		return summary
	}

	summary.SystemLoadIndex = round1(0.75*summary.LoadIndex.Avg + 0.25*summary.LoadIndex.Max.Value)
	switch {
	case summary.SystemLoadIndex < 35:
		summary.SystemLoadLevel = "idle"
	case summary.SystemLoadIndex < 65:
		summary.SystemLoadLevel = "normal"
	case summary.SystemLoadIndex < 85:
		summary.SystemLoadLevel = "busy"
	default:
		summary.SystemLoadLevel = "high"
	}
	return summary
}

func (m *LiveMetricStats) add(value float64, panel LivePanelStat) {
	extreme := LiveMetricExtreme{Value: value, PanelID: panel.ID, PanelName: panel.Name}
	if m.Samples == 0 || value < m.Min.Value {
		m.Min = extreme
	}
	if m.Samples == 0 || value > m.Max.Value {
		m.Max = extreme
	}
	m.Samples++
	m.Avg += value
}

func clampFloat(v, minVal, maxVal float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, -1) || v < minVal {
		return minVal
	}
	if math.IsInf(v, 1) || v > maxVal {
		return maxVal
	}
	return v
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func parseLiveInterval(raw string) time.Duration {
	switch strings.TrimSpace(raw) {
	case "2":
		return 2 * time.Second
	case "5":
		return 5 * time.Second
	case "10":
		return 10 * time.Second
	default:
		return 0
	}
}

func (s *Server) handleOverviewLive(c *gin.Context) {
	sess := c.MustGet(ctxSessionKey).(*model.Session)
	ctx, cancel := context.WithDeadline(c.Request.Context(), time.UnixMilli(sess.ExpiresAt))
	defer cancel()
	// Recheck before each write so revoked sessions cannot receive the next frame.
	validSession := func() bool {
		var count int64
		err := s.db.WithContext(ctx).Model(&model.Session{}).
			Joins("JOIN admins ON admins.id = sessions.admin_id").
			Where("sessions.token = ? AND sessions.admin_id = ? AND expires_at > ?", sess.Token, sess.AdminID, time.Now().UnixMilli()).
			Count(&count).Error
		return err == nil && count == 1 && ctx.Err() == nil
	}
	ch, initial, ok := s.live.SubscribeWithInterval(parseLiveInterval(c.Query("interval")))
	if !ok {
		failMsg(c, http.StatusServiceUnavailable, "实时监控服务正在关闭")
		return
	}
	defer s.live.Unsubscribe(ch)

	hdr := c.Writer.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("Cache-Control", "no-cache, no-transform")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()

	if initial != nil {
		if !validSession() || !writeSSEEvent(c.Writer, "snapshot", initial) {
			return
		}
	}

	hbInterval := s.live.heartbeat
	if hbInterval <= 0 {
		hbInterval = defaultLiveHeartbeat
	}
	ping := time.NewTicker(hbInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.live.Done():
			return
		case snap := <-ch:
			if !validSession() || !writeSSEEvent(c.Writer, "snapshot", snap) {
				return
			}
		case <-ping.C:
			if !validSession() || !writeSSEComment(c.Writer, "ping") {
				return
			}
		}
	}
}

func writeSSEEvent(w gin.ResponseWriter, event string, payload any) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return false
	}
	w.Flush()
	return true
}

func writeSSEComment(w gin.ResponseWriter, comment string) bool {
	if _, err := fmt.Fprintf(w, ": %s\n\n", comment); err != nil {
		return false
	}
	w.Flush()
	return true
}
