package skills

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

func TestBuiltinSeedSyncPreservesUserState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "skills.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.AutoMigrate(&model.Skill{}, &model.UserSkillState{}, &model.SkillVersion{}, &model.SkillFile{}, &model.User{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir(), nil)
	var definitions []builtinSkillDefinition
	if err := json.Unmarshal(builtinSkillsJSON, &definitions); err != nil {
		t.Fatal(err)
	}
	const communitySkillID = "16000000000081"
	communitySkillName := ""
	community := 0
	for _, def := range definitions {
		if def.OwnerUID == "" || def.OwnerUID != def.EffectiveUser.UID {
			t.Fatalf("invalid author identity: %s", def.SkillID)
		}
		for _, stamp := range []int64{def.CreateTime, def.UpdateTime} {
			if time.UnixMilli(stamp).Year() < 2020 || time.UnixMilli(stamp).After(time.Now().Add(24*time.Hour)) {
				t.Fatalf("invalid millisecond timestamp for %s: %d", def.SkillID, stamp)
			}
		}
		if def.OwnerUID == "community-itswyatt-k" {
			community++
		}
		if def.SkillID == communitySkillID {
			communitySkillName = def.SkillName
		}
	}
	if community != 35 {
		t.Fatalf("community skills = %d, want 35", community)
	}
	if communitySkillName == "" {
		t.Fatalf("community seed skill %s is missing", communitySkillID)
	}
	if err := svc.EnsureBuiltinSkills(); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureSkillPackages(); err != nil {
		t.Fatal(err)
	}
	state := model.UserSkillState{ID: "test-state", UserID: "test-user", SkillID: communitySkillID, Added: true, Liked: true}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureBuiltinSkills(); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureSkillPackages(); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.Skill{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if want := int64(len(definitions) + len(builtinImageEditingSkillDefinitions()) + len(builtinDirectorSkillDefinitions())); count != want {
		t.Fatalf("seed count = %d, want %d", count, want)
	}
	var saved model.UserSkillState
	if err := db.First(&saved, "id = ?", state.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !saved.Added || !saved.Liked {
		t.Fatalf("user state lost: %#v", saved)
	}
	var skill model.Skill
	if err := db.First(&skill, "id = ?", communitySkillID).Error; err != nil {
		t.Fatal(err)
	}
	if skill.OwnerID != "community-itswyatt-k" || skill.CreatedAt.Year() != 2026 {
		t.Fatalf("invalid persisted seed: %#v", skill)
	}
	var persisted []model.Skill
	if err := db.Find(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range persisted {
		if item.CurrentVersionID == "" || item.FileCount != 1 {
			t.Fatalf("skill %s has no initialized package", item.ID)
		}
		assertSkillVersionCount(t, db, item.ID, 1)
		version, err := svc.repo.SkillVersion(item.CurrentVersionID)
		if err != nil {
			t.Fatal(err)
		}
		body, err := svc.readSkillArchiveEntry(version, "SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != item.Instruction {
			t.Fatalf("skill %s package instruction was changed", item.ID)
		}
	}
	list, err := svc.Skills("test-user", SkillListRequest{Scope: "public", Search: communitySkillName})
	if err != nil {
		t.Fatal(err)
	}
	if list.TotalCount != 1 || len(list.Skills) != 1 || list.Skills[0].SkillID != communitySkillID {
		t.Fatalf("community skill not visible in public search: %#v", list)
	}
}
