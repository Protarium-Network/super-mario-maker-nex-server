package nex_smm

import (
	"bytes"
	"context"

	"github.com/Protarium-Network/super-mario-maker-nex-server/globals"
	"github.com/Protarium-Network/super-mario-maker-nex-server/smmdatabase"
	"github.com/minio/minio-go/v7"
)

// eventCourseMetadataDataID is the reserved DataID Super Mario Maker
// requests on boot for its "event course" metadata. Nintendo's real file
// here is proprietary binary game data this project has no legitimate way
// to obtain or reproduce. Rather than leave the object missing (which
// surfaces as DataStore::NotFound / 106-1204 and blocks Course World from
// finishing initialization), this uploads a minimal, empty, originally-
// authored placeholder object so the lookup succeeds - it just describes
// zero event courses rather than replicating Nintendo's actual file.
const eventCourseMetadataDataID = 900000

func ensureEventCourseMetadataExists(minioClient *minio.Client, bucket string) {
	key := "smm/900000.bin"

	placeholder := []byte{} // Zero event courses - an empty, original placeholder, not Nintendo's file

	info, err := minioClient.PutObject(context.Background(), bucket, key, bytes.NewReader(placeholder), int64(len(placeholder)), minio.PutObjectOptions{})
	if err != nil {
		globals.Logger.Errorf("[SMM] Failed to upload event course metadata placeholder: %s", err.Error())
		return
	}

	if err := smmdatabase.EnsureEventCourseMetadataObject(eventCourseMetadataDataID, uint32(info.Size)); err != nil {
		globals.Logger.Errorf("[SMM] Failed to register event course metadata placeholder: %s", err.Error())
		return
	}

	globals.Logger.Success("[SMM] Event course metadata placeholder ready (Course World will show no event courses)")
}
