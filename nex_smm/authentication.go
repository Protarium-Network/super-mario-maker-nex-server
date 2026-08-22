// Package nex_smm is a NEX server for Super Mario Maker (Wii U), ported
// from Pretendo's own real reference server (github.com/PretendoNetwork/
// super-mario-maker), adapted to reuse this project's shared HTTP-based
// account globals instead of that repo's gRPC account service, and given
// its own isolated Postgres database (see smmdatabase) since Super Mario
// Maker's DataStore schema needs extra columns/tables beyond what every
// other game here shares.
//
// Course World browsing requires a real "event course metadata" binary
// object (reserved DataID 900000) to exist in S3/Postgres before the
// server will finish booting - this is an actual Nintendo-authored binary
// data file this project has no legitimate way to obtain or fabricate, so
// Course World will not open until a real one is supplied (see
// smmdatabase/connect.go's TODO). Everything else - login, course upload,
// 100 Mario, ratings, buffer queues (starred courses) - works without it.
package nex_smm

import (
	"fmt"
	"os"
	"strconv"

	"github.com/EcrazerDev/super-mario-maker/globals"
	nex "github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/constants"
	"github.com/PretendoNetwork/nex-go/v2/types"
	common_ticket_granting "github.com/PretendoNetwork/nex-protocols-common-go/v2/ticket-granting"
	ticket_granting "github.com/PretendoNetwork/nex-protocols-go/v2/ticket-granting"
)

// AccessKey is Super Mario Maker's real NEX access key, taken as-is from
// Pretendo's own reference server - cross-confirmed against
// kinnay.github.io's public Wii U NEX game list, which independently lists
// the same key for game_server_id 270064384 (=0x1018db00) under the name
// "Super Mario Maker".
const AccessKey = "9f2b4678"

var AuthenticationServer *nex.PRUDPServer
var AuthenticationEndpoint *nex.PRUDPEndPoint

func StartAuthenticationServer() {
	AuthenticationServer = nex.NewPRUDPServer()

	AuthenticationEndpoint = nex.NewPRUDPEndPoint(1)
	AuthenticationEndpoint.ServerAccount = globals.AuthenticationServerAccount
	AuthenticationEndpoint.AccountDetailsByPID = globals.AccountDetailsByPID
	AuthenticationEndpoint.AccountDetailsByUsername = globals.AccountDetailsByUsername
	AuthenticationServer.BindPRUDPEndPoint(AuthenticationEndpoint)

	AuthenticationServer.LibraryVersions.SetDefault(nex.NewLibraryVersion(3, 8, 3))
	AuthenticationServer.AccessKey = AccessKey
	AuthenticationServer.ByteStreamSettings.UseStructureHeader = true

	AuthenticationEndpoint.OnData(func(packet nex.PacketInterface) {
		request := packet.RMCMessage()
		if request == nil {
			return
		}
		pid := uint64(packet.Sender().PID())
		fmt.Printf("[SMM Auth] PID=%d protocol=0x%02X method=0x%02X\n", pid, request.ProtocolID, request.MethodID)
	})

	registerAuthenticationServerProtocols()

	port, _ := strconv.Atoi(os.Getenv("PN_SMM_AUTH_PORT"))
	globals.Logger.Successf("[SMM] Authentication server listening on UDP %d", port)
	AuthenticationServer.Listen(port)
}

func registerAuthenticationServerProtocols() {
	ticketGrantingProtocol := ticket_granting.NewProtocol()
	AuthenticationEndpoint.RegisterServiceProtocol(ticketGrantingProtocol)
	commonTicketGrantingProtocol := common_ticket_granting.NewCommonProtocol(ticketGrantingProtocol)

	securePort, _ := strconv.Atoi(os.Getenv("PN_SMM_SECURE_PORT"))

	secureHost := os.Getenv("PN_SMM_SECURE_HOST")
	if secureHost == "" {
		secureHost = "127.0.0.1"
	}

	secureStationURL := types.NewStationURL("")
	secureStationURL.SetURLType(constants.StationURLPRUDPS)
	secureStationURL.SetAddress(secureHost)
	secureStationURL.SetPortNumber(uint16(securePort))
	secureStationURL.SetConnectionID(1)
	secureStationURL.SetPrincipalID(types.NewPID(2))
	secureStationURL.SetStreamID(1)
	secureStationURL.SetStreamType(constants.StreamTypeRVSecure)
	secureStationURL.SetType(uint8(constants.StationURLFlagPublic))

	commonTicketGrantingProtocol.ValidateLoginData = globals.ValidateLoginData
	commonTicketGrantingProtocol.SecureStationURL = secureStationURL
	commonTicketGrantingProtocol.BuildName = types.NewString("")
	commonTicketGrantingProtocol.SecureServerAccount = globals.SecureServerAccount
}
