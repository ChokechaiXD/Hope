package httpapi

import (
	"strings"

	"cortex.local/cortex/internal/cortex"
)

type dashboardCandidateGroup struct {
	Key         string
	Label       string
	Description string
	Count       int
	Imported    bool
	Active      bool
}

var candidateGroupDefinitions = []dashboardCandidateGroup{
	{
		Key:         "progress",
		Label:       "สถานะงานเก่า",
		Description: "ข้อมูลระหว่างทำงานมักหมดอายุเร็ว ตรวจแล้วเก็บเข้าคลังเมื่อไม่ใช่งานปัจจุบัน",
	},
	{
		Key:         "snapshot",
		Label:       "โปรไฟล์แบบ Snapshot",
		Description: "ภาพจำจากระบบเดิม ควรเทียบกับข้อมูลปัจจุบันก่อนรับไปใช้",
	},
	{
		Key:         "project",
		Label:       "บทเรียนจากโปรเจกต์",
		Description: "ข้อสรุปและประสบการณ์จากงานจริง เหมาะกับการตรวจเป็นชุดตามโปรเจกต์",
	},
	{
		Key:         "agent",
		Label:       "บทบาทและวิธีทำงานของเอเจนต์",
		Description: "ตรวจว่ายังตรงกับตัวตนและหน้าที่ของเอเจนต์ในปัจจุบันหรือไม่",
	},
	{
		Key:         "review",
		Label:       "ควรตรวจแยก",
		Description: "ข้อมูลที่ Cortex ยังจัดหมวดไม่ได้ จึงไม่ควรถูกอนุมัติอัตโนมัติ",
	},
}

func candidateInboxGroups(memories []cortex.Memory) []dashboardCandidateGroup {
	groups := make([]dashboardCandidateGroup, len(candidateGroupDefinitions))
	copy(groups, candidateGroupDefinitions)
	for _, memory := range memories {
		key := candidateInboxKey(memory)
		for index := range groups {
			if groups[index].Key != key {
				continue
			}
			groups[index].Count++
			groups[index].Imported = groups[index].Imported || isImportedMemory(memory)
			break
		}
	}

	nonEmpty := groups[:0]
	for _, group := range groups {
		if group.Count > 0 {
			nonEmpty = append(nonEmpty, group)
		}
	}
	return nonEmpty
}

func filterCandidateInbox(memories []cortex.Memory, groupKey string) []cortex.Memory {
	filtered := make([]cortex.Memory, 0, len(memories))
	for _, memory := range memories {
		if candidateInboxKey(memory) == groupKey {
			filtered = append(filtered, memory)
		}
	}
	return filtered
}

func candidateInboxKey(memory cortex.Memory) string {
	searchable := strings.ToLower(strings.Join([]string{
		memory.Title,
		memory.Content,
		strings.Join(memory.Tags, " "),
	}, " "))

	switch {
	case strings.Contains(searchable, "active_task"):
		return "progress"
	case strings.Contains(searchable, "quick cache") || strings.HasPrefix(strings.TrimSpace(memory.Title), "#"):
		return "snapshot"
	case containsAny(searchable,
		"legacy-category:project", "novelclaw", "aiproxy", "skill auditor", "project state"):
		return "project"
	case containsAny(searchable,
		"legacy-category:agent", "mika", "sora", "aura", "nua", "nari", "orchestration", "context-dispatch", "agent pipeline"):
		return "agent"
	default:
		return "review"
	}
}

func isImportedMemory(memory cortex.Memory) bool {
	for _, tag := range memory.Tags {
		if strings.EqualFold(tag, "holographic") || strings.HasPrefix(strings.ToLower(tag), "imported") {
			return true
		}
	}
	return strings.HasPrefix(strings.ToLower(memory.SourceRef), "import")
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
