package smmdatabase

import (
	"database/sql"
	"errors"
	"time"

	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	datastore_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/types"
	"github.com/lib/pq"
)

// IsObjectAvailable checks an object exists, finished uploading, isn't
// deleted, and isn't flagged under_review (SMM-specific - held-back courses
// pending moderation are treated as unavailable rather than outright gone).
func IsObjectAvailable(dataID types.UInt64) *nex.Error {
	var underReview bool

	err := Postgres.QueryRow(`SELECT under_review FROM datastore.objects WHERE data_id=$1 AND upload_completed=TRUE AND deleted=FALSE`, dataID).Scan(&underReview)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nex.NewError(nex.ResultCodes.DataStore.NotFound, "Object not found")
		}
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	if underReview {
		return nex.NewError(nex.ResultCodes.DataStore.UnderReviewing, "This object is currently under review")
	}

	return nil
}

func GetObjectInfoByDataID(dataID types.UInt64) (datastore_types.DataStoreMetaInfo, *nex.Error) {
	if nexError := IsObjectAvailable(dataID); nexError != nil {
		return datastore_types.NewDataStoreMetaInfo(), nexError
	}

	metaInfo := datastore_types.NewDataStoreMetaInfo()
	metaInfo.Permission = datastore_types.NewDataStorePermission()
	metaInfo.DelPermission = datastore_types.NewDataStorePermission()
	metaInfo.ExpireTime = types.NewDateTime(0x9C3F3E0000) // 9999-12-31T00:00:00.000Z, matches the real server
	metaInfo.Ratings = types.NewList[datastore_types.DataStoreRatingInfoWithSlot]()

	var createdDate, updatedDate time.Time
	var tagArray []string

	err := Postgres.QueryRow(`SELECT
		data_id, owner, size, name, data_type, meta_binary,
		permission, permission_recipients, delete_permission, delete_permission_recipients,
		period, refer_data_id, flag, tags, creation_date, update_date
		FROM datastore.objects WHERE data_id=$1`, dataID).Scan(
		&metaInfo.DataID, &metaInfo.OwnerID, &metaInfo.Size, &metaInfo.Name, &metaInfo.DataType, &metaInfo.MetaBinary,
		&metaInfo.Permission.Permission, pq.Array(&metaInfo.Permission.RecipientIDs),
		&metaInfo.DelPermission.Permission, pq.Array(&metaInfo.DelPermission.RecipientIDs),
		&metaInfo.Period, &metaInfo.ReferDataID, &metaInfo.Flag, pq.Array(&tagArray),
		&createdDate, &updatedDate,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return datastore_types.NewDataStoreMetaInfo(), nex.NewError(nex.ResultCodes.DataStore.NotFound, "Object not found")
		}
		return datastore_types.NewDataStoreMetaInfo(), nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	ratings, nexError := GetObjectRatingsWithSlotByDataID(metaInfo.DataID)
	if nexError != nil {
		return datastore_types.NewDataStoreMetaInfo(), nexError
	}

	metaInfo.Tags = make(types.List[types.String], 0, len(tagArray))
	for i := range tagArray {
		metaInfo.Tags = append(metaInfo.Tags, types.String(tagArray[i]))
	}
	metaInfo.Ratings = ratings

	metaInfo.CreatedTime.FromTimestamp(createdDate)
	metaInfo.UpdatedTime.FromTimestamp(updatedDate)
	metaInfo.ReferredTime.FromTimestamp(createdDate)

	return metaInfo, nil
}

func GetObjectInfoByDataIDWithPassword(dataID types.UInt64, password types.UInt64) (datastore_types.DataStoreMetaInfo, *nex.Error) {
	var storedPassword int64

	err := Postgres.QueryRow(`SELECT access_password FROM datastore.objects WHERE data_id = $1`, int64(dataID)).Scan(&storedPassword)
	if errors.Is(err, sql.ErrNoRows) {
		return datastore_types.DataStoreMetaInfo{}, nex.NewError(nex.ResultCodes.DataStore.NotFound, "not found")
	}
	if err != nil {
		return datastore_types.DataStoreMetaInfo{}, nex.NewError(nex.ResultCodes.DataStore.Unknown, "unknown")
	}

	if storedPassword != 0 && storedPassword != int64(password) {
		return datastore_types.DataStoreMetaInfo{}, nex.NewError(nex.ResultCodes.DataStore.PermissionDenied, "wrong password")
	}

	return GetObjectInfoByDataID(dataID)
}

func GetObjectInfoByPersistenceTargetWithPassword(persistenceTarget datastore_types.DataStorePersistenceTarget, password types.UInt64) (datastore_types.DataStoreMetaInfo, *nex.Error) {
	var dataID uint64

	err := Postgres.QueryRow(`
		SELECT data_id FROM datastore.objects
		WHERE owner = $1 AND persistence_slot_id = $2 AND deleted = false
		ORDER BY update_date DESC
		LIMIT 1
	`, int64(persistenceTarget.OwnerID), int64(persistenceTarget.PersistenceSlotID)).Scan(&dataID)
	if errors.Is(err, sql.ErrNoRows) {
		return datastore_types.DataStoreMetaInfo{}, nex.NewError(nex.ResultCodes.DataStore.NotFound, "not found")
	}
	if err != nil {
		return datastore_types.DataStoreMetaInfo{}, nex.NewError(nex.ResultCodes.DataStore.Unknown, "unknown")
	}

	return GetObjectInfoByDataID(types.NewUInt64(dataID))
}

func GetObjectOwnerByDataID(dataID types.UInt64) (uint32, *nex.Error) {
	var owner uint32

	err := Postgres.QueryRow(`SELECT owner FROM datastore.objects WHERE data_id=$1`, dataID).Scan(&owner)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nex.NewError(nex.ResultCodes.DataStore.NotFound, "Object not found")
		}
		return 0, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return owner, nil
}

func GetObjectSizeByDataID(dataID types.UInt64) (uint32, *nex.Error) {
	var size uint32

	err := Postgres.QueryRow(`SELECT size FROM datastore.objects WHERE data_id=$1`, dataID).Scan(&size)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nex.NewError(nex.ResultCodes.DataStore.NotFound, "Object not found")
		}
		return 0, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return size, nil
}

func GetObjectRatingsWithSlotByDataID(dataID types.UInt64) (types.List[datastore_types.DataStoreRatingInfoWithSlot], *nex.Error) {
	results := types.NewList[datastore_types.DataStoreRatingInfoWithSlot]()

	rows, err := Postgres.Query(`SELECT slot, total_value, count FROM datastore.object_ratings WHERE data_id=$1`, dataID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			entry := datastore_types.NewDataStoreRatingInfoWithSlot()
			entry.Rating = datastore_types.NewDataStoreRatingInfo()

			var slot int16

			if err := rows.Scan(&slot, &entry.Rating.TotalValue, &entry.Rating.Count); err != nil {
				continue
			}

			entry.Slot = types.NewInt8(int8(slot))
			results = append(results, entry)
		}
	}

	return results, nil
}

func InitializeObjectByPreparePostParam(ownerPID types.PID, param datastore_types.DataStorePreparePostParam) (uint64, *nex.Error) {
	var dataID uint64

	tagArray := make([]string, 0, len(param.Tags))
	for i := range param.Tags {
		tagArray = append(tagArray, string(param.Tags[i]))
	}

	extraDataArray := make([]string, 0, len(param.ExtraData))
	for i := range param.ExtraData {
		extraDataArray = append(extraDataArray, string(param.ExtraData[i]))
	}

	now := time.Now()
	err := Postgres.QueryRow(`INSERT INTO datastore.objects (
		owner, size, name, data_type, meta_binary,
		permission, permission_recipients, delete_permission, delete_permission_recipients,
		flag, period, refer_data_id, tags, persistence_slot_id, extra_data,
		creation_date, update_date
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
	RETURNING data_id`,
		ownerPID, param.Size, param.Name, param.DataType, param.MetaBinary,
		param.Permission.Permission, pq.Array(param.Permission.RecipientIDs),
		param.DelPermission.Permission, pq.Array(param.DelPermission.RecipientIDs),
		param.Flag, param.Period, param.ReferDataID, pq.Array(tagArray),
		param.PersistenceInitParam.PersistenceSlotID, pq.Array(extraDataArray),
		now, now,
	).Scan(&dataID)
	if err != nil {
		return 0, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return dataID, nil
}

func InitializeObjectRatingWithSlot(dataID uint64, param datastore_types.DataStoreRatingInitParamWithSlot) *nex.Error {
	_, err := Postgres.Exec(`INSERT INTO datastore.object_ratings (
		data_id, slot, flag, internal_flag, lock_type, initial_value,
		range_min, range_max, period_hour, period_duration, total_value, count
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,0)
	ON CONFLICT (data_id, slot) DO NOTHING`,
		dataID, param.Slot, param.Param.Flag, param.Param.InternalFlag, param.Param.LockType,
		param.Param.InitialValue, param.Param.RangeMin, param.Param.RangeMax,
		param.Param.PeriodHour, param.Param.PeriodDuration, param.Param.InitialValue,
	)
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return nil
}

func RateObjectWithPassword(dataID types.UInt64, slot types.UInt8, ratingValue types.Int32, accessPassword types.UInt64) (datastore_types.DataStoreRatingInfo, *nex.Error) {
	ratingInfo := datastore_types.NewDataStoreRatingInfo()

	_, err := Postgres.Exec(`UPDATE datastore.object_ratings SET total_value = total_value + $1, count = count + 1
		WHERE data_id=$2 AND slot=$3`, ratingValue, dataID, slot)
	if err != nil {
		return ratingInfo, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	err = Postgres.QueryRow(`SELECT total_value, count FROM datastore.object_ratings WHERE data_id=$1 AND slot=$2`, dataID, slot).
		Scan(&ratingInfo.TotalValue, &ratingInfo.Count)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ratingInfo, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return ratingInfo, nil
}

func DeleteObjectByDataID(dataID types.UInt64) *nex.Error {
	_, err := Postgres.Exec(`UPDATE datastore.objects SET deleted = TRUE, update_date = $1 WHERE data_id = $2`, time.Now(), dataID)
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	return nil
}

func UpdateObjectPeriodByDataIDWithPassword(dataID types.UInt64, period types.UInt16, updatePassword types.UInt64) *nex.Error {
	_, err := Postgres.Exec(`UPDATE datastore.objects SET period = $1, update_date = $2 WHERE data_id = $3`, uint16(period), time.Now().UTC(), int64(dataID))
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, "update error")
	}
	return nil
}

func UpdateObjectMetaBinaryByDataIDWithPassword(dataID types.UInt64, metaBinary types.QBuffer, updatePassword types.UInt64) *nex.Error {
	_, err := Postgres.Exec(`UPDATE datastore.objects SET meta_binary = $1, update_date = $2 WHERE data_id = $3`, []byte(metaBinary), time.Now().UTC(), int64(dataID))
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, "update error")
	}
	return nil
}

func UpdateObjectDataTypeByDataIDWithPassword(dataID types.UInt64, dataType types.UInt16, updatePassword types.UInt64) *nex.Error {
	_, err := Postgres.Exec(`UPDATE datastore.objects SET data_type = $1, update_date = $2 WHERE data_id = $3`, uint16(dataType), time.Now().UTC(), int64(dataID))
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, "update error")
	}
	return nil
}

func UpdateObjectUploadCompletedByDataID(dataID types.UInt64, completed bool) *nex.Error {
	_, err := Postgres.Exec(`UPDATE datastore.objects SET upload_completed = $1, update_date = $2 WHERE data_id = $3`, completed, time.Now().UTC(), int64(dataID))
	if err != nil {
		return nex.NewError(nex.ResultCodes.DataStore.Unknown, "update error")
	}
	return nil
}
