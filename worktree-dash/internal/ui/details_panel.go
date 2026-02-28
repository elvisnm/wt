package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/worktree"

	"github.com/charmbracelet/lipgloss"
)

var (
	label_style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("248")).
			Width(10)

	value_style = lipgloss.NewStyle()
)

// service_base_ports for port table in details
var service_base_ports = []struct {
	name string
	port int
}{
	{"socket_server", 3000},
	{"app", 3001},
	{"sync", 3002},
	{"ship_server", 3003},
	{"api", 3004},
	{"job_server", 3005},
	{"www", 3006},
	{"cache_server", 3008},
	{"insights_server", 3010},
	{"order_table_server", 3012},
	{"inventory_table_server", 3013},
	{"admin_server", 3050},
}

var minimal_services = map[string]bool{
	"socket_server": true, "app": true, "api": true,
	"admin_server": true, "cache_server": true,
	"order_table_server": true, "inventory_table_server": true,
}

func RenderDetailsPanel(wt *worktree.Worktree, width, height, scroll, spin_frame int, focused bool, cfg *config.Config) string {
	title := TitleStyle(focused).Render(" d - Details ")
	style := PanelStyle(width, height, focused)
	inner_w := width - 4

	if wt == nil {
		styled := style.Render(lipgloss.NewStyle().Foreground(DimTextColor).Render("No worktree selected"))
		return inject_title(styled, title)
	}

	lines := build_detail_lines(wt, inner_w, spin_frame, cfg)

	inner_h := height - 2
	total := len(lines)

	if scroll > total-inner_h {
		scroll = total - inner_h
	}
	if scroll < 0 {
		scroll = 0
	}

	end := scroll + inner_h
	if end > total {
		end = total
	}
	visible := lines[scroll:end]

	content := strings.Join(visible, "\n")
	styled := style.Render(content)
	styled = OverlayScrollbar(styled, total, inner_h, scroll, focused)
	return inject_title(styled, title)
}

// DetailLineCount returns total lines for scroll bounds.
func DetailLineCount(wt *worktree.Worktree, cfg *config.Config) int {
	if wt == nil {
		return 0
	}
	return len(build_detail_lines(wt, 100, 0, cfg))
}

func build_detail_lines(wt *worktree.Worktree, inner_w, spin_frame int, cfg *config.Config) []string {
	var lines []string

	lines = append(lines, detail_line("Branch", wt.Branch, inner_w))
	lines = append(lines, detail_line("Alias", wt.Alias, inner_w))
	lines = append(lines, detail_line("Type", string(wt.Type), inner_w))

	// Action in progress (e.g. "removing...", "starting...")
	if strings.HasSuffix(wt.Health, "...") {
		action := strings.TrimSuffix(wt.Health, "...")
		frame := spinFrames[spin_frame%len(spinFrames)]
		status_text := fmt.Sprintf("%s %s", frame, action)
		lines = append(lines, detail_line("Status",
			lipgloss.NewStyle().Foreground(StartingColor).Render(status_text), inner_w))
		lines = append(lines, "")
		lines = append(lines, detail_line("Path", wt.Path, inner_w))
		return lines
	}

	if wt.Type == worktree.TypeDocker {
		status_text := "stopped"
		status_color := StoppedColor
		if wt.Running {
			status_text = wt.Health
			if status_text == "" {
				status_text = "running"
			}
			status_color = RunningColor
			if wt.Health == "starting" {
				status_color = StartingColor
			}
		}
		lines = append(lines, detail_line("Status",
			lipgloss.NewStyle().Foreground(status_color).Render(status_text), inner_w))

		if wt.Mode != "" {
			lines = append(lines, detail_line("Mode", wt.Mode, inner_w))
		}
		if wt.HostBuild {
			tag := lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Bold(true).
				Render("host-build")
			lines = append(lines, detail_line("Build", tag, inner_w))
		}
		if wt.Domain != "" {
			lines = append(lines, detail_line("Domain", wt.Domain, inner_w))
		}
		if wt.DBName != "" {
			lines = append(lines, detail_line("Database", wt.DBName, inner_w))
		}
		if wt.Running {
			lines = append(lines, detail_line("CPU", wt.CPU, inner_w))
			lines = append(lines, detail_line("Memory", wt.Mem, inner_w))
			if wt.Uptime != "" {
				lines = append(lines, detail_line("Uptime", wt.Uptime, inner_w))
			}
		}
		if wt.Container != "" {
			lines = append(lines, detail_line("Container", wt.Container, inner_w))
		}

		// Quick Links
		lines = append(lines, "")
		link_style := lipgloss.NewStyle().Foreground(HintColor)
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render("Quick Links"))
		lines = append(lines, build_quick_links(wt, cfg, link_style, inner_w)...)

		// Service Ports
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Ports (%s)", wt.Mode)))
		lines = append(lines, build_port_lines(wt, cfg)...)
	} else if wt.Type == worktree.TypeLocal {
		if wt.Running {
			status_label := "running (pm2)"
			if cfg != nil && cfg.ServiceManager() != "pm2" {
				status_label = "running (dev)"
			}
			lines = append(lines, detail_line("Status",
				lipgloss.NewStyle().Foreground(RunningColor).Render(status_label), inner_w))
		} else {
			lines = append(lines, detail_line("Status",
				lipgloss.NewStyle().Foreground(StoppedColor).Render("stopped"), inner_w))
		}
	}

	lines = append(lines, "")
	lines = append(lines, detail_line("Path", wt.Path, inner_w))

	return lines
}

// build_quick_links returns the quick link lines using config when available,
// falling back to hardcoded defaults otherwise.
func build_quick_links(wt *worktree.Worktree, cfg *config.Config, link_style lipgloss.Style, inner_w int) []string {
	var lines []string

	if cfg != nil && len(cfg.Services.QuickLinks) > 0 {
		for _, ql := range cfg.Services.QuickLinks {
			base_port, ok := cfg.Services.Ports[ql.Service]
			if !ok {
				continue
			}
			port := base_port + wt.Offset
			url := fmt.Sprintf("http://localhost:%d%s", port, ql.PathPrefix)
			// Use domain for root links (empty or "/" prefix)
			if wt.Domain != "" && (ql.PathPrefix == "" || ql.PathPrefix == "/") {
				url = "http://" + wt.Domain + "/"
			}
			lines = append(lines, detail_line(ql.Label, link_style.Render(url), inner_w))
		}
	} else {
		// Hardcoded fallback
		if wt.Domain != "" {
			lines = append(lines, detail_line("Web", link_style.Render("http://"+wt.Domain+"/"), inner_w))
		} else {
			lines = append(lines, detail_line("Web", link_style.Render(fmt.Sprintf("http://localhost:%d/", 3001+wt.Offset)), inner_w))
		}
		lines = append(lines, detail_line("API", link_style.Render(fmt.Sprintf("http://localhost:%d/", 3004+wt.Offset)), inner_w))
		lines = append(lines, detail_line("Admin", link_style.Render(fmt.Sprintf("http://localhost:%d/", 3050+wt.Offset)), inner_w))
	}

	return lines
}

// build_port_lines returns the service port table using config when available,
// falling back to hardcoded defaults otherwise.
func build_port_lines(wt *worktree.Worktree, cfg *config.Config) []string {
	port_name_style := lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	port_val_style := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	if cfg != nil && len(cfg.Services.Ports) > 0 {
		// Determine which services to show based on mode
		var mode_services map[string]bool
		if wt.Mode != "" {
			mode_list := cfg.ServicesForMode(wt.Mode)
			if mode_list != nil {
				mode_services = make(map[string]bool, len(mode_list))
				for _, s := range mode_list {
					mode_services[s] = true
				}
			}
		}

		// Sort ports by value for stable display order
		type portEntry struct {
			name string
			port int
		}
		entries := make([]portEntry, 0, len(cfg.Services.Ports))
		for name, port := range cfg.Services.Ports {
			entries = append(entries, portEntry{name, port})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].port < entries[j].port
		})

		var lines []string
		for _, e := range entries {
			// If mode filtering is active, skip services not in the mode list
			if mode_services != nil && !mode_services[e.name] {
				continue
			}
			port := e.port + wt.Offset
			line := fmt.Sprintf("%s %s",
				port_name_style.Render(fmt.Sprintf("%-22s", e.name)),
				port_val_style.Render(fmt.Sprintf("%d", port)),
			)
			lines = append(lines, line)
		}
		return lines
	}

	// Hardcoded fallback
	var lines []string
	for _, sp := range service_base_ports {
		if wt.Mode == "minimal" && !minimal_services[sp.name] {
			continue
		}
		port := sp.port + wt.Offset
		line := fmt.Sprintf("%s %s",
			port_name_style.Render(fmt.Sprintf("%-22s", sp.name)),
			port_val_style.Render(fmt.Sprintf("%d", port)),
		)
		lines = append(lines, line)
	}
	return lines
}

func detail_line(label, value string, max_w int) string {
	line := fmt.Sprintf("%s %s",
		label_style.Render(label+":"),
		value_style.Render(value),
	)
	if lipgloss.Width(line) > max_w {
		line = lipgloss.NewStyle().MaxWidth(max_w).Render(line)
	}
	return line
}
