# Mem0 快速配置指南

## 1. 获取API Key

1. 访问 https://mem0.ai 注册账号
2. 进入 Dashboard -> API Keys
3. 复制您的API Key

## 2. 配置API Key

编辑 `config.yaml`，替换 `mem0.api_key`:

```yaml
mem0:
  host: "https://api.mem0.ai"
  api_key: "您的API Key"  # 替换这里
```

## 3. 重启服务

```bash
cd /Users/guoliang/www/gocode/meihuoai/star-meet-backend
pkill -f star-meet-backend
./star-meet-backend &
```

## 4. 验证配置

访问健康检查端点，确认mem0状态:

```bash
curl http://localhost:8080/health
```

查看返回的 `ai_services.mem0` 是否为 `"configured"`

---

## 免费额度

Hobby计划（免费）:
- 10,000条记忆
- 1000次/月检索API调用

足够开发和测试使用！
