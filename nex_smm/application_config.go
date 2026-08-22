package nex_smm

import (
	"github.com/Protarium-Network/super-mario-maker-nex-server/smmdatabase"

	nex "github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	datastore_smm "github.com/PretendoNetwork/nex-protocols-go/v2/datastore/super-mario-maker"
)

// MaxCourseUploads is the number of course upload slots every player gets.
// Nintendo's real server starts players at 10 and grants more up to 100 as
// they play - since there's no progression system here, everyone just
// starts at the max.
var MaxCourseUploads uint32 = 100

// applicationConfig returns the Wii U feature/configuration tables expected
// by Super Mario Maker v272. In particular, application 0 is not a simple
// key/value pair: the client indexes the full positional table to decide
// whether 100 Mario and Super Expert are enabled. Returning only
// [1, MaxCourseUploads] makes Course World work but deliberately selects the
// game's local "This service will be available soon" screen before it ever
// calls RecommendedCourseSearchObject.
func applicationConfig(applicationID uint32) ([]uint32, bool) {
	switch applicationID {
	case 0:
		return []uint32{
			0x1, 0x32, 0x96, 0x12C, 0x1F4, 0x320, 0x514, 0x7D0,
			0xBB8, 0x1388, 0xA, 0x14, 0x1E, 0x28, 0x32, 0x3C,
			0x46, 0x50, 0x5A, 0x64, 0x23, 0x4B, 0x23, 0x4B,
			0x32, 0x0, 0x3, 0x3, MaxCourseUploads, 0x6, 0x1, 0x60,
			0x5, 0x60, 0x1, 0x7E4, 0x1, 0x1, 0xC, 0x0,
		}, true
	case 2:
		return []uint32{0x7DF, 0xC, 0x16, 0x5, 0x0}, true
	default:
		return nil, false
	}
}

// GetApplicationConfig answers the numeric feature/configuration tables the
// client requests while entering Course World.
func GetApplicationConfig(err error, packet nex.PacketInterface, callID uint32, applicationID types.UInt32) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	config, known := applicationConfig(uint32(applicationID))
	if !known && applicationID == 1 { // "Official" makers shown in the MAKERS section - driven by the
		// admin panel's verified-maker list (see smmdatabase/admin.go)
		config = []uint32{2} // Not a real user PID - the internal Quazal Rendez-Vous account
		if pids, dbErr := smmdatabase.ListVerifiedMakers(); dbErr == nil {
			for _, pid := range pids {
				config = append(config, uint32(pid))
			}
		}
	} else if !known {
		return nil, nex.NewError(nex.ResultCodes.DataStore.InvalidArgument, "Unknown application config")
	}

	configNative := make(types.List[types.UInt32], 0, len(config))
	for i := range config {
		configNative = append(configNative, types.NewUInt32(config[i]))
	}

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	configNative.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetApplicationConfig
	rmcResponse.CallID = callID

	return rmcResponse, nil
}

// GetApplicationConfigString answers per-application string list configs -
// on the real server these are course-name word blacklists. Left empty
// here (no filtering) rather than reproducing Nintendo's own curated list,
// which isn't this project's content to redistribute.
func GetApplicationConfigString(err error, packet nex.PacketInterface, callID uint32, applicationID types.UInt32) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	configNative := types.NewList[types.String]()

	rmcResponseStream := nex.NewByteStreamOut(SecureServer.LibraryVersions, SecureServer.ByteStreamSettings)
	configNative.WriteTo(rmcResponseStream)

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, rmcResponseStream.Bytes())
	rmcResponse.ProtocolID = datastore_smm.ProtocolID
	rmcResponse.MethodID = datastore_smm.MethodGetApplicationConfigString
	rmcResponse.CallID = callID

	return rmcResponse, nil
}
