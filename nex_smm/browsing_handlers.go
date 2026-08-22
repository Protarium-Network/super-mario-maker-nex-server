package nex_smm

import (
	"github.com/Protarium-Network/super-mario-maker-nex-server/smmdatabase"

	nex "github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	datastore_smm "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/super-mario-maker"
	datastore_smm_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/super-mario-maker/types"
	datastore_types "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/types"
)

// LatestCourseSearchObject backs the "Latest Courses" browsing tab and is
// also used by 100 Mario Challenge to pull its course pool - left
// unimplemented at first, which surfaces client-side as Core::NotImplemented
// (106-0103), the same class of bug Puyo hit for its own missing
// matchmaking callbacks earlier this project.
func LatestCourseSearchObject(err error, packet nex.PacketInterface, callID uint32, param datastore_types.DataStoreSearchParam, extraData types.List[types.String]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	length := int(param.ResultRange.Length)
	if length <= 0 || length > maxSearchResults {
		length = maxSearchResults
	}

	pRankingResults, nexError := smmdatabase.GetLatestCoursesWithLimit(length)
	if nexError != nil {
		return nil, nexError
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pRankingResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodLatestCourseSearchObject
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// BestScoreRateCourseSearchObject backs a clear-rate-sorted browsing mode.
// Clear/fail counts aren't tracked here, so this falls back to the same
// random pool used by Recommended/Suggested - not a real ranking by
// difficulty, but a working, non-crashing response instead of
// Core::NotImplemented.
func BestScoreRateCourseSearchObject(err error, packet nex.PacketInterface, callID uint32, param datastore_types.DataStoreSearchParam, extraData types.List[types.String]) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	length := int(param.ResultRange.Length)
	if length <= 0 || length > maxSearchResults {
		length = maxSearchResults
	}

	pRankingResults, nexError := smmdatabase.GetRandomCoursesWithLimit(length)
	if nexError != nil {
		return nil, nexError
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pRankingResults.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodBestScoreRateCourseSearchObject
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// GetCustomRanking is a real leaderboard-style browse (sorted by actual
// ranking value, unlike GetRandomCoursesWithLimit's unordered pool) - most
// likely the actual method backing "top creators"/leaderboard browsing,
// which is what surfaced Core::NotImplemented (106-0103) for the creators
// list even after LatestCourseSearchObject/BestScoreRateCourseSearchObject
// were wired.
func GetCustomRanking(err error, packet nex.PacketInterface, callID uint32, param datastore_smm_types.DataStoreGetCustomRankingParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	length := int(param.ResultRange.Length)
	if length <= 0 || length > maxSearchResults {
		length = maxSearchResults
	}

	pRankingResults, nexError := smmdatabase.GetTopRankedCoursesWithLimit(uint32(param.ApplicationID), length)
	if nexError != nil {
		return nil, nexError
	}

	// Strip large fields the client didn't ask for, per the real
	// ResultOption bit flags.
	for i := range pRankingResults {
		if param.ResultOption&0x1 == 0 {
			pRankingResults[i].MetaInfo.Tags = types.NewList[types.String]()
		}
		if param.ResultOption&0x2 == 0 {
			pRankingResults[i].MetaInfo.Ratings = types.NewList[datastore_types.DataStoreRatingInfoWithSlot]()
		}
		if param.ResultOption&0x4 == 0 {
			pRankingResults[i].MetaInfo.MetaBinary = types.NewQBuffer(nil)
		}
		if param.ResultOption&0x20 == 0 {
			pRankingResults[i].Score = 0
		}
	}

	// ROOT CAUSE of the long-standing 106-0116 Core::BufferOverflow: per
	// kinnay's NintendoClients wiki (Data Store Protocol (SMM), method 49 /
	// 0x31), GetCustomRanking's response is actually TWO lists back to
	// back - `List<DataStoreCustomRankingResult> pRankingResult` followed
	// by `List<Result> pResults` (a QResult per entry). Every earlier fix
	// attempt only ever touched the content of the FIRST list, so the
	// response was always missing this trailing second list entirely - the
	// client kept trying to read a list that was never there, which reads
	// as a buffer overflow client-side regardless of what the first list
	// contained. Pretendo's own reference server never implements this
	// method at all, which is why this was never caught until checking an
	// external protocol reference.
	pResults := types.NewList[types.QResult]()
	for range pRankingResults {
		pResults = append(pResults, types.NewQResultSuccess(0))
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	pRankingResults.WriteTo(rmcResponseStream)
	pResults.WriteTo(rmcResponseStream)
	responseBytes := rmcResponseStream.Bytes()

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, responseBytes)
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetCustomRanking
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// GetMetaByOwnerID backs the "creators" / maker-profile screen - given a
// set of maker PIDs, return their published courses.
func GetMetaByOwnerID(err error, packet nex.PacketInterface, callID uint32, param datastore_smm_types.DataStoreGetMetaByOwnerIDParam) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	results := smmdatabase.GetObjectsByOwnerIDs(param.OwnerIDs, param.DataTypes, param.ResultRange)

	for i := range results {
		if param.ResultOption&0x1 == 0 {
			results[i].Tags = types.NewList[types.String]()
		}
		if param.ResultOption&0x2 == 0 {
			results[i].Ratings = types.NewList[datastore_types.DataStoreRatingInfoWithSlot]()
		}
		if param.ResultOption&0x4 == 0 {
			results[i].MetaBinary = types.NewQBuffer(nil)
		}
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	results.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetMetaByOwnerID
	rmcResponse.CallID = callID

	return rmcResponse, nil
}
