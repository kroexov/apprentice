-- This file is idempotent: re-running it on an already-seeded DB is a no-op.
INSERT INTO "statuses" ( "statusId", "title", "alias" ) VALUES ( 1, 'Опубликован', 'enabled' )    ON CONFLICT ("statusId") DO NOTHING;
INSERT INTO "statuses" ( "statusId", "title", "alias" ) VALUES ( 2, 'Не опубликован', 'disabled' ) ON CONFLICT ("statusId") DO NOTHING;
INSERT INTO "statuses" ( "statusId", "title", "alias" ) VALUES ( 3, 'Удален', 'deleted' )         ON CONFLICT ("statusId") DO NOTHING;

-- password is 12345
INSERT INTO "users" ( "login", "password", "statusId" ) VALUES ( 'admin', '$2y$14$4IpqlaJ2Rvfgs.wb8f6lPODVLb/Ygl6zw1ZCUKz5CuT6WB6CV44AG', 1 ) ON CONFLICT ("login") WHERE "statusId" <> 3 DO NOTHING;

-- =============================================================================
-- Apprentice seed: 5 этапов + 5 кандидатов с детерминированными оценками
-- =============================================================================

INSERT INTO "stages" ("stageId", "alias", "order", "title", "shortTitle", "description", "maxScore") VALUES
	(1, 'first-project',     1, 'Первый проект',          'Проект',     'Первый учебный проект целиком', 10),
	(2, 'word-programming',  2, 'Программирование в Word', 'Word',       'Учимся раскладывать решение по шагам — текстом',                10),
	(3, 'first-pr',          3, 'Первый PR',              'PR',         'Первый pull request в общий репозиторий',                       10),
	(4, 'first-readable-pr', 4, 'Первый читаемый PR',     'Чит. PR',    'PR, который можно ревьювить без боли',                          10),
	(5, 'pet-project',       5, 'Свой пет-проект',        'Пет',        'Самостоятельный пет-проект',                                    10)
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

-- Оценки: за все пройденные этапы (order < currentStageId).
INSERT INTO "stageScores" ("candidateId", "stageId", "score") VALUES
	(1, 1, 8),
	(1, 2, 7),
	(2, 1, 9),
	(2, 2, 8),
	(2, 3, 7),
	(2, 4, 9),
	(4, 1, 6),
	(4, 2, 7),
	(4, 3, 8),
	(5, 1, 7)
ON CONFLICT ("candidateId", "stageId") DO NOTHING;

