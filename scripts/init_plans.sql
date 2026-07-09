-- 初始化订阅计划
INSERT INTO plans (plan_id, name, price, original_price, duration_days, description, features, is_active, created_at, updated_at) VALUES
('monthly', '月卡', 30, 38, 30, '开通VIP，享无限特权', '{"语音消息":true,"视频消息":true,"高清图片":true,"免广告":true,"专属表情":true}', true, NOW(), NOW()),
('quarterly', '季卡', 78, 98, 90, '季卡优惠，限时特惠', '{"语音消息":true,"视频消息":true,"高清图片":true,"免广告":true,"专属表情":true,"专属头像框":true}', true, NOW(), NOW()),
('yearly', '年卡', 268, 368, 365, '年卡超值，全年最低价', '{"语音消息":true,"视频消息":true,"高清图片":true,"免广告":true,"专属表情":true,"专属头像框":true,"专属称号":true,"优先客服":true}', true, NOW(), NOW())
ON CONFLICT (plan_id) DO NOTHING;
