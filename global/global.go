package global

import (
	"star-meet-backend/core/deepseek"
	"star-meet-backend/core/embedding"
	"star-meet-backend/core/gemini"
	"star-meet-backend/core/mem0"
	"star-meet-backend/core/minimax"
	openai_compat "star-meet-backend/core/openai_compat"
	"star-meet-backend/core/qdrant"
	"star-meet-backend/core/qwen"
	"star-meet-backend/service"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

var (
	GVA_DB               *gorm.DB
	GVA_REDIS            *redis.Client
	Mem0Client           *mem0.Client
	QwenClient           *qwen.Client
	MiniMaxClient        *minimax.Client
	OpenAICompatClient   *openai_compat.Client
	DeepSeekClient       *deepseek.Client
	GeminiClient         *gemini.Client
	EmbeddingClient      *embedding.Client
	QdrantClient         *qdrant.Client
	ChatService          *service.ChatService
	CharacterService     *service.CharacterService
	MemoryService        *service.MemoryService
	MobileService        *service.MobileService
	UserService          *service.UserService
	KnowledgeService     *service.KnowledgeService
	VerifyCodeService    *service.VerifyCodeService
	RuntimeConfigService *service.RuntimeConfigService
)

// NewRuntimeConfigService 创建运行时配置服务
func NewRuntimeConfigService(db *gorm.DB) *service.RuntimeConfigService {
	return &service.RuntimeConfigService{DB: db}
}

// NewMem0Client 创建Mem0客户端
func NewMem0Client(host, apiKey string) *mem0.Client {
	return mem0.NewClient(host, apiKey)
}

// NewQwenClient 创建Qwen客户端
func NewQwenClient(apiKey string) *qwen.Client {
	return qwen.NewClient(apiKey)
}

// NewMiniMaxClient 创建MiniMax客户端
func NewMiniMaxClient(apiKey string) *minimax.Client {
	return minimax.NewClient(apiKey)
}

func NewOpenAICompatClient(baseURL, apiKey, model string) *openai_compat.Client {
	return openai_compat.NewClient(baseURL, apiKey, model)
}

// NewDeepSeekClient 创建DeepSeek客户端
func NewDeepSeekClient(apiKey string) *deepseek.Client {
	return deepseek.NewClient(apiKey)
}

// NewGeminiClient 创建Gemini客户端
func NewGeminiClient(apiKey string, model string) *gemini.Client {
	return gemini.NewClient(apiKey, model)
}

// NewEmbeddingClient 创建Embedding客户端
func NewEmbeddingClient(apiKey string) *embedding.Client {
	return embedding.NewClient(apiKey)
}

// NewQdrantClient 创建Qdrant客户端
func NewQdrantClient(baseURL string) *qdrant.Client {
	return qdrant.NewClient(baseURL)
}

// NewUserService 创建用户服务
func NewUserService(db *gorm.DB, verifyCodeService *service.VerifyCodeService) *service.UserService {
	return &service.UserService{DB: db, VerifyCodeService: verifyCodeService}
}

// NewKnowledgeService 创建知识库服务
func NewKnowledgeService(db *gorm.DB) *service.KnowledgeService {
	return &service.KnowledgeService{DB: db}
}

// NewCharacterService 创建角色服务
func NewCharacterService(db *gorm.DB) *service.CharacterService {
	return &service.CharacterService{DB: db}
}

// NewMemoryService 创建记忆服务
func NewMemoryService(db *gorm.DB, mem0Client *mem0.Client) *service.MemoryService {
	return &service.MemoryService{DB: db, Mem0Client: mem0Client}
}

// NewMobileService 创建移动端聚合服务
func NewMobileService(db *gorm.DB) *service.MobileService {
	return service.NewMobileService(db)
}

// NewChatService 创建聊天服务
func NewChatService(db *gorm.DB, redisClient *redis.Client, mem0Client *mem0.Client, qwenClient *qwen.Client, miniMaxClient *minimax.Client, openAICompatClient *openai_compat.Client, deepSeekClient *deepseek.Client, geminiClient *gemini.Client, embeddingClient *embedding.Client, qdrantClient *qdrant.Client, knowledgeService *service.KnowledgeService, runtimeConfig *service.RuntimeConfigService) *service.ChatService {
	return &service.ChatService{
		DB:                 db,
		Redis:              redisClient,
		Mem0Client:         mem0Client,
		QwenClient:         qwenClient,
		MiniMaxClient:      miniMaxClient,
		OpenAICompatClient: openAICompatClient,
		DeepSeekClient:     deepSeekClient,
		GeminiClient:       geminiClient,
		EmbeddingClient:    embeddingClient,
		QdrantClient:       qdrantClient,
		KnowledgeService:   knowledgeService,
		RuntimeConfig:      runtimeConfig,
	}
}

// NewVerifyCodeService 创建验证码服务
func NewVerifyCodeService(db *gorm.DB, redisClient *redis.Client) *service.VerifyCodeService {
	return &service.VerifyCodeService{DB: db, Redis: redisClient}
}
