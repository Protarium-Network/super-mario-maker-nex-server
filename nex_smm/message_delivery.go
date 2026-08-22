package nex_smm

import (
	nex "github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	message_delivery "github.com/PretendoNetwork/nex-protocols-go/v2/message-delivery"
)

// DeliverMessage's real purpose is unclear even in Pretendo's own
// reference server ("TODO - See what this does") - stubbed the same way
// they left it, just acknowledging success.
func DeliverMessage(err error, packet nex.PacketInterface, callID uint32, oUserMessage types.DataHolder) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		return nil, nex.NewError(nex.ResultCodes.DataStore.Unknown, err.Error())
	}

	rmcResponse := nex.NewRMCSuccess(SecureEndpoint, nil)
	rmcResponse.ProtocolID = message_delivery.ProtocolID
	rmcResponse.MethodID = message_delivery.MethodDeliverMessage
	rmcResponse.CallID = callID

	return rmcResponse, nil
}
