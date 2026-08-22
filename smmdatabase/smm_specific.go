package smmdatabase

import (
	"database/sql"
	"errors"
	"time"

	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	datastore_smm_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/super-mario-maker/types"
	"github.com/lib/pq"
)

// InsertOrUpdateCourseRecord tracks a course's best clear per slot. On a
// conflict, only replaces the best score/holder if the new score actually
// beats the stored one - matches how a leaderboard should behave.
func InsertOrUpdateCourseRecord(dataID types.UInt64, slot types.UInt8, pid types.PID, score types.Int32) *nex.Error {
	if nexError := IsObjectAvailable(dataID); nexError != nil {
		return nexError
	}

	now := time.Now()

	_, err := Postgres.Exec(`INSERT INTO datastore.course_records (
		data_id, slot, first_pid, best_pid, best_score, creation_date, update_date
	) VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (data_id, slot) DO UPDATE
	SET best_score = CASE WHEN datastore.course_records.best_score > $5 THEN $5 ELSE datastore.course_records.best_score END,
		best_pid = CASE WHEN datastore.course_records.best_score > $5 THEN $4 ELSE datastore.course_records.best_pid END,
		update_date = CASE WHEN datastore.course_records.best_score > $5 THEN $7 ELSE datastore.course_records.update_date END`,
		dataID, slot, pid, pid, score, now, now,
	)
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return nil
}

func GetCourseRecordByDataIDAndSlot(dataID types.UInt64, slot types.UInt8) (datastore_smm_types.DataStoreGetCourseRecordResult, *nex.Error) {
	if nexError := IsObjectAvailable(dataID); nexError != nil {
		return datastore_smm_types.NewDataStoreGetCourseRecordResult(), nexError
	}

	courseRecord := datastore_smm_types.NewDataStoreGetCourseRecordResult()
	courseRecord.DataID = dataID
	courseRecord.Slot = slot

	var createdDate, updatedDate time.Time

	err := Postgres.QueryRow(`SELECT first_pid, best_pid, best_score, creation_date, update_date
		FROM datastore.course_records WHERE data_id=$1 AND slot=$2`, dataID, slot).Scan(
		&courseRecord.FirstPID, &courseRecord.BestPID, &courseRecord.BestScore, &createdDate, &updatedDate,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return datastore_smm_types.NewDataStoreGetCourseRecordResult(), nex.NewError(nex.ResultCodes.DataStore.NotFound, "Object not found")
		}
		return datastore_smm_types.NewDataStoreGetCourseRecordResult(), nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	courseRecord.CreatedTime.FromTimestamp(createdDate)
	courseRecord.UpdatedTime.FromTimestamp(updatedDate)

	return courseRecord, nil
}

// GetRandomCoursesWithLimit backs Course World's browsing tabs (Recommended,
// Suggested, 100 Mario, the 3DS pick-up tab) - all of them just want "some
// published courses" without real difficulty/success-rate filtering, since
// that data isn't tracked here.
func GetRandomCoursesWithLimit(limit int) (types.List[datastore_smm_types.DataStoreCustomRankingResult], *nex.Error) {
	courses := types.NewList[datastore_smm_types.DataStoreCustomRankingResult]()

	rows, err := Postgres.Query(`
		SELECT data_id FROM datastore.objects
		WHERE upload_completed = TRUE AND deleted = FALSE AND under_review = FALSE
		AND data_id IN (SELECT data_id FROM datastore.object_custom_rankings WHERE application_id = 0)
		ORDER BY RANDOM()
		LIMIT $1
	`, limit)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}
	if rows == nil {
		return courses, nil
	}
	defer rows.Close()

	var dataIDs []uint64
	for rows.Next() {
		var dataID uint64
		if err := rows.Scan(&dataID); err != nil {
			continue
		}
		dataIDs = append(dataIDs, dataID)
	}

	for _, dataID := range dataIDs {
		results := GetCustomRankingsByDataIDs(types.NewUInt32(0), types.List[types.UInt64]{types.NewUInt64(dataID)})
		courses = append(courses, results...)
	}

	return courses, nil
}

func GetUserCourseObjectIDs(ownerPID types.PID) (types.List[types.UInt64], *nex.Error) {
	courseObjectIDs := types.NewList[types.UInt64]()

	// Course objects have data_type > 2 and < 50 (1 is reserved for
	// "maker" objects, 2 for objects created via PrepareAttachFile, 50/51
	// for the event course metadata file and event courses).
	rows, err := Postgres.Query(`SELECT data_id FROM datastore.objects WHERE owner=$1 AND data_type > 2 AND data_type < 50`, ownerPID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}
	if rows == nil {
		return courseObjectIDs, nil
	}
	defer rows.Close()

	for rows.Next() {
		var dataID types.UInt64
		if err := rows.Scan(&dataID); err != nil {
			continue
		}
		if nexError := IsObjectAvailable(dataID); nexError != nil {
			continue
		}
		courseObjectIDs = append(courseObjectIDs, dataID)
	}

	return courseObjectIDs, nil
}

func InsertOrUpdateBufferQueueData(dataID types.UInt64, slot types.UInt32, buffer types.QBuffer) *nex.Error {
	if nexError := IsObjectAvailable(dataID); nexError != nil {
		return nexError
	}

	now := time.Now()

	// Duplicate buffers in a slot aren't allowed even across different
	// uploaders - a re-upload just bumps the creation time instead of
	// creating a second row.
	_, err := Postgres.Exec(`INSERT INTO datastore.buffer_queues (data_id, slot, creation_date, buffer)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (data_id, slot, buffer) DO UPDATE SET creation_date=$3`, dataID, slot, now, buffer)
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return nil
}

func GetBufferQueuesByDataIDAndSlot(dataID types.UInt64, slot types.UInt32) (types.List[types.QBuffer], *nex.Error) {
	if nexError := IsObjectAvailable(dataID); nexError != nil {
		return nil, nexError
	}

	bufferQueues := types.NewList[types.QBuffer]()

	rows, err := Postgres.Query(`SELECT buffer FROM datastore.buffer_queues WHERE data_id=$1 AND slot=$2 ORDER BY creation_date`, dataID, slot)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}
	if rows == nil {
		return bufferQueues, nil
	}
	defer rows.Close()

	for rows.Next() {
		var buffer types.QBuffer
		if err := rows.Scan(&buffer); err != nil {
			continue
		}
		bufferQueues = append(bufferQueues, buffer)
	}

	return bufferQueues, nil
}

// InsertOrUpdateCustomRanking adds to a running total rather than
// overwriting - custom rankings (e.g. star counts) accumulate over time as
// more players rate/complete a course.
func InsertOrUpdateCustomRanking(dataID types.UInt64, applicationID, score types.UInt32) *nex.Error {
	if nexError := IsObjectAvailable(dataID); nexError != nil {
		return nexError
	}

	_, err := Postgres.Exec(`INSERT INTO datastore.object_custom_rankings (data_id, application_id, value)
		VALUES ($1, $2, $3)
		ON CONFLICT (data_id, application_id) DO UPDATE SET value=datastore.object_custom_rankings.value+EXCLUDED.value`,
		dataID, applicationID, score,
	)
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return nil
}

func GetCustomRankingsByDataIDs(applicationID types.UInt32, dataIDs types.List[types.UInt64]) types.List[datastore_smm_types.DataStoreCustomRankingResult] {
	results := make(types.List[datastore_smm_types.DataStoreCustomRankingResult], 0, len(dataIDs))

	rows, err := Postgres.Query(`
		SELECT rankings.data_id, rankings.value
		FROM datastore.object_custom_rankings rankings
		JOIN UNNEST($1::bigint[]) WITH ORDINALITY AS rows(data_id, ord)
			ON rankings.data_id = rows.data_id AND rankings.application_id = $2
		ORDER BY rows.ord`,
		pq.Array(dataIDs), applicationID,
	)
	if err != nil {
		return results
	}
	defer rows.Close()

	for rows.Next() {
		var dataID types.UInt64
		var value types.UInt32

		if err := rows.Scan(&dataID, &value); err != nil {
			continue
		}

		objectInfo, nexError := GetObjectInfoByDataID(dataID)
		if nexError != nil {
			continue
		}

		result := datastore_smm_types.NewDataStoreCustomRankingResult()
		result.Score = value
		result.MetaInfo = objectInfo

		results = append(results, result)
	}

	return results
}

func InitializeObjectByAttachFileParam(ownerPID types.PID, param datastore_smm_types.DataStoreAttachFileParam) (types.UInt64, *nex.Error) {
	now := time.Now()

	tagArray := make([]string, 0, len(param.PostParam.Tags))
	for i := range param.PostParam.Tags {
		tagArray = append(tagArray, string(param.PostParam.Tags[i]))
	}

	extraDataArray := make([]string, 0, len(param.PostParam.ExtraData))
	for i := range param.PostParam.ExtraData {
		extraDataArray = append(extraDataArray, string(param.PostParam.ExtraData[i]))
	}

	var dataID types.UInt64
	err := Postgres.QueryRow(`INSERT INTO datastore.objects (
		owner, size, name, data_type, meta_binary,
		permission, permission_recipients, delete_permission, delete_permission_recipients,
		flag, period, refer_data_id, tags, persistence_slot_id, extra_data,
		creation_date, update_date
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
	RETURNING data_id`,
		ownerPID, param.PostParam.Size, param.PostParam.Name, param.PostParam.DataType, param.PostParam.MetaBinary,
		param.PostParam.Permission.Permission, pq.Array(param.PostParam.Permission.RecipientIDs),
		param.PostParam.DelPermission.Permission, pq.Array(param.PostParam.DelPermission.RecipientIDs),
		param.PostParam.Flag, param.PostParam.Period, param.ReferDataID, pq.Array(tagArray),
		param.PostParam.PersistenceInitParam.PersistenceSlotID, pq.Array(extraDataArray),
		now, now,
	).Scan(&dataID)
	if err != nil {
		return types.NewUInt64(0), nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return dataID, nil
}
