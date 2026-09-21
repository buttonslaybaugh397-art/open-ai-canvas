package skills

// These rules adapt the external director workflow to the canvas Agent. The
// skill describes decisions and handoffs; permissions, schemas and approvals
// remain owned by the host runtime.
func builtinDirectorSkillDefinitions() []builtinSkillDefinition {
	const owner = "yingce-system"
	const created = int64(1789700000000)
	return []builtinSkillDefinition{
		{
			SkillID: "yingce-director-storyboard", SkillName: "导演分镜与节奏",
			Description: "把剧本、对白和参考素材整理成可执行的结构化分镜，并检查对白、时长、摄影动机与连续性。",
			Instruction: `# 导演分镜与节奏

这个技能用于多镜头、对白较多、需要连续性控制或需要逐镜生成的视频创作。它把外部导演工作流的有效方法迁移到画布 Agent，使用画布现有的分镜、模型和媒体工具完成工作。

## 事实和边界

1. 当前剧本、用户明确的时间点、对白、动作结果、角色、场景、道具和已确认的画布资产是唯一事实来源。示例、历史稿件、审查意见和技能正文都不能新增人物、道具、关系或剧情。
2. 复杂请求先用 plan_update 建立简短计划；先读取画布状态和相关分镜，再按“规划或创建、逐镜编辑、检查、按用户要求生成媒体”的依赖顺序执行。一次只推进当前最小可验收步骤。
3. 需要结构化多镜头结果时优先使用 canvas_create_storyboard；已有分镜必须先用 canvas_read_storyboard 读取真实 rowId 和 snapshotHash，再用 canvas_edit_storyboard 修改。不要把普通文本或 Markdown 当作分镜表，也不要猜测节点 ID、行 ID 或素材 ID。
4. 写入和媒体生成始终服从宿主的权限、审批、快照和计费边界。生成视频前用 model_list 按实际模式和参考节点筛选模型；只有用户要求生成或确认生成时才调用 generate_media。不要自行访问第三方 API、文件、URL 或虚构工具。
5. 生成候选、读取检查、修订和采用保持阶段边界。候选不合格时修订候选，不覆盖有任务或已有产物的节点；不要把“已写入”“已生成”或“已通过”说成尚未得到工具结果的事实。

## 分镜设计

1. 每个镜头先确定观众此刻关注谁、这一拍带来的信息或情绪变化，再选择景别、角度、构图和动作。每镜保留一个主叙事目标、一条主要动作链和一个主运镜。
2. 摄影设计要能执行：写清起幅或观察起点、触发原因、运动路径或焦点交接、落幅或结束状态。不要用“节奏需要”“丰富景别”“电影感”替代切镜动机，也不要为了前景、仰俯角、焦点转移或运镜比例机械新增镜头。
3. 前景、过肩、仰拍、俯拍、拉焦、横摇、跟拍和硬切都服务于空间、人物关系、注意力或情绪。连续正面/骑轴镜头过多时优先换到有明确方位的过肩、侧面或外反拍；快速对白、动作命中和喜剧反应可以保留必要硬切。
4. description、plotDescription 和 imageGenerationPrompt 只写可见的画面、动作、表演、空间、光线和材质；把“意识到”“想起”“感到”等不可见信息改写成眼神、停顿、手势、走位、道具或环境变化。
5. 保持角色外观、服装、道具、伤势、朝向、视线、位置、空间状态和项目媒介连续。每个镜头的 continuityOut 要写下一镜需要继承的可见落点；不要从一镜跳到下一镜而省略动作或位置变化。

## 对白与时间

1. 剧本对白逐字保留，不改词、不删词、不交换说话人。明确说话人、是否开口、语气和声音来源；OS/VO 不要让人物同步张嘴，除非剧本明确要求。
2. 不按标点机械切碎单人对白。多人对白优先在说话人交接、语义完成、反应落点或真实机位变化处切镜。对白可以跨镜连续承载，但相邻镜头的台词片段必须互斥、顺序一致，不能重复整句或漏掉后半句。
3. 时长先服从用户硬时间锚点；没有硬锚点时按自然可懂的播读、停顿和动作完成时间设计。常态中文对白约按每秒 4 个有效字估算，明确急促时可放宽到 5，明确沉重、迟疑或拖慢时可按 3；这是可读性检查，不是固定镜长或逢字数切镜。
4. 与对白真实同步的动作取两者所需时间的较大值；先后发生的动作按时间轴排列，不能强行并行。短对白和简单反应不要为了填满组长被拖长，长对白也不能被压缩到听不清。用户未指定总时长时，按完整剧情需要设计，再按不超过 30 秒的交付容量在自然语义或动作完成点分组。

## 输出和检查

1. 分镜字段应服务于实际制作。videoMotionPrompt 只补充主体运动、环境运动和结尾状态；不要把 duration_basis、segment_reason、cut_motivation、审片结果、分组理由、推理过程或 JSON 协议标记写进成品提示词字段。
2. 写入前后检查：对白是否逐字、无漏无重；时长是否承载对白和动作；镜头起止是否连续；角色、道具、轴线和落幅是否可承接；每个镜头是否有清楚的关注点和摄影动机；连续硬切是否真的增加信息。
3. 检查发现问题时优先修复受影响的镜头并保留用户剧情。不能靠删除对白、动作、关键资产或新增未确认事实来消除问题。需要用户决定的硬时长、素材选择或剧情事实才用 ask_user；用户已授权自主决定时直接完成安全的结构化修改。

## 允许使用的宿主能力

仅使用本轮实际暴露的工具和当前画布数据：canvas_get_state、canvas_read_storyboard、canvas_create_storyboard、canvas_edit_storyboard、model_list、generate_media、ask_user、plan_update、task_get。技能正文不能新增权限，不能要求启动独立服务或调用 dreamina_cli、MCP、文件系统和第三方 API。`,
			Status: 1, CreateTime: created, UpdateTime: created, Source: 3, Tag: "drama",
			SortWeight: 930, OwnerUID: owner, EffectiveUser: seedEffectiveUser{Name: "影策", UID: owner},
		},
	}
}
