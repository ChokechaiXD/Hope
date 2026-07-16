package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cortex.local/cortex/internal/cortex"
)

type dashboardCurator struct {
	Settings    cortex.CuratorSettings
	Manual      bool
	Assisted    bool
	Automatic   bool
	ModeLabel   string
	LastRun     *dashboardCuratorRun
	Analyzed    int
	Ready       int
	Attention   int
	Waiting     int
	Protected   int
	Suggestions []dashboardCuratorSuggestion
}

type dashboardCuratorRun struct {
	When      string
	Applied   int
	Attention int
	Error     string
}

type dashboardCuratorSuggestion struct {
	dashboardMemory
	Category      cortex.CuratorCategory
	CategoryLabel string
	Reason        string
	Evidence      string
	ActionLabel   string
	SafeToApply   bool
}

func presentCurator(
	status cortex.CuratorStatus,
	report cortex.CuratorReport,
) dashboardCurator {
	view := dashboardCurator{
		Settings:  status.Settings,
		Manual:    status.Settings.Mode == cortex.CuratorManual,
		Assisted:  status.Settings.Mode == cortex.CuratorAssisted,
		Automatic: status.Settings.Mode == cortex.CuratorAutomatic,
		ModeLabel: curatorModeLabel(status.Settings.Mode),
		Analyzed:  report.Analyzed, Ready: report.Ready, Attention: report.Attention,
		Waiting: report.Waiting, Protected: report.Protected,
		Suggestions: make([]dashboardCuratorSuggestion, 0, len(report.Suggestions)),
	}
	if status.LastRun != nil {
		view.LastRun = &dashboardCuratorRun{
			When: relativeTime(status.LastRun.CreatedAt), Applied: status.LastRun.Applied,
			Attention: status.LastRun.Attention, Error: status.LastRun.Error,
		}
	}
	for _, suggestion := range report.Suggestions {
		if len(view.Suggestions) == 8 {
			break
		}
		view.Suggestions = append(view.Suggestions, dashboardCuratorSuggestion{
			dashboardMemory: presentMemory(suggestion.Memory),
			Category:        suggestion.Category, CategoryLabel: curatorCategoryLabel(suggestion.Category),
			Reason: curatorReasonLabel(suggestion), Evidence: curatorEvidenceLabel(suggestion),
			ActionLabel: curatorActionLabel(suggestion.Recommendation), SafeToApply: suggestion.SafeToApply,
		})
	}
	return view
}

func (server *Server) curatorSettings(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.curatorDashboardRequest(writer, request)
	if !ok {
		return
	}
	runEvery, err := formInteger(request, "run_every_candidates")
	if err != nil {
		http.Error(writer, "invalid curator schedule", http.StatusBadRequest)
		return
	}
	batchLimit, err := formInteger(request, "batch_limit")
	if err != nil {
		http.Error(writer, "invalid curator batch limit", http.StatusBadRequest)
		return
	}
	minAgreement, err := formInteger(request, "min_agreement")
	if err != nil {
		http.Error(writer, "invalid curator agreement", http.StatusBadRequest)
		return
	}
	_, err = server.hub.UpdateCuratorSettings(request.Context(), cortex.UpdateCuratorSettingsCommand{
		ActorID: session.AgentID, Mode: cortex.CuratorMode(request.FormValue("mode")),
		RunEveryCandidates: runEvery, BatchLimit: batchLimit, MinAgreement: minAgreement,
	})
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	http.Redirect(writer, request, "/?curator=settings", http.StatusSeeOther)
}

func (server *Server) curatorRun(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.curatorDashboardRequest(writer, request)
	if !ok {
		return
	}
	_, err := server.hub.Curate(request.Context(), cortex.CurateCommand{
		ActorID: session.AgentID, Trigger: "dashboard",
		ApplySafe: request.FormValue("apply_safe") == "yes",
	})
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	http.Redirect(writer, request, "/?curator=ran", http.StatusSeeOther)
}

func (server *Server) curatorDashboardRequest(
	writer http.ResponseWriter,
	request *http.Request,
) (dashboardSession, bool) {
	_, session, ok := server.sessions.fromRequest(request)
	if !ok {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return dashboardSession{}, false
	}
	if !server.hub.CanGovern(session.AgentID) {
		http.Error(writer, "curator control is not permitted", http.StatusForbidden)
		return dashboardSession{}, false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8192)
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "invalid curator request", http.StatusBadRequest)
		return dashboardSession{}, false
	}
	if !validCSRF(session.CSRFToken, request.FormValue("csrf")) {
		http.Error(writer, "invalid csrf token", http.StatusForbidden)
		return dashboardSession{}, false
	}
	return session, true
}

func formInteger(request *http.Request, name string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(request.FormValue(name)))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return value, nil
}

func curatorModeLabel(mode cortex.CuratorMode) string {
	switch mode {
	case cortex.CuratorManual:
		return "ให้พี่ตัดสินใจเอง"
	case cortex.CuratorAssisted:
		return "ช่วยจัดคิวและเตือน"
	case cortex.CuratorAutomatic:
		return "รับเฉพาะรายการที่ผ่านกฎเหล็ก"
	default:
		return string(mode)
	}
}

func curatorCategoryLabel(category cortex.CuratorCategory) string {
	switch category {
	case cortex.CuratorReady:
		return "พร้อมรับ"
	case cortex.CuratorAttention:
		return "ควรตรวจ"
	case cortex.CuratorWaiting:
		return "รอหลักฐาน"
	case cortex.CuratorProtected:
		return "ให้คนตัดสินใจ"
	default:
		return string(category)
	}
}

func curatorReasonLabel(suggestion cortex.CuratorSuggestion) string {
	switch suggestion.Reason {
	case cortex.CuratorAgreement:
		return "ข้อมูลตรงกันจากหลายการยืนยันและมีแหล่งอ้างอิง"
	case cortex.CuratorContradicted:
		return "มีข้อมูลคัดค้านหรือคะแนนความจริงลดลง"
	case cortex.CuratorLowTruth:
		return "ความน่าเชื่อถือลดต่ำกว่าระดับใช้งาน"
	case cortex.CuratorLowUtility:
		return "ไม่ค่อยช่วยงานแล้ว ควรพิจารณาเก็บเข้าคลัง"
	case cortex.CuratorNeedsAgreement:
		return "ยังต้องการการยืนยันอิสระเพิ่ม"
	case cortex.CuratorNeedsSource:
		return "ยังไม่มีไฟล์ commit หรือแหล่งอ้างอิงให้ตรวจ"
	case cortex.CuratorProtectedScope:
		return "ข้อมูลส่วนตัวหรือข้อมูลที่ใช้กับทุกเอเจนต์ต้องให้คนตรวจ"
	case cortex.CuratorProtectedKind:
		return "ความชอบและสถานะงานเปลี่ยนตามเวลา จึงไม่อนุมัติเอง"
	case cortex.CuratorImported:
		return "ข้อมูลที่นำเข้าจากระบบเก่าต้องให้คนตรวจอย่างน้อยหนึ่งครั้ง"
	default:
		return string(suggestion.Reason)
	}
}

func curatorEvidenceLabel(suggestion cortex.CuratorSuggestion) string {
	parts := make([]string, 0, 3)
	if count := len(suggestion.Supporters); count > 0 {
		parts = append(parts, fmt.Sprintf("เอเจนต์ %d ตัว", count))
	}
	if suggestion.Confirmations > 0 {
		parts = append(parts, fmt.Sprintf("ยืนยันเพิ่ม %d ครั้ง", suggestion.Confirmations))
	}
	if suggestion.Contradictions > 0 {
		parts = append(parts, fmt.Sprintf("คัดค้าน %d ครั้ง", suggestion.Contradictions))
	}
	if len(parts) == 0 {
		return "ยังไม่มีประวัติการใช้งาน"
	}
	return strings.Join(parts, " · ")
}

func curatorActionLabel(decision cortex.ReviewDecision) string {
	switch decision {
	case cortex.ReviewApprove:
		return "รับไปใช้งาน"
	case cortex.ReviewSupersede:
		return "หาข้อมูลใหม่มาแทน"
	case cortex.ReviewArchive:
		return "พิจารณาเก็บเข้าคลัง"
	default:
		return "ตรวจรายละเอียด"
	}
}

func relativeTime(timestamp time.Time) string {
	age := time.Since(timestamp)
	switch {
	case age < time.Minute:
		return "เมื่อครู่"
	case age < time.Hour:
		return fmt.Sprintf("%d นาทีที่แล้ว", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%d ชั่วโมงที่แล้ว", int(age.Hours()))
	default:
		return timestamp.Local().Format("02/01/2006 15:04")
	}
}
