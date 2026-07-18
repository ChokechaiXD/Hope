package httpapi

import (
	"context"
	"net/http"
	"strings"

	"cortex.local/cortex/internal/controlplane"
	"cortex.local/cortex/internal/hope"
	"cortex.local/cortex/internal/integrationhub"
)

type hopeDashboardView struct {
	AgentID      string
	OwnerName    string
	DeputyName   string
	CSRFToken    string
	Section      string
	Notice       string
	TotalMemory  int
	Pending      int
	Connections  []hopeConnectionView
	ModeSystems  []hopeModeSystemView
	Agents       []hopeAgentView
	Modes        []hope.WorkMode
	Projects     []hope.Project
	Roots        []string
	Skills       []hope.Skill
	SkillDetail  *hopeSkillDetailView
	SkillID      string
	Events       []hopeEventView
	Jobs         any
	JobsError    string
	Passwordless bool
}

type hopeConnectionView struct {
	ID, Name, State, StateLabel, Detail, URL string
	Managed, CanStart, CanStop               bool
}

type hopeModeSystemView struct {
	ID, Name string
}

type hopeAgentView struct {
	Agent                               hope.Agent
	Initial                             string
	State, StateLabel                   string
	Managed, CanStart, CanStop, CanOpen bool
}

type hopeEventView struct {
	Target, Action, Status, Message, When string
}

type hopeSkillDetailView struct {
	Skill hope.Skill
	Body  string
}

func (server *Server) hopeDashboard(writer http.ResponseWriter, request *http.Request) {
	setDashboardHeaders(writer)
	session, ok := server.dashboardSession(writer, request)
	if !ok {
		if access, supported := server.auth.(dashboardAccess); supported {
			_, passwordless := access.DashboardAccess()
			if passwordless && isLoopbackRequest(request) {
				return
			}
		}
		writer.Header().Set("Cache-Control", "no-store")
		_ = dashboardTemplates.ExecuteTemplate(writer, "login.html", nil)
		return
	}
	if server.hope == nil {
		http.Error(writer, "HOPE is unavailable", http.StatusServiceUnavailable)
		return
	}
	section := strings.TrimSpace(request.URL.Query().Get("section"))
	if section == "" {
		section = "today"
	}
	counts, _ := server.hub.Counts(request.Context(), session.AgentID)
	view := hopeDashboardView{
		AgentID: session.AgentID, OwnerName: hopeOwnerName, DeputyName: hopeDeputyName,
		CSRFToken: session.CSRFToken, Section: section,
		Notice:  hopeNotice(request.URL.Query().Get("notice")),
		Pending: counts["candidate"], SkillID: strings.TrimSpace(request.URL.Query().Get("skill")),
	}
	for _, count := range counts {
		view.TotalMemory += count
	}
	if access, supported := server.auth.(dashboardAccess); supported {
		_, view.Passwordless = access.DashboardAccess()
	}
	if err := server.populateHOPEView(request, &view); err != nil {
		http.Error(writer, "load HOPE: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	if err := dashboardTemplates.ExecuteTemplate(writer, "hope.html", view); err != nil {
		http.Error(writer, "render HOPE", http.StatusInternalServerError)
	}
}

func (server *Server) populateHOPEView(request *http.Request, view *hopeDashboardView) error {
	ctx := request.Context()
	switch view.Section {
	case "today":
		modes, err := server.hope.WorkModes(ctx)
		if err != nil {
			return err
		}
		view.Modes = modes
		if err := server.loadHOPEAgents(ctx, view, false); err != nil {
			return err
		}
		server.loadHOPEConnections(ctx, view)
		return server.loadHOPEEvents(ctx, view)
	case "agents":
		return server.loadHOPEAgents(ctx, view, true)
	case "projects":
		if err := server.loadHOPEAgents(ctx, view, false); err != nil {
			return err
		}
		return server.loadHOPEProjects(ctx, view, true)
	case "skills":
		if err := server.loadHOPEAgents(ctx, view, false); err != nil {
			return err
		}
		if err := server.loadHOPEProjects(ctx, view, false); err != nil {
			return err
		}
		return server.loadHOPESkills(ctx, view)
	case "automations":
		jobs, err := server.hope.Automations(ctx)
		if err != nil {
			view.JobsError = err.Error()
			return nil
		}
		view.Jobs = jobs
	case "connections":
		server.loadHOPEConnections(ctx, view)
	case "settings":
		return nil
	default:
		view.Section = "today"
		return server.populateHOPEView(request, view)
	}
	return nil
}

func (server *Server) loadHOPEAgents(ctx context.Context, view *hopeDashboardView, withGateway bool) error {
	if withGateway {
		statuses, err := server.hope.AgentStatuses(ctx)
		if err != nil {
			return err
		}
		for _, item := range statuses {
			view.Agents = append(view.Agents, presentAgent(item))
		}
		return nil
	}
	agents, err := server.hope.Agents(ctx)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		view.Agents = append(view.Agents, hopeAgentView{Agent: agent, Initial: firstRune(agent.Name), State: "unknown", StateLabel: "ยังไม่ได้ตรวจ", CanOpen: agent.TelegramURL != ""})
	}
	return nil
}

func (server *Server) loadHOPEProjects(ctx context.Context, view *hopeDashboardView, includeRoots bool) error {
	projects, err := server.hope.Projects(ctx)
	if err != nil {
		return err
	}
	view.Projects = projects
	if includeRoots {
		roots, err := server.hope.ProjectRoots(ctx)
		if err != nil {
			return err
		}
		view.Roots = roots
	}
	return nil
}

func (server *Server) loadHOPESkills(ctx context.Context, view *hopeDashboardView) error {
	skills, err := server.hope.Skills(ctx)
	if err != nil {
		return err
	}
	view.Skills = skills
	if id := view.SkillID; id != "" {
		skill, body, err := server.hope.ReadSkill(ctx, id)
		if err == nil {
			view.SkillDetail = &hopeSkillDetailView{Skill: skill, Body: body}
		}
	}
	return nil
}

func (server *Server) loadHOPEEvents(ctx context.Context, view *hopeDashboardView) error {
	events, err := server.hope.RecentEvents(ctx, 20)
	if err != nil {
		return err
	}
	for _, event := range events {
		view.Events = append(view.Events, hopeEventView{
			Target: event.Target, Action: event.Action, Status: event.Status, Message: event.Message,
			When: event.CreatedAt.Local().Format("02 Jan 15:04"),
		})
	}
	return nil
}

func (server *Server) loadHOPEConnections(ctx context.Context, view *hopeDashboardView) {
	for _, status := range server.hope.Connections(ctx) {
		if status.ID == "telegram" {
			continue
		}
		view.Connections = append(view.Connections, presentConnection(status))
		if status.ID != "hermes" {
			view.ModeSystems = append(view.ModeSystems, hopeModeSystemView{ID: status.ID, Name: status.Name})
		}
	}
}

func presentAgent(item controlplane.AgentStatus) hopeAgentView {
	return hopeAgentView{
		Agent: item.Agent, State: string(item.Status.State), StateLabel: stateLabel(item.Status.State),
		Initial: firstRune(item.Agent.Name),
		Managed: item.Status.Managed, CanStart: item.Status.State == integrationhub.StateStopped,
		CanStop: item.Status.State == integrationhub.StateRunning && item.Status.Managed,
		CanOpen: item.Agent.TelegramURL != "",
	}
}

func firstRune(value string) string {
	for _, character := range value {
		return string(character)
	}
	return "?"
}

func presentConnection(status integrationhub.Status) hopeConnectionView {
	return hopeConnectionView{
		ID: status.ID, Name: status.Name, State: string(status.State), StateLabel: stateLabel(status.State),
		Detail: status.Detail, URL: status.URL, Managed: status.Managed,
		CanStart: status.State == integrationhub.StateStopped,
		CanStop:  status.Managed && status.State != integrationhub.StateStopped,
	}
}

func stateLabel(state integrationhub.State) string {
	switch state {
	case integrationhub.StateRunning:
		return "กำลังทำงาน"
	case integrationhub.StateExternal:
		return "กำลังทำงาน"
	case integrationhub.StateStopped:
		return "ปิดอยู่"
	case integrationhub.StateMissing:
		return "ไม่พบ"
	case integrationhub.StateConflict:
		return "พอร์ตถูกใช้งาน"
	default:
		return "ควรตรวจสอบ"
	}
}

func hopeNotice(code string) string {
	switch code {
	case "agent-saved":
		return "บันทึก Agent แล้ว"
	case "projects-found":
		return "ค้นหาและบันทึก Project แล้ว"
	case "root-added":
		return "เพิ่มโฟลเดอร์ค้นหาแล้ว"
	case "project-saved":
		return "บันทึก Project แล้ว"
	case "project-deleted":
		return "ลบ Project จาก HOPE แล้ว"
	case "skills-synced":
		return "อัปเดตคลัง Skill แล้ว"
	case "skill-created":
		return "สร้าง Skill ในคลัง HOPE แล้ว"
	case "skill-saved":
		return "บันทึก Skill แล้ว"
	case "skill-imported":
		return "ติดตั้ง Skill จาก GitHub แล้ว"
	case "skill-deployed":
		return "ส่ง Skill ไป Hermes แล้ว"
	case "automation-updated":
		return "สั่ง Automation แล้ว"
	case "security-saved":
		return "บันทึกการเข้าใช้หน้าเว็บแล้ว"
	case "mode-saved":
		return "บันทึก Work Mode แล้ว"
	default:
		return ""
	}
}
