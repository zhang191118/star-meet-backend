-- StarMeet weekly VIP plan with a 3-day free trial (introductory offer is
-- configured in App Store Connect; the backend honors Apple's expires_date_ms,
-- which already points at the trial end during the intro period).
-- The public catalog now returns `weekly` and `monthly`; quarterly/yearly
-- remain legacy rows for receipt validation and subscription history.

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
    'weekly',
    'StarMeet VIP',
    '无限聊天，解锁全部官方角色，优先体验后续陪伴功能',
    'weekly',
    'com.guoliang.starmeet.weekly',
    90.00,
    'TWD',
    7,
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
