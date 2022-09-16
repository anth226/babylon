package keeper

import (
	"github.com/babylonchain/babylon/crypto/bls12381"
	"github.com/babylonchain/babylon/x/checkpointing/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/pflag"
)

type BlsSigner interface {
	GetAddress() sdk.ValAddress
	SignMsgWithBls(msg []byte) (bls12381.Signature, error)
	GetBlsPubkey() (bls12381.PublicKey, error)
}

// SendBlsSig prepares a BLS signature message and sends it to Tendermint
func (k Keeper) SendBlsSig(ctx sdk.Context, epochNum uint64, lch types.LastCommitHash) error {
	// get self address
	curValSet := k.GetValidatorSet(ctx, epochNum)
	addr := k.blsSigner.GetAddress()

	// check if itself is the validator
	_, _, err := curValSet.FindValidatorWithIndex(addr)
	if err != nil {
		// only send the BLS sig when the node itself is a validator, not being a validator is not an error
		return nil
	}

	// get BLS signature by signing
	signBytes := append(sdk.Uint64ToBigEndian(epochNum), lch...)
	blsSig, err := k.blsSigner.SignMsgWithBls(signBytes)
	if err != nil {
		return err
	}

	// create MsgAddBlsSig message
	msg := types.NewMsgAddBlsSig(epochNum, lch, blsSig, addr)

	// insert the message into the transaction
	fs := pflag.NewFlagSet("", pflag.ContinueOnError)
	// TODO: hardcoded for now, will set fees as a parameter for the checkpointing module
	fs.String(flags.FlagFees, "", "Fees to pay along with transaction; eg: 10ubbn")
	err = fs.Set(flags.FlagFees, "100stake")
	//err = fs.Set(flags.FlagGasPrices, "1stake")
	err = tx.GenerateOrBroadcastTxCLI(k.clientCtx, fs, msg)
	if err != nil {
		return err
	}

	return nil
}
