package v1

import (
	"star-meet-backend/api/v1"
	"star-meet-backend/global"
	"star-meet-backend/middleware"

	"github.com/gin-gonic/gin"
)

func InitStarMeetRouter(Router *gin.RouterGroup) {
	runtimeConfigAPI := v1.NewRuntimeConfigAPI(global.RuntimeConfigService)
	runtimeConfigPublicRouter := Router.Group("configs")
	{
		runtimeConfigPublicRouter.GET("/public", runtimeConfigAPI.Public)
	}

	// 运行时配置管理模块 (Admin token)
	runtimeConfigRouter := Router.Group("admin/configs")
	{
		runtimeConfigRouter.GET("", runtimeConfigAPI.List)
		runtimeConfigRouter.GET("/:key", runtimeConfigAPI.Get)
		runtimeConfigRouter.PUT("/:key", runtimeConfigAPI.Upsert)
	}

	// 移动端聚合模块 (Private)
	mobileAPI := v1.NewMobileAPI(global.MobileService)
	mobileRouter := Router.Group("mobile")
	mobileRouter.Use(middleware.Auth())
	{
		mobileRouter.GET("/home", mobileAPI.GetHome)
		mobileRouter.POST("/batch", mobileAPI.PostBatch)
	}

	meRouter := Router.Group("me")
	meRouter.Use(middleware.Auth())
	{
		meRouter.GET("/dashboard", mobileAPI.GetDashboard)
	}

	// 用户模块 (Public)
	userAPI := v1.NewUserAPI(global.UserService, global.VerifyCodeService)
	userRouter := Router.Group("user")
	{
		userRouter.POST("/register", userAPI.Register)               // 注册
		userRouter.POST("/login", userAPI.Login)                     // 登录
		userRouter.POST("/wechat-login", userAPI.WechatLogin)        // 微信登录
		userRouter.POST("/apple-login", userAPI.AppleLogin)          // 苹果登录
		userRouter.POST("/refresh", userAPI.RefreshToken)            // 刷新Token
		userRouter.POST("/send-verify-code", userAPI.SendVerifyCode) // 发送验证码
		userRouter.POST("/reset-password", userAPI.ResetPassword)    // 重置密码
	}

	// 用户模块 (Private)
	userAuthRouter := Router.Group("user")
	userAuthRouter.Use(middleware.Auth())
	{
		userAuthRouter.POST("/logout", userAPI.Logout)                  // 登出
		userAuthRouter.GET("/info", userAPI.GetUserInfo)                // 获取用户信息
		userAuthRouter.PUT("/info", userAPI.UpdateUserInfo)             // 更新用户信息
		userAuthRouter.GET("/settings", userAPI.GetUserSettings)        // 获取陪伴偏好设置
		userAuthRouter.PUT("/settings", userAPI.UpdateUserSettings)     // 更新陪伴偏好设置
		userAuthRouter.POST("/avatar", userAPI.UploadAvatar)            // 上传头像
		userAuthRouter.GET("/get-or-create", userAPI.GetOrCreateUser)   // 获取或创建用户（测试用）
		userAuthRouter.GET("/member-status", userAPI.CheckMemberStatus) // 检查会员状态
	}

	// 聊天模块 (Private + MinorProtection)
	chatAPI := v1.NewChatAPI(global.ChatService)
	chatRouter := Router.Group("chat")
	chatRouter.Use(middleware.Auth())
	chatRouter.Use(middleware.MinorProtection())
	{
		chatRouter.POST("/message", chatAPI.SendMessage)                 // 发送消息
		chatRouter.POST("/stream", chatAPI.StreamMessage)                // 流式发送消息
		chatRouter.GET("/history", chatAPI.GetHistory)                   // 获取历史
		chatRouter.GET("/sessions", chatAPI.GetSessions)                 // 会话列表
		chatRouter.PUT("/session/:session_id", chatAPI.UpdateSession)    // 更新会话（置顶等）
		chatRouter.DELETE("/session/:session_id", chatAPI.DeleteSession) // 删除会话
		chatRouter.DELETE("/session", chatAPI.DeleteSessionByCharacter)  // 按角色删除会话
		chatRouter.DELETE("/clear-history", chatAPI.ClearChatHistory)    // 清除聊天历史

		// 新增：消息已读/撤回/状态
		chatRouter.POST("/message/read", chatAPI.MarkMessageRead)        // 标记已读
		chatRouter.GET("/message/read", chatAPI.GetMessageReadStatus)    // 获取已读状态
		chatRouter.DELETE("/message/:message_id", chatAPI.RecallMessage) // 撤回消息
	}

	// 角色模块 (Public)
	charAPI := v1.NewCharacterAPI(global.CharacterService)
	charPublicRouter := Router.Group("character")
	{
		charPublicRouter.GET("/search", charAPI.Search)            // 搜索角色
		charPublicRouter.GET("/public", charAPI.GetPublicList)     // 公开角色列表（发现页）
		charPublicRouter.GET("/categories", charAPI.GetCategories) // 角色分类
		charPublicRouter.GET("/recommend", charAPI.GetRecommend)   // 推荐角色
		charPublicRouter.GET("/:character_id", charAPI.GetDetail)  // 角色详情
	}

	// 角色模块 (Private)
	charAuthRouter := Router.Group("character")
	charAuthRouter.Use(middleware.Auth())
	{
		charAuthRouter.POST("/create", charAPI.Create)                       // 创建角色
		charAuthRouter.POST("/upload-image", charAPI.UploadImage)            // 上传角色图片
		charAuthRouter.GET("/list", charAPI.GetList)                         // 我的角色列表
		charAuthRouter.GET("/my", charAPI.GetMyCharacters)                   // 我的私有角色列表
		charAuthRouter.GET("/init-default", charAPI.InitDefault)             // 初始化默认角色
		charAuthRouter.GET("/init-presets", charAPI.InitPresets)             // 初始化预设角色列表
		charAuthRouter.PUT("/:character_id", charAPI.Update)                 // 更新角色
		charAuthRouter.DELETE("/:character_id", charAPI.Delete)              // 删除角色
		charAuthRouter.POST("/:character_id/favorite", charAPI.Favorite)     // 收藏
		charAuthRouter.POST("/:character_id/unfavorite", charAPI.Unfavorite) // 取消收藏
		charAuthRouter.POST("/:character_id/encounter", charAPI.Encounter)
		charAuthRouter.GET("/:character_id/config", charAPI.GetConfig)
		charAuthRouter.PUT("/:character_id/config", charAPI.UpdateConfig) // 邂逅角色
	}

	// 记忆模块 (Private)
	memoryAPI := v1.NewMemoryAPI(global.MemoryService)
	memoryRouter := Router.Group("memory")
	memoryRouter.Use(middleware.Auth())
	{
		memoryRouter.GET("/list", memoryAPI.GetList)         // 记忆列表
		memoryRouter.GET("/stats", memoryAPI.GetStats)       // 记忆统计
		memoryRouter.POST("/add", memoryAPI.Add)             // 添加记忆
		memoryRouter.PUT("/:memory_id", memoryAPI.Update)    // 更新记忆
		memoryRouter.DELETE("/:memory_id", memoryAPI.Delete) // 删除记忆
		memoryRouter.GET("/search", memoryAPI.Search)        // 搜索记忆
	}

	// 订阅模块
	subAPI := v1.NewSubscriptionAPI()
	subRouter := Router.Group("subscription")
	{
		subRouter.GET("/plans", subAPI.GetPlans) // 订阅计划列表公开
	}

	subAuthRouter := Router.Group("subscription")
	subAuthRouter.Use(middleware.Auth())
	{
		subAuthRouter.GET("/status", subAPI.GetMySubscription)
		subAuthRouter.POST("/apple/verify", subAPI.VerifyAppleReceipt)
		subAuthRouter.POST("/cancel", subAPI.CancelSubscription)
	}

	// 抽卡模块 (Public)
	gachaAPI := v1.NewGachaAPI()
	gachaRouter := Router.Group("gacha")
	{
		gachaRouter.GET("/pools", gachaAPI.GetPools) // 卡池列表
	}

	// 抽卡模块 (Private)
	gachaAuthRouter := Router.Group("gacha")
	gachaAuthRouter.Use(middleware.Auth())
	{
		gachaAuthRouter.GET("/characters", gachaAPI.GetCharacters) // 我的角色
		gachaAuthRouter.GET("/history", gachaAPI.GetHistory)       // 抽卡历史
		gachaAuthRouter.POST("/draw", gachaAPI.Draw)               // 抽卡
		gachaAuthRouter.POST("/favorite", gachaAPI.ToggleFavorite) // 切换喜欢
	}

	// 货币模块 (Private)
	currencyAPI := v1.NewCurrencyAPI()
	currencyAuthRouter := Router.Group("currency")
	currencyAuthRouter.Use(middleware.Auth())
	{
		currencyAuthRouter.GET("/balance", currencyAPI.GetBalance)           // 货币余额
		currencyAuthRouter.GET("/packages", currencyAPI.GetPackages)         // 充值套餐
		currencyAuthRouter.GET("/transactions", currencyAPI.GetTransactions) // 交易记录
		currencyAuthRouter.POST("/recharge", currencyAPI.Recharge)           // 充值
	}
}
