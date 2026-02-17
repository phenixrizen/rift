package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/phenixrizen/rift/internal/discovery"
	"github.com/phenixrizen/rift/internal/state"
	"github.com/phenixrizen/rift/internal/version"
	"github.com/spf13/cobra"
)

func newUICmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Interactive Rift TUI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := app.loadState()
			if err != nil {
				return err
			}
			model := newUIModel(app, st)
			prog := tea.NewProgram(model, tea.WithAltScreen())
			_, err = prog.Run()
			return err
		},
	}
	return cmd
}

type syncDoneMsg struct {
	report SyncReport
	err    error
	logs   string
}

type authCheckDoneMsg struct {
	needsAuth bool
	err       error
}

type authDoneMsg struct {
	err  error
	logs string
}

type refreshDoneMsg struct {
	state state.State
	err   error
}

type useDoneMsg struct {
	context string
	err     error
	output  string
}

type k9sDoneMsg struct {
	context string
	err     error
}

type uiModel struct {
	app      *App
	state    state.State
	all      []state.ClusterRecord
	filtered []state.ClusterRecord
	table    table.Model
	search   textinput.Model
	searchOn bool
	status   string
	modalOn  bool
	modal    string
	modalHdr string
	modalVP  viewport.Model
	modalW   int
	spin     spinner.Model
	busy     bool
	busyText string
	width    int
	height   int
	commit   string
}

func newUIModel(app *App, st state.State) uiModel {
	columns := []table.Column{
		{Title: "Env", Width: 6},
		{Title: "Account", Width: 20},
		{Title: "Role", Width: 18},
		{Title: "Region", Width: 10},
		{Title: "Cluster", Width: 20},
		{Title: "Context", Width: 28},
	}
	t := table.New(table.WithColumns(columns), table.WithRows([]table.Row{}), table.WithFocused(true), table.WithHeight(16))
	styles := table.DefaultStyles()
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("81")).Bold(true)
	t.SetStyles(styles)

	s := textinput.New()
	s.Placeholder = "search"
	s.Prompt = "/ "
	s.CharLimit = 128
	s.Blur()

	m := uiModel{
		app:    app,
		state:  st,
		all:    st.Clusters,
		table:  t,
		search: s,
		status: fmt.Sprintf("Loaded %d contexts", len(st.Clusters)),
		commit: version.ShortCommit(),
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	m.spin = sp
	m.modalVP = viewport.New(1, 1)
	m.modalVP.MouseWheelEnabled = true
	m.applyFilter()
	return m
}

func (m uiModel) Init() tea.Cmd {
	return runUIAuthCheckCmd(m.app)
}

func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		if m.modalOn {
			m.resizeModalViewport(false)
		}
		return m, nil
	case authCheckDoneMsg:
		if msg.err != nil {
			m.status = "auth check failed: " + msg.err.Error()
			m.openModal("Auth Check Failed", msg.err.Error(), "", nil)
			return m, nil
		}
		if !msg.needsAuth {
			return m, nil
		}
		m.busy = true
		m.busyText = "authenticating with AWS SSO..."
		m.openModal(
			"AWS SSO Login Required",
			"No valid SSO token found.\nRunning rift auth now.\nApprove application: botocore-client-rift",
			"",
			nil,
		)
		return m, tea.Batch(runUIAuthCmd(m.app), m.spin.Tick)
	case authDoneMsg:
		m.busy = false
		m.busyText = ""
		if msg.err != nil {
			m.status = "auth failed: " + msg.err.Error()
			m.openModal("Auth Failed", msg.err.Error(), msg.logs, nil)
			return m, nil
		}
		m.status = "auth complete"
		m.openModal("Auth Complete", "AWS SSO login completed.", msg.logs, nil)
		return m, nil
	case syncDoneMsg:
		m.busy = false
		m.busyText = ""
		if msg.err != nil {
			m.status = "sync failed: " + msg.err.Error()
			m.openModal("Sync Failed", msg.err.Error(), msg.logs, nil)
			return m, nil
		}
		m.state = msg.report.State
		m.all = msg.report.State.Clusters
		m.applyFilter()
		m.status = fmt.Sprintf("sync complete (%d contexts)", len(m.all))
		if strings.TrimSpace(msg.logs) != "" {
			m.openModal("Sync Warnings", "Sync completed with warnings/logs.", msg.logs, &msg.report)
		}
		return m, nil
	case refreshDoneMsg:
		m.busy = false
		m.busyText = ""
		if msg.err != nil {
			m.status = "refresh failed: " + msg.err.Error()
			return m, nil
		}
		m.state = msg.state
		m.all = msg.state.Clusters
		m.applyFilter()
		m.status = fmt.Sprintf("reloaded %d contexts", len(m.all))
		return m, nil
	case useDoneMsg:
		if msg.err != nil {
			m.status = "use failed: " + msg.err.Error()
			return m, nil
		}
		m.status = "active context: " + msg.context
		return m, nil
	case k9sDoneMsg:
		if msg.err != nil {
			m.status = "k9s failed: " + msg.err.Error()
			return m, nil
		}
		m.status = "k9s exited for context: " + msg.context
		return m, nil
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		if m.modalOn {
			switch msg.String() {
			case "esc", "enter", "q":
				m.modalOn = false
				m.modal = ""
				m.modalHdr = ""
				m.modalW = 0
				m.modalVP.SetContent("")
				m.modalVP.GotoTop()
				return m, nil
			case "j":
				m.modalVP.LineDown(1)
				return m, nil
			case "k":
				m.modalVP.LineUp(1)
				return m, nil
			case "g":
				m.modalVP.GotoTop()
				return m, nil
			case "G":
				m.modalVP.GotoBottom()
				return m, nil
			}
			var cmd tea.Cmd
			m.modalVP, cmd = m.modalVP.Update(msg)
			return m, cmd
		}
		if m.searchOn {
			switch msg.String() {
			case "esc", "enter":
				m.searchOn = false
				m.search.Blur()
				m.applyFilter()
				m.syncTableLayout()
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.applyFilter()
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "\\":
			if strings.TrimSpace(m.search.Value()) != "" {
				m.search.SetValue("")
				m.applyFilter()
				m.status = fmt.Sprintf("search cleared (%d contexts)", len(m.filtered))
			} else {
				m.status = "search already clear"
			}
			return m, nil
		case "/":
			m.searchOn = true
			m.search.Focus()
			m.status = "search mode: type to filter (enter/esc close)"
			m.syncTableLayout()
			return m, nil
		case "s":
			m.busy = true
			m.busyText = "syncing..."
			return m, tea.Batch(runUISyncCmd(m.app), m.spin.Tick)
		case "r":
			m.busy = true
			m.busyText = "reloading state..."
			return m, tea.Batch(runUIRefreshCmd(m.app), m.spin.Tick)
		case "enter":
			rec := m.selected()
			if rec == nil {
				return m, nil
			}
			m.status = "switching context..."
			return m, runUIUseCmd(rec.KubeContext)
		case "k":
			rec := m.selected()
			if rec == nil {
				return m, nil
			}
			m.status = "launching k9s..."
			return m, runUIK9sCmd(*rec)
		}
	}

	m.syncTableLayout()
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m uiModel) View() string {
	header := m.topHeaderView()
	top := header

	termWidth := m.width
	if termWidth <= 0 {
		termWidth = 130
	}
	termHeight := m.height
	if termHeight <= 0 {
		termHeight = 40
	}

	leftOuterWidth := int(float64(termWidth) * 0.62)
	if leftOuterWidth < 22 {
		leftOuterWidth = 22
	}
	if leftOuterWidth > termWidth-20 {
		leftOuterWidth = termWidth - 20
	}
	rightOuterWidth := termWidth - leftOuterWidth
	if rightOuterWidth < 20 {
		rightOuterWidth = 20
		leftOuterWidth = termWidth - rightOuterWidth
	}
	if leftOuterWidth < 1 {
		leftOuterWidth = 1
	}
	if rightOuterWidth < 1 {
		rightOuterWidth = 1
	}
	leftInnerWidth := leftOuterWidth - 2
	if leftInnerWidth < 1 {
		leftInnerWidth = 1
	}
	rightInnerWidth := rightOuterWidth - 2
	if rightInnerWidth < 1 {
		rightInnerWidth = 1
	}
	if m.searchOn {
		top = lipgloss.JoinVertical(lipgloss.Left, header, m.searchBoxView(leftOuterWidth))
	}

	statusText := m.status
	if m.busy {
		statusText = m.spin.View() + " " + m.busyText
	}
	status := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render(statusText)
	statusHeight := lipgloss.Height(status)
	hotkeys := m.hotkeysLineView()
	hotkeysHeight := lipgloss.Height(hotkeys)
	paneHeight := termHeight - lipgloss.Height(top) - statusHeight - hotkeysHeight
	if paneHeight < 5 {
		paneHeight = 5
	}

	innerPaneHeight := paneHeight - 2
	if innerPaneHeight < 1 {
		innerPaneHeight = 1
	}
	tableHeight := innerPaneHeight - 1
	if tableHeight < 1 {
		tableHeight = 1
	}
	m.table.SetHeight(tableHeight)
	m.table.SetWidth(leftInnerWidth)

	leftContent := lipgloss.NewStyle().
		Width(leftInnerWidth).
		MaxWidth(leftInnerWidth).
		Height(innerPaneHeight).
		MaxHeight(innerPaneHeight).
		Render(m.table.View())
	left := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Render(leftContent)

	rightContent := m.rightPaneView(rightInnerWidth, innerPaneHeight)
	right := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Render(rightContent)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	screen := lipgloss.JoinVertical(lipgloss.Left, top, panes, status, hotkeys)
	screen = lipgloss.NewStyle().
		Width(termWidth).
		MaxWidth(termWidth).
		Height(termHeight).
		MaxHeight(termHeight).
		Render(screen)
	if m.modalOn {
		return m.renderModal(termWidth, termHeight)
	}
	return screen
}

func (m uiModel) topHeaderView() string {
	left := m.traverseLogoView()
	right := m.riftLogoView(0)
	if m.width <= 0 {
		return lipgloss.JoinVertical(lipgloss.Left, left, right)
	}

	rightWidth := lipgloss.Width(right)
	if rightWidth < 20 {
		rightWidth = 20
	}
	leftWidth := m.width - rightWidth
	if leftWidth < 24 {
		leftWidth = 24
	}
	if leftWidth+rightWidth > m.width {
		rightWidth = m.width - leftWidth
		if rightWidth < 1 {
			rightWidth = 1
		}
	}
	right = m.riftLogoView(rightWidth)

	leftBox := lipgloss.NewStyle().Width(leftWidth).MaxWidth(leftWidth).Align(lipgloss.Left).Render(left)
	rightBox := lipgloss.NewStyle().Width(rightWidth).MaxWidth(rightWidth).Align(lipgloss.Right).Render(right)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
}

func (m uiModel) traverseLogoView() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true).Padding(0, 1)
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Padding(0, 1)
	title := titleStyle.Render("TRAVERSE THE CLOUD RIFT")
	version := versionStyle.Render("version: " + m.commit)
	return lipgloss.JoinVertical(lipgloss.Left, title, version)
}

func (m uiModel) shortcutsBoxView(maxWidth int) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	rows := []string{
		keyStyle.Render("</>") + " " + labelStyle.Render("search"),
		keyStyle.Render("<enter>") + " " + labelStyle.Render("use context"),
		keyStyle.Render("<k>") + " " + labelStyle.Render("k9s namespaces"),
		keyStyle.Render("<s>") + " " + labelStyle.Render("sync"),
		keyStyle.Render("<r>") + " " + labelStyle.Render("refresh"),
		keyStyle.Render("<q>") + " " + labelStyle.Render("quit"),
	}
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true).Render("Hotkeys")
	body := strings.Join(rows, "\n")
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, body))
	if maxWidth > 0 {
		return lipgloss.NewStyle().MaxWidth(maxWidth).Render(box)
	}
	return box
}

func (m uiModel) riftLogoView(maxWidth int) string {
	artStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true).Padding(0, 1)
	lineWidth := maxWidth - 2
	if lineWidth < 1 {
		lineWidth = 1
	}
	lines := []string{
		"██████╗ ██╗███████╗████████╗",
		"██╔══██╗██║██╔════╝╚══██╔══╝",
		"██████╔╝██║█████╗     ██║   ",
		"██╔══██╗██║██╔══╝     ██║   ",
		"██║  ██║██║██║        ██║   ",
		"╚═╝  ╚═╝╚═╝╚═╝        ╚═╝   ",
	}
	if maxWidth > 0 {
		for i, line := range lines {
			lines[i] = cutRunes(line, lineWidth)
		}
	}
	return artStyle.Render(strings.Join(lines, "\n"))
}

func (m uiModel) rightPaneView(width, height int) string {
	if width < 20 {
		width = 20
	}
	if height < 1 {
		height = 1
	}
	details := m.detailView(width)
	content := details
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Height(height).
		MaxHeight(height).
		Render(content)
}

func (m uiModel) hotkeysLineView() string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("  ")

	parts := []string{
		keyStyle.Render("</>") + " " + labelStyle.Render("search"),
		keyStyle.Render("<\\>") + " " + labelStyle.Render("clear filter"),
		keyStyle.Render("<enter>") + " " + labelStyle.Render("use context"),
		keyStyle.Render("<k>") + " " + labelStyle.Render("k9s namespaces"),
		keyStyle.Render("<s>") + " " + labelStyle.Render("sync"),
		keyStyle.Render("<r>") + " " + labelStyle.Render("refresh"),
		keyStyle.Render("<up/down>") + " " + labelStyle.Render("scroll modal"),
		keyStyle.Render("<esc>") + " " + labelStyle.Render("close modal"),
		keyStyle.Render("<q>") + " " + labelStyle.Render("quit"),
	}
	line := strings.Join(parts, sep)
	if m.width > 0 {
		return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).MaxHeight(1).Render(line)
	}
	return line
}

func (m *uiModel) openModal(title, summary, logs string, report *SyncReport) {
	lines := []string{title, "", summary}
	if report != nil {
		lines = append(lines,
			"",
			fmt.Sprintf("Discovered roles:    %d", len(report.State.Roles)),
			fmt.Sprintf("Discovered clusters: %d", len(report.State.Clusters)),
		)
		if report.NS.Enabled {
			lines = append(lines, fmt.Sprintf("Namespaces: tried=%d updated=%d errors=%d", report.NS.ClustersTried, report.NS.ClustersUpdated, report.NS.Errors))
		}
		lines = append(lines,
			fmt.Sprintf("AWS profiles: +%d ~%d -%d", report.AWS.Added, report.AWS.Updated, report.AWS.Removed),
			fmt.Sprintf("Kube contexts: +%d ~%d -%d", report.Kube.AddedContexts, report.Kube.UpdatedContexts, report.Kube.RemovedContexts),
		)
	}
	if strings.TrimSpace(logs) != "" {
		lines = append(lines, "", "Logs:")
		lines = append(lines, strings.Split(strings.TrimSpace(logs), "\n")...)
	}
	lines = append(lines, "", "Use up/down/PgUp/PgDn to scroll")
	m.modalHdr = title
	m.modal = strings.Join(lines, "\n")
	m.modalOn = true
	m.resizeModalViewport(true)
}

func (m uiModel) renderModal(termWidth, termHeight int) string {
	contentWidth := m.modalVP.Width
	if contentWidth < 1 {
		contentWidth = 1
	}
	headerText := wrapTextBlock(cutRunes(m.modalHdr, contentWidth), contentWidth)
	footerText := wrapTextBlock(cutRunes("up/down scroll  PgUp/PgDn page  Esc/Enter close", contentWidth), contentWidth)
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true).Render(headerText)
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Render(footerText)
	body := m.modalVP.View()
	content := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("81")).
		Padding(0, 1).
		Render(content)
	boxHeight := lipgloss.Height(box)
	if boxHeight < 1 {
		boxHeight = 1
	}
	boxWidth := lipgloss.Width(box)
	if boxWidth < 1 {
		boxWidth = 1
	}
	if boxWidth > termWidth-2 {
		boxWidth = termWidth - 2
		if boxWidth < 1 {
			boxWidth = 1
		}
	}
	leftPad := 0
	if termWidth > boxWidth {
		leftPad = (termWidth - boxWidth) / 2
	}
	if leftPad < 0 {
		leftPad = 0
	}
	maxLeft := termWidth - boxWidth - 1
	if maxLeft < 0 {
		maxLeft = 0
	}
	if leftPad > maxLeft {
		leftPad = maxLeft
	}
	topPad := 0
	if termHeight > boxHeight {
		topPad = (termHeight - boxHeight) / 2
	}

	lines := strings.Split(box, "\n")
	maxVisibleWidth := termWidth - 1
	if maxVisibleWidth < 1 {
		maxVisibleWidth = 1
	}
	if leftPad > 0 {
		prefix := strings.Repeat(" ", leftPad)
		for i := range lines {
			line := prefix + lines[i]
			if lipgloss.Width(line) > maxVisibleWidth {
				line = ansi.Cut(line, 0, maxVisibleWidth)
			}
			lines[i] = line
		}
	} else {
		for i := range lines {
			if lipgloss.Width(lines[i]) > maxVisibleWidth {
				lines[i] = ansi.Cut(lines[i], 0, maxVisibleWidth)
			}
		}
	}
	centered := strings.Join(lines, "\n")
	if topPad > 0 {
		centered = strings.Repeat("\n", topPad) + centered
	}
	return centered
}

func (m *uiModel) resizeModalViewport(reset bool) {
	if !m.modalOn {
		return
	}
	modalWidth, modalHeight := m.modalDims(m.width, m.height)
	// Border(2) + padding(2): wrap body before styling so content controls width.
	innerWidth := modalWidth - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	bodyHeight := modalHeight - 4
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	offset := m.modalVP.YOffset
	m.modalVP.Width = innerWidth
	m.modalVP.Height = bodyHeight
	if m.modalW != innerWidth || reset {
		m.modalVP.SetContent(wrapTextBlock(m.modal, innerWidth))
		m.modalW = innerWidth
	}
	if reset {
		m.modalVP.GotoTop()
	} else {
		m.modalVP.SetYOffset(offset)
	}
}

func (m uiModel) modalDims(termWidth, termHeight int) (int, int) {
	if termWidth <= 0 {
		termWidth = 120
	}
	if termHeight <= 0 {
		termHeight = 40
	}
	// Keep the modal wide, but with a bit more side margin for readability.
	modalWidth := termWidth - 10
	if modalWidth < 20 {
		modalWidth = termWidth
	}
	if modalWidth > termWidth {
		modalWidth = termWidth
	}
	if modalWidth < 1 {
		modalWidth = 1
	}
	modalHeight := termHeight - 6
	if modalHeight < 6 {
		modalHeight = termHeight - 1
	}
	if modalHeight > termHeight {
		modalHeight = termHeight
	}
	if modalHeight < 1 {
		modalHeight = 1
	}
	return modalWidth, modalHeight
}

func (m uiModel) searchBoxView(outerWidth int) string {
	if outerWidth < 20 {
		outerWidth = 20
	}
	if outerWidth < 1 {
		outerWidth = 1
	}
	// Border(2) + padding(2) => content width.
	contentWidth := outerWidth - 4
	if contentWidth < 1 {
		contentWidth = 1
	}

	title := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true).Render("SEARCH")
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Render("type to filter   enter/esc close")
	topLine := padToWidth(cutRunes(title+"  "+hint, contentWidth), contentWidth)

	m.search.Width = contentWidth - 2 // leave room for "/ " prompt
	if m.search.Width < 1 {
		m.search.Width = 1
	}
	fieldLine := padToWidth(m.search.View(), contentWidth)

	content := topLine + "\n" + fieldLine
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("81")).
		Padding(0, 1).
		Render(content)

	lines := strings.Split(box, "\n")
	for i := range lines {
		line := lines[i]
		if lipgloss.Width(line) > outerWidth {
			line = ansi.Cut(line, 0, outerWidth)
		}
		if lipgloss.Width(line) < outerWidth {
			line += strings.Repeat(" ", outerWidth-lipgloss.Width(line))
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (m *uiModel) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	m.filtered = m.filtered[:0]
	for _, row := range m.all {
		if query == "" {
			m.filtered = append(m.filtered, row)
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{row.Env, row.AccountName, row.AccountID, row.RoleName, row.Region, row.ClusterName, row.KubeContext}, " "))
		if strings.Contains(haystack, query) {
			m.filtered = append(m.filtered, row)
		}
	}
	rows := make([]table.Row, 0, len(m.filtered))
	for _, row := range m.filtered {
		account := row.AccountName
		if account == "" {
			account = row.AccountID
		}
		rows = append(rows, table.Row{displayEnv(row.Env), account, row.RoleName, row.Region, row.ClusterName, row.KubeContext})
	}
	m.table.SetRows(rows)
	if cursor := m.table.Cursor(); cursor >= len(rows) && len(rows) > 0 {
		m.table.SetCursor(len(rows) - 1)
	}
	if len(rows) == 0 {
		m.table.SetCursor(0)
	}
}

func displayEnv(env string) string {
	if strings.EqualFold(strings.TrimSpace(env), "staging") {
		return "stg"
	}
	return env
}

func (m *uiModel) selected() *state.ClusterRecord {
	if len(m.filtered) == 0 {
		return nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.filtered) {
		idx = 0
	}
	return &m.filtered[idx]
}

func (m *uiModel) detailView(width int) string {
	rec := m.selected()
	if rec == nil {
		return "No contexts"
	}
	lines := []string{
		"Context: " + rec.KubeContext,
		"Env: " + rec.Env,
		"Account: " + rec.AccountName,
		"Account ID: " + rec.AccountID,
		"Role: " + rec.RoleName,
		"AWS Profile: " + rec.AWSProfile,
		"Region: " + rec.Region,
		"Cluster: " + rec.ClusterName,
		"Cluster ARN: " + rec.ClusterARN,
	}
	if rec.Namespace != "" {
		lines = append(lines, "Namespace: "+rec.Namespace)
	}
	return lipgloss.NewStyle().Width(width).Render(wrapTextBlock(strings.Join(lines, "\n"), width))
}

func (m *uiModel) resize() {
	m.syncTableLayout()
}

func (m *uiModel) syncTableLayout() {
	termWidth := m.width
	if termWidth <= 0 {
		termWidth = 130
	}
	termHeight := m.height
	if termHeight <= 0 {
		termHeight = 40
	}

	leftOuterWidth := int(float64(termWidth) * 0.62)
	if leftOuterWidth < 22 {
		leftOuterWidth = 22
	}
	if leftOuterWidth > termWidth-20 {
		leftOuterWidth = termWidth - 20
	}
	rightOuterWidth := termWidth - leftOuterWidth
	if rightOuterWidth < 20 {
		rightOuterWidth = 20
		leftOuterWidth = termWidth - rightOuterWidth
	}
	if leftOuterWidth < 1 {
		leftOuterWidth = 1
	}

	header := m.topHeaderView()
	top := header
	if m.searchOn {
		top = lipgloss.JoinVertical(lipgloss.Left, header, m.searchBoxView(leftOuterWidth))
	}

	statusText := m.status
	if m.busy {
		statusText = m.spin.View() + " " + m.busyText
	}
	status := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render(statusText)
	hotkeys := m.hotkeysLineView()

	paneHeight := termHeight - lipgloss.Height(top) - lipgloss.Height(status) - lipgloss.Height(hotkeys)
	if paneHeight < 5 {
		paneHeight = 5
	}
	innerPaneHeight := paneHeight - 2
	if innerPaneHeight < 1 {
		innerPaneHeight = 1
	}
	tableHeight := innerPaneHeight - 1
	if tableHeight < 1 {
		tableHeight = 1
	}
	leftInnerWidth := leftOuterWidth - 2
	if leftInnerWidth < 1 {
		leftInnerWidth = 1
	}

	m.table.SetHeight(tableHeight)
	m.table.SetWidth(leftInnerWidth)
}

func runUISyncCmd(app *App) tea.Cmd {
	return func() tea.Msg {
		var logBuf bytes.Buffer
		oldLogger := app.Logger
		level := slog.LevelInfo
		if app.Debug {
			level = slog.LevelDebug
		}
		app.Logger = slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: level}))
		defer func() {
			app.Logger = oldLogger
		}()

		report, err := app.RunSync(context.Background(), false)
		return syncDoneMsg{report: report, err: err, logs: strings.TrimSpace(logBuf.String())}
	}
}

func runUIAuthCheckCmd(app *App) tea.Cmd {
	return func() tea.Msg {
		cfg, err := app.loadConfig()
		if err != nil {
			return authCheckDoneMsg{err: err}
		}
		err = discovery.ValidateSSOLogin(cfg, time.Now().UTC())
		if err == nil {
			return authCheckDoneMsg{}
		}
		if errors.Is(err, discovery.ErrSSONotLoggedIn) {
			return authCheckDoneMsg{needsAuth: true}
		}
		return authCheckDoneMsg{err: err}
	}
}

func runUIAuthCmd(app *App) tea.Cmd {
	return func() tea.Msg {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		err := runAuthFlow(app, nil, &stdout, &stderr, false)

		logParts := make([]string, 0, 2)
		if out := strings.TrimSpace(stdout.String()); out != "" {
			logParts = append(logParts, out)
		}
		if out := strings.TrimSpace(stderr.String()); out != "" {
			logParts = append(logParts, out)
		}

		return authDoneMsg{
			err:  err,
			logs: strings.TrimSpace(strings.Join(logParts, "\n")),
		}
	}
}

func runUIRefreshCmd(app *App) tea.Cmd {
	return func() tea.Msg {
		st, err := app.loadState()
		return refreshDoneMsg{state: st, err: err}
	}
}

func runUIUseCmd(contextName string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.CommandContext(context.Background(), "kubectl", "config", "use-context", contextName)
		output, err := cmd.CombinedOutput()
		return useDoneMsg{context: contextName, err: err, output: string(output)}
	}
}

func runUIK9sCmd(rec state.ClusterRecord) tea.Cmd {
	args := []string{"--context", rec.KubeContext, "--command", "ns"}
	cmd := exec.Command("k9s", args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return k9sDoneMsg{context: rec.KubeContext, err: err}
	})
}

func wrapTextBlock(text string, width int) string {
	if width <= 1 {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.ReplaceAll(line, "\t", "    ")
		out = append(out, wrapLineRunes(line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapLineRunes(line string, width int) []string {
	if width <= 1 {
		return []string{line}
	}
	if visualWidth(line) <= width {
		return []string{line}
	}
	// Use fixed spaces for tab-style indentation; real tabs can render wider
	// than expected and blow past terminal width in some emulators.
	indent := "    "
	indentWidth := visualWidth(indent)
	if indentWidth >= width {
		indent = ""
		indentWidth = 0
	}
	runes := []rune(line)
	out := make([]string, 0, (len(runes)/width)+1)
	first := true
	for len(runes) > 0 {
		prefix := ""
		available := width
		if !first && indent != "" {
			prefix = indent
			available = width - indentWidth
			if available < 1 {
				available = 1
			}
		}

		var b strings.Builder
		consumed := 0
		for i, r := range runes {
			candidate := b.String() + string(r)
			if visualWidth(candidate) > available {
				if b.Len() == 0 {
					b.WriteRune(r)
					consumed = i + 1
				} else {
					consumed = i
				}
				break
			}
			b.WriteRune(r)
			consumed = i + 1
		}
		if consumed <= 0 {
			consumed = 1
		}
		out = append(out, prefix+b.String())
		runes = runes[consumed:]
		first = false
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

func visualWidth(s string) int {
	// Normalize tabs to a fixed width so wrapping is stable across terminals.
	return lipgloss.Width(strings.ReplaceAll(s, "\t", "    "))
}

func padToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

func cutRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	runes := []rune(s)
	var b strings.Builder
	for _, r := range runes {
		candidate := b.String() + string(r)
		if lipgloss.Width(candidate+"…") > max {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "…"
	}
	return b.String() + "…"
}
