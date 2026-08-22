package smmdatabase

import (
	"database/sql"
	"errors"

	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	datastore_smm_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/super-mario-maker/types"
	datastore_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/types"
	"github.com/lib/pq"
)

// GetLatestCoursesWithLimit backs the "Latest Courses" browsing tab -
// unlike GetRandomCoursesWithLimit (used for Recommended/Suggested/100
// Mario), this sorts by upload date so it's actually "latest".
func GetLatestCoursesWithLimit(limit int) (types.List[datastore_smm_types.DataStoreCustomRankingResult], *nex.Error) {
	courses := types.NewList[datastore_smm_types.DataStoreCustomRankingResult]()

	rows, err := Postgres.Query(`
		SELECT data_id FROM datastore.objects
		WHERE upload_completed = TRUE AND deleted = FALSE AND under_review = FALSE
		AND data_id IN (SELECT data_id FROM datastore.object_custom_rankings WHERE application_id = 0)
		ORDER BY creation_date DESC
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

// GetTopRankedCoursesWithLimit backs DataStore::GetCustomRanking - a real
// ranking browse (as opposed to GetRandomCoursesWithLimit's unordered
// pool), sorted by actual custom-ranking value descending. This is what
// most directly backs "top creators"/leaderboard-style browsing screens.
func GetTopRankedCoursesWithLimit(applicationID uint32, limit int) (types.List[datastore_smm_types.DataStoreCustomRankingResult], *nex.Error) {
	courses := types.NewList[datastore_smm_types.DataStoreCustomRankingResult]()

	rows, err := Postgres.Query(`
		SELECT r.data_id FROM datastore.object_custom_rankings r
		JOIN datastore.objects o ON o.data_id = r.data_id
		WHERE r.application_id = $1 AND o.upload_completed = TRUE AND o.deleted = FALSE AND o.under_review = FALSE
		ORDER BY r.value DESC
		LIMIT $2
	`, applicationID, limit)
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
		results := GetCustomRankingsByDataIDs(types.NewUInt32(applicationID), types.List[types.UInt64]{types.NewUInt64(dataID)})
		courses = append(courses, results...)
	}

	return courses, nil
}

// GetObjectsByOwnerIDs backs the "creators" / maker-profile browsing
// screen (DataStore::GetMetaByOwnerID) - every course object belonging to
// a set of owners, optionally filtered to specific data types.
func GetObjectsByOwnerIDs(ownerIDs types.List[types.UInt32], dataTypes types.List[types.UInt16], resultRange types.ResultRange) types.List[datastore_types.DataStoreMetaInfo] {
	results := types.NewList[datastore_types.DataStoreMetaInfo]()
	if len(ownerIDs) == 0 {
		return results
	}

	limit := int(resultRange.Length)
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	for i := range ownerIDs {
		var rows *sql.Rows
		var err error

		if len(dataTypes) > 0 {
			typeInts := make([]int32, len(dataTypes))
			for j := range dataTypes {
				typeInts[j] = int32(dataTypes[j])
			}
			rows, err = Postgres.Query(`SELECT data_id FROM datastore.objects
				WHERE owner=$1 AND data_type = ANY($2) AND upload_completed=TRUE AND deleted=FALSE
				ORDER BY creation_date DESC LIMIT $3`, ownerIDs[i], pq.Array(typeInts), limit)
		} else {
			rows, err = Postgres.Query(`SELECT data_id FROM datastore.objects
				WHERE owner=$1 AND upload_completed=TRUE AND deleted=FALSE
				ORDER BY creation_date DESC LIMIT $2`, ownerIDs[i], limit)
		}

		if err != nil {
			continue
		}

		for rows.Next() {
			var dataID types.UInt64
			if err := rows.Scan(&dataID); err != nil {
				continue
			}

			objectInfo, nexError := GetObjectInfoByDataID(dataID)
			if nexError != nil {
				continue
			}

			results = append(results, objectInfo)
		}
		rows.Close()
	}

	return results
}
