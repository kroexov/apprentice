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
-- Apprentice domain: stages, candidates, stageScores
-- =============================================================================

CREATE TABLE "stages" (
	"stageId" SERIAL NOT NULL,
	"alias" varchar(64) NOT NULL,
	"order" int4 NOT NULL,
	"title" varchar(255) NOT NULL,
	"shortTitle" varchar(64) NOT NULL,
	"description" text NOT NULL DEFAULT '',
	"maxScore" int4 NOT NULL DEFAULT 10,
	"statusId" int4 NOT NULL DEFAULT 1,
	CONSTRAINT "stages_pkey" PRIMARY KEY ("stageId"),
	CONSTRAINT "stages_maxScore_check" CHECK ("maxScore" > 0 AND "maxScore" <= 100),
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
	"city" varchar(128) NOT NULL DEFAULT '',
	"age" int2,
	"bio" text NOT NULL DEFAULT '',
	"avatarColor" varchar(16) NOT NULL DEFAULT '',
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
	CONSTRAINT "candidates_initials_check" CHECK (char_length("initials") BETWEEN 1 AND 3),
	CONSTRAINT "candidates_strengths_check" CHECK (array_length("strengths", 1) IS NULL OR array_length("strengths", 1) <= 10),
	CONSTRAINT "candidates_weaknesses_check" CHECK (array_length("weaknesses", 1) IS NULL OR array_length("weaknesses", 1) <= 10)
);

-- Partial unique on handle: soft-deleted rows release the handle for re-use.
CREATE UNIQUE INDEX "candidates_handle_key" ON "candidates" ("handle") WHERE "statusId" <> 3;
CREATE INDEX "IX_candidates_currentStageId" ON "candidates" USING BTREE ("currentStageId");
CREATE INDEX "IX_candidates_statusId" ON "candidates" USING BTREE ("statusId");

CREATE TABLE "stageScores" (
	"scoreId" SERIAL NOT NULL,
	"candidateId" int4 NOT NULL,
	"stageId" int4 NOT NULL,
	"score" int2 NOT NULL,
	"scoredAt" timestamp with time zone NOT NULL DEFAULT now(),
	CONSTRAINT "stageScores_pkey" PRIMARY KEY ("scoreId"),
	CONSTRAINT "stageScores_candidate_stage_key" UNIQUE ("candidateId", "stageId"),
	CONSTRAINT "stageScores_score_check" CHECK ("score" >= 1 AND "score" <= 100)
);

CREATE INDEX "IX_stageScores_candidateId" ON "stageScores" USING BTREE ("candidateId");
CREATE INDEX "IX_stageScores_stageId" ON "stageScores" USING BTREE ("stageId");

ALTER TABLE "stages" ADD CONSTRAINT "stages_statusId_fkey" FOREIGN KEY ("statusId")
	REFERENCES "statuses"("statusId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidates" ADD CONSTRAINT "candidates_currentStageId_fkey" FOREIGN KEY ("currentStageId")
	REFERENCES "stages"("stageId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "candidates" ADD CONSTRAINT "candidates_statusId_fkey" FOREIGN KEY ("statusId")
	REFERENCES "statuses"("statusId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "stageScores" ADD CONSTRAINT "stageScores_candidateId_fkey" FOREIGN KEY ("candidateId")
	REFERENCES "candidates"("candidateId")
	MATCH SIMPLE ON DELETE CASCADE ON UPDATE RESTRICT NOT DEFERRABLE;

ALTER TABLE "stageScores" ADD CONSTRAINT "stageScores_stageId_fkey" FOREIGN KEY ("stageId")
	REFERENCES "stages"("stageId")
	MATCH SIMPLE ON DELETE RESTRICT ON UPDATE RESTRICT NOT DEFERRABLE;


