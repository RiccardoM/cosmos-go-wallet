package wallet

import (
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
)

type Client interface {
	GetTxConfig() sdkclient.TxConfig

	GetAccountPrefix() string
	GetChainID() (string, error)
	GetAccount(address string) (sdk.AccountI, error)
	GetFees(gas int64) sdk.Coins

	SimulateTx(tx signing.Tx) (uint64, error)
	BroadcastTxAsync(tx signing.Tx) (*sdk.TxResponse, error)
	BroadcastTxSync(tx signing.Tx) (*sdk.TxResponse, error)
	BroadcastTxCommit(tx signing.Tx) (*sdk.TxResponse, error)
}
