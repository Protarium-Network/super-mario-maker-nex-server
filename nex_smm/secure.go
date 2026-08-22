package nex_smm

import (
	"fmt"
	"os"
	"strconv"

	"github.com/EcrazerDev/super-mario-maker/globals"
	"github.com/EcrazerDev/super-mario-maker/smmdatabase"

	nex "github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	datastorecommon "github.com/PretendoNetwork/nex-protocols-common-go/v2/datastore"
	common_secure "github.com/PretendoNetwork/nex-protocols-common-go/v2/secure-connection"
	datastore_smm "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/super-mario-maker"
	message_delivery "github.com/PretendoNetwork/nex-protocols-go/v2/message-delivery"
	secure "github.com/PretendoNetwork/nex-protocols-go/v2/secure-connection"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var SecureServer *nex.PRUDPServer
var SecureEndpoint *nex.PRUDPEndPoint

// presigner generates presigned S3 URLs for the SMM-specific DataStore
// methods (GetObjectInfos, PrepareAttachFile, CompleteAttachFile) that
// bypass the generic DataStore common protocol's own built-in presigning.
var presigner *datastorecommon.S3Presigner

func StartSecureServer() {
	SecureServer = nex.NewPRUDPServer()

	SecureEndpoint = nex.NewPRUDPEndPoint(1)
	SecureEndpoint.IsSecureEndPoint = true
	SecureEndpoint.ServerAccount = globals.SecureServerAccount
	SecureEndpoint.AccountDetailsByPID = globals.AccountDetailsByPID
	SecureEndpoint.AccountDetailsByUsername = globals.AccountDetailsByUsername
	SecureServer.BindPRUDPEndPoint(SecureEndpoint)

	SecureServer.LibraryVersions.SetDefault(nex.NewLibraryVersion(3, 8, 3))
	SecureServer.AccessKey = AccessKey
	SecureServer.ByteStreamSettings.UseStructureHeader = true

	SecureEndpoint.OnData(func(packet nex.PacketInterface) {
		request := packet.RMCMessage()
		if request == nil {
			return
		}
		pid := uint64(packet.Sender().PID())
		fmt.Printf("[SMM Secure] PID=%d protocol=0x%02X method=0x%02X\n", pid, request.ProtocolID, request.MethodID)
	})

	SecureEndpoint.OnConnectionEnded(func(connection *nex.PRUDPConnection) {
		fmt.Printf("[SMM Secure] PID=%d disconnected\n", uint64(connection.PID()))
	})

	registerSecureServerProtocols()

	port, _ := strconv.Atoi(os.Getenv("PN_SMM_SECURE_PORT"))
	globals.Logger.Successf("[SMM] Secure server listening on UDP %d", port)
	SecureServer.Listen(port)
}

func registerSecureServerProtocols() {
	secureProtocol := secure.NewProtocol()
	SecureEndpoint.RegisterServiceProtocol(secureProtocol)
	secureCommon := common_secure.NewCommonProtocol(secureProtocol)
	// Super Mario Maker uses TicketGranting::LoginEx like every other game
	// here, not a full NEX1-style secure login - without this,
	// SecureConnection::Register unconditionally returns
	// Authentication::ValidationFailed (106-0807). Same fix already needed
	// for Wii U Chat, Puyo, and Rio 2016.
	secureCommon.EnableInsecureRegister()
	secureCommon.CreateReportDBRecord = func(pid types.PID, reportID types.UInt32, reportData types.QBuffer) error {
		return nil
	}

	messageDeliveryProtocol := message_delivery.NewProtocol()
	SecureEndpoint.RegisterServiceProtocol(messageDeliveryProtocol)
	messageDeliveryProtocol.DeliverMessage = DeliverMessage

	s3Endpoint := os.Getenv("PN_S3_ENDPOINT")
	if s3Endpoint == "" {
		globals.Logger.Warning("[SMM] PN_S3_ENDPOINT not set - DataStore will fail")
		return
	}

	minioClient, err := minio.New(s3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("PN_S3_ACCESS_KEY"), os.Getenv("PN_S3_SECRET_KEY"), ""),
		Secure: true,
	})
	if err != nil {
		globals.Logger.Errorf("[SMM] Failed to create MinIO client: %s", err.Error())
		return
	}

	presigner = datastorecommon.NewS3Presigner(minioClient)

	s3Bucket := os.Getenv("PN_S3_BUCKET")
	if s3Bucket == "" {
		s3Bucket = "smm-datastore"
	}

	ensureEventCourseMetadataExists(minioClient, s3Bucket)

	// Super Mario Maker's own DataStore protocol variant - course
	// upload/browsing/rankings/buffer queues. Registered BEFORE the
	// generic DataStore common protocol below (matches Pretendo's own
	// reference server, which registers this one first "so that they can
	// be overridden if needed").
	smmDatastore := datastore_smm.NewProtocol(SecureEndpoint)
	smmDatastore.GetObjectInfos = GetObjectInfos
	smmDatastore.RateCustomRanking = RateCustomRanking
	smmDatastore.GetCustomRanking = GetCustomRanking
	smmDatastore.GetCustomRankingByDataID = GetCustomRankingByDataID
	smmDatastore.AddToBufferQueues = AddToBufferQueues
	smmDatastore.GetBufferQueue = GetBufferQueue
	smmDatastore.CompleteAttachFile = CompleteAttachFile
	smmDatastore.PrepareAttachFile = PrepareAttachFile
	smmDatastore.GetApplicationConfig = GetApplicationConfig
	smmDatastore.GetApplicationConfigString = GetApplicationConfigString
	smmDatastore.FollowingsLatestCourseSearchObject = FollowingsLatestCourseSearchObject
	smmDatastore.RecommendedCourseSearchObject = RecommendedCourseSearchObject
	smmDatastore.SuggestedCourseSearchObject = SuggestedCourseSearchObject
	smmDatastore.CTRPickUpCourseSearchObject = CTRPickUpCourseSearchObject
	smmDatastore.LatestCourseSearchObject = LatestCourseSearchObject
	smmDatastore.BestScoreRateCourseSearchObject = BestScoreRateCourseSearchObject
	smmDatastore.GetMetaByOwnerID = GetMetaByOwnerID
	smmDatastore.UploadCourseRecord = UploadCourseRecord
	smmDatastore.GetCourseRecord = GetCourseRecord
	smmDatastore.GetDeletionReason = GetDeletionReason
	smmDatastore.GetMetasWithCourseRecord = GetMetasWithCourseRecord
	smmDatastore.CheckRateCustomRankingCounter = CheckRateCustomRankingCounter
	SecureEndpoint.RegisterServiceProtocol(smmDatastore)

	commonDataStoreProtocol := datastorecommon.NewCommonProtocol(smmDatastore)
	commonDataStoreProtocol.SetMinIOClient(minioClient)
	commonDataStoreProtocol.S3Bucket = s3Bucket
	commonDataStoreProtocol.SetDataKeyBase("smm")

	commonDataStoreProtocol.GetObjectInfoByDataID = smmdatabase.GetObjectInfoByDataID
	commonDataStoreProtocol.GetObjectInfoByPersistenceTargetWithPassword = smmdatabase.GetObjectInfoByPersistenceTargetWithPassword
	commonDataStoreProtocol.GetObjectInfoByDataIDWithPassword = smmdatabase.GetObjectInfoByDataIDWithPassword
	commonDataStoreProtocol.GetObjectOwnerByDataID = smmdatabase.GetObjectOwnerByDataID
	commonDataStoreProtocol.GetObjectSizeByDataID = smmdatabase.GetObjectSizeByDataID
	commonDataStoreProtocol.UpdateObjectPeriodByDataIDWithPassword = smmdatabase.UpdateObjectPeriodByDataIDWithPassword
	commonDataStoreProtocol.UpdateObjectMetaBinaryByDataIDWithPassword = smmdatabase.UpdateObjectMetaBinaryByDataIDWithPassword
	commonDataStoreProtocol.UpdateObjectDataTypeByDataIDWithPassword = smmdatabase.UpdateObjectDataTypeByDataIDWithPassword
	commonDataStoreProtocol.UpdateObjectUploadCompletedByDataID = smmdatabase.UpdateObjectUploadCompletedByDataID
	commonDataStoreProtocol.InitializeObjectByPreparePostParam = smmdatabase.InitializeObjectByPreparePostParam
	commonDataStoreProtocol.InitializeObjectRatingWithSlot = smmdatabase.InitializeObjectRatingWithSlot
	commonDataStoreProtocol.RateObjectWithPassword = smmdatabase.RateObjectWithPassword
	commonDataStoreProtocol.DeleteObjectByDataID = smmdatabase.DeleteObjectByDataID
}
