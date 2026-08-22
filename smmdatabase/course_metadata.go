package smmdatabase

import (
	"database/sql"
	"errors"
	"time"
)

// CourseMetadata mirrors the real per-course stat fields Super Mario Maker
// tracks (confirmed from a real course's own extracted metadata: stars,
// user_plays, clears, total_attempts, failures, is_event_course,
// is_official_maker_course, world_record.{best_time_pid,time_milliseconds}).
// Stars live in datastore.object_custom_rankings (application_id 0) and
// plays lives on datastore.objects.plays - both already admin-editable
// elsewhere; everything else lives here.
type CourseMetadata struct {
	DataID                uint64
	IsEventCourse         bool
	IsOfficialMakerCourse bool
	Clears                int64
	TotalAttempts         int64
	Failures              int64
	MiiverseComments      int64
	WorldRecordPID        uint32
	WorldRecordTimeMs     int64
}

func ensureCourseMetadataSchema() {
	_, err := Postgres.Exec(`CREATE TABLE IF NOT EXISTS datastore.course_metadata (
		data_id bigint PRIMARY KEY,
		is_event_course boolean NOT NULL DEFAULT FALSE,
		is_official_maker_course boolean NOT NULL DEFAULT FALSE,
		clears bigint NOT NULL DEFAULT 0,
		total_attempts bigint NOT NULL DEFAULT 0,
		failures bigint NOT NULL DEFAULT 0,
		miiverse_comments bigint NOT NULL DEFAULT 0,
		world_record_pid bigint NOT NULL DEFAULT 0,
		world_record_time_ms bigint NOT NULL DEFAULT 0,
		update_date timestamp
	)`)
	if err != nil {
		panic(err)
	}
}

func GetCourseMetadata(dataID uint64) (CourseMetadata, error) {
	meta := CourseMetadata{DataID: dataID}

	err := Postgres.QueryRow(`SELECT is_event_course, is_official_maker_course, clears, total_attempts,
		failures, miiverse_comments, world_record_pid, world_record_time_ms
		FROM datastore.course_metadata WHERE data_id=$1`, dataID).Scan(
		&meta.IsEventCourse, &meta.IsOfficialMakerCourse, &meta.Clears, &meta.TotalAttempts,
		&meta.Failures, &meta.MiiverseComments, &meta.WorldRecordPID, &meta.WorldRecordTimeMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return meta, nil // defaults are fine - no row yet means all-zero metadata
	}
	return meta, err
}

func UpsertCourseMetadata(meta CourseMetadata) error {
	_, err := Postgres.Exec(`INSERT INTO datastore.course_metadata (
		data_id, is_event_course, is_official_maker_course, clears, total_attempts,
		failures, miiverse_comments, world_record_pid, world_record_time_ms, update_date
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	ON CONFLICT (data_id) DO UPDATE SET
		is_event_course = EXCLUDED.is_event_course,
		is_official_maker_course = EXCLUDED.is_official_maker_course,
		clears = EXCLUDED.clears,
		total_attempts = EXCLUDED.total_attempts,
		failures = EXCLUDED.failures,
		miiverse_comments = EXCLUDED.miiverse_comments,
		world_record_pid = EXCLUDED.world_record_pid,
		world_record_time_ms = EXCLUDED.world_record_time_ms,
		update_date = EXCLUDED.update_date`,
		meta.DataID, meta.IsEventCourse, meta.IsOfficialMakerCourse, meta.Clears, meta.TotalAttempts,
		meta.Failures, meta.MiiverseComments, meta.WorldRecordPID, meta.WorldRecordTimeMs, time.Now(),
	)
	return err
}

// AdminAddPlays adjusts a course's tracked play count by a signed delta
// (positive to add, negative to remove) - mirrors the real "user_plays"
// field.
func AdminAddPlays(dataID uint64, delta int64) error {
	_, err := Postgres.Exec(`UPDATE datastore.objects SET plays = plays + $1, update_date = $2 WHERE data_id = $3`, delta, time.Now(), dataID)
	return err
}
