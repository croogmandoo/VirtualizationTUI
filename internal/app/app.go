// Package app implements the Bubble Tea root model: the app shell that wires the
// connections sidebar, resource table, detail view (with sparklines), action
// confirmations and help overlay to the application core. It contains no
// platform-specific logic — everything flows through core.Session and the
// provider abstraction.
package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/croogmandoo/virtualizationtui/internal/config"
	"github.com/croogmandoo/virtualizationtui/internal/core"
	"github.com/croogmandoo/virtualizationtui/internal/provider"
	"github.com/croogmandoo/virtualizationtui/internal/ui"
)

type focus int

const (
	focusSidebar focus = iota
	focusTable
)

type viewMode int

const (
	viewList viewMode = iota
	viewDetail
)

// Model is the Bubble Tea root model.
type Model struct {
	session *core.Session
	cfg     config.Config
	styles  ui.Styles
	keys    ui.KeyMap
	theme   string

	conns   []*core.Connection
	selConn int

	table  table.Model
	rows   []provider.Resource // parallel to table rows (current connection inventory)
	cols   []string            // dynamic field-column keys, in display order
	detail viewport.Model

	// metricHist accumulates per-resource metric history across polls, keyed by
	// resource ID then metric name, so sparklines build up even when a provider
	// only reports point-in-time values.
	metricHist map[string]map[string][]float64

	focus    focus
	mode     viewMode
	showHelp bool

	// confirm modal state
	confirm     bool
	pending     provider.Action
	pendingName string
	pendingDesc string

	status string
	width  int
	height int
	mainW  int // current main-pane content width, for column sizing
	ready  bool
}

// New builds the root model from a session and config.
func New(session *core.Session, cfg config.Config) Model {
	theme := cfg.UI.Theme
	if theme == "" {
		theme = ui.DefaultTheme
	}
	return Model{
		session:    session,
		cfg:        cfg,
		styles:     ui.NewStyles(ui.ThemeByName(theme)),
		keys:       ui.DefaultKeyMap(),
		theme:      theme,
		conns:      session.Connections(),
		focus:      focusSidebar,
		mode:       viewList,
		status:     "ready",
		metricHist: map[string]map[string][]float64{},
	}
}

// --- messages ---

type inventoryMsg struct {
	conn      string
	resources []provider.Resource
	err       error
}

type actionMsg struct {
	result provider.ActionResult
	err    error
}

type connectedMsg struct {
	conn string
	err  error
}

type tickMsg time.Time

// Init connects the first connection and starts the poll ticker.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if len(m.conns) > 0 {
		cmds = append(cmds, m.connectCmd(m.conns[0].Cfg.Name))
	}
	cmds = append(cmds, tickCmd(m.cfg.UI.PollInterval))
	return tea.Batch(cmds...)
}

func tickCmd(d time.Duration) tea.Cmd {
	if d <= 0 {
		d = 5 * time.Second
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) connectCmd(name string) tea.Cmd {
	s := m.session
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := s.Connect(ctx, name)
		return connectedMsg{conn: name, err: err}
	}
}

func (m Model) loadCmd(name string) tea.Cmd {
	s := m.session
	conn, ok := s.Get(name)
	if !ok || conn.Provider == nil {
		return nil
	}
	kinds := conn.Provider.Kinds()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var all []provider.Resource
		for _, k := range kinds {
			rs, err := s.List(ctx, name, k)
			if err != nil {
				return inventoryMsg{conn: name, err: err}
			}
			all = append(all, rs...)
		}
		sort.SliceStable(all, func(i, j int) bool {
			if all[i].Kind != all[j].Kind {
				return all[i].Kind < all[j].Kind
			}
			return all[i].Name < all[j].Name
		})
		return inventoryMsg{conn: name, resources: all}
	}
}

func (m Model) doCmd(name string, a provider.Action) tea.Cmd {
	s := m.session
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := s.Do(ctx, name, a)
		return actionMsg{result: res, err: err}
	}
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case connectedMsg:
		if msg.err != nil {
			m.status = "connect failed: " + msg.err.Error()
			return m, nil
		}
		m.status = "connected: " + msg.conn
		return m, m.loadCmd(msg.conn)

	case inventoryMsg:
		if msg.err != nil {
			m.status = "load failed: " + msg.err.Error()
			return m, nil
		}
		if m.currentConnName() == msg.conn {
			m.accumulateMetrics(msg.resources)
			m.rows = msg.resources
			m.rebuildTable()
			m.status = fmt.Sprintf("%s · %d resources · %s", msg.conn, len(msg.resources), time.Now().Format("15:04:05"))
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.status = "action failed: " + msg.err.Error()
			return m, nil
		}
		m.status = msg.result.Message
		return m, m.loadCmd(m.currentConnName())

	case tickMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, tickCmd(m.cfg.UI.PollInterval))
		if name := m.currentConnName(); name != "" {
			if c, ok := m.session.Get(name); ok && c.Connected {
				cmds = append(cmds, m.loadCmd(name))
			}
		}
		return m, tea.Batch(cmds...)
	}

	// Forward to the focused sub-component.
	var cmd tea.Cmd
	if m.mode == viewDetail {
		m.detail, cmd = m.detail.Update(msg)
	} else if m.focus == focusTable {
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Confirm modal intercepts input first.
	if m.confirm {
		switch msg.String() {
		case "y", "Y", "enter":
			m.confirm = false
			return m, m.doCmd(m.pendingName, m.pending)
		case "n", "N", "esc":
			m.confirm = false
			m.status = "cancelled"
			return m, nil
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil

	case m.showHelp:
		// Any other key closes help.
		m.showHelp = false
		return m, nil

	case key.Matches(msg, m.keys.Back):
		if m.mode == viewDetail {
			m.mode = viewList
		}
		return m, nil

	case key.Matches(msg, m.keys.Left):
		m.focus = focusSidebar
		return m, nil

	case key.Matches(msg, m.keys.Right):
		m.focus = focusTable
		m.table.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Refresh):
		m.status = "refreshing…"
		return m, m.loadCmd(m.currentConnName())

	case key.Matches(msg, m.keys.Theme):
		m.cycleTheme()
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if m.focus == focusSidebar {
			m.focus = focusTable
			m.table.Focus()
			return m, nil
		}
		if r, ok := m.selectedResource(); ok {
			m.mode = viewDetail
			m.renderDetail(r)
		}
		return m, nil

	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down):
		if m.focus == focusSidebar {
			cmd := m.moveSidebar(key.Matches(msg, m.keys.Down))
			return m, cmd
		}
	}

	// Action keys apply to the selected resource.
	if r, ok := m.selectedResource(); ok && m.focus == focusTable {
		switch {
		case key.Matches(msg, m.keys.Start):
			return m.askConfirm(r, "start", "Start "+r.Name+"?")
		case key.Matches(msg, m.keys.Stop):
			return m.askConfirm(r, "stop", "Stop "+r.Name+"?")
		case key.Matches(msg, m.keys.Reboot):
			return m.askConfirm(r, "reboot", "Reboot "+r.Name+"?")
		case key.Matches(msg, m.keys.Snapshot):
			return m.askConfirm(r, "snapshot", "Snapshot "+r.Name+"?")
		}
	}

	// Default: forward navigation to the table.
	if m.focus == focusTable && m.mode == viewList {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	if m.mode == viewDetail {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) askConfirm(r provider.Resource, verb, desc string) (tea.Model, tea.Cmd) {
	if m.session.ReadOnly() {
		m.status = "read-only mode: action blocked"
		return m, nil
	}
	m.confirm = true
	m.pending = provider.Action{Verb: verb, Kind: r.Kind, Target: r.ID}
	m.pendingName = m.currentConnName()
	m.pendingDesc = desc
	return m, nil
}

// --- selection helpers ---

func (m Model) currentConnName() string {
	if m.selConn < 0 || m.selConn >= len(m.conns) {
		return ""
	}
	return m.conns[m.selConn].Cfg.Name
}

// moveSidebar changes the selected connection and returns a command to connect
// (lazily) or reload inventory for the newly-selected connection.
func (m *Model) moveSidebar(down bool) tea.Cmd {
	prev := m.selConn
	if down {
		m.selConn++
	} else {
		m.selConn--
	}
	if m.selConn < 0 {
		m.selConn = 0
	}
	if m.selConn >= len(m.conns) {
		m.selConn = len(m.conns) - 1
	}
	if m.selConn == prev {
		return nil
	}
	m.rows = nil
	m.rebuildTable()
	name := m.currentConnName()
	c, ok := m.session.Get(name)
	if !ok {
		return nil
	}
	if c.Connected {
		return m.loadCmd(name)
	}
	m.status = "connecting to " + name + "…"
	return m.connectCmd(name)
}

func (m Model) selectedResource() (provider.Resource, bool) {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rows) {
		return provider.Resource{}, false
	}
	return m.rows[idx], true
}

// --- view construction ---

func (m *Model) layout() {
	if m.width == 0 {
		return
	}
	sidebarW := 26
	if m.width < 80 {
		sidebarW = 18
	}
	mainW := m.width - sidebarW - 6
	if mainW < 20 {
		mainW = 20
	}
	m.mainW = mainW
	bodyH := m.height - 4 // title + status bar + borders

	m.table = table.New(
		table.WithColumns(m.columnDefs()),
		table.WithFocused(m.focus == focusTable),
		table.WithHeight(bodyH-2),
	)
	m.applyTableStyles()

	m.detail = viewport.New(mainW, bodyH-2)
	m.rebuildTable()
}

// applyTableStyles paints the table header and selection in the current theme.
func (m *Model) applyTableStyles() {
	th := m.styles.Theme
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true).Foreground(th.Fg).BorderBottom(true)
	st.Selected = st.Selected.Bold(true).Foreground(th.Fg).Background(th.Accent)
	m.table.SetStyles(st)
}

// cycleTheme advances to the next registered theme and restyles the live UI in
// place, preserving the current selection and any open detail view.
func (m *Model) cycleTheme() {
	m.theme = ui.NextTheme(m.theme)
	m.styles = ui.NewStyles(ui.ThemeByName(m.theme))
	m.applyTableStyles()
	m.rebuildTable()
	if m.mode == viewDetail {
		if r, ok := m.selectedResource(); ok {
			m.renderDetail(r)
		}
	}
	m.status = "theme: " + m.theme
}

// accumulateMetrics threads metric history across polls. Providers that already
// report a multi-point History (e.g. the mock) keep their own; for point-in-time
// providers we append the current value to a per-resource rolling window so
// sparklines build up over successive polls. Stale resources are pruned because
// only currently-present IDs are carried into the next map.
func (m *Model) accumulateMetrics(rs []provider.Resource) {
	window := m.cfg.UI.MetricsWindow
	if window <= 0 {
		window = 24
	}
	next := make(map[string]map[string][]float64, len(rs))
	for i := range rs {
		r := &rs[i]
		for j := range r.Metrics {
			mt := &r.Metrics[j]
			if len(mt.History) > 1 {
				continue // provider manages its own history
			}
			h := append(m.metricHist[r.ID][mt.Name], mt.Value)
			if len(h) > window {
				h = h[len(h)-window:]
			}
			if next[r.ID] == nil {
				next[r.ID] = map[string][]float64{}
			}
			next[r.ID][mt.Name] = h
			mt.History = h
		}
	}
	m.metricHist = next
}

// fieldColumnPriority orders field keys when there is not enough width to show
// them all. Keys not listed here are appended afterwards in alphabetical order, so
// new provider fields still surface without a code change here.
var fieldColumnPriority = []string{
	"cpu", "mem", "ip", "node", "os", "type", "value", "ttl",
	"upstream", "match", "size", "used", "state", "uptime",
}

// selectFieldColumns derives the dynamic field columns from whatever fields the
// current rows actually carry — so a DNS connection shows type/value/ttl while a
// hypervisor shows cpu/mem, with no Kind-specific UI code.
func selectFieldColumns(rows []provider.Resource) []string {
	present := map[string]bool{}
	for _, r := range rows {
		for k := range r.Fields {
			present[k] = true
		}
	}
	var cols []string
	for _, k := range fieldColumnPriority {
		if present[k] {
			cols = append(cols, k)
			delete(present, k)
		}
	}
	rest := make([]string, 0, len(present))
	for k := range present {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	cols = append(cols, rest...)
	// Cap the number of field columns so the table stays readable on narrow terminals.
	const maxFieldCols = 5
	if len(cols) > maxFieldCols {
		cols = cols[:maxFieldCols]
	}
	return cols
}

// columnDefs builds the table column set from the current dynamic field columns
// and the available main-pane width.
func (m Model) columnDefs() []table.Column {
	const (
		stW    = 3
		kindW  = 10
		fieldW = 12
	)
	width := m.mainW
	if width <= 0 {
		width = 60
	}
	cols := []table.Column{{Title: "St", Width: stW}, {Title: "Name"}, {Title: "Kind", Width: kindW}}
	for _, k := range m.cols {
		cols = append(cols, table.Column{Title: columnTitle(k), Width: fieldW})
	}
	// Name takes whatever is left after the fixed/field columns (with a floor).
	used := stW + kindW + fieldW*len(m.cols) + 2*len(cols) // +2 per col for padding
	nameW := width - used
	if nameW < 12 {
		nameW = 12
	}
	cols[1].Width = nameW
	return cols
}

// columnTitle renders a field key as a short, human column header.
func columnTitle(k string) string {
	switch k {
	case "cpu":
		return "CPU"
	case "mem":
		return "Mem"
	case "ip":
		return "IP"
	case "ttl":
		return "TTL"
	case "os":
		return "OS"
	default:
		return strings.ToUpper(k[:1]) + k[1:]
	}
}

func (m *Model) rebuildTable() {
	m.cols = selectFieldColumns(m.rows)
	m.table.SetColumns(m.columnDefs())

	rows := make([]table.Row, 0, len(m.rows))
	for _, r := range m.rows {
		_, glyph := m.styles.Status(r.Status)
		cells := make(table.Row, 0, 3+len(m.cols))
		cells = append(cells, glyph, r.Name, string(r.Kind))
		for _, k := range m.cols {
			cells = append(cells, field(r, k))
		}
		rows = append(rows, cells)
	}
	m.table.SetRows(rows)
	// The bubbles table initialises its cursor to -1 until focused; keep a valid
	// selection whenever rows exist so action keys always have a target.
	if len(rows) > 0 && m.table.Cursor() < 0 {
		m.table.SetCursor(0)
	}
}

func field(r provider.Resource, k string) string {
	if v, ok := r.Fields[k]; ok {
		return v
	}
	return "-"
}

func (m *Model) renderDetail(r provider.Resource) {
	var b strings.Builder
	st, glyph := m.styles.Status(r.Status)
	fmt.Fprintf(&b, "%s %s  %s\n", st.Render(glyph), lipgloss.NewStyle().Bold(true).Render(r.Name), m.styles.Muted.Render(fmt.Sprintf("(%s · %s)", r.Kind, r.ID)))
	fmt.Fprintf(&b, "%s %s\n\n", m.styles.Muted.Render("status:"), string(r.Status))

	// Fields table.
	keys := make([]string, 0, len(r.Fields))
	for k := range r.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %-10s %s\n", k+":", r.Fields[k])
	}

	// Sparklines (DESIGN.md §6).
	if len(r.Metrics) > 0 {
		b.WriteString("\n" + m.styles.Muted.Render("metrics (rolling)") + "\n")
		for _, mt := range r.Metrics {
			spark := ui.Sparkline(mt.History)
			fmt.Fprintf(&b, "  %-5s %s %6.1f%s\n", mt.Name, spark, mt.Value, mt.Unit)
		}
	}
	m.detail.SetContent(b.String())
}

// View renders the whole UI.
func (m Model) View() string {
	if !m.ready {
		return "starting VirtualizationTUI…"
	}

	title := m.styles.Title.Render("VirtualizationTUI")
	hint := m.styles.Help.Render("  ? help   q quit")
	header := lipgloss.JoinHorizontal(lipgloss.Center, title, hint)

	sidebar := m.renderSidebar()
	var main string
	if m.mode == viewDetail {
		main = m.styles.Pane.Render(m.detail.View())
	} else {
		main = m.styles.Pane.Render(m.table.View())
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)

	status := m.styles.StatusBar.Width(m.width).Render(m.statusLine())
	screen := lipgloss.JoinVertical(lipgloss.Left, header, body, status)

	if m.showHelp {
		return m.overlay(screen, m.renderHelp())
	}
	if m.confirm {
		return m.overlay(screen, m.renderConfirm())
	}
	return screen
}

func (m Model) statusLine() string {
	mode := "list"
	if m.mode == viewDetail {
		mode = "detail"
	}
	ro := ""
	if m.session.ReadOnly() {
		ro = " · read-only"
	}
	return fmt.Sprintf("%s · %s%s", m.status, mode, ro)
}

func (m Model) renderSidebar() string {
	var lines []string
	lines = append(lines, m.styles.Muted.Render("Connections"))
	for i, c := range m.conns {
		label := fmt.Sprintf("%s (%s)", c.Cfg.Name, c.Cfg.Type)
		glyph := "○"
		if c.Connected {
			glyph = "●"
		} else if c.LastErr != nil {
			glyph = "✗"
		}
		line := fmt.Sprintf("%s %s", glyph, label)
		if i == m.selConn {
			line = m.styles.SidebarSel.Render("▸ " + line)
		} else {
			line = m.styles.SidebarItem.Render("  " + line)
		}
		lines = append(lines, line)
	}
	h := m.height - 4
	content := strings.Join(lines, "\n")
	return m.styles.Sidebar.Height(h).Render(content)
}

func (m Model) renderHelp() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Keybindings") + "\n\n")
	for _, group := range m.keys.FullHelp() {
		for _, kb := range group {
			h := kb.Help()
			fmt.Fprintf(&b, "  %s  %s\n", m.styles.Key.Render(fmt.Sprintf("%-7s", h.Key)), h.Desc)
		}
		b.WriteString("\n")
	}
	b.WriteString(m.styles.Muted.Render("press any key to close"))
	return m.styles.OverlayBox.Render(b.String())
}

func (m Model) renderConfirm() string {
	body := fmt.Sprintf("%s\n\n%s / %s",
		lipgloss.NewStyle().Bold(true).Render(m.pendingDesc),
		m.styles.Key.Render("y")+" "+m.styles.Muted.Render("confirm"),
		m.styles.Key.Render("n")+" "+m.styles.Muted.Render("cancel"))
	return m.styles.OverlayBox.Render(body)
}

// overlay centres a box over the base screen.
func (m Model) overlay(base, box string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
