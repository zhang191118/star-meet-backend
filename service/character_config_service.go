package service

import (
	"encoding/json"

	"star-meet-backend/model/database"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type TagGroup struct {
	Name       string   `json:"name"`
	SelectType string   `json:"selectType"`
	Tags       []string `json:"tags"`
}

type CharacterConfigResponse struct {
	TagGroups []TagGroup          `json:"tagGroups"`
	Gender    []map[string]string `json:"gender"`
	Scenes    []string            `json:"scenes"`
}

type CharacterConfigService struct {
	DB    *gorm.DB
	Redis *redis.Client
}

func (s *CharacterConfigService) GetConfig() *CharacterConfigResponse {
	return &CharacterConfigResponse{
		TagGroups: []TagGroup{
			{Name: "Roles", SelectType: "multi", Tags: []string{"醫生","律師","建築師","工程師","執行長","大學生","音樂人","運動員","軍人","警察","偵探","主廚","攝影師","作家","教師","船長","農場主人","酒保","隨拋","中醫師","半導體工程師","記者","新聞主播","遺產律師"}},
			{Name: "Personality", SelectType: "multi", Tags: []string{"保護慾強","溫柔體貼","冷淡疏離","佔有慾強","忠誠","脆弱敏感","自信霸道","腹黑","傲嬌","外冷內熱","幽默","神秘","沉默寡言","熱情似火"}},
			{Name: "Story", SelectType: "multi", Tags: []string{"暗戀","初戀重逢","契約婚姻","商業聯姻","仇敵變情人","青梅竹馬","失憶","秘密身份","豪門恩怨","師生戀","破鏡重圓","拯救與救贖"}},
			{Name: "Origin", SelectType: "single", Tags: []string{"原創","影視改編","遊戲改編","文學改編","歷史人物"}},
		},
		Gender: []map[string]string{{"label":"Male","value":"male"},{"label":"Female","value":"female"},{"label":"Not Set","value":"not_set"}},
		Scenes: []string{"酒吧","演唱會後台","辦公室","婚禮","醫院","捷運","書店","咖啡廳","夜市","工地","球場","機場","大學校園","寺廟","漁港","茶園"},
	}
}

func ParseTagsJSON(tagsJSON string) map[string][]string {
	if tagsJSON == "" {
		return nil
	}
	var tags map[string][]string
	if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
		return nil
	}
	return tags
}

func BuildTagsJSON(tags map[string][]string) string {
	b, _ := json.Marshal(tags)
	return string(b)
}

var _ = database.Character{}
