-- 星遇上架审核：角色标签合规化迁移
-- 将"恋人"关系标签改为"伙伴"，"撒娇型"风格改为"温暖型"
-- 执行前请先备份数据库

-- 1. 更新角色关系标签
UPDATE characters SET relationship = '伙伴' WHERE relationship = '恋人';
UPDATE characters SET relationship = '学长/助教' WHERE relationship = '恋人/助教';

-- 2. 更新角色风格标签
UPDATE characters SET style = '温暖型' WHERE style = '撒娇型';
UPDATE characters SET style = '阳光型' WHERE style = '奶狗型';
UPDATE characters SET style = '敏感型' WHERE style = '病娇型';
UPDATE characters SET style = '强势型' WHERE style = '霸道型';

-- 3. 更新 system_prompt 里的"恋人"措辞
UPDATE characters SET system_prompt = REPLACE(system_prompt, '和喜欢的恋人', '和你的伙伴') WHERE system_prompt LIKE '%恋人%';
UPDATE characters SET system_prompt = REPLACE(system_prompt, '和恋人', '和伙伴') WHERE system_prompt LIKE '%恋人%';
UPDATE characters SET system_prompt = REPLACE(system_prompt, '恋人（用户）', '伙伴（用户）') WHERE system_prompt LIKE '%恋人%';

-- 验证
SELECT relationship, COUNT(*) as cnt FROM characters GROUP BY relationship;
SELECT style, COUNT(*) as cnt FROM characters GROUP BY style;
