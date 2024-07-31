package wallet

import (
	"context"
	"fmt"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/riccardom/cosmos-go-wallet/types"
)

// Wallet represents a Cosmos wallet that should be used to create and send transactions to the chain
type Wallet struct {
	privKey cryptotypes.PrivKey
	client  Client
}

// NewWallet allows to build a new Wallet instance
func NewWallet(accountCfg *types.AccountConfig, client Client) (*Wallet, error) {
	// Get the private types
	algo := hd.Secp256k1
	derivedPriv, err := algo.Derive()(accountCfg.Mnemonic, "", accountCfg.HDPath)
	if err != nil {
		return nil, err
	}

	return &Wallet{
		privKey: algo.Generate()(derivedPriv),
		client:  client,
	}, nil
}

// AccAddress returns the address of the account that is going to be used to sign the transactions
func (w *Wallet) AccAddress() string {
	bech32Addr, err := bech32.ConvertAndEncode(w.client.GetAccountPrefix(), w.privKey.PubKey().Address())
	if err != nil {
		panic(err)
	}
	return bech32Addr
}

// getTransactionResponse builds a transactions from the provided data and broadcasts it using the provided method
func (w *Wallet) getTransactionResponse(data *types.TransactionData, broadcast types.TxBroadcastMethod) (*types.TransactionResponse, error) {
	account, builder, err := w.BuildTx(data)
	if err != nil {
		return nil, err
	}

	// Build the transaction
	builtTx := builder.GetTx()

	// Broadcast the transaction
	txResponse, err := broadcast(builtTx)
	if err != nil {
		return nil, fmt.Errorf("error while broadcasting the transaction: %s", err)
	}

	// Return the transaction response
	return &types.TransactionResponse{
		TxResponse: txResponse,
		Account:    account,
		Tx:         builtTx,
	}, nil
}

// BroadcastTxAsync creates and signs a transaction with the provided messages and fees,
// then broadcasts it using the async method
func (w *Wallet) BroadcastTxAsync(data *types.TransactionData) (*types.TransactionResponse, error) {
	return w.getTransactionResponse(data, w.client.BroadcastTxAsync)
}

// BroadcastTxSync creates and signs a transaction with the provided messages and fees,
// then broadcasts it using the sync method
func (w *Wallet) BroadcastTxSync(data *types.TransactionData) (*types.TransactionResponse, error) {
	return w.getTransactionResponse(data, w.client.BroadcastTxSync)
}

// BroadcastTxCommit creates and signs a transaction with the provided messages and fees,
// then broadcasts it using the commit method
func (w *Wallet) BroadcastTxCommit(data *types.TransactionData) (*types.TransactionResponse, error) {
	return w.getTransactionResponse(data, w.client.BroadcastTxCommit)
}

// BuildTx creates a transaction with the provided data
func (w *Wallet) BuildTx(data *types.TransactionData) (sdk.AccountI, sdkclient.TxBuilder, error) {
	// Get the account
	account, err := w.client.GetAccount(w.AccAddress())
	if err != nil {
		return nil, nil, fmt.Errorf("error while getting the account from the chain: %s", err)
	}

	// Set account sequence
	if data.Sequence != nil && *data.Sequence > 0 {
		err = account.SetSequence(*data.Sequence)
		if err != nil {
			return nil, nil, fmt.Errorf("error while setting the account sequence: %s", err)
		}
	}

	// Build the transaction
	builder := w.client.GetTxConfig().NewTxBuilder()
	if data.Memo != "" {
		builder.SetMemo(data.Memo)
	}
	if data.FeeGranter != nil {
		builder.SetFeeGranter(data.FeeGranter)
	}

	if len(data.Messages) == 0 {
		return nil, nil, fmt.Errorf("error while building a transaction with no messages")
	}

	err = builder.SetMsgs(data.Messages...)
	if err != nil {
		return nil, nil, err
	}

	gasLimit := data.GasLimit
	if data.GasAuto {
		adjusted, err := w.simulateTx(account, builder)
		if err != nil {
			return nil, nil, err
		}
		gasLimit = adjusted
	}

	feeAmount := data.FeeAmount
	if data.FeeAuto {
		// Compute the fee amount based on the gas limit and the gas price
		feeAmount = w.client.GetFees(int64(gasLimit))
	}

	// Set the new gas and fee
	builder.SetGasLimit(gasLimit)
	builder.SetFeeAmount(feeAmount)

	// Set an empty signature first
	sigData := signing.SingleSignatureData{
		SignMode: signing.SignMode_SIGN_MODE_DIRECT,
	}
	sig := signing.SignatureV2{
		PubKey:   w.privKey.PubKey(),
		Data:     &sigData,
		Sequence: account.GetSequence(),
	}

	err = builder.SetSignatures(sig)
	if err != nil {
		return nil, nil, err
	}

	chainID, err := w.client.GetChainID()
	if err != nil {
		return nil, nil, err
	}

	// Sign the transaction with the private key
	sig, err = tx.SignWithPrivKey(
		// Since we are only signing using the DIRECT method, the context
		// here is not important as it's only used for TEXT signing
		context.Background(),

		signing.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{
			ChainID:       chainID,
			AccountNumber: account.GetAccountNumber(),
			Sequence:      account.GetSequence(),
		},
		builder,
		w.privKey,
		w.client.GetTxConfig(),
		account.GetSequence(),
	)
	if err != nil {
		return nil, nil, err
	}

	err = builder.SetSignatures(sig)
	if err != nil {
		return nil, nil, err
	}

	return account, builder, nil
}

// simulateTx simulates the given transaction and returns the amount of adjusted gas that should be used
func (w *Wallet) simulateTx(account sdk.AccountI, builder sdkclient.TxBuilder) (uint64, error) {
	// Create an empty signature literal as the ante handler will populate with a
	// sentinel pubkey.
	sig := signing.SignatureV2{
		PubKey: &secp256k1.PubKey{},
		Data: &signing.SingleSignatureData{
			SignMode: signing.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: account.GetSequence(),
	}
	err := builder.SetSignatures(sig)
	if err != nil {
		return 0, err
	}

	// Set a fake amount of gas and fees
	builder.SetGasLimit(200_000)
	builder.SetFeeAmount(w.client.GetFees(int64(200_000)))

	// Simulate the execution of the transaction
	adjusted, err := w.client.SimulateTx(builder.GetTx())
	if err != nil {
		return 0, fmt.Errorf("error while simulating tx: %s", err)
	}
	return adjusted, nil
}
