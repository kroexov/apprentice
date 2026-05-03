-- This file is idempotent: re-running it on an already-seeded DB is a no-op.
INSERT INTO "statuses" ( "statusId", "title", "alias" ) VALUES ( 1, 'Опубликован', 'enabled' )    ON CONFLICT ("statusId") DO NOTHING;
INSERT INTO "statuses" ( "statusId", "title", "alias" ) VALUES ( 2, 'Не опубликован', 'disabled' ) ON CONFLICT ("statusId") DO NOTHING;
INSERT INTO "statuses" ( "statusId", "title", "alias" ) VALUES ( 3, 'Удален', 'deleted' )         ON CONFLICT ("statusId") DO NOTHING;

-- password is 12345
INSERT INTO "users" ( "login", "password", "statusId" ) VALUES ( 'admin', '$2y$14$4IpqlaJ2Rvfgs.wb8f6lPODVLb/Ygl6zw1ZCUKz5CuT6WB6CV44AG', 1 ) ON CONFLICT ("login") WHERE "statusId" <> 3 DO NOTHING;

-- =============================================================================
-- Apprentice seed: 15 этапов + 5 кандидатов с детерминированными оценками
-- =============================================================================

INSERT INTO "stages" ("stageId", "alias", "order", "title", "shortTitle", "description", "maxScore", "deadlineDays") VALUES
	( 1, 'project-draft',         1, 'Черновик проекта',                          'Черновик',     'Первый сквозной черновик учебного проекта',                                   10, 14),
	( 2, 'project-draft-fixes',   2, 'Правки к черновику',                        'Правки',       'Доработка черновика по фидбеку ментора',                                      10,  3),
	( 3, 'word-feature-1',        3, 'Программирование в Word (1 фича)',          'Word #1',      'Учимся расписывать фичу текстом до того, как приступать к работе',            10,  3),
	( 4, 'first-mr',              4, 'Первый MR',                                 'MR #1',        'Первый pull request в проект',                                                10,  7),
	( 5, 'pgdesigner-intro',      5, 'Знакомство с pgDesigner',                   'pgDesigner',   'Описание схемы БД в pgDesigner',                                              10,  3),
	( 6, 'go-pg-integration',     6, 'Интеграция БД через go-pg',                 'go-pg',        'Подключаем БД к сервису через go-pg',                                         10,  7),
	( 7, 'mfd-generator-intro',   7, 'Знакомство с mfd-generator',                'mfd-gen',      'Генерация моделей и репозиториев через инструменты MFD-generator',            10,  7),
	( 8, 'colgen-refactor',       8, 'Рефакторинг через colgen',                  'colgen',       'Рефакторим коллекции и конвертеры через colgen',                              10,  3),
	( 9, 'jwt-auth',              9, 'Авторизация через JWT',                     'JWT',          'Реализуем auth-middleware на JWT',                                            10,  7),
	(10, 'zenrpc-intro',         10, 'Знакомство с zenrpc, генерация RPC-сервера','zenrpc',       'Поднимаем JSON-RPC сервер через zenrpc',                                      10,  7),
	(11, 'spa-frontend',         11, 'SPA-фронтенд + кросс-сервис RPC',           'SPA',          'Новый SPA сервис для визуализации вашего бэкенда',                            10,  7),
	(12, 'frontend-client',      12, 'Полноценный фронт-клиент',                  'Фронт-клиент', 'Свой фронт-клиент с реальной логикой и состоянием',                           10,  7),
	(13, 'word-feature-final',   13, 'Финальная фича: программирование в Word',   'Word финал',   'Финальный текстовый разбор фичи перед реализацией',                           10,  3),
	(14, 'final-readable-mr',    14, 'Финальный читаемый MR',                     'MR финал',     'MR, который можно ревьювить без боли',                                        10,  7),
	(15, 'admin-call-referral',  15, 'Созвон с админом + подготовка к реферу',    'Рефер',        'Финальный созвон, разбор полётов, подготовка к собеседованию в vmkteam',      10, 14)
ON CONFLICT ("stageId") DO NOTHING;

SELECT setval('"stages_stageId_seq"', GREATEST((SELECT MAX("stageId") FROM "stages"), 1));

-- All seed candidates have password '12345' (same bcrypt cost-14 hash as admin).
INSERT INTO "candidates" (
	"candidateId", "name", "handle", "login", "password", "city", "age", "bio",
	"avatarColor", "initials", "strengths", "weaknesses",
	"currentStageId"
) VALUES
	(1, 'Иван Соколов',     'ivan.sokolov',     'ivan.sokolov',     '$2y$14$4IpqlaJ2Rvfgs.wb8f6lPODVLb/Ygl6zw1ZCUKz5CuT6WB6CV44AG', 'Москва',          22, 'Бекендер на старте, любит чистый код.',
		'#5b8def', 'ИС', ARRAY['алгоритмы','усидчивость'], ARRAY['тесты'],            3),
	(2, 'Мария Петрова',    'maria.petrova',    'maria.petrova',    '$2y$14$4IpqlaJ2Rvfgs.wb8f6lPODVLb/Ygl6zw1ZCUKz5CuT6WB6CV44AG', 'Санкт-Петербург', 24, 'Перешла из QA в разработку.',
		'#f06292', 'МП', ARRAY['внимание к деталям'],      ARRAY['скорость','архитектура'], 5),
	(3, 'Алексей Иванов',   'alex.ivanov',      'alex.ivanov',      '$2y$14$4IpqlaJ2Rvfgs.wb8f6lPODVLb/Ygl6zw1ZCUKz5CuT6WB6CV44AG', 'Екатеринбург',    20, 'Студент, ходит на стажировку первый раз.',
		'#ffb74d', 'АИ', ARRAY['обучаемость'],             ARRAY['опыт','review'],     1),
	(4, 'Ольга Новикова',   'olga.novikova',    'olga.novikova',    '$2y$14$4IpqlaJ2Rvfgs.wb8f6lPODVLb/Ygl6zw1ZCUKz5CuT6WB6CV44AG', 'Казань',          26, 'Перевелась с фронта.',
		'#81c784', 'ОН', ARRAY['UI','коммуникация'],       ARRAY['SQL'],               4),
	(5, 'Дмитрий Кузнецов', 'dmitry.kuznetsov', 'dmitry.kuznetsov', '$2y$14$4IpqlaJ2Rvfgs.wb8f6lPODVLb/Ygl6zw1ZCUKz5CuT6WB6CV44AG', 'Новосибирск',     28, 'Пет-проектами набивал руку три года.',
		'#9575cd', 'ДК', ARRAY['пет-проекты','сети'],      ARRAY['командная работа'],  2)
ON CONFLICT ("candidateId") DO NOTHING;

SELECT setval('"candidates_candidateId_seq"', GREATEST((SELECT MAX("candidateId") FROM "candidates"), 1));

-- candidateStages: записи о прохождении этапов.
-- Для каждого пройденного этапа (order < currentStageId) запись содержит score/scoredAt;
-- все timestamp'ы отсчитываются от now(), чтобы при перезапуске seed на свежей
-- БД даты выглядели актуально — а не "год назад".
-- Stage deadlineDays для stage 1..4: 14 / 3 / 3 / 7 (см. INSERT INTO stages выше);
-- deadline = createdAt + deadlineDays, scoredAt — варьируется (early/onTime/late).
-- Зашитые в записях offset'ы оставлены прежними — это демо-таймстемпы, небольшое
-- расхождение с актуальными deadlineDays некритично.
INSERT INTO "candidateStages" ("candidateId", "stageId", "link", "score", "scoredAt", "deadline", "createdAt") VALUES
	-- stage 1 для всех (createdAt -130d, deadline -116d)
	(1, 1, NULL, 8, now() - interval '116 days', now() - interval '116 days', now() - interval '130 days'),
	(2, 1, NULL, 9, now() - interval '120 days', now() - interval '116 days', now() - interval '130 days'),
	(4, 1, NULL, 6, now() - interval '111 days', now() - interval '116 days', now() - interval '130 days'),
	(5, 1, NULL, 7, now() - interval '118 days', now() - interval '116 days', now() - interval '130 days'),
	-- stage 2 (createdAt -100d, deadline -93d)
	(1, 2, NULL, 7, now() - interval '91 days',  now() - interval '93 days',  now() - interval '100 days'),
	(2, 2, NULL, 8, now() - interval '95 days',  now() - interval '93 days',  now() - interval '100 days'),
	(4, 2, NULL, 7, now() - interval '88 days',  now() - interval '93 days',  now() - interval '100 days'),
	-- stage 3 (createdAt -85d, deadline -78d)
	(2, 3, NULL, 7, now() - interval '80 days',  now() - interval '78 days',  now() - interval '85 days'),
	(4, 3, NULL, 8, now() - interval '78 days',  now() - interval '78 days',  now() - interval '85 days'),
	-- stage 4 (createdAt -65d, deadline -51d)
	(2, 4, NULL, 9, now() - interval '55 days',  now() - interval '51 days',  now() - interval '65 days')
ON CONFLICT ("candidateId", "stageId") DO NOTHING;

-- Пустая запись для текущего этапа каждого незавершённого кандидата.
-- deadline считаем от now() + stages.deadlineDays, чтобы при перезапуске seed
-- незавершённые кандидаты получали актуальный дедлайн.
INSERT INTO "candidateStages" ("candidateId", "stageId", "deadline")
SELECT c."candidateId", c."currentStageId",
	CASE WHEN s."deadlineDays" > 0 THEN now() + (s."deadlineDays" * interval '1 day') ELSE NULL END
FROM "candidates" c
JOIN "stages" s ON s."stageId" = c."currentStageId"
WHERE c."completedAt" IS NULL AND c."statusId" <> 3
ON CONFLICT ("candidateId", "stageId") DO NOTHING;

-- Демо: Иван (id=1) на этапе 3 — отметил «готово к проверке» час назад.
UPDATE "candidateStages"
SET "link" = 'https://example.com/ivan-stage3.md',
	"isReady" = true,
	"setReadyAt" = now() - interval '1 hour'
WHERE "candidateId" = 1 AND "stageId" = 3;

-- Демо: Дмитрий (id=5) на этапе 2 — после одной отправки на доработку.
UPDATE "candidateStages"
SET "link" = 'https://example.com/dmitry-stage2.md',
	"retries" = 1
WHERE "candidateId" = 5 AND "stageId" = 2;

