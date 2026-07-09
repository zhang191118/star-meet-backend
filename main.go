package main

import (
	"flag"
	"fmt"
	"html/template"
	"os"
	"strconv"
	"strings"
	"time"

	coreoss "star-meet-backend/core/oss"
	"star-meet-backend/global"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/pkg/logger"
	"star-meet-backend/pkg/utils"
	"star-meet-backend/router/v1"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	mem0 "star-meet-backend/core/mem0"
)

func init() {
	// 禁用代理
	os.Unsetenv("http_proxy")
	os.Unsetenv("HTTP_PROXY")
	os.Unsetenv("https_proxy")
	os.Unsetenv("HTTPS_PROXY")
	// 设置NO_PROXY
	os.Setenv("NO_PROXY", "localhost,127.0.0.1")
}

func ensureCharacterCustomColumns(db *gorm.DB) error {
	requiredColumns := []string{
		"speech_tone",
		"hobbies",
		"catchphrases",
		"strengths",
		"weaknesses",
		"interaction_mode",
		"appearance",
		"birthday",
		"constellation",
		"personality_tags",
	}

	for _, column := range requiredColumns {
		if db.Migrator().HasColumn(&database.Character{}, column) {
			continue
		}
		if err := db.Migrator().AddColumn(&database.Character{}, column); err != nil {
			return fmt.Errorf("add characters.%s failed: %w", column, err)
		}
		logger.Infof("characters.%s added by schema reconciliation", column)
	}

	return nil
}

func ensureUserCurrencyIndexes(db *gorm.DB) error {
	migrator := db.Migrator()
	legacyIndexes := []string{
		"idx_user_currencies_user_id",
		"user_id",
	}
	for _, indexName := range legacyIndexes {
		if !migrator.HasIndex(&database.UserCurrency{}, indexName) {
			continue
		}
		if err := migrator.DropIndex(&database.UserCurrency{}, indexName); err != nil {
			return fmt.Errorf("drop legacy user_currencies.%s failed: %w", indexName, err)
		}
		logger.Infof("legacy user_currencies.%s dropped by schema reconciliation", indexName)
	}

	if !migrator.HasIndex(&database.UserCurrency{}, "idx_user_currency") {
		if err := migrator.CreateIndex(&database.UserCurrency{}, "idx_user_currency"); err != nil {
			return fmt.Errorf("create user_currencies.idx_user_currency failed: %w", err)
		}
		logger.Info("user_currencies.idx_user_currency created by schema reconciliation")
	}

	return nil
}

func runtimeConfigValue(key, viperKey, envKey, defaultValue string) string {
	if global.RuntimeConfigService != nil {
		if value, err := global.RuntimeConfigService.Get(key); err == nil && strings.TrimSpace(value) != "" {
			return value
		}
	}
	if envKey != "" {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			return value
		}
	}
	if viperKey != "" {
		if value := strings.TrimSpace(viper.GetString(viperKey)); value != "" {
			return value
		}
	}
	return defaultValue
}

func runtimeConfigInt(key, viperKey, envKey string, defaultValue int) int {
	value := runtimeConfigValue(key, viperKey, envKey, "")
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func seedRuntimeConfigDefaults() {
	if global.RuntimeConfigService == nil {
		return
	}

	defaults := []service.RuntimeConfigDefault{
		{
			Key:         service.RuntimeConfigOpenAICompatBaseURL,
			Value:       runtimeConfigSeedValue("openai_compat.base_url", "OPENAI_COMPAT_BASE_URL", "https://ai98pro.xyz"),
			Sensitive:   false,
			Description: "OpenAI-compatible chat API base URL",
		},
		{
			Key:         service.RuntimeConfigOpenAICompatAPIKey,
			Value:       runtimeConfigSensitiveSeedValue("openai_compat.api_key", "OPENAI_COMPAT_API_KEY"),
			Sensitive:   true,
			Description: "OpenAI-compatible chat API key",
		},
		{
			Key:         service.RuntimeConfigOpenAICompatModel,
			Value:       runtimeConfigSeedValue("openai_compat.model", "OPENAI_COMPAT_MODEL", "grok-4.3-fast"),
			Sensitive:   false,
			Description: "OpenAI-compatible primary chat model",
		},
	}

	if err := global.RuntimeConfigService.SeedDefaults(defaults); err != nil {
		logger.Warnf("运行时配置默认项写入数据库失败: %v", err)
		return
	}
	logger.Info("运行时配置默认项已写入数据库")
}

func runtimeConfigSeedValue(viperKey, envKey, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" && !isRuntimeConfigPlaceholder(value) {
		return value
	}
	if value := strings.TrimSpace(viper.GetString(viperKey)); value != "" && !isRuntimeConfigPlaceholder(value) {
		return value
	}
	return defaultValue
}

func runtimeConfigSensitiveSeedValue(viperKey, envKey string) string {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" && !isRuntimeConfigPlaceholder(value) {
		return value
	}
	if value := strings.TrimSpace(viper.GetString(viperKey)); value != "" && !isRuntimeConfigPlaceholder(value) {
		return value
	}
	return ""
}

func isRuntimeConfigPlaceholder(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "" ||
		strings.HasPrefix(normalized, "your_") ||
		strings.HasPrefix(normalized, "your-") ||
		strings.HasPrefix(normalized, "replace-with") ||
		strings.HasPrefix(normalized, "change-me")
}

func main() {
	// 初始化配置
	port := flag.String("port", "8080", "server port")
	host := flag.String("host", "127.0.0.1", "server host")
	flag.Parse()

	// 读取配置文件
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	configLoaded := false
	for _, configName := range []string{"config", "config.example"} {
		viper.SetConfigName(configName)
		if err := viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				continue
			}
			logger.Warnf("读取配置文件失败: %v", err)
			continue
		}
		configLoaded = true
		break
	}

	if _, err := os.Stat("config.local.yaml"); err == nil {
		viper.SetConfigFile("config.local.yaml")
		if err := viper.MergeInConfig(); err != nil {
			logger.Warnf("读取本地覆盖配置失败: %v", err)
		}
	}

	if !configLoaded {
		logger.Warnf("配置文件未找到，使用环境变量")
	}

	// 初始化日志
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	if err := logger.Init(&logger.Config{
		Level:      logLevel,
		Path:       "./logs",
		MaxSize:    100,
		MaxBackups: 30,
		MaxAge:     7,
		Compress:   true,
		JSONFormat: false,
	}); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	logger.Info("Logger initialized successfully")

	// 初始化数据库
	var db *gorm.DB
	var err error

	// 从环境变量获取配置，默认为本地 ServBay 环境
	dbHost := os.Getenv("DATABASE_HOST")
	if dbHost == "" {
		dbHost = "127.0.0.1"
	}
	dbPort := os.Getenv("DATABASE_PORT")
	if dbPort == "" {
		dbPort = "3306"
	}
	dbUser := os.Getenv("DATABASE_USER")
	if dbUser == "" {
		dbUser = "root"
	}
	dbPassword := os.Getenv("DATABASE_PASSWORD")
	if dbPassword == "" {
		dbPassword = "ServBay.dev"
	}
	dbName := os.Getenv("DATABASE_NAME")
	if dbName == "" {
		dbName = "star_meet"
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local&timeout=10s&readTimeout=30s&writeTimeout=30s",
		dbUser, dbPassword, dbHost, dbPort, dbName)

	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		if dbHost != "127.0.0.1" && dbHost != "localhost" {
			logger.Fatalf("MySQL连接失败 (%s): %v。在非本地环境不使用SQLite回退。", dsn, err)
		}

		logger.Infof("MySQL连接失败 (%s): %v，尝试使用SQLite", dsn, err)

		// 使用SQLite作为备用
		db, err = gorm.Open(sqlite.Open("star_meet.db"), &gorm.Config{})
		if err != nil {
			logger.Fatalf("无法创建数据库: %v", err)
		}
	}

	// 自动迁移表
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	err = db.AutoMigrate(
		&database.RuntimeConfig{},
		&database.Plan{},
		&database.User{},
		&database.UserSettings{},
		&database.Character{},
		&database.Session{},
		&database.Message{},
		&database.Favorite{},
		&database.Memory{},
		&database.Knowledge{},
		&database.Subscription{},
		&database.UserCurrency{},
		&database.CurrencyTransaction{},
		&database.RechargePackage{},
		&database.GachaPool{},
		&database.GachaCharacter{},
		&database.UserGachaDraw{},
		&database.UserGachaRecord{},
		&database.UserCharacter{},
	)
	if err != nil {
		logger.Infof("表迁移失败: %v", err)
	} else {
		logger.Info("数据库迁移成功")
	}
	if err := ensureCharacterCustomColumns(db); err != nil {
		logger.Warnf("角色扩展字段补齐失败: %v", err)
	}
	if err := ensureUserCurrencyIndexes(db); err != nil {
		logger.Warnf("用户货币索引补齐失败: %v", err)
	}

	global.GVA_DB = db
	global.RuntimeConfigService = global.NewRuntimeConfigService(db)
	seedRuntimeConfigDefaults()

	// 配置数据库连接池
	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxOpenConns(100)
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetConnMaxLifetime(time.Hour)
		sqlDB.SetConnMaxIdleTime(time.Minute * 30)
		logger.Info("数据库连接池配置成功")
	}

	// 初始化Redis（可选）
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "127.0.0.1"
	}
	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}
	redisPassword := os.Getenv("REDIS_PASSWORD")
	if redisPassword == "" {
		redisPassword = ""
	}

	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})
	global.GVA_REDIS = redisClient

	// 初始化限流中间件 (使用已有的简单实现)
	logger.Info("Rate limiter initialized")

	jwtSecret := runtimeConfigValue("jwt.secret", "jwt.jwt_secret", "JWT_SECRET", "")
	utils.SetJWTSecret(jwtSecret)
	if jwtSecret == "" || jwtSecret == "star-meet-secret-key-change-in-production" {
		logger.Warn("JWT secret uses development fallback; configure runtime_configs.jwt.secret before production")
	}

	// 初始化AI客户端（可选配置）
	mem0Host := runtimeConfigValue("mem0.host", "mem0.host", "MEM0_HOST", "https://api.mem0.ai")
	mem0APIKey := runtimeConfigValue("mem0.api_key", "mem0.api_key", "MEM0_API_KEY", "")
	mem0Client := mem0.NewClient(mem0Host, mem0APIKey)
	global.Mem0Client = mem0Client

	if mem0APIKey != "" && mem0APIKey != "your_mem0_api_key" {
		logger.Infof("Mem0 client initialized: %s (API Key configured)", mem0Host)
	} else {
		logger.Warn("Mem0 API Key not configured")
	}

	qwenAPIKey := runtimeConfigValue("aliyun.qwen.api_key", "aliyun.qwen.api_key", "DASHSCOPE_API_KEY", "")
	if qwenAPIKey == "" || qwenAPIKey == "your-dashscope-api-key" {
		logger.Warn("Qwen API Key not configured")
	}
	qwenClient := global.NewQwenClient(qwenAPIKey)
	global.QwenClient = qwenClient
	logger.Info("Qwen client initialized")

	// MiniMax 客户端
	miniMaxAPIKey := runtimeConfigValue("minimax.api_key", "minimax.api_key", "MINIMAX_API_KEY", "")
	if miniMaxAPIKey == "" {
		logger.Warn("MiniMax API key is not configured")
	}
	miniMaxClient := global.NewMiniMaxClient(miniMaxAPIKey)
	global.MiniMaxClient = miniMaxClient
	logger.Info("MiniMax client initialized")

	// OpenAI-compatible chat gateway (for providers such as ai98pro/grok).
	openAICompatBaseURL := runtimeConfigValue(service.RuntimeConfigOpenAICompatBaseURL, "openai_compat.base_url", "OPENAI_COMPAT_BASE_URL", "")
	openAICompatAPIKey := runtimeConfigValue(service.RuntimeConfigOpenAICompatAPIKey, "openai_compat.api_key", "OPENAI_COMPAT_API_KEY", "")
	openAICompatModel := runtimeConfigValue(service.RuntimeConfigOpenAICompatModel, "openai_compat.model", "OPENAI_COMPAT_MODEL", "")
	openAICompatClient := global.NewOpenAICompatClient(openAICompatBaseURL, openAICompatAPIKey, openAICompatModel)
	global.OpenAICompatClient = openAICompatClient
	if openAICompatClient.Configured() {
		logger.Infof("OpenAI-compatible chat client initialized: %s", openAICompatModel)
	} else {
		logger.Warn("OpenAI-compatible chat client is not configured")
	}

	// DeepSeek 客户端
	deepSeekAPIKey := runtimeConfigValue("deepseek.api_key", "deepseek.api_key", "DEEPSEEK_API_KEY", "")
	if deepSeekAPIKey == "" {
		logger.Warn("DeepSeek API key is not configured")
	}
	deepSeekClient := global.NewDeepSeekClient(deepSeekAPIKey)
	global.DeepSeekClient = deepSeekClient
	logger.Info("DeepSeek client initialized")

	// Gemini 客户端 (Google AI)
	geminiAPIKey := runtimeConfigValue("gemini.api_key", "gemini.api_key", "GEMINI_API_KEY", "")
	if geminiAPIKey == "" {
		logger.Warn("Gemini API key is not configured")
	}
	geminiModel := runtimeConfigValue("gemini.model", "gemini.model", "GEMINI_MODEL", "gemini-2.0-flash")
	geminiClient := global.NewGeminiClient(geminiAPIKey, geminiModel)
	global.GeminiClient = geminiClient
	logger.Infof("Gemini client initialized: %s", geminiModel)

	// 邮箱配置
	emailHost := runtimeConfigValue("email.host", "email.host", "EMAIL_HOST", "smtp.gmail.com")
	emailPort := runtimeConfigInt("email.port", "email.port", "EMAIL_PORT", 587)
	emailUsername := runtimeConfigValue("email.username", "email.username", "EMAIL_USERNAME", "")
	emailPassword := runtimeConfigValue("email.password", "email.password", "EMAIL_PASSWORD", "")
	emailFromName := runtimeConfigValue("email.from_name", "email.from_name", "EMAIL_FROM_NAME", "星遇 StarMeet")
	emailFromEmail := runtimeConfigValue("email.from_email", "email.from_email", "EMAIL_FROM_EMAIL", "noreply@starmeet.app")

	if emailUsername != "" {
		logger.Infof("Email configured: %s", emailUsername)
	} else {
		logger.Warn("Email is not configured,验证码将只打印到控制台")
	}

	// 保存邮箱配置到全局
	global.EmailConfigInstance = &global.EmailConfig{
		Host:      emailHost,
		Port:      emailPort,
		Username:  emailUsername,
		Password:  emailPassword,
		FromName:  emailFromName,
		FromEmail: emailFromEmail,
	}
	service.SetEmailConfig(&service.EmailConfig{
		Host:      emailHost,
		Port:      emailPort,
		Username:  emailUsername,
		Password:  emailPassword,
		FromName:  emailFromName,
		FromEmail: emailFromEmail,
	})

	coreoss.SetConfig(coreoss.Config{
		Endpoint:        runtimeConfigValue("oss.endpoint", "oss.endpoint", "OSS_ENDPOINT", ""),
		AccessKeyID:     runtimeConfigValue("oss.access_key_id", "oss.access_key_id", "OSS_ACCESS_KEY_ID", ""),
		AccessKeySecret: runtimeConfigValue("oss.access_key_secret", "oss.access_key_secret", "OSS_ACCESS_KEY_SECRET", ""),
		BucketName:      runtimeConfigValue("oss.bucket_name", "oss.bucket_name", "OSS_BUCKET_NAME", ""),
	})

	// 智谱AI Embedding客户端 (用于向量记忆)
	embeddingClient := global.NewEmbeddingClient(miniMaxAPIKey)
	global.EmbeddingClient = embeddingClient

	// Qdrant向量数据库客户端
	qdrantHost := runtimeConfigValue("qdrant.host", "qdrant.host", "QDRANT_HOST", "http://localhost:6333")
	qdrantClient := global.NewQdrantClient(qdrantHost)
	global.QdrantClient = qdrantClient
	logger.Infof("Qdrant vector database initialized: %s", qdrantHost)

	// 初始化服务层
	global.VerifyCodeService = global.NewVerifyCodeService(db, redisClient)
	global.UserService = global.NewUserService(db, global.VerifyCodeService)
	global.CharacterService = global.NewCharacterService(db)
	global.MemoryService = global.NewMemoryService(db, mem0Client)
	global.MobileService = global.NewMobileService(db)
	knowledgeService := global.NewKnowledgeService(db)
	global.KnowledgeService = knowledgeService
	global.ChatService = global.NewChatService(db, redisClient, mem0Client, qwenClient, miniMaxClient, openAICompatClient, deepSeekClient, geminiClient, embeddingClient, qdrantClient, knowledgeService, global.RuntimeConfigService)

	// 初始化知识库
	if err := global.KnowledgeService.InitKnowledgeBase(); err != nil {
		logger.Infof("知识库初始化失败: %v", err)
	} else {
		logger.Info("知识库初始化完成")
	}

	// 强制初始化公共角色 (确保 季夏 等新角色出现)
	logger.Info("正在初始化公共角色...")
	// 使用统一的 system_admin ID，并清理之前的同名角色确保更新
	global.CharacterService.InitPresetCharacters("system_admin")
	if err := service.NewGachaService(db).EnsureDefaultGachaContent(); err != nil {
		logger.Warnf("默认抽卡池初始化失败: %v", err)
	} else {
		logger.Info("默认抽卡池初始化完成")
	}

	// 启动服务
	Router := gin.New()
	Router.Use(middleware.RequestID())
	Router.Use(gin.Logger())
	Router.Use(middleware.Recovery())

	// 配置可信代理 (解决"trusted proxies"警告)
	RouterTrustedProxies := []string{"127.0.0.1"}
	if err := Router.SetTrustedProxies(RouterTrustedProxies); err != nil {
		logger.Warnf("设置可信代理失败: %v", err)
	}

	// CORS
	Router.Use(middleware.CORS())

	// 设置HTML模板
	Router.SetHTMLTemplate(template.Must(template.ParseGlob("admin/*.html")))

	// 静态文件
	Router.Static("/static", "./static")
	// 后台管理页面
	Router.GET("/admin", func(c *gin.Context) {
		c.HTML(200, "index.html", nil)
	})

	// 限流中间件
	Router.Use(middleware.RateLimiter())

	// 健康检查
	Router.GET("/health", func(c *gin.Context) {
		status := "running"
		dbStatus := "connected"
		redisStatus := "connected"

		sqlDB, err := db.DB()
		if err != nil || sqlDB.Ping() != nil {
			status = "degraded"
			dbStatus = "disconnected"
		}

		if _, err := redisClient.Ping(c).Result(); err != nil {
			status = "degraded"
			redisStatus = "disconnected"
		}

		// 检查AI服务配置状态
		aiStatus := make(map[string]string)

		// MiniMax
		if miniMaxAPIKey != "" {
			aiStatus["minimax"] = "configured"
		} else {
			aiStatus["minimax"] = "not_configured"
		}

		healthOpenAICompatBaseURL := runtimeConfigValue(service.RuntimeConfigOpenAICompatBaseURL, "openai_compat.base_url", "OPENAI_COMPAT_BASE_URL", "")
		healthOpenAICompatAPIKey := runtimeConfigValue(service.RuntimeConfigOpenAICompatAPIKey, "openai_compat.api_key", "OPENAI_COMPAT_API_KEY", "")
		healthOpenAICompatModel := runtimeConfigValue(service.RuntimeConfigOpenAICompatModel, "openai_compat.model", "OPENAI_COMPAT_MODEL", "")
		if healthOpenAICompatBaseURL != "" && healthOpenAICompatAPIKey != "" && healthOpenAICompatModel != "" {
			aiStatus["openai_compat"] = "configured"
			aiStatus["chat_primary_model"] = healthOpenAICompatModel
		} else {
			aiStatus["openai_compat"] = "not_configured"
		}

		// DeepSeek
		if deepSeekAPIKey != "" {
			aiStatus["deepseek"] = "configured"
		} else {
			aiStatus["deepseek"] = "not_configured"
		}

		// Qwen
		if qwenAPIKey != "" && qwenAPIKey != "your-dashscope-api-key" {
			aiStatus["qwen"] = "configured"
		} else {
			aiStatus["qwen"] = "not_configured"
		}

		// Mem0
		if mem0APIKey != "" && mem0APIKey != "your_mem0_api_key" {
			aiStatus["mem0"] = "configured"
		} else {
			aiStatus["mem0"] = "not_configured"
		}

		// Qdrant
		if qdrantHost != "" {
			aiStatus["qdrant"] = "configured"
		} else {
			aiStatus["qdrant"] = "not_configured"
		}

		// 邮箱
		if emailUsername != "" {
			aiStatus["email"] = "configured"
		} else {
			aiStatus["email"] = "not_configured"
		}

		c.JSON(200, gin.H{
			"code":    200,
			"message": "ok",
			"data": gin.H{
				"service": "star-meet-backend",
				"status":  status,
				"components": gin.H{
					"database": dbStatus,
					"redis":    redisStatus,
				},
				"ai_services": aiStatus,
			},
		})
	})

	// 注册路由
	starMeet := Router.Group("/api/v1")
	v1.InitStarMeetRouter(starMeet)

	addr := fmt.Sprintf("%s:%s", *host, *port)
	logger.Infof("服务启动成功: http://%s", addr)
	logger.Infof("健康检查: http://%s/health", addr)

	Router.Run(addr)
}
