-- =============================================================================
-- Diagram Name: apisrv
-- Created on: 6/8/2022 12:02:23 PM
-- Diagram Version: 
-- =============================================================================

CREATE TABLE "statuses" (
	"statusId" SERIAL NOT NULL,
	"title" varchar(255) NOT NULL,
	"alias" varchar(64) NOT NULL,
	CONSTRAINT "statuses_pkey" PRIMARY KEY("statusId"),
	CONSTRAINT "statuses_alias_key" UNIQUE("alias")
);

CREATE TABLE "users" (
	"userId" SERIAL NOT NULL,
	"login" varchar(64) NOT NULL,
	"password" varchar(64) NOT NULL,
	"authKey" varchar(32),
	"createdAt" timestamp with time zone NOT NULL DEFAULT now(),
	"lastActivityAt" timestamp with time zone,
	"statusId" int4 NOT NULL,
	CONSTRAINT "users_pkey" PRIMARY KEY("userId")
);

CREATE INDEX "IX_FK_users_statusId_users" ON "users" USING BTREE (
	"statusId"
);

-- Partial unique on login: prevents duplicate logins for non-deleted users
-- and matches the partial-unique pattern used on candidates.login.
CREATE UNIQUE INDEX "users_login_key" ON "users" ("login") WHERE "statusId" <> 3;

-- Partial index on authKey for the RPC/VT auth middleware lookup.
CREATE INDEX "IX_users_authKey" ON "users" ("authKey") WHERE "authKey" IS NOT NULL;


CREATE TABLE "vfsFiles" (
	"fileId" SERIAL NOT NULL,
	"folderId" int4 NOT NULL,
	"title" varchar(255) NOT NULL,
	"path" varchar(255) NOT NULL,
	"params" text,
	"isFavorite" bool DEFAULT false,
	"mimeType" varchar(255) NOT NULL,
	"fileSize" int4 DEFAULT 0,
	"fileExists" bool NOT NULL DEFAULT true,
	"createdAt" timestamp NOT NULL DEFAULT now(),
	"statusId" int4 NOT NULL,
	CONSTRAINT "vfsFiles_pkey" PRIMARY KEY("fileId")
);

CREATE INDEX "IX_FK_vfsFiles_folderId_vfsFiles" ON "vfsFiles" USING BTREE (
	"folderId"
);


CREATE INDEX "IX_FK_vfsFiles_statusId_vfsFiles" ON "vfsFiles" USING BTREE (
	"statusId"
);


CREATE TABLE "vfsFolders" (
	"folderId" SERIAL NOT NULL,
	"parentFolderId" int4,
	"title" varchar(255) NOT NULL,
	"isFavorite" bool DEFAULT false,
	"createdAt" timestamp NOT NULL DEFAULT now(),
	"statusId" int4 NOT NULL,
	CONSTRAINT "vfsFolders_pkey" PRIMARY KEY("folderId")
);

CREATE INDEX "IX_FK_vfsFolders_folderId_vfsFolders" ON "vfsFolders" USING BTREE (
	"parentFolderId"
);


CREATE INDEX "IX_FK_vfsFolders_statusId_vfsFolders" ON "vfsFolders" USING BTREE (
	"statusId"
);


CREATE TABLE "vfsHashes" (
	"hash" varchar(40) NOT NULL,
	"namespace" varchar(32) NOT NULL,
	"extension" varchar(4) NOT NULL,
	"fileSize" int4 NOT NULL DEFAULT 0,
	"width" int4 NOT NULL DEFAULT 0,
	"height" int4 NOT NULL DEFAULT 0,
	"blurhash" text,
	"error" text,
	"createdAt" timestamp with time zone NOT NULL DEFAULT now(),
	"indexedAt" timestamp with time zone,
	CONSTRAINT "vfsHashes_pkey" PRIMARY KEY("hash","namespace")
);

CREATE INDEX "IX_vfsHashes_indexedAt" ON "vfsHashes" USING BTREE (
	"indexedAt"
);



ALTER TABLE "users" ADD CONSTRAINT "FK_users_statusId" FOREIGN KEY ("statusId")
	REFERENCES "statuses"("statusId")
	MATCH SIMPLE
	ON DELETE RESTRICT
	ON UPDATE RESTRICT
	NOT DEFERRABLE;

ALTER TABLE "vfsFiles" ADD CONSTRAINT "vfsFiles_folderId_fkey" FOREIGN KEY ("folderId")
	REFERENCES "vfsFolders"("folderId")
	MATCH SIMPLE
	ON DELETE RESTRICT
	ON UPDATE RESTRICT
	NOT DEFERRABLE;

ALTER TABLE "vfsFiles" ADD CONSTRAINT "vfsFiles_statusId_fkey" FOREIGN KEY ("statusId")
	REFERENCES "statuses"("statusId")
	MATCH SIMPLE
	ON DELETE RESTRICT
	ON UPDATE RESTRICT
	NOT DEFERRABLE;

ALTER TABLE "vfsFolders" ADD CONSTRAINT "vfsFolders_parentFolderId_fkey" FOREIGN KEY ("parentFolderId")
	REFERENCES "vfsFolders"("folderId")
	MATCH SIMPLE
	ON DELETE RESTRICT
	ON UPDATE RESTRICT
	NOT DEFERRABLE;

ALTER TABLE "vfsFolders" ADD CONSTRAINT "vfsFolders_statusId_fkey" FOREIGN KEY ("statusId")
	REFERENCES "statuses"("statusId")
	MATCH SIMPLE
	ON DELETE RESTRICT
	ON UPDATE RESTRICT
	NOT DEFERRABLE;

-- =============================================================================
-- Apprentice domain: stages, candidates, candidateStages
-- =============================================================================

CREATE TABLE "stages" (
	"stageId" SERIAL NOT NULL,
	"alias" varchar(64) NOT NULL,
	"order" int4 NOT NULL,
	"title" varchar(255) NOT NULL,
	"shortTitle" varchar(64) NOT NULL,
	"description" text NOT NULL DEFAULT '',
	"maxScore" int4 NOT NULL DEFAULT 10,
	"deadlineDays" int4 NOT NULL DEFAULT 0,
	"statusId" int4 NOT NULL DEFAULT 1,
	CONSTRAINT "stages_pkey" PRIMARY KEY ("stageId"),
	CONSTRAINT "stages_maxScore_check" CHECK ("maxScore" > 0 AND "maxScore" <= 100),
	CONSTRAINT "stages_deadlineDays_check" CHECK ("deadlineDays" >= 0 AND "deadlineDays" <= 365),
	CONSTRAINT "stages_alias_check" CHECK ("alias" ~ '^[a-z0-9.\-_]{2,64}$')
);

-- Partial unique indexes ignore soft-deleted rows (statusId = 3) so alias/order
-- become reusable after Delete. Reorder uses a single transaction with negative
-- offsets to dodge the alive-rows uniqueness while shuffling, which is why the
-- ">0" CHECK is intentionally absent.
CREATE UNIQUE INDEX "stages_alias_key" ON "stages" ("alias") WHERE "statusId" <> 3;
CREATE UNIQUE INDEX "stages_order_key" ON "stages" ("order") WHERE "statusId" <> 3;
CREATE INDEX "IX_stages_order" ON "stages" USING BTREE ("order" ASC);

CREATE TABLE "candidates" (
	"candidateId" SERIAL NOT NULL,
	"name" varchar(80) NOT NULL,
	"handle" varchar(40) NOT NULL,
	"login" varchar(64) NOT NULL,
	"password" varchar(64) NOT NULL DEFAULT '',
	"authKey" varchar(32),
	"lastActivityAt" timestamp with time zone,
	"city" varchar(128) NOT NULL DEFAULT '',
	"age" int2,
	"bio" text NOT NULL DEFAULT '',
	"avatarColor" varchar(32) NOT NULL DEFAULT '',
	"initials" varchar(3) NOT NULL DEFAULT '',
	"avatarUrl" text,
	"strengths" text[] NOT NULL DEFAULT ARRAY[]::text[],
	"weaknesses" text[] NOT NULL DEFAULT ARRAY[]::text[],
	"currentStageId" int4 NOT NULL,
	"createdAt" timestamp with time zone NOT NULL DEFAULT now(),
	"updatedAt" timestamp with time zone NOT NULL DEFAULT now(),
	"completedAt" timestamp with time zone,
	"statusId" int4 NOT NULL DEFAULT 1,
	CONSTRAINT "candidates_pkey" PRIMARY KEY ("candidateId"),
	CONSTRAINT "candidates_age_check" CHECK ("age" IS NULL OR ("age" >= 14 AND "age" <= 120)),
	CONSTRAINT "candidates_handle_check" CHECK ("handle" ~ '^[a-z0-9.\-_]{2,40}$'),
	CONSTRAINT "candidates_login_check" CHECK ("login" ~ '^[a-z0-9.\-_]{2,40}$'),
	CONSTRAINT "candidates_initials_check" CHECK (char_length("initials") BETWEEN 1 AND 3),
	CONSTRAINT "candidates_strengths_check" CHECK (array_length("strengths", 1) IS NULL OR array_length("strengths", 1) <= 10),
	CONSTRAINT "candidates_weaknesses_check" CHECK (array_length("weaknesses", 1) IS NULL OR array_length("weaknesses", 1) <= 10)
);

-- Partial unique on handle/login: soft-deleted rows release values for re-use.
CREATE UNIQUE INDEX "candidates_handle_key" ON "candidates" ("handle") WHERE "statusId" <> 3;
CREATE UNIQUE INDEX "candidates_login_key" ON "candidates" ("login") WHERE "statusId" <> 3;
-- Partial index on authKey: speeds up middleware lookup on every protected
-- request. NULL keys skipped so the index stays small.
CREATE INDEX "IX_candidates_authKey" ON "candidates" ("authKey") WHERE "authKey" IS NOT NULL;
CREATE INDEX "IX_candidates_currentStageId" ON "candidates" USING BTREE ("currentStageId");
CREATE INDEX "IX_candidates_statusId" ON "candidates" USING BTREE ("statusId");

CREATE TABLE "candidateStages" (
	"candidateStageId" SERIAL NOT NULL,
	"candidateId" int4 NOT NULL,
	"stageId" int4 NOT NULL,
	"link" text,
	"score" int2,
	"scoredAt" timestamp with time zone,
	"scoredBy" int4,
	"deadline" timestamp with time zone,
	"isReady" bool NOT NULL DEFAULT false,
	"setReadyAt" timestamp with time zone,
	"retries" int4 NOT NULL DEFAULT 0,
	"notes" text,
	"createdAt" timestamp with time zone NOT NULL DEFAULT now(),
	CONSTRAINT "candidateStages_pkey" PRIMARY KEY ("candidateStageId"),
	CONSTRAINT "candidateStages_candidate_stage_key" UNIQUE ("candidateId", "stageId"),
	CONSTRAINT "candidateStages_score_check" CHECK ("score" IS NULL OR ("score" >= 1 AND "score" <= 100)),
	CONSTRAINT "candidateStages_scored_consistency_check" CHECK (("score" IS NULL AND "scoredAt" IS NULL) OR ("score" IS NOT NULL AND "scoredAt" IS NOT NULL))
);

CREATE INDEX "IX_candidateStages_candidateId" ON "candidateStages" USING BTREE ("candidateId");
CREATE INDEX "IX_candidateStages_stageId" ON "candidateStages" USING BTREE ("stageId");

ALTER TABLE "stages" ADD CONSTRAINT "stages_statusId_fkey" FOREIGN KEY ("statusId")
	REFERENCES "statuses"("statusId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidates" ADD CONSTRAINT "candidates_currentStageId_fkey" FOREIGN KEY ("currentStageId")
	REFERENCES "stages"("stageId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidates" ADD CONSTRAINT "candidates_statusId_fkey" FOREIGN KEY ("statusId")
	REFERENCES "statuses"("statusId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidateStages" ADD CONSTRAINT "candidateStages_candidateId_fkey" FOREIGN KEY ("candidateId")
	REFERENCES "candidates"("candidateId")
	MATCH SIMPLE ON DELETE CASCADE ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidateStages" ADD CONSTRAINT "candidateStages_stageId_fkey" FOREIGN KEY ("stageId")
	REFERENCES "stages"("stageId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidateStages" ADD CONSTRAINT "candidateStages_scoredBy_fkey" FOREIGN KEY ("scoredBy")
	REFERENCES "users"("userId")
	MATCH SIMPLE ON DELETE SET NULL ON UPDATE RESTRICT NOT DEFERRABLE;

-- =============================================================================
-- Theory materials: catalogue + per-candidate progress
-- =============================================================================

CREATE TABLE "materials" (
	"materialId" SERIAL NOT NULL,
	"title" varchar(255) NOT NULL,
	"type" varchar(32) NOT NULL,
	"url" text NOT NULL,
	"description" text NOT NULL DEFAULT '',
	"maxScore" int4 NOT NULL DEFAULT 10,
	"order" int4 NOT NULL DEFAULT 0,
	"statusId" int4 NOT NULL DEFAULT 1,
	"createdAt" timestamp with time zone NOT NULL DEFAULT now(),
	"updatedAt" timestamp with time zone NOT NULL DEFAULT now(),
	CONSTRAINT "materials_pkey" PRIMARY KEY ("materialId"),
	CONSTRAINT "materials_type_check" CHECK ("type" IN ('book','article','video','test','other')),
	CONSTRAINT "materials_url_check" CHECK (char_length("url") <= 2048),
	CONSTRAINT "materials_maxScore_check" CHECK ("maxScore" > 0 AND "maxScore" <= 100)
);

-- Partial unique on title and order so soft-deleted rows release values for
-- re-use, mirroring the stages.alias / stages.order pattern.
CREATE UNIQUE INDEX "materials_title_key" ON "materials" ("title") WHERE "statusId" <> 3;
CREATE UNIQUE INDEX "materials_order_key" ON "materials" ("order") WHERE "statusId" <> 3;
CREATE INDEX "IX_materials_order" ON "materials" USING BTREE ("order" ASC);
CREATE INDEX "IX_materials_statusId" ON "materials" USING BTREE ("statusId");

CREATE TABLE "candidateMaterials" (
	"candidateMaterialId" SERIAL NOT NULL,
	"candidateId" int4 NOT NULL,
	"materialId" int4 NOT NULL,
	"readAt" timestamp with time zone,
	"score" int2,
	"scoredAt" timestamp with time zone,
	"scoredBy" int4,
	"notes" text,
	"createdAt" timestamp with time zone NOT NULL DEFAULT now(),
	CONSTRAINT "candidateMaterials_pkey" PRIMARY KEY ("candidateMaterialId"),
	CONSTRAINT "candidateMaterials_candidate_material_key" UNIQUE ("candidateId", "materialId"),
	CONSTRAINT "candidateMaterials_score_check" CHECK ("score" IS NULL OR ("score" >= 1 AND "score" <= 100)),
	CONSTRAINT "candidateMaterials_scored_consistency_check" CHECK (
		("score" IS NULL AND "scoredAt" IS NULL AND "scoredBy" IS NULL)
		OR ("score" IS NOT NULL AND "scoredAt" IS NOT NULL AND "scoredBy" IS NOT NULL)
	)
);

CREATE INDEX "IX_candidateMaterials_candidateId" ON "candidateMaterials" USING BTREE ("candidateId");
CREATE INDEX "IX_candidateMaterials_materialId" ON "candidateMaterials" USING BTREE ("materialId");

ALTER TABLE "materials" ADD CONSTRAINT "materials_statusId_fkey" FOREIGN KEY ("statusId")
	REFERENCES "statuses"("statusId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidateMaterials" ADD CONSTRAINT "candidateMaterials_candidateId_fkey" FOREIGN KEY ("candidateId")
	REFERENCES "candidates"("candidateId")
	MATCH SIMPLE ON DELETE CASCADE ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidateMaterials" ADD CONSTRAINT "candidateMaterials_materialId_fkey" FOREIGN KEY ("materialId")
	REFERENCES "materials"("materialId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidateMaterials" ADD CONSTRAINT "candidateMaterials_scoredBy_fkey" FOREIGN KEY ("scoredBy")
	REFERENCES "users"("userId")
	MATCH SIMPLE ON DELETE SET NULL ON UPDATE RESTRICT NOT DEFERRABLE;


