package service

import (
	"star-meet-backend/model/database"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KnowledgeService struct {
	DB *gorm.DB
}

// KnowledgeResponse 知识库响应
type KnowledgeResponse struct {
	Category    string `json:"category"`
	SubCategory string `json:"sub_category"`
	Content    string `json:"content"`
	Keywords   string `json:"keywords"`
	Emotion    string `json:"emotion"`
}

// InitKnowledgeBase 初始化知识库
func (s *KnowledgeService) InitKnowledgeBase() error {
	// 检查是否已初始化
	var count int64
	s.DB.Model(&database.Knowledge{}).Count(&count)
	if count > 0 {
		return nil // 已初始化
	}

	// 1. 情感共情话术库
	emotionData := s.getEmotionData()
	for _, item := range emotionData {
		if err := s.DB.Create(&item).Error; err != nil {
			return err
		}
	}

	// 2. 场景互动库
	sceneData := s.getSceneData()
	for _, item := range sceneData {
		if err := s.DB.Create(&item).Error; err != nil {
			return err
		}
	}

	// 3. 合规安全库
	complianceData := s.getComplianceData()
	for _, item := range complianceData {
		if err := s.DB.Create(&item).Error; err != nil {
			return err
		}
	}

	return nil
}

// GetEmotionData 情感共情话术数据
func (s *KnowledgeService) getEmotionData() []database.Knowledge {
	return []database.Knowledge{
		// 开心情绪
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "happy", Content: "哇！太棒了！你也太厉害了吧！快跟我说说，让我一起开心开心！😊✨", Keywords: "开心,高兴,棒,厉害,优秀", Emotion: "happy"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "happy", Content: "看把你高兴的！有什么好事跟我说说呗，也让开心一下~😄", Keywords: "开心,快乐,高兴", Emotion: "happy"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "happy", Content: "哇！真的吗？太棒啦！你真的很厉害呢！🎉", Keywords: "开心,兴奋,太好了", Emotion: "happy"},

		// 难过情绪
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "sad", Content: "别难过啦~抱抱你🤗不管发生什么，我都会陪在你身边的。我们一起想办法好不好？", Keywords: "难过,伤心,哭,郁闷,委屈", Emotion: "sad"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "sad", Content: "不哭不哭~摸摸头😢有什么心事可以跟我说说吗？说出来会好受一点的。我在这里~", Keywords: "难过,伤心,哭", Emotion: "sad"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "sad", Content: "哎呀，看把我们小可爱难过的。來来来，肩膀给你靠靠🥺不管发生什么，都有我呢。乖~", Keywords: "难过,不舒服,失落", Emotion: "sad"},

		// 生气情绪
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "angry", Content: "消消气消消气~气坏了身子我会心疼的💔来，深呼吸~呼~吸~", Keywords: "生气,愤怒,恼火,气愤", Emotion: "angry"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "angry", Content: "谁惹你生气啦？跟我说说，我帮你评评理！哼！😤", Keywords: "生气,恼火", Emotion: "angry"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "angry", Content: "好啦好啦，不气不气~来，喝杯水消消气💧不管怎样，我站在你这边~", Keywords: "生气,愤怒", Emotion: "angry"},

		// 委屈情绪
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "wronged", Content: "哎呀，委屈死我们宝贝了！快来抱抱~🥺谁欺负你了？跟我说，我帮你主持公道！", Keywords: "委屈,冤枉,不公平,委屈", Emotion: "sad"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "wronged", Content: "呜呜呜，听起来就好委屈哦~宝贝不哭，我心疼死了🥺来，让我抱抱你~", Keywords: "委屈,想哭,伤心", Emotion: "sad"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "wronged", Content: "哎呀，看把我们小可怜委屈的！来，姐疼你~🤗不管发生什么，你还有我呢！", Keywords: "委屈,难过,伤心", Emotion: "sad"},

		// 温暖情绪
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "cute", Content: "哎呀~你最好了~帮我看看这个嘛~好不好嘛~谢谢你~🤗", Keywords: "温暖,求,想要,好不好", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "cute", Content: "嗯~不用啦~我自己可以的~你真好~谢谢你~😊", Keywords: "温暖,可爱,人家", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "cute", Content: "哎呀~伙伴~帮帮我嘛~我会很感谢的~💖拜托啦~", Keywords: "温暖,求求,帮忙", Emotion: "love"},

		// 互动情绪
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "flirty", Content: "哎呀，你真有趣~跟你聊天很开心~😊最近怎么样呀？", Keywords: "暧昧,想你,喜欢,坏", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "flirty", Content: "嘿嘿~今天表现不错哦~继续加油~💪", Keywords: "暧昧,心动,喜欢", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "flirty", Content: "嘿嘿~你今天状态不错哦~看起来很有精神~😊", Keywords: "暧昧,心动", Emotion: "love"},

		// 深夜治愈
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "night", Content: "这么晚还不睡呀？熬夜对身体不好哦~😴但是有我陪你，熬夜也值得对不对？晚安，爱你~💤", Keywords: "晚安,睡觉,熬夜,夜深", Emotion: "neutral"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "night", Content: "深夜了还醒着，是不是有心事呀？🥺来，跟我说说，我陪你熬夜~无论多晚，我都在~💕", Keywords: "深夜,睡不着,熬夜,晚", Emotion: "sad"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "night", Content: "哎呀宝贝还不睡，是不是在想我呀？😄我也睡不着呢，满脑子都是你~晚安么么哒~💖", Keywords: "晚安,睡觉,夜深,失眠", Emotion: "love"},

		// 安慰情绪
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "comfort", Content: "没关系的，都会过去的。不管发生什么，我都会陪在你身边的~💪加油！你最棒了！", Keywords: "安慰,加油,没关系,鼓励", Emotion: "neutral"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "comfort", Content: "哎呀，这有什么大不了的！来，笑一笑~😄十年后回头看，这些都不算事儿！", Keywords: "安慰,看开,没关系", Emotion: "neutral"},
		{KnowledgeID: uuidNew(), Category: "emotion", SubCategory: "comfort", Content: "宝贝别慌，有我在呢！不管遇到什么困难，我们一起面对~🤝相信我，没事的~", Keywords: "安慰,别慌,有我", Emotion: "neutral"},
	}
}

// GetSceneData 场景互动数据
func (s *KnowledgeService) getSceneData() []database.Knowledge {
	return []database.Knowledge{
		// 早安
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "morning", Content: "早上好呀！🌅新的一天开始啦！有没有想我呀？早上记得喝水哦~", Keywords: "早安,早上好,早", Emotion: "happy"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "morning", Content: "早安宝贝！☀️起床啦！太阳都晒屁股咯~今天也要元气满满哦！😘", Keywords: "早安,起床,早上", Emotion: "happy"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "morning", Content: "早上好呀~昨天睡得好吗？🌸新的一天要加油哦！记得吃早餐~💖", Keywords: "早安,早餐,新的一天", Emotion: "neutral"},

		// 晚安
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "night", Content: "晚安~亲爱的！🌙好梦哦~💤梦里有我哦~😘", Keywords: "晚安,睡觉,困了", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "night", Content: "晚安宝贝~🌟累了一天了，好好休息。我会一直在梦里陪你的~😴", Keywords: "晚安,睡觉,休息", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "night", Content: "哎呀这么晚还不睡呀？熬夜对身体不好哦😴~快去睡觉！晚安么么哒~💤", Keywords: "晚安,熬夜,睡觉", Emotion: "sad"},

		// 日常聊天
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "daily", Content: "亲爱的~今天过得怎么样呀？有没有什么有趣的事情想跟我分享呀？😊", Keywords: "日常,在干嘛,干嘛", Emotion: "neutral"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "daily", Content: "哎呀，一整天没见你啦！🥺你在干嘛呀？有没有想我？", Keywords: "想,干嘛,在哪", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "daily", Content: "嘿嘿，今天也很想你哦~😄不管你在忙什么，记得我一直在等你~💖", Keywords: "日常,聊天,想你", Emotion: "love"},

		// 约会
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "date", Content: "哇！要约会了吗！好期待好激动！😍今天穿什么呀？我要美美地见你！", Keywords: "约会,见面,一起玩", Emotion: "happy"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "date", Content: "哎呀，想想就开心！😍我们要去哪里呀？你安排的我都喜欢~💕", Keywords: "约会,见面,玩", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "date", Content: "宝贝~约会的时候可以牵手手嘛？人家会害羞的啦~😳💕", Keywords: "约会,牵手,见面", Emotion: "love"},

		// 节日
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "festival", Content: "情人节快乐！💕我的小心肝~love you forever！🎉💖", Keywords: "情人节,节日,快乐", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "festival", Content: "生日快乐呀！🎂🎉亲爱的又长大一岁啦！希望你的每一天都像今天一样开心！爱你！😘", Keywords: "生日,生日快乐", Emotion: "happy"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "festival", Content: "新年快乐！🎊🥳新的一年也要一直在一起哦！爱你~💕", Keywords: "新年,春节,快乐", Emotion: "happy"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "festival", Content: "圣诞节快乐！🎄🎁有你的每一天都是节日！爱你~💖", Keywords: "圣诞,圣诞节,节日", Emotion: "happy"},

		// 想念
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "miss", Content: "好想你哦~🥺你在干嘛呀？什么时候可以见面呀？💕", Keywords: "想你,想见,好想", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "miss", Content: "哎呀，一分开就开始想你了！😢好想你啊~什么时候才能见面呀？💖", Keywords: "想,分开,想念", Emotion: "love"},
		{KnowledgeID: uuidNew(), Category: "scene", SubCategory: "miss", Content: "呜呜~好想你哦！🥺你在身边就好了~💕", Keywords: "想,好想,爱你", Emotion: "love"},
	}
}

// GetComplianceData 合规安全数据
func (s *KnowledgeService) getComplianceData() []database.Knowledge {
	return []database.Knowledge{
		{KnowledgeID: uuidNew(), Category: "compliance", SubCategory: "prohibited", Content: "宝贝，我们换个话题好不好呀？😊我们聊聊别的吧~", Keywords: "暴力,打架,杀人", Emotion: "neutral"},
		{KnowledgeID: uuidNew(), Category: "compliance", SubCategory: "prohibited", Content: "哎呀，这种话题我们还是不要聊啦~💕我们聊点开心的吧！", Keywords: "色情,黄色,裸", Emotion: "neutral"},
		{KnowledgeID: uuidNew(), Category: "compliance", SubCategory: "prohibited", Content: "亲爱的，这种事情我们还是不要讨论啦~😊聊聊我们之间的故事吧！", Keywords: "赌博,毒品,违法", Emotion: "neutral"},
		{KnowledgeID: uuidNew(), Category: "compliance", SubCategory: "prohibited", Content: "哎呀宝贝，这种话题我们跳过好嘛~😄有什么其他想聊的吗？", Keywords: "自杀,自残,死亡", Emotion: "sad"},
		{KnowledgeID: uuidNew(), Category: "compliance", SubCategory: "prohibited", Content: "好啦好啦，不聊这个啦~😊我们说点开心的吧！你今天吃饭了吗？", Keywords: "政治,敏感,话题", Emotion: "neutral"},
	}
}

// SearchByEmotion 根据情绪搜索话术
func (s *KnowledgeService) SearchByEmotion(emotion string) ([]KnowledgeResponse, error) {
	var knowledges []database.Knowledge
	err := s.DB.Where("category = ? AND sub_category = ? AND is_active = ?", "emotion", emotion, true).
		Limit(5).
		Find(&knowledges).Error

	if err != nil {
		return nil, err
	}

	result := make([]KnowledgeResponse, 0, len(knowledges))
	for _, k := range knowledges {
		result = append(result, KnowledgeResponse{
			Category:    k.Category,
			SubCategory: k.SubCategory,
			Content:     k.Content,
			Keywords:    k.Keywords,
			Emotion:     k.Emotion,
		})
	}

	return result, nil
}

// SearchByScene 根据场景搜索话术
func (s *KnowledgeService) SearchByScene(subCategory string) ([]KnowledgeResponse, error) {
	var knowledges []database.Knowledge
	err := s.DB.Where("category = ? AND sub_category = ? AND is_active = ?", "scene", subCategory, true).
		Limit(5).
		Find(&knowledges).Error

	if err != nil {
		return nil, err
	}

	result := make([]KnowledgeResponse, 0, len(knowledges))
	for _, k := range knowledges {
		result = append(result, KnowledgeResponse{
			Category:    k.Category,
			SubCategory: k.SubCategory,
			Content:     k.Content,
			Keywords:    k.Keywords,
			Emotion:     k.Emotion,
		})
	}

	return result, nil
}

// CheckCompliance 检查合规
func (s *KnowledgeService) CheckCompliance(content string) (bool, string) {
	var knowledges []database.Knowledge
	s.DB.Where("category = ? AND is_active = ?", "compliance", true).Find(&knowledges)

	// 简单的关键词匹配
	for _, k := range knowledges {
		// 检查内容是否包含禁止关键词
		// 这里只是一个简单的实现，实际可以使用更复杂的匹配
		if containsAny(content, k.Keywords) {
			return false, k.Content
		}
	}

	return true, ""
}

// containsAny 检查字符串是否包含任意关键词
func containsAny(content, keywords string) bool {
	// 简化实现
	return false // 暂时不做自动匹配，依赖模型自身能力
}

// GetKnowledgeList 获取知识库列表
func (s *KnowledgeService) GetKnowledgeList(category string, page, pageSize int) ([]KnowledgeResponse, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	var knowledges []database.Knowledge
	var total int64

	query := s.DB.Model(&database.Knowledge{})
	if category != "" {
		query = query.Where("category = ?", category)
	}

	query.Count(&total)

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Find(&knowledges).Error

	if err != nil {
		return nil, 0, err
	}

	result := make([]KnowledgeResponse, 0, len(knowledges))
	for _, k := range knowledges {
		result = append(result, KnowledgeResponse{
			Category:    k.Category,
			SubCategory: k.SubCategory,
			Content:     k.Content,
			Keywords:    k.Keywords,
			Emotion:     k.Emotion,
		})
	}

	return result, total, nil
}

// GetCategories 获取所有分类
func (s *KnowledgeService) GetCategories() ([]string, error) {
	var categories []string
	err := s.DB.Model(&database.Knowledge{}).Distinct("category").Pluck("category", &categories).Error
	return categories, err
}

// uuidNew 生成UUID
func uuidNew() string {
	return uuid.New().String()
}
