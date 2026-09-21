package skills

import (
	"strings"
	"testing"
)

func TestBuiltinDirectorSkillUsesCanvasAgentContract(t *testing.T) {
	definitions := builtinDirectorSkillDefinitions()
	if len(definitions) != 1 {
		t.Fatalf("director builtin skill count = %d, want 1", len(definitions))
	}
	skill := definitions[0]
	if skill.SkillID != "yingce-director-storyboard" || skill.Tag != "drama" || skill.Source != 3 {
		t.Fatalf("unexpected director skill metadata: %+v", skill)
	}
	for _, required := range []string{
		"canvas_get_state", "canvas_read_storyboard", "canvas_create_storyboard", "canvas_edit_storyboard",
		"model_list", "generate_media", "plan_update", "对白逐字保留", "每秒 4 个有效字",
		"起幅或观察起点", "落幅或结束状态", "continuityOut", "duration_basis", "审查意见",
	} {
		if !strings.Contains(skill.Instruction, required) {
			t.Fatalf("director skill is missing %q", required)
		}
	}
	for _, forbidden := range []string{"dreamina_cli", "独立服务", "第三方 API"} {
		if !strings.Contains(skill.Instruction, forbidden) {
			t.Fatalf("director skill should state that %q is unavailable", forbidden)
		}
	}
}
