package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Bold(true)
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
	m.applyFilter()
	return m
}

func (m uiModel) Init() tea.Cmd {
	return nil
}

func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case syncDoneMsg:
		if msg.err != nil {
			m.status = "sync failed: " + msg.err.Error()
			return m, nil
		}
		m.state = msg.report.State
		m.all = msg.report.State.Clusters
		m.applyFilter()
		m.status = fmt.Sprintf("sync complete (%d contexts)", len(m.all))
		return m, nil
	case refreshDoneMsg:
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
	case tea.KeyMsg:
		if m.searchOn {
			switch msg.String() {
			case "esc", "enter":
				m.searchOn = false
				m.search.Blur()
				m.applyFilter()
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
		case "/":
			m.searchOn = true
			m.search.Focus()
			m.status = "search mode: type to filter (enter/esc to close)"
			return m, nil
		case "s":
			m.status = "syncing..."
			return m, runUISyncCmd(m.app)
		case "r":
			m.status = "reloading state..."
			return m, runUIRefreshCmd(m.app)
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

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m uiModel) View() string {
	header := m.topHeaderView()
	searchBox := ""
	if m.searchOn {
		searchBox = m.searchBoxView()
	}
	top := header
	if searchBox != "" {
		top = lipgloss.JoinVertical(lipgloss.Left, header, searchBox)
	}

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

	status := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render(m.status)
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
	return screen
}

func (m uiModel) topHeaderView() string {
	left := m.traverseLogoView()
	right := m.riftLogoView()
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

	leftBox := lipgloss.NewStyle().Width(leftWidth).MaxWidth(leftWidth).Align(lipgloss.Left).Render(left)
	rightBox := lipgloss.NewStyle().Width(rightWidth).MaxWidth(rightWidth).Align(lipgloss.Right).Render(right)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
}

func (m uiModel) traverseLogoView() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true).Padding(0, 1)
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Padding(0, 1)
	title := titleStyle.Render("TRAVERSE THE CLOUD RIFT")
	version := versionStyle.Render("version: " + m.commit)
	return lipgloss.JoinVertical(lipgloss.Left, title, version)
}

func (m uiModel) shortcutsBoxView(maxWidth int) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true)
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

func (m uiModel) riftLogoView() string {
	artStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true).Padding(0, 1)
	lines := []string{
		"██████╗ ██╗███████╗████████╗",
		"██╔══██╗██║██╔════╝╚══██╔══╝",
		"██████╔╝██║█████╗     ██║   ",
		"██╔══██╗██║██╔══╝     ██║   ",
		"██║  ██║██║██║        ██║   ",
		"╚═╝  ╚═╝╚═╝╚═╝        ╚═╝   ",
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
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("  ")

	parts := []string{
		keyStyle.Render("</>") + " " + labelStyle.Render("search"),
		keyStyle.Render("<enter>") + " " + labelStyle.Render("use context"),
		keyStyle.Render("<k>") + " " + labelStyle.Render("k9s namespaces"),
		keyStyle.Render("<s>") + " " + labelStyle.Render("sync"),
		keyStyle.Render("<r>") + " " + labelStyle.Render("refresh"),
		keyStyle.Render("<q>") + " " + labelStyle.Render("quit"),
	}
	line := strings.Join(parts, sep)
	if m.width > 0 {
		return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(line)
	}
	return line
}

func (m uiModel) searchBoxView() string {
	boxWidth := 72
	if m.width > 0 {
		boxWidth = m.width - 2
	}
	if boxWidth < 40 {
		boxWidth = 40
	}
	if m.width > 0 && boxWidth > m.width {
		boxWidth = m.width
	}
	innerWidth := boxWidth - 6
	if innerWidth < 20 {
		innerWidth = 20
	}
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true).Render("SEARCH")
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Render("type to filter   enter/esc close")
	top := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", hint)
	field := lipgloss.NewStyle().Width(innerWidth).Render(m.search.View())
	content := lipgloss.JoinVertical(lipgloss.Left, top, field)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("45")).
		Padding(0, 1).
		MaxWidth(boxWidth).
		Render(content)
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
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (m *uiModel) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	height := m.height - 6
	if height < 8 {
		height = 8
	}
	m.table.SetHeight(height)
}

func runUISyncCmd(app *App) tea.Cmd {
	return func() tea.Msg {
		report, err := app.RunSync(context.Background(), false)
		return syncDoneMsg{report: report, err: err}
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
