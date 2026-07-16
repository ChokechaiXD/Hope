package httpapi

import (
	"fmt"
	"strings"

	"cortex.local/cortex/internal/cortex"
)

type dashboardMemory struct {
	cortex.Memory
	LifecycleLabel string
	KindLabel      string
	ScopeLabel     string
	AppliesTo      string
	TrustLabel     string
	UtilityLabel   string
	EvidenceLabel  string
	Guidance       string
	CanApprove     bool
	CanPromote     bool
	CanReject      bool
	CanSupersede   bool
	CanArchive     bool
}

func presentMemory(memory cortex.Memory) dashboardMemory {
	return dashboardMemory{
		Memory:         memory,
		LifecycleLabel: lifecycleLabel(memory.Lifecycle),
		KindLabel:      kindLabel(memory.Kind),
		ScopeLabel:     scopeLabel(memory.Scope),
		AppliesTo:      appliesTo(memory.Scope, memory.ScopeKey),
		TrustLabel:     trustLabel(memory.TruthScore),
		UtilityLabel:   utilityLabel(memory.UtilityScore),
		EvidenceLabel:  evidenceLabel(memory.SourceRef),
		Guidance:       reviewGuidance(memory),
		CanApprove:     memory.Lifecycle == cortex.LifecycleCandidate,
		CanPromote:     memory.Lifecycle == cortex.LifecycleActive,
		CanReject:      memory.Lifecycle == cortex.LifecycleCandidate || memory.Lifecycle == cortex.LifecycleActive,
		CanSupersede:   memory.Lifecycle != cortex.LifecycleSuperseded && memory.Lifecycle != cortex.LifecycleArchived,
		CanArchive:     memory.Lifecycle != cortex.LifecycleArchived,
	}
}

func lifecycleLabel(lifecycle cortex.Lifecycle) string {
	switch lifecycle {
	case cortex.LifecycleCandidate:
		return "รอตรวจ"
	case cortex.LifecycleActive:
		return "ใช้งานได้"
	case cortex.LifecycleCanonical:
		return "กฎหลัก"
	case cortex.LifecycleRejected:
		return "ไม่รับ"
	case cortex.LifecycleSuperseded:
		return "มีข้อมูลใหม่แทนแล้ว"
	case cortex.LifecycleArchived:
		return "เก็บเข้าคลังแล้ว"
	default:
		return string(lifecycle)
	}
}

func kindLabel(kind cortex.MemoryKind) string {
	switch kind {
	case cortex.KindFact:
		return "ข้อมูล"
	case cortex.KindPreference:
		return "ความชอบ"
	case cortex.KindDecision:
		return "ข้อตัดสินใจ"
	case cortex.KindFailedAttempt:
		return "วิธีที่เคยพลาด"
	case cortex.KindSolution:
		return "วิธีที่ใช้ได้"
	case cortex.KindProjectState:
		return "สถานะงาน"
	default:
		return string(kind)
	}
}

func scopeLabel(scope cortex.Scope) string {
	switch scope {
	case cortex.ScopeGlobal:
		return "ทุกเอเจนต์"
	case cortex.ScopeProject:
		return "โปรเจกต์"
	case cortex.ScopeDomain:
		return "สายงาน"
	case cortex.ScopePrivate:
		return "เฉพาะเอเจนต์"
	default:
		return string(scope)
	}
}

func appliesTo(scope cortex.Scope, scopeKey string) string {
	label := scopeLabel(scope)
	if key := strings.TrimSpace(scopeKey); key != "" {
		return label + " · " + key
	}
	return label
}

func trustLabel(score float64) string {
	switch {
	case score >= 0.8:
		return "น่าเชื่อถือสูง"
	case score >= 0.6:
		return "ค่อนข้างน่าเชื่อถือ"
	case score >= 0.4:
		return "ควรตรวจเพิ่ม"
	default:
		return "ความน่าเชื่อถือต่ำ"
	}
}

func utilityLabel(score float64) string {
	switch {
	case score >= 0.8:
		return "ช่วยงานบ่อย"
	case score >= 0.6:
		return "เคยช่วยงาน"
	case score >= 0.4:
		return "ยังมีข้อมูลไม่พอ"
	default:
		return "ยังไม่ช่วยงาน"
	}
}

func evidenceLabel(sourceRef string) string {
	if source := strings.TrimSpace(sourceRef); source != "" {
		return "มีแหล่งอ้างอิง · " + source
	}
	return "ยังไม่มีแหล่งอ้างอิงโดยตรง"
}

func reviewGuidance(memory cortex.Memory) string {
	switch memory.Lifecycle {
	case cortex.LifecycleCandidate:
		if memory.TruthScore < 0.4 {
			return "ความน่าเชื่อถือต่ำ ควรขอหลักฐานเพิ่มก่อนรับ"
		}
		if strings.TrimSpace(memory.SourceRef) != "" && memory.TruthScore >= 0.6 {
			return "มีแหล่งอ้างอิงและคะแนนเหมาะสม ควรตรวจเนื้อหาก่อนรับไปใช้งาน"
		}
		return "เป็นข้อมูลใหม่ ควรตรวจบริบทและแหล่งที่มาก่อนรับ"
	case cortex.LifecycleActive:
		if memory.TruthScore >= 0.8 && memory.UtilityScore >= 0.7 {
			return "น่าเชื่อถือและเคยช่วยงาน อาจเลื่อนเป็นกฎหลักถ้าเป็นหลักที่ต้องใช้ซ้ำ"
		}
		return "ใช้งานได้แล้ว ควรรอผลการใช้เพิ่มก่อนเลื่อนเป็นกฎหลัก"
	case cortex.LifecycleCanonical:
		return "เป็นกฎหลักปัจจุบัน แต่ยังแทนที่ได้เมื่อมีหลักฐานใหม่"
	case cortex.LifecycleRejected:
		return "ข้อมูลนี้ไม่ถูกนำมาใช้ เว้นแต่จะมีหลักฐานใหม่"
	case cortex.LifecycleSuperseded:
		return "มีข้อมูลใหม่แทนแล้ว เก็บรายการนี้ไว้เพื่อดูประวัติ"
	case cortex.LifecycleArchived:
		return "เก็บไว้ในคลังและจะไม่ถูกนำมาใช้โดยอัตโนมัติ"
	default:
		return fmt.Sprintf("สถานะปัจจุบัน: %s", memory.Lifecycle)
	}
}

func eventLabel(eventType cortex.EventType) string {
	switch eventType {
	case cortex.EventCreated:
		return "สร้างข้อมูล"
	case cortex.EventImported:
		return "นำเข้าข้อมูล"
	case cortex.EventObserved:
		return "พบซ้ำ"
	case cortex.EventRevised:
		return "แก้ไขเนื้อหา"
	case cortex.EventApproved:
		return "รับไปใช้งาน"
	case cortex.EventPromoted:
		return "เลื่อนเป็นกฎหลัก"
	case cortex.EventRejected:
		return "ไม่รับข้อมูล"
	case cortex.EventSuperseded:
		return "มีข้อมูลใหม่แทน"
	case cortex.EventArchived:
		return "เก็บเข้าคลัง"
	case cortex.EventRecalled:
		return "ถูกเรียกใช้"
	case cortex.EventConfirmed:
		return "ยืนยันว่าถูกต้อง"
	case cortex.EventContradicted:
		return "พบว่าขัดแย้ง"
	case cortex.EventHelpful:
		return "ช่วยงาน"
	case cortex.EventUnhelpful:
		return "ไม่ช่วยงาน"
	case cortex.EventApplied:
		return "นำไปใช้แล้ว"
	default:
		return string(eventType)
	}
}
