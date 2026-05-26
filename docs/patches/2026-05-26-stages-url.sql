ALTER TABLE "stages" ADD COLUMN "url" text;
ALTER TABLE "stages" ADD CONSTRAINT "stages_url_check" CHECK (char_length("url") <= 2048);
