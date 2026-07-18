package hope

import (
	"context"
	"fmt"
	"strings"
)

func (hub *Hub) Skills(ctx context.Context) ([]Skill, error) {
	rows, err := hub.db.QueryContext(ctx, `SELECT id,name,description,path,source,source_url,keywords_json,role,project,enabled,use_count,success_count,failure_count,updated_at FROM skills ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()
	var result []Skill
	for rows.Next() {
		var item Skill
		var keywords, updated string
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Path, &item.Source, &item.SourceURL, &keywords, &item.Role, &item.Project, &enabled, &item.UseCount, &item.SuccessCount, &item.FailureCount, &updated); err != nil {
			return nil, err
		}
		item.Keywords = decodeStrings(keywords)
		item.Enabled = enabled == 1
		item.UpdatedAt, _ = parseTime(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (hub *Hub) SaveSkill(ctx context.Context, skill Skill) error {
	skill.ID = slug(skill.ID)
	if skill.ID == "" {
		skill.ID = slug(skill.Name)
	}
	if skill.ID == "" || strings.TrimSpace(skill.Name) == "" {
		return fmt.Errorf("skill id and name are required")
	}
	keywords, _ := encodeStrings(uniqueStrings(skill.Keywords))
	tx, err := hub.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO skills(id,name,description,path,source,source_url,keywords_json,role,project,enabled,use_count,success_count,failure_count,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,path=excluded.path,source=excluded.source,source_url=excluded.source_url,keywords_json=excluded.keywords_json,role=excluded.role,project=excluded.project,enabled=excluded.enabled,use_count=excluded.use_count,success_count=excluded.success_count,failure_count=excluded.failure_count,updated_at=excluded.updated_at`,
		skill.ID, strings.TrimSpace(skill.Name), strings.TrimSpace(skill.Description), skill.Path, skill.Source, strings.TrimSpace(skill.SourceURL), keywords,
		skill.Role, skill.Project, boolInt(skill.Enabled), skill.UseCount, skill.SuccessCount, skill.FailureCount, nowText())
	if err != nil {
		return fmt.Errorf("save skill: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM skill_fts WHERE skill_id=?`, skill.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO skill_fts(skill_id,name,description,keywords) VALUES(?,?,?,?)`,
		skill.ID, skill.Name, skill.Description, strings.Join(skill.Keywords, " ")); err != nil {
		return err
	}
	return tx.Commit()
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	lastDash := false
	for _, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if valid {
			out.WriteRune(r)
			lastDash = false
		} else if !lastDash && out.Len() > 0 {
			out.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
