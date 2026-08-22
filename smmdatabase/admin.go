package smmdatabase

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"time"

	ash0 "github.com/PretendoNetwork/ASH0"
	"github.com/lib/pq"
)

// splitCourseSegments splits a raw course object's bytes into its 4
// ASH0-compressed segments in file order: thumbnail0, course_data,
// course_data_sub, thumbnail1 - see BuildCourseMetaBinary's docs.
func splitCourseSegments(rawCourseData []byte) ([][]byte, error) {
	sep := []byte("ASH0")
	var starts []int
	idx := 0
	for {
		found := bytes.Index(rawCourseData[idx:], sep)
		if found == -1 {
			break
		}
		starts = append(starts, idx+found)
		idx = idx + found + len(sep)
	}
	if len(starts) != 4 {
		return nil, fmt.Errorf("expected 4 ASH0-compressed segments in course data, found %d", len(starts))
	}

	segs := make([][]byte, 4)
	for i, s := range starts {
		end := len(rawCourseData)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		segs[i] = rawCourseData[s:end]
	}
	return segs, nil
}

// AdminRegisterCourseAttachFile registers the "attach file" companion
// object a course's preview thumbnail needs - confirmed by comparing two
// real, correctly-displaying client-uploaded courses already on this
// server (940002/940005): each has its own separate datastore.objects row
// (data_type=2, refer_data_id pointing back to the course, permission=3,
// delete_permission=3, flag=0, period=90, persistence_slot_id=65535,
// upload_completed=TRUE) alongside its own S3 object at smm/<id>.jpg - the
// exact same real client flow as PrepareAttachFile/CompleteAttachFile
// (nex_smm/datastore_handlers.go), just done here for a file the admin
// already has instead of one the client is uploading live. Admin/bulk
// imports that only PUT the JPEG straight to S3 (skipping this row
// entirely) don't get their course's title text shown in the Course World
// browse list at all - the client appears to treat a course as
// incompletely published without a valid attach-file object for it.
func AdminRegisterCourseAttachFile(ownerPID uint32, referDataID uint64, size uint32) error {
	now := time.Now()
	_, err := Postgres.Exec(`INSERT INTO datastore.objects (
		owner, size, name, data_type, meta_binary,
		permission, permission_recipients, delete_permission, delete_permission_recipients,
		flag, period, refer_data_id, tags, persistence_slot_id, extra_data,
		upload_completed, creation_date, update_date
	) VALUES ($1, $2, '', 2, '', 3, '{}', 3, '{}', 0, 90, $3, '{}', 65535, '{}', TRUE, $4, $4)`,
		ownerPID, size, referDataID, now,
	)
	return err
}

// courseGameStyleIndex decompresses a course_data segment and reads its
// game-mode marker at offset 0x6A (documented in
// github.com/Treeki/MarioUnmaker/FormatNotes.md, reverse engineered from
// the actual game executable: char[2], "M1"=Super Mario Bros,
// "M3"=Super Mario Bros. 3, "MW"=Super Mario World, "WU"=New Super Mario
// Bros. U), then maps it to the style index BuildCourseMetaBinary's word0
// expects (0/1/2/3, SMM1's real canonical style order).
func courseGameStyleIndex(courseDataSegment []byte) (uint32, error) {
	cdt := ash0.Decompress(courseDataSegment)
	if len(cdt) < 0x6C {
		return 0, fmt.Errorf("decompressed course_data too short to read game mode (%d bytes)", len(cdt))
	}

	switch string(cdt[0x6A:0x6C]) {
	case "M1":
		return 0, nil
	case "M3":
		return 1, nil
	case "MW":
		return 2, nil
	case "WU":
		return 3, nil
	default:
		return 0, fmt.Errorf("unrecognized game mode marker %q in course_data", cdt[0x6A:0x6C])
	}
}

// ExtractCourseThumbnailJPEG pulls the real preview JPEG out of a course's
// own thumbnail0 segment, for uploading to S3 as smm/<DataID>.jpg.
//
// Real client uploads never send this JPEG as part of the main course
// object - a real player's course only gets one AFTER the fact, via a
// separate PrepareAttachFile round-trip (nex_smm/datastore_handlers.go)
// where the client uploads its own screenshot. Admin/bulk imports never go
// through that flow, so without this, smm/<DataID>.jpg never exists at
// all - and the Course World browse list falls back to a generic built-in
// placeholder for every course missing one, which is why every
// admin-imported course showed the exact same (wrong) style/category in the
// browse list regardless of its real content.
//
// The decompressed thumbnail0 segment (a real "*.tnl" file, always 51200
// bytes) is: bytes[0:4] CRC32(IEEE) of bytes[4:], bytes[4:6] padding,
// bytes[6:8] JPEG length (big-endian uint16), followed by that many bytes
// of an actual standalone JPEG (starts FFD8, ends FFD9), then zero padding
// to the fixed 51200-byte size - confirmed by decoding a real course's
// thumbnail0 and validating both the embedded CRC32 and the extracted
// JPEG's own magic bytes.
func ExtractCourseThumbnailJPEG(rawCourseData []byte) ([]byte, error) {
	segs, err := splitCourseSegments(rawCourseData)
	if err != nil {
		return nil, err
	}

	thumb := ash0.Decompress(segs[0])
	if len(thumb) < 8 {
		return nil, fmt.Errorf("decompressed thumbnail0 too short (%d bytes)", len(thumb))
	}

	jpegLen := int(binary.BigEndian.Uint16(thumb[6:8]))
	jpegStart := 8
	jpegEnd := jpegStart + jpegLen
	if jpegEnd > len(thumb) {
		return nil, fmt.Errorf("thumbnail0 JPEG length %d exceeds buffer (%d)", jpegLen, len(thumb))
	}

	jpegData := thumb[jpegStart:jpegEnd]
	if len(jpegData) < 4 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		return nil, fmt.Errorf("extracted thumbnail0 data doesn't look like a JPEG")
	}

	return jpegData, nil
}

// ErrCourseAlreadyExists is returned by AdminImportCourse when the
// requested explicitDataID is already used by another object - most often
// because the admin re-submitted an import (or left a stale value in the
// explicit_data_id field) for a course that's already on the server.
var ErrCourseAlreadyExists = errors.New("a course with this DataID is already on the server")

func ensureAdminSchema() {
	_, err := Postgres.Exec(`CREATE TABLE IF NOT EXISTS datastore.verified_makers (
		pid bigint PRIMARY KEY,
		added_at timestamp
	)`)
	if err != nil {
		panic(err)
	}
}

// CourseRow is a flattened row for the admin panel's course list/search.
type CourseRow struct {
	DataID  uint64
	Owner   uint32
	Name    string
	Size    uint32
	Stars   int64
	Plays   int64
	Deleted bool
	Created time.Time
}

// ListCourses returns real course objects (data_type > 2, the same range
// GetUserCourseObjectIDs uses to distinguish courses from maker/attach-file
// objects), optionally filtered by a DataID or owner PID substring match.
func ListCourses(search string, limit int) ([]CourseRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	query := `
		SELECT o.data_id, o.owner, COALESCE(o.name, ''), COALESCE(o.size, 0), o.deleted, o.creation_date, o.plays,
			COALESCE((SELECT value FROM datastore.object_custom_rankings r WHERE r.data_id = o.data_id AND r.application_id = 0), 0)
		FROM datastore.objects o
		WHERE o.data_type > 2 AND o.data_type < 50
	`
	args := []interface{}{}

	if search != "" {
		query += ` AND (o.data_id::text = $1 OR o.owner::text = $1)`
		args = append(args, search)
	}

	args = append(args, limit)
	query += ` ORDER BY o.creation_date DESC LIMIT $` + strconv.Itoa(len(args))

	rows, err := Postgres.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CourseRow
	for rows.Next() {
		var row CourseRow
		if err := rows.Scan(&row.DataID, &row.Owner, &row.Name, &row.Size, &row.Deleted, &row.Created, &row.Plays, &row.Stars); err != nil {
			continue
		}
		results = append(results, row)
	}

	return results, nil
}

// AdminDeleteCourse hard-deletes a course object outright (unlike the
// normal client-facing DeleteObjectByDataID, which just sets deleted=TRUE -
// same soft-delete behavior here, kept consistent with how the game itself
// expects deleted objects to behave if a client somehow still references
// the DataID).
func AdminDeleteCourse(dataID uint64) error {
	_, err := Postgres.Exec(`UPDATE datastore.objects SET deleted = TRUE, update_date = $1 WHERE data_id = $2`, time.Now(), dataID)
	return err
}

// AdminAddStars boosts a course's star/custom-ranking count directly -
// same underlying mechanism as a real player rating it, just applied
// without requiring real plays.
func AdminAddStars(dataID uint64, amount int64) error {
	_, err := Postgres.Exec(`INSERT INTO datastore.object_custom_rankings (data_id, application_id, value)
		VALUES ($1, 0, $2)
		ON CONFLICT (data_id, application_id) DO UPDATE SET value=datastore.object_custom_rankings.value+EXCLUDED.value`,
		dataID, amount,
	)
	return err
}

// AdminSetStars sets a course's star/custom-ranking count to an exact
// value, rather than adding a delta - used when importing a course whose
// real star count is already known.
func AdminSetStars(dataID uint64, value int64) error {
	_, err := Postgres.Exec(`INSERT INTO datastore.object_custom_rankings (data_id, application_id, value)
		VALUES ($1, 0, $2)
		ON CONFLICT (data_id, application_id) DO UPDATE SET value=EXCLUDED.value`,
		dataID, value,
	)
	return err
}

// AdminSetVerifiedMaker adds or removes a PID from the verified-maker list
// that feeds GetApplicationConfig's "Official Makers" (applicationID 1)
// response.
func AdminSetVerifiedMaker(pid uint64, verified bool) error {
	if verified {
		_, err := Postgres.Exec(`INSERT INTO datastore.verified_makers (pid, added_at) VALUES ($1, $2) ON CONFLICT (pid) DO NOTHING`, pid, time.Now())
		return err
	}
	_, err := Postgres.Exec(`DELETE FROM datastore.verified_makers WHERE pid = $1`, pid)
	return err
}

// AdminSetCourseRatings populates all 7 real DataStore rating slots a
// course object carries (0=plays, 1=unknown1, 2=clears, 3=total_attempts,
// 4=failures, 5=unknown2, 6=miiverse_comments) - confirmed straight from
// Pretendo's own archive.py (github.com/PretendoNetwork/smm1-course-archive),
// which reads a real course's metadata this exact way
// (meta_info.ratings[0..6].info.total_value). Admin-imported courses never
// had these slots at all before, unlike real client-uploaded ones.
func AdminSetCourseRatings(dataID uint64, plays, unknown1, clears, totalAttempts, failures, unknown2, miiverseComments int64) error {
	values := [7]int64{plays, unknown1, clears, totalAttempts, failures, unknown2, miiverseComments}

	for slot, value := range values {
		_, err := Postgres.Exec(`INSERT INTO datastore.object_ratings (
			data_id, slot, flag, internal_flag, lock_type, initial_value,
			range_min, range_max, period_hour, period_duration, total_value, count
		) VALUES ($1, $2, 0, 0, 0, 0, -2147483648, 2147483647, 0, 0, $3, 1)
		ON CONFLICT (data_id, slot) DO UPDATE SET total_value = EXCLUDED.total_value`,
			dataID, slot, value,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// AdminSetCourseRecord directly writes a course's world-record row (slot 0),
// setting first_pid and best_pid independently - unlike
// InsertOrUpdateCourseRecord (which only ever moves best_pid/first_pid
// together as part of the "does this new score beat the old one" gameplay
// path), an import already knows the real historical first-clear and
// best-time holders, which can be two different people.
func AdminSetCourseRecord(dataID uint64, firstPID, bestPID uint32, bestScore int32, createdAt, updatedAt time.Time) error {
	_, err := Postgres.Exec(`INSERT INTO datastore.course_records (
		data_id, slot, first_pid, best_pid, best_score, creation_date, update_date
	) VALUES ($1, 0, $2, $3, $4, $5, $6)
	ON CONFLICT (data_id, slot) DO UPDATE SET
		first_pid = EXCLUDED.first_pid,
		best_pid = EXCLUDED.best_pid,
		best_score = EXCLUDED.best_score,
		creation_date = EXCLUDED.creation_date,
		update_date = EXCLUDED.update_date`,
		dataID, firstPID, bestPID, bestScore, createdAt, updatedAt,
	)
	return err
}

func ListVerifiedMakers() ([]uint64, error) {
	rows, err := Postgres.Query(`SELECT pid FROM datastore.verified_makers ORDER BY added_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pids []uint64
	for rows.Next() {
		var pid uint64
		if err := rows.Scan(&pid); err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// BuildCourseMetaBinary constructs the REAL course meta_binary structure -
// this has nothing to do with the course's name (that was a wrong earlier
// assumption). Reverse engineered by decoding the DB meta_binary of two
// real, currently-working course objects already on this server (940002
// and 940005) as 11 big-endian uint32 words and cross-checking every word
// against each object's own actual raw file on S3:
//
//	word0 (0x00): game style index shown as the course's style banner
//	              ("SUPER MARIO BROS." etc.) wherever the client displays a
//	              course without fully parsing course_data itself (e.g. the
//	              course-ID lookup screen) - confirmed against two real
//	              courses: 940002 (embedded course_data game mode byte "WU"
//	              = New Super Mario Bros. U) has word0=3, 940005 (game mode
//	              "M1" = Super Mario Bros.) has word0=0. This matches SMM1's
//	              real, canonical style ordering (SMB=0, SMB3=1, SMW=2,
//	              NSMBU=3). Previously hardcoded to 0 for every import,
//	              which is why every admin-imported course showed "SUPER
//	              MARIO BROS." regardless of its real style.
//	word1 (0x04): 0 in both samples.
//	word2 (0x08): compressed (ASH0) byte length of the course_data segment.
//	word3 (0x0c): compressed byte length of the course_data_sub segment.
//	word4 (0x10): compressed byte length of the thumbnail0 segment.
//	word5 (0x14): compressed byte length of the thumbnail1 segment.
//	word6 (0x18): 3 in both samples (constant/marker for what follows).
//	word7 (0x1c): CRC32 (IEEE) of the course_data segment's compressed bytes.
//	word8 (0x20): CRC32 of the course_data_sub segment's compressed bytes.
//	word9 (0x24): CRC32 of the thumbnail0 segment's compressed bytes.
//	word10(0x28): CRC32 of the thumbnail1 segment's compressed bytes.
//
// This is exactly what the game's own State::cMarioClubCRCError check
// validates against (string "header crc mismatch" found in Block.rpx):
// the client uses these lengths to slice the single downloaded blob into
// its 4 ASH0-compressed segments and CRC-checks each one BEFORE even
// attempting to decompress it. Writing one course's meta_binary onto a
// different course's file (as this function previously did by copying
// 940002's blob onto every import) makes the client compute a checksum
// mismatch against the wrong segment boundaries entirely - this is what
// "Les données du stage sont corrompues. Il ne peut pas être ouvert." was.
//
// rawCourseData is the exact bytes of the course object as it will be
// uploaded to S3 (course.bin) - i.e. the 4 ASH0-compressed segments
// (thumbnail0, course_data, course_data_sub, thumbnail1) concatenated back
// to back, the same format `smm1-level-downloader` splits with by
// searching for repeated "ASH0" magic markers.
func BuildCourseMetaBinary(rawCourseData []byte) ([]byte, error) {
	segs, err := splitCourseSegments(rawCourseData)
	if err != nil {
		return nil, err
	}
	thumbnail0, courseData, courseDataSub, thumbnail1 := segs[0], segs[1], segs[2], segs[3]

	styleIndex, err := courseGameStyleIndex(courseData)
	if err != nil {
		return nil, err
	}

	out := make([]byte, 44)
	binary.BigEndian.PutUint32(out[0x00:], styleIndex)
	binary.BigEndian.PutUint32(out[0x04:], 0)
	binary.BigEndian.PutUint32(out[0x08:], uint32(len(courseData)))
	binary.BigEndian.PutUint32(out[0x0c:], uint32(len(courseDataSub)))
	binary.BigEndian.PutUint32(out[0x10:], uint32(len(thumbnail0)))
	binary.BigEndian.PutUint32(out[0x14:], uint32(len(thumbnail1)))
	binary.BigEndian.PutUint32(out[0x18:], 3)
	binary.BigEndian.PutUint32(out[0x1c:], crc32.ChecksumIEEE(courseData))
	binary.BigEndian.PutUint32(out[0x20:], crc32.ChecksumIEEE(courseDataSub))
	binary.BigEndian.PutUint32(out[0x24:], crc32.ChecksumIEEE(thumbnail0))
	binary.BigEndian.PutUint32(out[0x28:], crc32.ChecksumIEEE(thumbnail1))

	return out, nil
}

// AdminImportCourse directly registers an already-valid course file the
// admin provides (e.g. a real player's own exported course, or a course
// recovered from their own past upload) - this does NOT generate course
// data; it just writes the object row for a file the admin already has and
// is separately uploading to S3.
//
// explicitDataID, when non-zero, forces the new row to use that exact
// DataID instead of the next value from the sequence. Real course .bin
// files embed the DataID they were originally uploaded under (it's baked
// into the course share-code checksum), so re-hosting a real exported
// course under a freshly-generated DataID makes the client's own
// consistency check fail and show the level as corrupted - importing under
// the course's real, original DataID avoids that mismatch. The sequence is
// bumped past any explicitly-supplied ID so later auto-generated imports
// never collide with it.
//
// flag=3840, delete_permission=3 and period=90 are the real values
// confirmed from real data_type=10 courses already on this server (940002,
// 940005). metaBinary must come from BuildCourseMetaBinary(rawCourseData),
// computed from the SAME bytes being uploaded to S3 for this course - see
// that function's docs for why reusing another course's meta_binary breaks
// the client's own CRC check.
func AdminImportCourse(ownerPID uint32, name string, size uint32, explicitDataID uint64, metaBinary []byte) (uint64, error) {
	var dataID uint64
	now := time.Now()

	if explicitDataID != 0 {
		err := Postgres.QueryRow(`INSERT INTO datastore.objects (
			data_id, upload_completed, owner, size, name, data_type, meta_binary,
			permission, permission_recipients, delete_permission, delete_permission_recipients,
			flag, period, refer_data_id, tags, persistence_slot_id, extra_data,
			creation_date, update_date
		) VALUES ($1, TRUE, $2, $3, $4, 10, $6, 0, '{}', 3, '{}', 3840, 90, 0, '{}', 65535, ARRAY['WUP','4','EUR','77','FR',''], $5, $5)
		RETURNING data_id`, explicitDataID, ownerPID, size, name, now, metaBinary).Scan(&dataID)
		if err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23505" {
				return 0, ErrCourseAlreadyExists
			}
			return 0, err
		}

		// Keep the auto-increment sequence ahead of any manually-imported ID
		// so it never later hands out (and collides with) this same value.
		_, _ = Postgres.Exec(`SELECT setval('datastore.object_data_id_seq', GREATEST($1, (SELECT last_value FROM datastore.object_data_id_seq)))`, explicitDataID)
	} else {
		err := Postgres.QueryRow(`INSERT INTO datastore.objects (
			upload_completed, owner, size, name, data_type, meta_binary,
			permission, permission_recipients, delete_permission, delete_permission_recipients,
			flag, period, refer_data_id, tags, persistence_slot_id, extra_data,
			creation_date, update_date
		) VALUES (TRUE, $1, $2, $3, 10, $5, 0, '{}', 3, '{}', 3840, 90, 0, '{}', 65535, ARRAY['WUP','4','EUR','77','FR',''], $4, $4)
		RETURNING data_id`, ownerPID, size, name, now, metaBinary).Scan(&dataID)
		if err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23505" {
				return 0, ErrCourseAlreadyExists
			}
			return 0, err
		}
	}

	_, _ = Postgres.Exec(`INSERT INTO datastore.object_custom_rankings (data_id, application_id, value) VALUES ($1, 0, 0) ON CONFLICT DO NOTHING`, dataID)

	// Always seed the 7 rating slots (plays/unknown1/clears/attempts/failures/
	// unknown2/comments), even when no metadata_file supplied real starting
	// values (they just start at 0 in that case). RateObjectWithPassword
	// (generic_datastore.go) does a bare UPDATE with no upsert fallback - if
	// this row doesn't exist yet, the real client's in-game play/attempt/star
	// submissions silently affect 0 rows and nothing ever gets recorded.
	_ = AdminSetCourseRatings(dataID, 0, 0, 0, 0, 0, 0, 0)

	return dataID, nil
}
