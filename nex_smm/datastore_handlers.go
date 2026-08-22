package nex_smm

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/Protarium-Network/super-mario-maker-nex-server/globals"
	"github.com/Protarium-Network/super-mario-maker-nex-server/smmdatabase"

	nex "github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	datastore_smm "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/super-mario-maker"
	datastore_smm_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/super-mario-maker/types"
	datastore_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/types"
)

func s3Bucket() string {
	bucket := os.Getenv("PN_S3_BUCKET")
	if bucket == "" {
		bucket = "smm-datastore"
	}
	return bucket
}

// GetObjectInfos returns presigned download URLs for a batch of course/
// object DataIDs - used across Course World, 100 Mario, and course preview
// image loading.
func GetObjectInfos(err error, packet nex.PacketInterface, callID uint32, dataIDs types.List[types.UInt64]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	pInfos := types.NewList[datastore_smm_types.DataStoreFileServerObjectInfo]()

	for i := range dataIDs {
		objectInfo, nexError := smmdatabase.GetObjectInfoByDataID(dataIDs[i])
		if nexError != nil {
			return nil, nexError
		}

		key := fmt.Sprintf("smm/%d.bin", objectInfo.DataID)

		URL, err := presigner.GetObject(s3Bucket(), key, time.Minute*15)
		if err != nil {
			return nil, nex.NewError(nex.ResultCodes.DataStore.OperationNotAllowed, "Operation not allowed")
		}

		info := datastore_smm_types.NewDataStoreFileServerObjectInfo()
		info.DataID = objectInfo.DataID
		info.GetInfo.URL = types.NewString(URL.String())
		info.GetInfo.Size = objectInfo.Size
		info.GetInfo.DataID = objectInfo.DataID

		pInfos = append(pInfos, info)
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pInfos.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetObjectInfos
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// RateCustomRanking adds to a course's star/ranking counters - the
// underlying period check the real server does isn't understood, so (like
// Pretendo's own reference server) it's skipped here.
func RateCustomRanking(err error, packet nex.PacketInterface, callID uint32, params types.List[datastore_smm_types.DataStoreRateCustomRankingParam]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	for i := range params {
		smmdatabase.InsertOrUpdateCustomRanking(params[i].DataID, params[i].ApplicationID, params[i].Score)
	}

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, nil)
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodRateCustomRanking
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

func GetCustomRankingByDataID(err error, packet nex.PacketInterface, callID uint32, param datastore_smm_types.DataStoreGetCustomRankingByDataIDParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	pRankingResult := smmdatabase.GetCustomRankingsByDataIDs(param.ApplicationID, param.DataIDList)
	pResults := make(types.List[types.QResult], 0, len(param.DataIDList))

	for i := range pRankingResult {
		if param.ResultOption&0x1 == 0 {
			pRankingResult[i].MetaInfo.Tags = types.NewList[types.String]()
		}
		if param.ResultOption&0x2 == 0 {
			pRankingResult[i].MetaInfo.Ratings = types.NewList[datastore_types.DataStoreRatingInfoWithSlot]()
		}
		if param.ResultOption&0x4 == 0 {
			pRankingResult[i].MetaInfo.MetaBinary = types.NewQBuffer(nil)
		}
		if param.ResultOption&0x20 == 0 {
			pRankingResult[i].Score = 0
		}

		pResults = append(pResults, types.NewQResultSuccess(nex.ResultCodes.Core.Unknown))
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pRankingResult.WriteTo(rmcResponseStream)
	pResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetCustomRankingByDataID
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// AddToBufferQueues writes small per-object binary blobs - most notably a
// maker's "Starred Courses" list, where slot 0 of their own maker object
// gets one buffer per starred course DataID.
func AddToBufferQueues(err error, packet nex.PacketInterface, callID uint32, params types.List[datastore_smm_types.BufferQueueParam], buffers types.List[types.QBuffer]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	client := packet.Sender()
	pResults := types.NewList[types.QResult]()

	iterations := len(params)
	if len(buffers) < iterations {
		iterations = len(buffers)
	}

	for i := 0; i < iterations; i++ {
		param := params[i]
		buffer := buffers[i]

		globals.Logger.Infof("[SMM DEBUG] AddToBufferQueues client_pid=%d param.DataID=%d param.Slot=%d buffer_len=%d", client.PID(), param.DataID, param.Slot, len([]byte(buffer)))

		if param.Slot == 0 {
			objectInfo, nexError := smmdatabase.GetObjectInfoByDataID(param.DataID)
			if nexError != nil {
				globals.Logger.Infof("[SMM DEBUG] AddToBufferQueues GetObjectInfoByDataID(%d) failed: %s", param.DataID, nexError.Error())
				return nil, nexError
			}

			// DataType 1 objects are "maker" objects - only their owner
			// can write to slot 0 (their Starred Courses list), otherwise
			// anyone could star courses onto random players' lists.
			if objectInfo.DataType == 1 && objectInfo.OwnerID != client.PID() {
				return nil, nex.NewError(nex.ResultCodes.DataStore.PermissionDenied, "Permission denied")
			}
		}

		nexError := smmdatabase.InsertOrUpdateBufferQueueData(param.DataID, param.Slot, buffer)
		if nexError != nil {
			globals.Logger.Infof("[SMM DEBUG] AddToBufferQueues InsertOrUpdateBufferQueueData(%d,%d) failed: %s", param.DataID, param.Slot, nexError.Error())
			return nil, nexError
		}

		pResults = append(pResults, types.NewQResultSuccess(nex.ResultCodes.Core.Unknown))
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodAddToBufferQueues
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

func GetBufferQueue(err error, packet nex.PacketInterface, callID uint32, param datastore_smm_types.BufferQueueParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	pBufferQueue, nexError := smmdatabase.GetBufferQueuesByDataIDAndSlot(param.DataID, param.Slot)
	if nexError != nil {
		return nil, nexError
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pBufferQueue.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetBufferQueue
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// PrepareAttachFile hands out a presigned upload URL for attaching a file
// (in practice, a course's preview thumbnail) to an already-uploaded
// course object.
func PrepareAttachFile(err error, packet nex.PacketInterface, callID uint32, param datastore_smm_types.DataStoreAttachFileParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	dataID, nexError := smmdatabase.InitializeObjectByAttachFileParam(packet.Sender().PID(), param)
	if nexError != nil {
		return nil, nexError
	}

	for i := range param.PostParam.RatingInitParams {
		if nexError := smmdatabase.InitializeObjectRatingWithSlot(uint64(dataID), param.PostParam.RatingInitParams[i]); nexError != nil {
			return nil, nexError
		}
	}

	key := fmt.Sprintf("smm/%d.jpg", dataID)
	URL, formData, err := presigner.PostObject(s3Bucket(), key, time.Minute*15)
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.OperationNotAllowed, "Operation not allowed")
	}

	pReqPostInfo := datastore_types.NewDataStoreReqPostInfo()
	pReqPostInfo.DataID = dataID
	pReqPostInfo.URL = types.NewString(URL.String())
	pReqPostInfo.FormFields = make(types.List[datastore_types.DataStoreKeyValue], 0, len(formData))

	for key, value := range formData {
		field := datastore_types.NewDataStoreKeyValue()
		field.Key = types.NewString(key)
		field.Value = types.NewString(value)
		pReqPostInfo.FormFields = append(pReqPostInfo.FormFields, field)
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pReqPostInfo.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodPrepareAttachFile
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

func CompleteAttachFile(err error, packet nex.PacketInterface, callID uint32, param datastore_types.DataStoreCompletePostParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	if !param.IsSuccess {
		return nil, nex.NewError(nex.ResultCodes.DataStore.InvalidArgument, "Invalid argument")
	}

	nexError := smmdatabase.UpdateObjectUploadCompletedByDataID(param.DataID, true)
	if nexError != nil {
		return nil, nexError
	}

	key := fmt.Sprintf("smm/%d.jpg", param.DataID)
	pURL, err := presigner.GetObject(s3Bucket(), key, time.Minute*15)
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.OperationNotAllowed, "Operation not allowed")
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	types.NewString(pURL.String()).WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodCompleteAttachFile
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// Kept low - even after stripping Tags/Ratings/MetaBinary, nex-go/v2's
// PRUDP fragment size is only 1300 bytes (see PRUDPServer.SetFragmentSize),
// and GetCustomRanking hit Core::BufferOverflow (106-0116) at 25 results
// even fully stripped, suggesting a fairly low ceiling on total fragmented
// message size too.
const maxSearchResults = 8

// FollowingsLatestCourseSearchObject returns courses uploaded by a
// specific set of owners (followed makers).
func FollowingsLatestCourseSearchObject(err error, packet nex.PacketInterface, callID uint32, param datastore_types.DataStoreSearchParam, extraData types.List[types.String]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	pRankingResults := types.NewList[datastore_smm_types.DataStoreCustomRankingResult]()

	for i := range param.OwnerIDs {
		courseObjectIDs, nexError := smmdatabase.GetUserCourseObjectIDs(param.OwnerIDs[i])
		if nexError != nil {
			return nil, nexError
		}

		results := smmdatabase.GetCustomRankingsByDataIDs(types.NewUInt32(0), courseObjectIDs)

		for j := range results {
			if param.ResultOption&0x1 == 0 {
				results[j].MetaInfo.Tags = types.NewList[types.String]()
			}
			if param.ResultOption&0x2 == 0 {
				results[j].MetaInfo.Ratings = types.NewList[datastore_types.DataStoreRatingInfoWithSlot]()
			}
			if param.ResultOption&0x4 == 0 {
				results[j].MetaInfo.MetaBinary = types.NewQBuffer(nil)
			}
			if param.ResultOption&0x20 == 0 {
				results[j].Score = 0
			}

			pRankingResults = append(pRankingResults, results[j])
		}
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pRankingResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodFollowingsLatestCourseSearchObject
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// RecommendedCourseSearchObject backs Course World's browsing tabs. No
// difficulty/success-rate filtering is applied (that data isn't tracked) -
// every request is treated as "show me some published courses", matching
// what Pretendo's own reference server does today.
func RecommendedCourseSearchObject(err error, packet nex.PacketInterface, callID uint32, param datastore_types.DataStoreSearchParam, extraData types.List[types.String]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	// The Wii U requests a candidate pool for 100 Mario, then selects the
	// actual 8/16/6-course run client-side. This method therefore needs a
	// larger ceiling than ordinary Course World pages. A zero length is the
	// client's "server default" value, not a request for an empty result.
	const maxRecommendedResults = 25
	requestedLength := int(param.ResultRange.Length)
	length := requestedLength
	if length <= 0 || length > maxRecommendedResults {
		length = maxRecommendedResults
	}

	globals.Logger.Infof("[SMM Recommended] pid=%d requested=%d offset=%d effective=%d filters=%v",
		packet.Sender().PID(), requestedLength, param.ResultRange.Offset, length, extraData)

	pRankingResults, nexError := smmdatabase.GetRandomCoursesWithLimit(length)
	if nexError != nil {
		globals.Logger.Errorf("[SMM Recommended] query failed: %s", nexError.Error())
		return nil, nexError
	}
	globals.Logger.Infof("[SMM Recommended] returning=%d", len(pRankingResults))

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pRankingResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodRecommendedCourseSearchObject
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// SuggestedCourseSearchObject backs the scrolling course suggestions shown
// after clearing a course.
func SuggestedCourseSearchObject(err error, packet nex.PacketInterface, callID uint32, param datastore_types.DataStoreSearchParam, extraData types.List[types.String]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	if len(extraData) == 0 {
		return nil, nex.NewError(nex.ResultCodes.DataStore.InvalidArgument, "Invalid argument")
	}

	if _, err := strconv.ParseUint(string(extraData[0]), 0, 64); err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.InvalidArgument, "Invalid argument")
	}

	pRankingResults, nexError := smmdatabase.GetRandomCoursesWithLimit(int(param.ResultRange.Length))
	if nexError != nil {
		return nil, nexError
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pRankingResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodSuggestedCourseSearchObject
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// CTRPickUpCourseSearchObject is the 3DS version's equivalent of
// RecommendedCourseSearchObject.
func CTRPickUpCourseSearchObject(err error, packet nex.PacketInterface, callID uint32, param datastore_types.DataStoreSearchParam, extraData types.List[types.String]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	pRankingResults, nexError := smmdatabase.GetRandomCoursesWithLimit(int(param.ResultRange.Length))
	if nexError != nil {
		return nil, nexError
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pRankingResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodCTRPickUpCourseSearchObject
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

func UploadCourseRecord(err error, packet nex.PacketInterface, callID uint32, param datastore_smm_types.DataStoreUploadCourseRecordParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	globals.Logger.Infof("[SMM RECORD] upload data_id=%d slot=%d pid=%d incoming_score=%d",
		param.DataID, param.Slot, packet.Sender().PID(), param.Score)

	nexError := smmdatabase.InsertOrUpdateCourseRecord(param.DataID, param.Slot, packet.Sender().PID(), param.Score)
	if nexError != nil {
		globals.Logger.Errorf("[SMM RECORD] update failed data_id=%d slot=%d: %s",
			param.DataID, param.Slot, nexError.Error())
		return nil, nexError
	}

	stored, storedError := smmdatabase.GetCourseRecordByDataIDAndSlot(param.DataID, param.Slot)
	if storedError != nil {
		globals.Logger.Errorf("[SMM RECORD] readback failed data_id=%d slot=%d: %s",
			param.DataID, param.Slot, storedError.Error())
	} else {
		globals.Logger.Infof("[SMM RECORD] stored data_id=%d slot=%d best_pid=%d best_score=%d",
			stored.DataID, stored.Slot, stored.BestPID, stored.BestScore)
	}

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, nil)
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodUploadCourseRecord
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

func GetCourseRecord(err error, packet nex.PacketInterface, callID uint32, param datastore_smm_types.DataStoreGetCourseRecordParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	result, nexError := smmdatabase.GetCourseRecordByDataIDAndSlot(param.DataID, param.Slot)
	if nexError != nil {
		return nil, nexError
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	result.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetCourseRecord
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// GetDeletionReason is stubbed the same way Pretendo's own reference
// server leaves it - the real deletion-reason value scheme isn't known, and
// every course checked has come back 0 regardless of moderation state.
func GetDeletionReason(err error, packet nex.PacketInterface, callID uint32, dataIDLst types.List[types.UInt64]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	pDeletionReasons := types.NewList[types.UInt32]()
	for range dataIDLst {
		pDeletionReasons = append(pDeletionReasons, types.NewUInt32(0))
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pDeletionReasons.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetDeletionReason
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

func GetMetasWithCourseRecord(err error, packet nex.PacketInterface, callID uint32, params types.List[datastore_smm_types.DataStoreGetCourseRecordParam], metaParam datastore_types.DataStoreGetMetaParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	pMetaInfo := make(types.List[datastore_types.DataStoreMetaInfo], 0, len(params))
	pCourseResults := make(types.List[datastore_smm_types.DataStoreGetCourseRecordResult], 0, len(params))
	pResults := make(types.List[types.QResult], 0, len(params))

	for i := range params {
		objectInfo, nexError := smmdatabase.GetObjectInfoByDataID(params[i].DataID)
		if nexError != nil {
			objectInfo = datastore_types.NewDataStoreMetaInfo()
		} else {
			if metaParam.ResultOption&0x1 == 0 {
				objectInfo.Tags = types.NewList[types.String]()
			}
			if metaParam.ResultOption&0x2 == 0 {
				objectInfo.Ratings = types.NewList[datastore_types.DataStoreRatingInfoWithSlot]()
			}
			if metaParam.ResultOption&0x4 == 0 {
				objectInfo.MetaBinary = types.NewQBuffer(nil)
			}
		}

		courseRecord, nexError := smmdatabase.GetCourseRecordByDataIDAndSlot(params[i].DataID, params[i].Slot)
		if nexError != nil || objectInfo.DataID == 0 {
			courseRecord = datastore_smm_types.NewDataStoreGetCourseRecordResult()
		}

		pMetaInfo = append(pMetaInfo, objectInfo)
		pCourseResults = append(pCourseResults, courseRecord)
		pResults = append(pResults, types.NewQResultSuccess(nex.ResultCodes.Core.Unknown))
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pMetaInfo.WriteTo(rmcResponseStream)
	pCourseResults.WriteTo(rmcResponseStream)
	pResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetMetasWithCourseRecord
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// CheckRateCustomRankingCounter's real purpose is unclear (only ever seen
// called with applicationID 0, always returning true on the real server) -
// stubbed the same way.
func CheckRateCustomRankingCounter(err error, packet nex.PacketInterface, callID uint32, applicationID types.UInt32) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	isBelowThreshold := types.NewBool(true)

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	isBelowThreshold.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodCheckRateCustomRankingCounter
	rmcResponse.CallID = callID

	return rmcResponse, nil
}
