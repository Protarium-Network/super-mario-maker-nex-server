// Package smmdatabase holds the Postgres connection and DataStore queries
// for Super Mario Maker. Own isolated database (not just a schema) - Super
// Mario Maker's DataStore needs extra columns (deletion_reason,
// under_review) and extra tables (custom rankings, buffer queues, course
// records) beyond what WSC's own shared datastore.objects table has, so
// sharing that table would either break WSC or require an intrusive schema
// migration on a table every other game already depends on. Isolating here
// is the same call already made for Puyo and Wii U Chat.
package smmdatabase

import (
	"database/sql"
	"os"

	_ "github.com/lib/pq"

	"github.com/EcrazerDev/super-mario-maker/globals"
)

var Postgres *sql.DB

func ConnectPostgres() {
	var err error

	Postgres, err = sql.Open("postgres", os.Getenv("PN_SMM_POSTGRES_URI"))
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	if err = Postgres.Ping(); err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	initSchema()
	ensureAdminSchema()
	ensureCourseMetadataSchema()

	globals.Logger.Success("[SMM] Connected to Postgres!")
}

func initSchema() {
	_, err := Postgres.Exec(`CREATE SCHEMA IF NOT EXISTS datastore`)
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	// Super Mario Maker uses non-standard DataIDs: course share codes are
	// generated from a course's DataID as an 8-byte hex string (top 2
	// bytes checksum, bottom 6 bytes the actual ID), so the game can only
	// display codes up to 0xFFFFFFFFFFFF. Starting well above 0 avoids
	// ever brushing that ceiling in practice.
	_, err = Postgres.Exec(`CREATE SEQUENCE IF NOT EXISTS datastore.object_data_id_seq
		INCREMENT 1
		MINVALUE 1
		MAXVALUE 281474976710656
		START 940000
		CACHE 1`)
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	_, err = Postgres.Exec(`CREATE TABLE IF NOT EXISTS datastore.objects (
		data_id bigint NOT NULL DEFAULT nextval('datastore.object_data_id_seq') PRIMARY KEY,
		upload_completed boolean NOT NULL DEFAULT FALSE,
		deleted boolean NOT NULL DEFAULT FALSE,
		deletion_reason int NOT NULL DEFAULT 0,
		under_review boolean NOT NULL DEFAULT FALSE,
		owner int,
		size int,
		name text,
		data_type int,
		meta_binary bytea,
		permission int,
		permission_recipients int[],
		delete_permission int,
		delete_permission_recipients int[],
		flag int,
		period int,
		refer_data_id bigint,
		tags text[],
		persistence_slot_id int,
		extra_data text[],
		access_password bigint NOT NULL DEFAULT 0,
		update_password bigint NOT NULL DEFAULT 0,
		creation_date timestamp,
		update_date timestamp,
		plays bigint NOT NULL DEFAULT 0
	)`)
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	// plays didn't exist on the original table definition - add it for
	// databases created before this column was introduced.
	_, err = Postgres.Exec(`ALTER TABLE datastore.objects ADD COLUMN IF NOT EXISTS plays bigint NOT NULL DEFAULT 0`)
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	_, err = Postgres.Exec(`CREATE TABLE IF NOT EXISTS datastore.object_ratings (
		data_id bigint,
		slot smallint,
		flag smallint,
		internal_flag smallint,
		lock_type smallint,
		initial_value bigint,
		range_min int,
		range_max int,
		period_hour smallint,
		period_duration int,
		total_value bigint,
		count int NOT NULL DEFAULT 0,
		PRIMARY KEY(data_id, slot)
	)`)
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	// Custom rankings power Course World's "Recommended"/"Suggested"
	// browsing and course star counts - SMM-specific, no equivalent in
	// WSC's own datastore.
	_, err = Postgres.Exec(`CREATE TABLE IF NOT EXISTS datastore.object_custom_rankings (
		data_id bigint,
		application_id bigint,
		value bigint,
		PRIMARY KEY(data_id, application_id)
	)`)
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	// Buffer queues hold small per-object binary blobs (e.g. a maker's
	// "Starred Courses" list is a set of buffers on slot 0 of their maker
	// object, one buffer per starred course DataID).
	_, err = Postgres.Exec(`CREATE TABLE IF NOT EXISTS datastore.buffer_queues (
		data_id bigint,
		slot int,
		creation_date timestamp,
		buffer bytea,
		PRIMARY KEY(data_id, slot, buffer)
	)`)
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	// Course records track the best clear time/score per course+slot,
	// keyed by whoever first cleared it and whoever holds the best score.
	_, err = Postgres.Exec(`CREATE TABLE IF NOT EXISTS datastore.course_records (
		data_id bigint,
		slot int,
		first_pid int,
		best_pid int,
		best_score int,
		creation_date timestamp,
		update_date timestamp,
		PRIMARY KEY(data_id, slot)
	)`)
	if err != nil {
		globals.Logger.Critical(err.Error())
		os.Exit(1)
	}

	globals.Logger.Success("[SMM] Postgres tables created")
}
