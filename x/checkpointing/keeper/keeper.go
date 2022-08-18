package keeper

import (
	"errors"
	"fmt"
	"github.com/babylonchain/babylon/crypto/bls12381"
	"github.com/babylonchain/babylon/x/checkpointing/types"
	epochingtypes "github.com/babylonchain/babylon/x/epoching/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/tendermint/tendermint/libs/log"
)

type (
	Keeper struct {
		cdc            codec.BinaryCodec
		storeKey       sdk.StoreKey
		memKey         sdk.StoreKey
		epochingKeeper types.EpochingKeeper
		hooks          types.CheckpointingHooks
		paramstore     paramtypes.Subspace
	}
)

func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey,
	memKey sdk.StoreKey,
	ek types.EpochingKeeper,
	ps paramtypes.Subspace,
) Keeper {
	// set KeyTable if it has not already been set
	if !ps.HasKeyTable() {
		ps = ps.WithKeyTable(types.ParamKeyTable())
	}

	return Keeper{
		cdc:            cdc,
		storeKey:       storeKey,
		memKey:         memKey,
		epochingKeeper: ek,
		paramstore:     ps,
		hooks:          nil,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// SetHooks sets the validator hooks
func (k *Keeper) SetHooks(sh types.CheckpointingHooks) *Keeper {
	if k.hooks != nil {
		panic("cannot set validator hooks twice")
	}

	k.hooks = sh

	return k
}

// addBlsSig adds a BLS signature to the raw checkpoint and updates the status
// if sufficient signatures are accumulated for the epoch.
func (k Keeper) addBlsSig(ctx sdk.Context, sig *types.BlsSig) error {
	// assuming stateless checks have done in Antehandler

	// get raw checkpoint
	ckptWithMeta, err := k.GetRawCheckpoint(ctx, sig.GetEpochNum())
	if err != nil {
		return err
	}

	// the checkpoint is not accumulating
	if ckptWithMeta.Status != types.Accumulating {
		return nil
	}

	// get signer's address
	signerAddr, err := sdk.ValAddressFromBech32(sig.SignerAddress)
	if err != nil {
		return err
	}

	// get validators for the epoch
	vals := k.GetValidatorSet(ctx, sig.GetEpochNum())
	signerBlsKey, err := k.GetBlsPubKey(ctx, signerAddr)
	if err != nil {
		return err
	}

	// accumulate BLS signatures
	updated, err := ckptWithMeta.Accumulate(
		vals, signerAddr, signerBlsKey, *sig.BlsSig, k.GetTotalVotingPower(ctx, sig.GetEpochNum()))
	if err != nil {
		return err
	}

	if updated {
		err = k.UpdateCheckpoint(ctx, ckptWithMeta)
	}
	if err != nil {
		return err
	}

	return nil
}

func (k Keeper) GetRawCheckpoint(ctx sdk.Context, epochNum uint64) (*types.RawCheckpointWithMeta, error) {
	return k.CheckpointsState(ctx).GetRawCkptWithMeta(epochNum)
}

func (k Keeper) GetStatus(ctx sdk.Context, epochNum uint64) (types.CheckpointStatus, error) {
	ckptWithMeta, err := k.GetRawCheckpoint(ctx, epochNum)
	if err != nil {
		return -1, err
	}
	return ckptWithMeta.Status, nil
}

// AddRawCheckpoint adds a raw checkpoint into the storage
func (k Keeper) AddRawCheckpoint(ctx sdk.Context, ckptWithMeta *types.RawCheckpointWithMeta) error {
	return k.CheckpointsState(ctx).CreateRawCkptWithMeta(ckptWithMeta)
}

func (k Keeper) BuildRawCheckpoint(ctx sdk.Context, epochNum uint64, lch types.LastCommitHash) error {
	ckptWithMeta := types.NewCheckpointWithMeta(types.NewCheckpoint(epochNum, lch), types.Accumulating)

	return k.AddRawCheckpoint(ctx, ckptWithMeta)
}

// CheckpointEpoch verifies checkpoint from BTC and returns epoch number if
// it equals to the existing raw checkpoint. Otherwise, it further verifies
// the raw checkpoint and decides whether it is an invalid checkpoint or a
// conflicting checkpoint. A conflicting checkpoint indicates the existence
// of a fork
func (k Keeper) CheckpointEpoch(ctx sdk.Context, rawCkptBytes []byte) (uint64, error) {
	ckptWithMeta, err := k.verifyCkptBytes(ctx, rawCkptBytes)
	if err != nil {
		return 0, err
	}
	return ckptWithMeta.Ckpt.EpochNum, nil
}

// verifyCkptBytes verifies checkpoint from BTC. A checkpoint is valid if
// it equals to the existing raw checkpoint. Otherwise, it further verifies
// the raw checkpoint and decides whether it is an invalid checkpoint or a
// conflicting checkpoint. A conflicting checkpoint indicates the existence
// of a fork
func (k Keeper) verifyCkptBytes(ctx sdk.Context, rawCkptBytes []byte) (*types.RawCheckpointWithMeta, error) {
	ckpt, err := types.BytesToRawCkpt(k.cdc, rawCkptBytes)
	if err != nil {
		return nil, err
	}
	// sanity check
	err = ckpt.ValidateBasic()
	if err != nil {
		return nil, err
	}
	ckptWithMeta, err := k.GetRawCheckpoint(ctx, ckpt.EpochNum)
	if err != nil {
		return nil, err
	}

	// a valid checkpoint should equal to the existing one according to epoch number
	if ckptWithMeta.Ckpt.Equal(ckpt) {
		return ckptWithMeta, nil
	}

	// next verify if the multi signature is valid
	// check whether sufficient voting power is accumulated
	totalPower := k.GetTotalVotingPower(ctx, ckpt.EpochNum)
	signerSet, err := k.GetValidatorSet(ctx, ckpt.EpochNum).FindSubset(ckpt.Bitmap)
	if err != nil {
		return nil, err
	}
	var sum int64
	signersPubKeys := make([]bls12381.PublicKey, len(signerSet))
	for i, v := range signerSet {
		signersPubKeys[i], err = k.GetBlsPubKey(ctx, v.Addr)
		if err != nil {
			return nil, err
		}
		sum += v.Power
	}
	if sum <= totalPower*1/3 {
		return nil, errors.New("insufficient voting power")
	}
	msgBytes := ckpt.LastCommitHash.MustMarshal()
	ok, err := bls12381.VerifyMultiSig(*ckpt.BlsMultiSig, signersPubKeys, msgBytes)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("invalid BLS multi-sig")
	}

	// TODO: needs to stall the node since a conflicting checkpoint is found
	return nil, types.ErrInvalidRawCheckpoint.Wrapf("a conflicting checkpoint is found")
}

// SetCheckpointSubmitted sets the status of a checkpoint to SUBMITTED
func (k Keeper) SetCheckpointSubmitted(ctx sdk.Context, epoch uint64) {
	k.setCheckpointStatus(ctx, epoch, types.Sealed, types.Submitted)
}

// SetCheckpointConfirmed sets the status of a checkpoint to CONFIRMED
func (k Keeper) SetCheckpointConfirmed(ctx sdk.Context, epoch uint64) {
	k.setCheckpointStatus(ctx, epoch, types.Submitted, types.Confirmed)
}

// SetCheckpointFinalized sets the status of a checkpoint to FINALIZED
func (k Keeper) SetCheckpointFinalized(ctx sdk.Context, epoch uint64) {
	k.setCheckpointStatus(ctx, epoch, types.Confirmed, types.Finalized)
}

func (k Keeper) SetCheckpointForgotten(ctx sdk.Context, epoch uint64) {
	k.setCheckpointStatus(ctx, epoch, types.Submitted, types.Sealed)
}

func (k Keeper) setCheckpointStatus(ctx sdk.Context, epoch uint64, from types.CheckpointStatus, to types.CheckpointStatus) {
	ckptWithMeta, err := k.GetRawCheckpoint(ctx, epoch)
	if err != nil {
		// TODO: ignore err for now
		return
	}
	if ckptWithMeta.Status != from {
		err = types.ErrInvalidCkptStatus.Wrapf("the status of the checkpoint should be %s", from.String())
		if err != nil {
			// TODO: ignore err for now
			return
		}
	}
	ckptWithMeta.Status = to
	err = k.UpdateCheckpoint(ctx, ckptWithMeta)
	if err != nil {
		panic("failed to update checkpoint status")
	}
}

func (k Keeper) UpdateCheckpoint(ctx sdk.Context, ckptWithMeta *types.RawCheckpointWithMeta) error {
	return k.CheckpointsState(ctx).UpdateCheckpoint(ckptWithMeta)
}

func (k Keeper) CreateRegistration(ctx sdk.Context, blsPubKey bls12381.PublicKey, valAddr sdk.ValAddress) error {
	return k.RegistrationState(ctx).CreateRegistration(blsPubKey, valAddr)
}

func (k Keeper) GetBlsPubKey(ctx sdk.Context, address sdk.ValAddress) (bls12381.PublicKey, error) {
	return k.RegistrationState(ctx).GetBlsPubKey(address)
}

func (k Keeper) GetEpoch(ctx sdk.Context) epochingtypes.Epoch {
	return k.epochingKeeper.GetEpoch(ctx)
}

func (k Keeper) GetValidatorSet(ctx sdk.Context, epochNumber uint64) epochingtypes.ValidatorSet {
	return k.epochingKeeper.GetValidatorSet(ctx, epochNumber)
}

func (k Keeper) GetTotalVotingPower(ctx sdk.Context, epochNumber uint64) int64 {
	return k.epochingKeeper.GetTotalVotingPower(ctx, epochNumber)
}
