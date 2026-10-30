package v1

import (
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

type MusicAPI struct {
	Service *service.MusicService
}

func NewMusicAPI(service *service.MusicService) *MusicAPI {
	return &MusicAPI{Service: service}
}

// GetPlaylist 获取播放列表（客户端用）
func (a *MusicAPI) GetPlaylist(c *gin.Context) {
	musics, err := a.Service.GetEnabled()
	if err != nil {
		response.FailWithMessage(c, "获取音乐列表失败")
		return
	}
	if musics == nil {
		musics = []response.MusicResp{}
	}
	response.OkWithData(c, musics)
}
