package prompts

import (
	"strings"
	"testing"
)

func TestStoryboardExecutionContractKeepsDirectorBoundaries(t *testing.T) {
	contract := StoryboardExecutionContract("按用户给出的硬时长执行", "镜头数量按剧情需要")
	for _, required := range []string{
		"对白必须逐字保留",
		"每秒 4 个有效字",
		"同步的动作取两者所需时间的较大值",
		"观察起点",
		"结束状态",
		"duration_basis",
		"segment_reason",
		"当前剧情与已确认资产",
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("protected storyboard contract is missing %q: %s", required, contract)
		}
	}
}
