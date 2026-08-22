package smmdatabase

import (
	"time"

	"github.com/lib/pq"
)

// EnsureEventCourseMetadataObject inserts (or refreshes the size of) the
// reserved-DataID placeholder object described in
// nex_smm/event_metadata.go. Idempotent - safe to call on every boot.
func EnsureEventCourseMetadataObject(dataID uint64, size uint32) error {
	var exists bool

	err := Postgres.QueryRow(`SELECT EXISTS(SELECT 1 FROM datastore.objects WHERE data_id=$1)`, dataID).Scan(&exists)
	if err != nil {
		return err
	}

	now := time.Now()

	if !exists {
		_, err := Postgres.Exec(`INSERT INTO datastore.objects (
			data_id, upload_completed, owner, size, name, data_type, meta_binary,
			permission, permission_recipients, delete_permission, delete_permission_recipients,
			flag, period, refer_data_id, tags, persistence_slot_id, extra_data,
			creation_date, update_date
		) VALUES ($1, TRUE, $2, $3, '', 50, '', 0, $4, 0, $5, 0, 64306, 0, $6, 0, $7, $8, $9)`,
			dataID,
			2, // "Quazal Rendez-Vous" special account, matches how the real game treats this object
			size,
			pq.Array([]uint32{}),
			pq.Array([]uint32{}), // Real server sets delete_permission=0 (everyone) here - kept consistent
			pq.Array([]string{}),
			pq.Array([]string{}),
			now, now,
		)
		return err
	}

	_, err = Postgres.Exec(`UPDATE datastore.objects SET size=$1, update_date=$2 WHERE data_id=$3`, size, now, dataID)
	return err
}
