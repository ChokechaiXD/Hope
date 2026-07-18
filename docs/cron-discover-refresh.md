---
title: Cron Idea — discover-refresh (Evo loop) ถูกกระตุ้นจากข่าว
kind: cron-proposal
status: proposal
tags: [cron, automation, news-briefing, cortex, capability-escalation]
---

# Cron: discover-refresh — รอบ Capability Escalation Loop

> ไอเดียที่เกิดจาก Evo Round 4: ทำเป็น cron อัตโนมัติให้ loop นี้รันเองได้

## วัตถุประสงค์
รัน loop "หาข่าว AI/tech → สรุป → ปรับใช้กับ workspace → สร้างของจากข่าว"
เป็นประจำ (แนว discover-refresh) เพื่อ escalate ถึงสเตปสร้างสรรค์โดยอัตโนมัติ

## Pipeline (ต่อ cron 1 รอบ)
1. **discover** — `web_search` ข่าว AI/tech 3 ข้อ (รอบ 7 วัน)
2. **summarize** — สรุปแต่ละข้อ + หาจุดเชื่อมกับ workspace
3. **apply** — เขียน note ลง `Cortex/docs/news-YYYY-MM-evoN.md` (candidate memory)
4. **create** — สร้างของจริงจากข่าว 1 ชิ้น (skill ใหม่ / cron idea / note ลง Cortex /
   แนวทางอัพเกรด) แล้วรายงานกลับ

## การตั้งค่า (แนะนำ)
- ความถี่:  weekly (เช่น ทุกอาทิตย์ 09:00)
- ฮุคเข้ากับ skill `news-briefing` ที่มีอยู่แล้ว — ขยายจาก "สรุปข่าว" เป็น
  "สรุป + ปรับใช้ + สร้าง"
- เอาต์พุต: ไฟล์ note ใน Cortex + สรุปสั้นส่งกลับ parent agent

## เงื่อนไขความสำเร็จ (escalation gate)
รอบถัดไปต้อง "สร้างผลลัพธ์ที่ใช้งานได้จริง" ไม่ใช่แค่สรุป — เช่น skill ที่รันผ่าน
selftest, หรือ cron config ที่ลงมือได้

## สถานะ
เสนอเป็น proposal จาก Evo Round 4 (2026-07-18)。 รออนุมัติก่อนลง cron จริง。
 ponytail: ยังไม่สร้าง cron job จริง — รอผู้ใช้ยืนยันความถี่/ช่องทางแจ้งเตือน。
