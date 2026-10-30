-- StarMeet Taiwan launch: expose one monthly auto-renewing VIP plan.
-- Legacy plans remain in the database for receipt validation and subscription history,
-- but the public catalog only returns the `monthly` plan.

INSERT INTO plans (
    plan_id,
    name,
    description,
    level,
    apple_product_id,
    price,
    currency,
    duration,
    created_at,
    updated_at
) VALUES (
    'monthly',
    'StarMeet VIP',
    '无限聊天，解锁全部官方角色，优先体验后续陪伴功能',
    'monthly',
    'com.guoliang.starmeet.monthly',
    170.00,
    'TWD',
    30,
    NOW(),
    NOW()
)
ON DUPLICATE KEY UPDATE
    name = VALUES(name),
    description = VALUES(description),
    level = VALUES(level),
    apple_product_id = VALUES(apple_product_id),
    price = VALUES(price),
    currency = VALUES(currency),
    duration = VALUES(duration),
    updated_at = NOW();
