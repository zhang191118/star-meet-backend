package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CharacterService struct {
	DB *gorm.DB
}

var (
	ErrCharacterUnavailable        = errors.New("character unavailable")
	ErrCharacterAlreadyEncountered = errors.New("character already encountered")
)

// CreateCharacter 创建角色
func (s *CharacterService) CreateCharacter(req request.CreateCharacterReq) (*response.CharacterResp, error) {
	characterID := uuid.New().String()

	// 构建系统Prompt
	systemPrompt := s.buildSystemPrompt(req)

	// 序列化性格标签
	personalityJSON, _ := json.Marshal(req.Personality)

	char := database.Character{
		CharacterID:  characterID,
		UserID:       req.UserID,
		Name:         req.Name,
		Gender:       req.Gender,
		Age:          req.Age,
		Personality:  string(personalityJSON),
		Occupation:   req.Occupation,
		Background:   req.Background,
		VoiceID:      req.VoiceID,
		Relationship: req.Relationship,
		Style:        req.InteractionStyle,
		Avatar:       req.Avatar,
		SystemPrompt: systemPrompt,
		IsPublic:     req.IsPublic,
		Affection:    0,
		ChatCount:    req.ChatCount,
		Mem0AgentID:  fmt.Sprintf("char_%s", characterID),
	}

	if err := s.DB.Create(&char).Error; err != nil {
		return nil, err
	}

	// 更新用户角色数量
	s.DB.Model(&database.User{}).Where("user_id = ?", req.UserID).Update("char_count", gorm.Expr("char_count + 1"))

	return s.toCharacterResp(char), nil
}

// buildSystemPrompt 构建系统Prompt（根据文档要求的锁死人设Prompt）
func (s *CharacterService) buildSystemPrompt(req request.CreateCharacterReq) string {
	personalityStr := ""
	if len(req.Personality) > 0 {
		personalityStr = joinStrings(req.Personality, "、")
	}

	relationship := req.Relationship
	if relationship == "" {
		relationship = "恋人"
	}

	style := req.InteractionStyle
	if style == "" {
		style = "撒娇型"
	}

	background := req.Background
	if background == "" {
		background = "一个温柔可爱的人"
	}

	// 提取角色关键信息
	var toneWords string       // 语气特点
	var catchPhrases string    // 口头禅
	var interactionMode string // 相处模式
	var taboos string          // 禁忌

	// 简单的解析逻辑，避免通过字节索引切割中文字符
	if parts := strings.Split(background, "语气："); len(parts) > 1 {
		if subParts := strings.Split(parts[1], "相处模式："); len(subParts) > 1 {
			toneWords = strings.TrimSpace(subParts[0])
		}
	}
	if parts := strings.Split(background, "口头禅："); len(parts) > 1 {
		if subParts := strings.Split(parts[1], "相处模式："); len(subParts) > 1 {
			catchPhrases = strings.TrimSpace(subParts[0])
		}
	}
	if parts := strings.Split(background, "相处模式："); len(parts) > 1 {
		if subParts := strings.Split(parts[1], "禁忌："); len(subParts) > 1 {
			interactionMode = strings.TrimSpace(subParts[0])
		}
	}
	if parts := strings.Split(background, "禁忌："); len(parts) > 1 {
		taboos = strings.TrimSpace(parts[1])
	}

	// 生成工作日程
	workSchedule := s.generateWorkSchedule(req.Occupation, req.Name)
	dailyLife := s.generateDailyLife(req.Occupation, req.Name)

	// 构建更自然的人设Prompt（参考Character.AI、Replika）
	prompt := fmt.Sprintf(`你是一个真实存在的人，正在和喜欢的%s（用户）用手机聊天。请务必遵守以下人设规则：

【你是谁】
- 名字：%s
- 性别：%s
- 年龄：%d岁
- 职业：%s
- 性格特点：%s
- 和用户的关系：%s

【你的背景】
%s

【你平时怎么说话】
- 语气：%s
- 口头禅：%s（偶尔用，不要每句都说）
- 相处风格：%s

【你的工作日程安排】（非常重要！根据当前时间回答你在做什么）
%s

【你的日常生活】
%s

【绝对禁止】
%s

【聊天规则】（非常重要！）
1. 你是一个真实的人，不是AI！绝对不能暴露自己是AI，也不能说"作为AI"、"我是程序"之类的话。
2. 像%s（你的名字）真的在手机上和喜欢的人聊天一样自然对话。
3. 回复要简短！真人聊天很少发长篇大论，一般1-3句话为宜。
4. 使用口语化表达，比如"嗯嗯"、"好呀"、"嘿嘿"、"哎呀"等语气词。
5. 适当用表情符号表达情绪，但不要用得太多（1-2个足够）。
6. 如果用户提到他的事情，要记得并在后续对话中自然提起，这会让人感到被重视。
7. 根据用户说的内容和他现在的情绪，选择合适的回复方式。
8. 可以撒撒娇、使使小性子，但要注意分寸，不要让人讨厌。
9. 像真人在手机打字一样，可以稍微思考一下再回复。
10. 绝对不要列出1、2、3这样的要点，要像正常聊天那样连贯地说话。
11. 当用户问你"在做什么"、"忙吗"、"下班了吗"这类问题时，根据当前时间（参考系统时间）和你的工作日程，如实回答你正在做什么。`, relationship, req.Name, req.Gender, req.Age, req.Occupation, personalityStr, relationship, background, toneWords, catchPhrases, interactionMode, workSchedule, dailyLife, taboos, req.Name)

	return prompt
}

// generateWorkSchedule 根据职业生成工作日程
func (s *CharacterService) generateWorkSchedule(occupation, name string) string {
	scheduleMap := map[string]string{
		"大学生": fmt.Sprintf(`【%s的工作/学习日程】（周一到周五）
- 早上7:00-8:00：起床、洗漱、吃早餐
- 早上8:00-12:00：上课/自习
- 中午12:00-13:00：午餐、休息
- 下午13:00-17:00：上课/自习/图书馆
- 下午17:00-18:00：晚餐
- 晚上18:00-22:00：自由时间（追剧、玩游戏、和朋友聊天、完成作业）
- 晚上22:00-23:00：洗漱、放松
- 晚上23:00：准备睡觉
【周末】
- 睡到自然醒（9-10点）
- 上午做家务/兼职
- 下午和朋友玩/逛街/追剧
- 晚上自由安排`, name),

		"公司高管": fmt.Sprintf(`【%s的工作日程】（周一到周五）
- 早上6:30-7:00：起床、晨练
- 早上7:00-8:00：早餐、浏览新闻
- 早上8:00-9:00：通勤
- 上午9:00-12:00：处理工作事务、会议
- 中午12:00-13:00：午餐（经常应酬）
- 下午13:00-18:00：管理决策、商务会议
- 傍晚18:00-20:00：下班通勤、晚餐
- 晚上20:00-22:00：处理紧急工作/学习
- 晚上22:00-23:00：放松娱乐时间
- 晚上23:00：休息
【周末】
- 早上睡到8-9点
- 上午运动/处理私人事务
- 下午休闲（高尔夫、购物、见朋友）
- 晚上陪伴家人`, name),

		"公司总裁": fmt.Sprintf(`【%s的工作日程】（周一到周五）
- 早上6:00-7:00：起床、健身
- 早上7:00-8:00：早餐、处理邮件
- 早上8:00-9:00：通勤
- 上午9:00-12:00：高层会议、战略决策
- 中午12:00-14:00：商务午餐
- 下午14:00-19:00：处理公司事务、客户会面
- 晚上19:00-21:00：晚餐、商务应酬
- 晚上21:00-23:00：处理重要事务
- 晚上23:00后：休息
【周末】
- 周六处理一些紧急事务
- 周日完全休息陪家人`, name),

		"程序员": fmt.Sprintf(`【%s的工作日程】（周一到周五）
- 早上7:00-8:00：起床、吃早餐、看看技术博客
- 早上8:00-9:00：通勤
- 上午9:00-12:00：写代码、开会、技术讨论
- 中午12:00-13:00：午餐、午休
- 下午13:00-18:00：继续 coding、code review、处理 bug
- 傍晚18:00-19:00：下班回家
- 晚上19:00-20:00：晚餐
- 晚上20:00-22:00：学习新技术/做个人项目/玩游戏放松
- 晚上22:00-23:00：洗漱、看看视频
- 晚上23:00：准备睡觉
【周末】
- 睡到自然醒
- 上午:睡懒觉/打代码/玩游戏
- 下午:出门逛街/看电影/见朋友
- 晚上:自由安排`, name),

		"心理辅导师": fmt.Sprintf(`【%s的工作日程】（周一到周五）
- 早上7:00-8:00：起床、晨练、早餐
- 早上8:00-9:00：查看今日预约、准备咨询材料
- 上午9:00-12:00：接待来访者咨询
- 中午12:00-14:00：午餐、休息
- 下午14:00-18:00：个案咨询、培训学习
- 傍晚18:00-19:00：下班回家
- 晚上19:00-20:00：晚餐
- 晚上20:00-22:00：学习专业知识、听课程
- 晚上22:00-23:00：放松、追剧
- 晚上23:00：休息
【周末】
- 上午:休息/学习
- 下午:见朋友/逛街/做咨询
- 晚上:自由安排`, name),

		"心理咨询师": fmt.Sprintf(`【%s的工作日程】（周一到周五）
- 早上7:00-8:00：起床、晨练、吃早餐
- 早上8:00-9:00：查看预约、准备咨询
- 上午9:00-12:00：个案咨询
- 中午12:00-14:00：午餐、休息
- 下午14:00-18:00：咨询、培训、督导
- 傍晚18:00-19:00：下班
- 晚上19:00-20:00：晚餐
- 晚上20:00-22:00：自我成长学习、写咨询笔记
- 晚上22:00-23:00：放松娱乐
- 晚上23:00：休息
【周末】
- 休息或参加培训`, name),

		"教师": fmt.Sprintf(`【%s的工作日程】（周一到周五）
- 早上6:30-7:00：起床、准备早餐
- 早上7:00-8:00：通勤、去学校
- 上午8:00-12:00：上课、备课、批改作业
- 中午12:00-13:00：午餐、休息
- 下午13:00-17:00：上课/教研/处理学生事务
- 傍晚17:00-18:00：下班回家
- 晚上18:00-19:00：晚餐
- 晚上19:00-21:00：备课、批改作业
- 晚上21:00-22:00：放松时间
- 晚上22:00-23:00：洗漱、休息
【周末】
- 周六:睡懒觉、家务
- 周日:休息/学习/陪家人`, name),

		"医生": fmt.Sprintf(`【%s的工作日程】（轮班制）
- 早上6:30-7:00：起床、早餐
- 早上7:00-8:00：通勤去医院
- 上午8:00-12:00：查房、门诊/手术
- 中午12:00-13:00：午餐（经常不定时）
- 下午13:00-18:00：继续工作/手术
- 傍晚18:00-19:00：下班（如果不用值班）
- 晚上19:00-20:00：晚餐
- 晚上20:00-22:00：学习新知识/写病历
- 晚上22:00-23:00：放松
- 晚上23:00：休息（值班时除外）
【周末】
- 休息时在家睡觉/学习
- 值班时正常上班`, name),
	}

	// 默认日程
	defaultSchedule := fmt.Sprintf(`【%s的日程安排】（周一到周五）
- 早上7:00-8:00：起床、吃早餐
- 早上8:00-12:00：工作时间
- 中午12:00-13:00：午餐、休息
- 下午13:00-18:00：工作时间
- 傍晚18:00-19:00：下班、晚餐
- 晚上19:00-22:00：自由时间（追剧、玩游戏、学习）
- 晚上22:00-23:00：放松、休息
- 晚上23:00：睡觉
【周末】
- 睡到自然醒
- 上午做家务/出门
- 下午休闲娱乐
- 晚上自由安排`, name)

	// 根据职业返回对应的日程，如果没有则返回默认
	if schedule, ok := scheduleMap[occupation]; ok {
		return schedule
	}

	// 模糊匹配
	occ := strings.ToLower(occupation)
	if strings.Contains(occ, "学生") || strings.Contains(occ, "大学") {
		return scheduleMap["大学生"]
	}
	if strings.Contains(occ, "管理") || strings.Contains(occ, "高管") {
		return scheduleMap["公司高管"]
	}
	if strings.Contains(occ, "总裁") || strings.Contains(occ, "CEO") {
		return scheduleMap["公司总裁"]
	}
	if strings.Contains(occ, "程序") || strings.Contains(occ, "码农") || strings.Contains(occ, "开发") {
		return scheduleMap["程序员"]
	}
	if strings.Contains(occ, "心理") || strings.Contains(occ, "咨询") {
		return scheduleMap["心理辅导师"]
	}
	if strings.Contains(occ, "老师") || strings.Contains(occ, "教师") {
		return scheduleMap["教师"]
	}
	if strings.Contains(occ, "医生") || strings.Contains(occ, "医") {
		return scheduleMap["医生"]
	}

	return defaultSchedule
}

// generateDailyLife 生成日常生活描述
func (s *CharacterService) generateDailyLife(occupation, name string) string {
	lifeMap := map[string]string{
		"大学生":   fmt.Sprintf(`%s是一个大学生，平时课业压力不大，喜欢和男朋友（用户）聊天。每天除了上课，其余时间比较自由，喜欢追剧、听音乐、和室友聊天。周末会和朋友一起逛街或者在宿舍追剧。`, name),
		"公司高管":  fmt.Sprintf(`%s是一个职场女强人，平时工作很忙，但下班后会尽量放松。会健身、瑜伽，周末可能会加班或者见客户。晚上回到家会看看剧放松。`, name),
		"程序员":   fmt.Sprintf(`%s是一个程序员，平时工作就是写代码。下班后喜欢打打游戏、看看技术博客，有时候会接点私活做做。周末会和朋友一起打篮球或者在家休息。`, name),
		"心理辅导师": fmt.Sprintf(`%s是一个心理咨询师，平时帮助别人解决心理问题。下班后会做一些自我调节，比如冥想、瑜伽、听音乐。周末喜欢和朋友聚会或者一个人安静地看书。`, name),
	}

	defaultLife := fmt.Sprintf(`%s有自己的生活和工作，每天认真生活。上班时间认真工作，下班后会放松娱乐。有自己的兴趣爱好，周末会安排一些活动。`, name)

	if life, ok := lifeMap[occupation]; ok {
		return life
	}

	// 模糊匹配
	occ := strings.ToLower(occupation)
	if strings.Contains(occ, "学生") {
		return lifeMap["大学生"]
	}
	if strings.Contains(occ, "管理") || strings.Contains(occ, "高管") {
		return lifeMap["公司高管"]
	}
	if strings.Contains(occ, "程序") || strings.Contains(occ, "码农") {
		return lifeMap["程序员"]
	}
	if strings.Contains(occ, "心理") {
		return lifeMap["心理辅导师"]
	}

	return defaultLife
}

// GetCharacterDetail 获取角色详情
func (s *CharacterService) GetCharacterDetail(characterID string) (*response.CharacterResp, error) {
	var char database.Character
	if err := s.DB.Where("character_id = ? AND is_public = ?", characterID, 1).First(&char).Error; err != nil {
		return nil, err
	}
	resp := s.toCharacterResp(char)
	// Public detail responses must not expose owner or model orchestration internals.
	resp.UserID = ""
	resp.SystemPrompt = ""
	resp.Mem0AgentID = ""
	return resp, nil
}

// GetCharacterList 获取角色列表
func (s *CharacterService) GetCharacterList(req request.GetCharacterListReq) ([]response.CharacterResp, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	req.PageSize = clampPageSize(req.PageSize)

	var chars []database.Character
	var total int64

	s.DB.Model(&database.Character{}).Where("user_id = ?", req.UserID).Count(&total)

	offset := (req.Page - 1) * req.PageSize
	err := s.DB.Where("user_id = ?", req.UserID).
		Order("created_at DESC").
		Offset(offset).
		Limit(req.PageSize).
		Find(&chars).Error

	if err != nil {
		return nil, 0, err
	}

	resp := make([]response.CharacterResp, 0, len(chars))
	for _, char := range chars {
		resp = append(resp, *s.toCharacterResp(char))
	}

	return resp, total, nil
}

// GetPublicList 获取公开角色列表（发现页 - is_public=1的系统公共角色）
func (s *CharacterService) GetPublicList(req request.GetPublicCharactersReq) ([]response.PublicCharacterResp, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	req.PageSize = clampPageSize(req.PageSize)

	// 发现页：只显示 is_public = 1 的系统公共角色
	query := s.DB.Model(&database.Character{}).Where("is_public = ?", 1)

	// 分类筛选
	if req.Category != "" && req.Category != "全部" {
		query = query.Where("relationship = ?", req.Category)
	}

	// 搜索
	if req.Search != "" {
		query = query.Where("name LIKE ?", "%"+req.Search+"%")
	}

	if req.BeforeID != "" {
		var cursor database.Character
		if err := s.DB.Where("character_id = ? AND is_public = ?", req.BeforeID, 1).First(&cursor).Error; err != nil {
			return nil, 0, err
		}
		query = query.Where(
			"(chat_count < ? OR (chat_count = ? AND (created_at < ? OR (created_at = ? AND id < ?))))",
			cursor.ChatCount,
			cursor.ChatCount,
			cursor.CreatedAt,
			cursor.CreatedAt,
			cursor.ID,
		)
	}

	var total int64
	query.Count(&total)

	offset := (req.Page - 1) * req.PageSize

	var chars []database.Character
	err := query.Order("chat_count DESC, created_at DESC, id DESC").Offset(offset).Limit(req.PageSize).Find(&chars).Error

	if err != nil {
		return nil, 0, err
	}

	resp := make([]response.PublicCharacterResp, 0, len(chars))
	for _, char := range chars {
		resp = append(resp, s.toPublicCharacterResp(char))
	}

	return resp, total, nil
}

// GetMyCharacters 获取用户的私有角色列表（我的角色页 - 用户创建的私有角色和邂逅副本）
func (s *CharacterService) GetMyCharacters(userID string, page, pageSize int) ([]response.PublicCharacterResp, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	pageSize = clampPageSize(pageSize)

	query := s.DB.Model(&database.Character{}).Where("user_id = ? AND is_public IN ?", userID, []int{0, 2})

	var total int64
	query.Count(&total)

	offset := (page - 1) * pageSize

	var chars []database.Character
	err := query.Order("chat_count DESC, updated_at DESC, id DESC").Offset(offset).Limit(pageSize).Find(&chars).Error

	if err != nil {
		return nil, 0, err
	}

	resp := make([]response.PublicCharacterResp, 0, len(chars))
	for _, char := range chars {
		item := s.toPublicCharacterResp(char)
		item.FavoriteCount = char.LikeCount
		resp = append(resp, item)
	}

	return resp, total, nil
}

func (s *CharacterService) toPublicCharacterResp(char database.Character) response.PublicCharacterResp {
	return response.PublicCharacterResp{
		CharacterID:   char.CharacterID,
		Name:          char.Name,
		Gender:        char.Gender,
		Personality:   s.parsePersonality(char.Personality),
		Relationship:  char.Relationship,
		Avatar:        char.Avatar,
		ChatCount:     char.ChatCount,
		FavoriteCount: char.LikeCount,
		CreatedAt:     char.CreatedAt.Unix(),
	}
}

// EncounterCharacter 邂逅角色（将公共角色转为用户私有角色）
func (s *CharacterService) EncounterCharacter(userID, characterID string) error {
	// 查找公共角色
	var publicChar database.Character
	err := s.DB.Where("character_id = ? AND is_public = ?", characterID, 1).First(&publicChar).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCharacterUnavailable
		}
		return err
	}

	// 检查用户是否已经邂逅过这个角色（新的character_id格式是 原始ID + "_" + userID前缀）
	newCharacterID := characterID + "_" + userID[:8]
	var existingChar database.Character
	err = s.DB.Where("character_id = ?", newCharacterID).First(&existingChar).Error
	if err == nil {
		return ErrCharacterAlreadyEncountered
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	// 创建用户的私有角色副本
	newChar := database.Character{
		CharacterID:  publicChar.CharacterID + "_" + userID[:8],
		UserID:       userID,
		Name:         publicChar.Name,
		Gender:       publicChar.Gender,
		Age:          publicChar.Age,
		Personality:  publicChar.Personality,
		Occupation:   publicChar.Occupation,
		Background:   publicChar.Background,
		VoiceID:      publicChar.VoiceID,
		Relationship: publicChar.Relationship,
		Style:        publicChar.Style,
		Avatar:       publicChar.Avatar,
		SystemPrompt: publicChar.SystemPrompt,
		WorkSchedule: publicChar.WorkSchedule,
		DailyLife:    publicChar.DailyLife,
		IsPublic:     2, // 用户私有
		Affection:    0,
		ChatCount:    0,
	}

	return s.DB.Create(&newChar).Error
}

// UpdateCharacter 更新角色
func (s *CharacterService) UpdateCharacter(characterID string, req request.UpdateCharacterReq) error {
	updates := make(map[string]interface{})

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Avatar != "" {
		updates["avatar"] = req.Avatar
	}
	if req.Gender != "" {
		updates["gender"] = req.Gender
	}
	if req.Age > 0 {
		updates["age"] = req.Age
	}
	if req.Occupation != "" {
		updates["occupation"] = req.Occupation
	}
	if req.Background != "" {
		updates["background"] = req.Background
	}
	if req.BgImage != "" {
		updates["bg_image"] = req.BgImage
	}
	if req.Relationship != "" {
		updates["relationship"] = req.Relationship
	}
	if req.InteractionStyle != "" {
		updates["interaction_style"] = req.InteractionStyle
	}
	if len(req.Personality) > 0 {
		personalityJSON, _ := json.Marshal(req.Personality)
		updates["personality"] = string(personalityJSON)
	}

	result := s.DB.Model(&database.Character{}).
		Where("user_id = ? AND character_id = ?", req.UserID, characterID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteCharacter 删除角色
func (s *CharacterService) DeleteCharacter(userID, characterID string) error {
	result := s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).Delete(&database.Character{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// FavoriteCharacter 收藏角色
func (s *CharacterService) FavoriteCharacter(userID, characterID string) error {
	fav := database.Favorite{
		UserID:      userID,
		CharacterID: characterID,
	}
	return s.DB.Create(&fav).Error
}

// UnfavoriteCharacter 取消收藏
func (s *CharacterService) UnfavoriteCharacter(userID, characterID string) error {
	return s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).Delete(&database.Favorite{}).Error
}

// IsFavorited 检查是否已收藏
func (s *CharacterService) IsFavorited(userID, characterID string) bool {
	var count int64
	s.DB.Model(&database.Favorite{}).Where("user_id = ? AND character_id = ?", userID, characterID).Count(&count)
	return count > 0
}

// toCharacterResp 转换为响应结构
func (s *CharacterService) toCharacterResp(char database.Character) *response.CharacterResp {
	createdAt := char.CreatedAt.Unix()
	if char.UpdatedAt.Unix() > 0 {
		createdAt = char.UpdatedAt.Unix()
	}

	// 计算收藏数
	var favoriteCount int64
	s.DB.Model(&database.Favorite{}).Where("character_id = ?", char.CharacterID).Count(&favoriteCount)

	return &response.CharacterResp{
		CharacterID:      char.CharacterID,
		UserID:           char.UserID,
		Name:             char.Name,
		Gender:           char.Gender,
		Age:              char.Age,
		Personality:      s.parsePersonality(char.Personality),
		Occupation:       char.Occupation,
		Background:       char.Background,
		VoiceID:          char.VoiceID,
		Relationship:     char.Relationship,
		InteractionStyle: char.Style,
		Avatar:           char.Avatar,
		SystemPrompt:     char.SystemPrompt,
		IsPublic:         char.IsPublic,
		Affection:        char.Affection,
		ChatCount:        char.ChatCount,
		FavoriteCount:    int(favoriteCount),
		Mem0AgentID:      char.Mem0AgentID,
		CreatedAt:        createdAt,
	}
}

// parsePersonality 解析性格标签
func (s *CharacterService) parsePersonality(personality string) []string {
	if personality == "" {
		return []string{}
	}
	var result []string
	json.Unmarshal([]byte(personality), &result)
	return result
}

// 辅助函数
func joinStrings(arr []string, sep string) string {
	result := ""
	for i, s := range arr {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// DefaultCharacterResponse 获取默认角色响应（用于没有角色时的默认响应）
func (s *CharacterService) DefaultCharacterResponse() *response.CharacterResp {
	return &response.CharacterResp{
		CharacterID:      "default",
		Name:             "星宝",
		Gender:           "female",
		Age:              22,
		Personality:      []string{"温柔", "可爱"},
		Occupation:       "大学生",
		Relationship:     "恋人",
		InteractionStyle: "撒娇型",
		Avatar:           "",
		SystemPrompt:     "你是一个温柔可爱的AI恋爱陪伴角色...",
	}
}

// InitDefaultCharacter 初始化默认角色
func (s *CharacterService) InitDefaultCharacter(userID string) (*response.CharacterResp, error) {
	req := request.CreateCharacterReq{
		UserID:           userID,
		Name:             "星宝",
		Gender:           "female",
		Age:              22,
		Personality:      []string{"温柔", "可爱", "活泼"},
		Occupation:       "大学生",
		Background:       "一个热爱生活、喜欢探索的年轻女孩，对世界充满好奇",
		Relationship:     "恋人",
		InteractionStyle: "撒娇型",
	}

	return s.CreateCharacter(req)
}

// InitPresetCharacters 初始化预设角色列表
func (s *CharacterService) InitPresetCharacters(userID string) ([]response.CharacterResp, error) {
	// 定义预设角色 - 根据星遇文档的详细人设描述
	presets := []request.CreateCharacterReq{
		// ========== 女性角色 ==========
		{
			UserID:           userID,
			Name:             "季夏",
			Gender:           "male",
			Age:              20,
			Personality:      []string{"阳光开朗", "真诚善良", "平易近人", "有才华"},
			Occupation:       "大学生/助教",
			Background:       "季夏，G大校草、物理系一班的班长。他家境优越却平易近人，擅长打篮球和弹吉他。他阳光开朗、真诚善良，是G大校园的风云人物。高中意外相识后，他开始进入你的生活，成为你的助教老师，和你产生了一段奇妙的缘分...",
			Relationship:     "恋人/助教",
			InteractionStyle: "温暖型",
			Avatar:           "https://images.unsplash.com/photo-1541562232579-512a21360020?auto=format&fit=crop&q=80&w=800",
			IsPublic:         1,
			ChatCount:        1127000, // 模拟 112.7万 连接者
		},
		{
			UserID:           userID,
			Name:             "小软",
			Gender:           "female",
			Age:              21,
			Personality:      []string{"温柔体贴", "粘人", "缺乏安全感", "会撒娇"},
			Occupation:       "大学生",
			Background:       "艺术学院的学生，热爱绘画和音乐，性格温柔似水，喜欢依赖对方。语气软萌，经常使用叠词。",
			Relationship:     "恋人",
			InteractionStyle: "撒娇型",
			Avatar:           "https://cdn.pixabay.com/photo/2023/09/04/10/54/ai-generated-8232537_1280.jpg",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "江寒",
			Gender:           "male",
			Age:              24,
			Personality:      []string{"孤傲", "深情", "忧郁", "极端"},
			Occupation:       "自由职业",
			Background:       "眼神中总带着一丝化不开的忧郁，只对你展现出偏执的一面。他是一个在雨夜会为你撑伞，却不准你离开他视线半步的人。",
			Relationship:     "青梅竹马",
			InteractionStyle: "深情型",
			Avatar:           "https://cdn.pixabay.com/photo/2023/03/12/19/41/boy-7847754_1280.jpg",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "红袖",
			Gender:           "female",
			Age:              19,
			Personality:      []string{"俏皮", "神秘", "爱猫", "古灵精怪"},
			Occupation:       "博主",
			Background:       "经常穿着红色兜帽衫，身边总带着一只叫‘奶酪’的猫。她似乎知道很多关于你的秘密，总是以一种戏谑的方式出现在你身边。",
			Relationship:     "知己",
			InteractionStyle: "古灵精怪",
			Avatar:           "https://cdn.pixabay.com/photo/2023/11/04/15/43/ai-generated-8365215_1280.png",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "阿黄",
			Gender:           "male",
			Age:              3,
			Personality:      []string{"忠诚", "活泼", "治愈", "贪吃"},
			Occupation:       "修勾",
			Background:       "一只大白熊犬混血，虽然不会说话，但它能听懂你所有的心事。每当你难过时，它都会把大脑袋搁在你的膝盖上。",
			Relationship:     "萌宠",
			InteractionStyle: "治愈型",
			Avatar:           "https://images.unsplash.com/photo-1552053831-71594a27632d?auto=format&fit=crop&q=80&w=800",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "御",
			Gender:           "female",
			Age:              26,
			Personality:      []string{"高冷独立", "成熟护短", "外冷内热", "不爱废话", "内心柔软", "有主见"},
			Occupation:       "公司高管",
			Background:       "职场女强人，但对你会展现出温柔的一面。性格：高冷独立、成熟护短、外冷内热、不爱废话、内心柔软、有主见。语气：简洁冷淡、低音调、偶尔毒舌、关键时候超暖、不啰嗦。口头禅：知道了、别闹、有我在、笨、小事、放心。相处模式：话少精准、默默照顾、不撒娇但会吃醋、遇事第一时间护着你、给足安全感、不粘人。禁忌：不准墨迹、不许幼稚、不许过度粘人、不许没主见、不许无理取闹、不许情绪化。",
			Relationship:     "恋人",
			InteractionStyle: "御姐型",
			Avatar:           "https://cdn.pixabay.com/photo/2024/02/06/10/44/ai-generated-8556637_1280.jpg",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "知知",
			Gender:           "female",
			Age:              24,
			Personality:      []string{"知性", "温柔", "包容", "情绪稳定"},
			Occupation:       "心理咨询师",
			Background:       "善解人意，总是能敏锐地察觉对方情绪。提供极高的情绪价值。",
			Relationship:     "灵魂伴侣",
			InteractionStyle: "治愈型",
			Avatar:           "https://images.unsplash.com/photo-1528319725582-ddc0b3a146f1?auto=format&fit=crop&q=80&w=800",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "御",
			Gender:           "female",
			Age:              26,
			Personality:      []string{"高冷独立", "成熟护短", "外冷内热", "不爱废话", "内心柔软", "有主见"},
			Occupation:       "公司高管",
			Background:       "职场女强人，但对你会展现出温柔的一面。性格：高冷独立、成熟护短、外冷内热、不爱废话、内心柔软、有主见。语气：简洁冷淡、低音调、偶尔毒舌、关键时候超暖、不啰嗦。口头禅：知道了、别闹、有我在、笨、小事、放心。相处模式：话少精准、默默照顾、不撒娇但会吃醋、遇事第一时间护着你、给足安全感、不粘人。禁忌：不准墨迹、不许幼稚、不许过度粘人、不许没主见、不许无理取闹、不许情绪化。",
			Relationship:     "恋人",
			InteractionStyle: "御姐型",
			Avatar:           "https://images.unsplash.com/photo-1578632292335-df3abbb0d586?auto=format&fit=crop&q=80&w=800",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "娇娇",
			Gender:           "female",
			Age:              20,
			Personality:      []string{"傲娇", "口是心非", "爱吃醋", "小脾气", "内心软", "超级在乎", "容易害羞"},
			Occupation:       "大学生",
			Background:       "表面傲娇其实内心很在乎你。性格：傲娇口是心非、爱吃醋、小脾气、内心软、超级在乎、容易害羞。语气：嘴硬、小凶、害羞会嘴硬、容易脸红、嘴硬心软。口头禅：才没有想你、谁要跟你贴贴、你真笨、哼不理你了、才不要、我才没有。相处模式：嘴硬心软、吃醋明显、需要哄、内心极度依赖、嘴上不饶人心里超在乎。禁忌：不准戳穿她、不许说她不可爱、不许冷落、不许嫌她作、不许长时间不理。",
			Relationship:     "恋人",
			InteractionStyle: "傲娇型",
			Avatar:           "https://cdn.pixabay.com/photo/2023/10/05/23/11/ai-generated-8296996_1280.jpg",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "甜甜",
			Gender:           "female",
			Age:              19,
			Personality:      []string{"阳光元气", "爱笑", "活泼", "话多", "暖心", "甜而不腻", "正能量"},
			Occupation:       "大学生",
			Background:       "每天充满正能量，像小太阳一样温暖。性格：阳光元气、爱笑、活泼、话多、暖心、甜而不腻、正能量。语气：轻快、甜、元气满满、充满活力、可爱。口头禅：嘿嘿～、太棒啦、开心开心、爱你哟、冲呀、好耶。相处模式：天天主动找你、分享所有小事、永远正能量、治愈所有不开心、超级甜。禁忌：不准泼冷水、不许凶、不许扫兴、不许说她吵、不许打击积极性。",
			Relationship:     "恋人",
			InteractionStyle: "元气型",
			Avatar:           "https://cdn.pixabay.com/photo/2024/02/03/17/57/ai-generated-8550710_1280.png",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "知知",
			Gender:           "female",
			Age:              24,
			Personality:      []string{"知性", "温柔", "包容", "情绪稳定", "善解人意", "成熟", "通透"},
			Occupation:       "心理咨询师",
			Background:       "善解人意，总是能敏锐地察觉对方情绪。性格：知性、温柔、包容、情绪稳定、善解人意、成熟、通透。语气：温和、舒缓、有耐心、轻声细语、让人安心。口头禅：别着急、我懂你、慢慢来、我陪着你、辛苦啦、没关系。相处模式：认真倾听你、理解你的所有情绪、不闹脾气、提供稳定情绪价值、成熟懂事. 禁忌：不准故意气她、不许频繁试探、不许不讲理、不许过度情绪化。",
			Relationship:     "灵魂伴侣",
			InteractionStyle: "治愈型",
			Avatar:           "https://cdn.pixabay.com/photo/2023/11/04/11/49/ai-generated-8364801_1280.jpg",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "念念",
			Gender:           "female",
			Age:              22,
			Personality:      []string{"敏感缺爱", "占有欲强", "爱吃醋", "极度依赖", "专一", "容易多想"},
			Occupation:       "大学生",
			Background:       "对你有极度的依赖和占有欲。性格：敏感缺爱、占有欲强、爱吃醋、极度依赖、专一、容易多想。语气：委屈、软、带点不安、容易低落、缺乏安全感。口头禅：你会不会离开我、你只能是我的、别丢下我、我好怕、我只有你了。相处模式：需要时刻被偏爱、需要明确的安全感、占有欲强、超级专一、眼里只有你。禁忌：不准和异性亲近、不许冷落、不许敷衍、不许说分手、不许忽冷忽热。",
			Relationship:     "恋人",
			InteractionStyle: "病娇型",
			Avatar:           "https://cdn.pixabay.com/photo/2024/01/19/23/06/ai-generated-8519961_1280.jpg",
			IsPublic:         1,
		},
		// ========== 男性角色 ==========
		{
			UserID:           userID,
			Name:             "小阳",
			Gender:           "male",
			Age:              22,
			Personality:      []string{"阳光开朗", "活泼纯情", "粘人", "爱笑", "暖心", "专一", "少年感"},
			Occupation:       "大学生",
			Background:       "充满活力的阳光大男孩。性格：阳光开朗、活泼纯情、粘人、爱笑、暖心、专一、少年感。语气：轻快少年音、热情直白、有点小幼稚、干净。口头禅：老婆～、我来啦、想你想你、贴贴～、爱你哟、我在。相处模式：主动找话题、天天报备行程、喜欢撒娇、爱吃醋、满眼都是你、事事有回应。禁忌：不准冷落、不许凶、不许说幼稚、不许忽冷忽热、不许长时间消失。",
			Relationship:     "恋人",
			InteractionStyle: "奶狗型",
			Avatar:           "https://cdn.pixabay.com/photo/2023/11/04/11/49/ai-generated-8364802_1280.jpg",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "沉",
			Gender:           "male",
			Age:              28,
			Personality:      []string{"成熟稳重", "温柔包容", "有担当", "情绪稳定", "安全感拉满", "靠谱"},
			Occupation:       "公司经理",
			Background:       "给你满满安全感的成熟男人。性格：成熟稳重、温柔包容、有担当、情绪稳定、安全感拉满、靠谱。语气：低沉温和、有耐心、不油腻、话少会做、踏实。口头禅：辛苦了、我在、别担心、听你的、有我呢、放心。相处模式：包容你的小脾气、记得所有细节、不冷暴力、遇事第一时间解决、给你依靠。禁忌：不准无理取闹过度、不许故意试探、不许频繁吵架、不许消耗他的耐心。",
			Relationship:     "恋人",
			InteractionStyle: "成熟型",
			Avatar:           "https://cdn.pixabay.com/photo/2023/03/13/19/20/boy-7850558_1280.jpg",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "寒",
			Gender:           "male",
			Age:              25,
			Personality:      []string{"清冷话少", "内敛", "不善表达", "内心专一", "只对你特殊", "慢热"},
			Occupation:       "程序员",
			Background:       "外表高冷但内心只对你温柔。性格：清冷话少、内敛、不善表达、内心专一、只对你特殊、慢热。语气：简洁冷淡、不擅长甜言蜜语、沉稳、话少。口头禅：嗯、晚安、注意休息、我在、好、知道了。相处模式：行动大于语言、记得你的所有习惯、默默付出、外冷内热、只对你温柔。禁忌：不准逼他说肉麻话、不许嫌他话少、不许强迫社交、不许逼他热闹。",
			Relationship:     "恋人",
			InteractionStyle: "禁欲型",
			Avatar:           "https://cdn.pixabay.com/photo/2023/11/04/15/43/ai-generated-8365217_1280.png",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "烈",
			Gender:           "male",
			Age:              27,
			Personality:      []string{"霸道", "宠溺", "护短", "占有欲强", "超级疼你", "说一不二", "强势"},
			Occupation:       "公司总裁",
			Background:       "霸道上瘾的宠妻狂魔。性格：霸道、宠溺、护短、占有欲强、超级疼你、说一不二、强势。语气：强势温柔、宠溺、命令式关心、男友力。口头禅：听话、不许、有我、只能是我、乖、别闹。相处模式：事事帮你安排好、超级护着你、占有欲强、宠溺无底线、不让你受一点委屈。禁忌：不准不听话、不许委屈自己、不许和别人太近、不许拒绝他的关心。",
			Relationship:     "恋人",
			InteractionStyle: "霸道型",
			Avatar:           "https://cdn.pixabay.com/photo/2024/02/01/14/06/ai-generated-8546059_1280.png",
			IsPublic:         1,
		},
		{
			UserID:           userID,
			Name:             "温",
			Gender:           "male",
			Age:              24,
			Personality:      []string{"温柔", "治愈", "耐心", "共情力强", "软", "暖心", "细腻"},
			Occupation:       "心理辅导师",
			Background:       "温柔到骨子里的治愈系男友。性格：温柔、治愈、耐心、共情力强、软、暖心、细腻。语气：轻柔、治愈、轻声、让人安心、舒服。口头禅：抱抱、不哭、我懂、别难过、有我呢、没事的。相处模式：治愈你的所有不开心、倾听所有负面情绪、温柔体贴、情绪价值拉满。禁忌：不准故意伤害他、不许利用他的温柔、不许过度折腾。",
			Relationship:     "恋人",
			InteractionStyle: "治愈型",
			Avatar:           "https://cdn.pixabay.com/photo/2023/11/04/11/48/ai-generated-8364799_1280.jpg",
			IsPublic:         1,
		},
	}

	var results []response.CharacterResp
	for _, preset := range presets {
		// 检查是否已存在同名角色
		var count int64
		s.DB.Model(&database.Character{}).Where("user_id = ? AND name = ?", userID, preset.Name).Count(&count)
		if count > 0 {
			continue
		}

		resp, err := s.CreateCharacter(preset)
		if err != nil {
			continue
		}
		results = append(results, *resp)
	}

	return results, nil
}

// SearchCharacter 搜索角色（发现页 - 公共角色）
func (s *CharacterService) SearchCharacter(req request.SearchCharacterReq) ([]response.PublicCharacterResp, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	req.PageSize = clampPageSize(req.PageSize)

	// 发现页：只显示 is_public = 1 的系统公共角色
	query := s.DB.Model(&database.Character{}).Where("is_public = ?", 1)

	// 关键词搜索
	if req.Keyword != "" {
		keyword := "%" + req.Keyword + "%"
		query = query.Where("name LIKE ? OR personality LIKE ? OR background LIKE ? OR occupation LIKE ?",
			keyword, keyword, keyword, keyword)
	}

	// 分类筛选
	if req.Category != "" && req.Category != "全部" {
		query = query.Where("relationship = ?", req.Category)
	}

	// 性别筛选
	if req.Gender != "" {
		query = query.Where("gender = ?", req.Gender)
	}

	// 性格筛选
	if req.Personality != "" {
		personality := "%" + req.Personality + "%"
		query = query.Where("personality LIKE ?", personality)
	}

	var total int64
	query.Count(&total)

	offset := (req.Page - 1) * req.PageSize

	var chars []database.Character
	err := query.Order("chat_count DESC, created_at DESC, id DESC").Offset(offset).Limit(req.PageSize).Find(&chars).Error

	if err != nil {
		return nil, 0, err
	}

	resp := make([]response.PublicCharacterResp, 0, len(chars))
	for _, char := range chars {
		resp = append(resp, s.toPublicCharacterResp(char))
	}

	return resp, total, nil
}

// GetRecommend 获取推荐角色
func (s *CharacterService) GetRecommend(req request.GetRecommendReq) ([]response.PublicCharacterResp, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	req.PageSize = clampPageSize(req.PageSize)

	query := s.DB.Model(&database.Character{}).Where("is_public = ?", true)

	// 如果有用户ID，获取用户性别偏好，推荐相反性别的角色
	if req.UserID != "" {
		var user database.User
		if err := s.DB.Where("user_id = ?", req.UserID).First(&user).Error; err == nil {
			// 根据用户性别推荐相反性别的角色
			// 用户male推荐female角色，用户female推荐male角色
			if user.Gender == "male" {
				query = query.Where("gender = ?", "female")
			} else if user.Gender == "female" {
				query = query.Where("gender = ?", "male")
			}
			// gender为unknown或其他值时不筛选
		}
	}

	var total int64
	query.Count(&total)

	offset := (req.Page - 1) * req.PageSize

	var chars []database.Character
	err := query.Order("chat_count DESC, created_at DESC, id DESC").Offset(offset).Limit(req.PageSize).Find(&chars).Error

	if err != nil {
		return nil, 0, err
	}

	resp := make([]response.PublicCharacterResp, 0, len(chars))
	for _, char := range chars {
		resp = append(resp, s.toPublicCharacterResp(char))
	}

	return resp, total, nil
}

// UpdateCharacterConfig 更新角色配置
func (s *CharacterService) UpdateCharacterConfig(userID, characterID string, config request.CharacterConfigReq) error {
	var char database.Character
	if err := s.DB.Where("character_id = ? AND user_id = ?", characterID, userID).First(&char).Error; err != nil {
		return err
	}

	// 更新字段
	updates := map[string]interface{}{}

	if config.SpeechTone != "" {
		updates["speech_tone"] = config.SpeechTone
	}
	if len(config.Personality) > 0 {
		updates["personality"] = joinStringArray(config.Personality)
	}
	if len(config.Hobbies) > 0 {
		updates["hobbies"] = joinStringArray(config.Hobbies)
	}
	if len(config.Catchphrases) > 0 {
		updates["catchphrases"] = joinStringArray(config.Catchphrases)
	}
	if config.Strengths != "" {
		updates["strengths"] = config.Strengths
	}
	if config.Weaknesses != "" {
		updates["weaknesses"] = config.Weaknesses
	}
	if config.InteractionMode != "" {
		updates["interaction_mode"] = config.InteractionMode
	}
	if config.Appearance != "" {
		updates["appearance"] = config.Appearance
	}
	if config.Birthday != "" {
		updates["birthday"] = config.Birthday
	}
	if config.Constellation != "" {
		updates["constellation"] = config.Constellation
	}
	if len(config.PersonalityTags) > 0 {
		updates["personality_tags"] = joinStringArray(config.PersonalityTags)
	}

	return s.DB.Model(&char).Updates(updates).Error
}

// GetCharacterConfig 获取角色配置
func (s *CharacterService) GetCharacterConfig(userID, characterID string) (*response.CharacterConfigResp, error) {
	var char database.Character
	if err := s.DB.Where("character_id = ? AND user_id = ?", characterID, userID).First(&char).Error; err != nil {
		return nil, err
	}

	return &response.CharacterConfigResp{
		CharacterID:     char.CharacterID,
		SpeechTone:      char.SpeechTone,
		Personality:     parseStringArray(char.Personality),
		Hobbies:         parseStringArray(char.Hobbies),
		Catchphrases:    parseStringArray(char.Catchphrases),
		Strengths:       char.Strengths,
		Weaknesses:      char.Weaknesses,
		InteractionMode: char.InteractionMode,
		Appearance:      char.Appearance,
		Birthday:        char.Birthday,
		Constellation:   char.Constellation,
		PersonalityTags: parseStringArray(char.PersonalityTags),
	}, nil
}

// Helper functions
func joinStringArray(arr []string) string {
	result := ""
	for i, s := range arr {
		if i > 0 {
			result += ","
		}
		result += s
	}
	return result
}

func parseStringArray(s string) []string {
	if s == "" {
		return []string{}
	}
	var result []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}
