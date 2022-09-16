package keeper

import (
	"fmt"

	"github.com/babylonchain/babylon/x/epoching/types"
	abci "github.com/tendermint/tendermint/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// ApplyMatureUnbonding
// - unbonds all mature validators/delegations, and
// - finishes all mature redelegations
// in the corresponding queues, where
// - an unbonding/redelegation becomes mature when its corresponding epoch and all previous epochs have been checkpointed.
// Triggered by the checkpointing module upon the above condition.
// (adapted from https://github.com/cosmos/cosmos-sdk/blob/v0.45.5/x/staking/keeper/val_state_change.go#L32-L91)
func (k *Keeper) ApplyMatureUnbonding(ctx sdk.Context, epochBoundaryHeader tmproto.Header) {
	currHeader := ctx.BlockHeader()

	// unbond all mature validators till the epoch boundary from the unbonding queue
	ctx.WithBlockHeader(epochBoundaryHeader)
	k.unbondAllMatureValidators(ctx)
	ctx.WithBlockHeader(currHeader)

	// get all mature unbonding delegations the epoch boundary from the ubd queue.
	ctx.WithBlockHeader(epochBoundaryHeader)
	matureUnbonds := k.stk.DequeueAllMatureUBDQueue(ctx, epochBoundaryHeader.Time)
	ctx.WithBlockHeader(currHeader)
	// unbond all mature delegations
	for _, dvPair := range matureUnbonds {
		valAddr, err := sdk.ValAddressFromBech32(dvPair.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		delAddr, err := sdk.AccAddressFromBech32(dvPair.DelegatorAddress)
		if err != nil {
			panic(err)
		}
		balances, err := k.stk.CompleteUnbonding(ctx, delAddr, valAddr)
		if err != nil {
			continue
		}

		// Babylon modification: record delegation state
		// AFTER mature, unbonded from the validator
		k.RecordNewDelegationState(ctx, delAddr, valAddr, types.BondState_UNBONDED)

		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				stakingtypes.EventTypeCompleteUnbonding,
				sdk.NewAttribute(sdk.AttributeKeyAmount, balances.String()),
				sdk.NewAttribute(stakingtypes.AttributeKeyValidator, dvPair.ValidatorAddress),
				sdk.NewAttribute(stakingtypes.AttributeKeyDelegator, dvPair.DelegatorAddress),
			),
		)
	}

	// get all mature redelegations till the epoch boundary from the red queue.
	ctx.WithBlockHeader(epochBoundaryHeader)
	matureRedelegations := k.stk.DequeueAllMatureRedelegationQueue(ctx, epochBoundaryHeader.Time)
	ctx.WithBlockHeader(currHeader)
	// finish all mature redelegations
	for _, dvvTriplet := range matureRedelegations {
		valSrcAddr, err := sdk.ValAddressFromBech32(dvvTriplet.ValidatorSrcAddress)
		if err != nil {
			panic(err)
		}
		valDstAddr, err := sdk.ValAddressFromBech32(dvvTriplet.ValidatorDstAddress)
		if err != nil {
			panic(err)
		}
		delAddr, err := sdk.AccAddressFromBech32(dvvTriplet.DelegatorAddress)
		if err != nil {
			panic(err)
		}
		balances, err := k.stk.CompleteRedelegation(
			ctx,
			delAddr,
			valSrcAddr,
			valDstAddr,
		)
		if err != nil {
			continue
		}

		// Babylon modification: record delegation state
		// AFTER mature, unbonded from the source validator, created/bonded to the destination validator
		k.RecordNewDelegationState(ctx, delAddr, valSrcAddr, types.BondState_UNBONDED)
		k.RecordNewDelegationState(ctx, delAddr, valDstAddr, types.BondState_CREATED)
		k.RecordNewDelegationState(ctx, delAddr, valDstAddr, types.BondState_BONDED)

		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				stakingtypes.EventTypeCompleteRedelegation,
				sdk.NewAttribute(sdk.AttributeKeyAmount, balances.String()),
				sdk.NewAttribute(stakingtypes.AttributeKeyDelegator, dvvTriplet.DelegatorAddress),
				sdk.NewAttribute(stakingtypes.AttributeKeySrcValidator, dvvTriplet.ValidatorSrcAddress),
				sdk.NewAttribute(stakingtypes.AttributeKeyDstValidator, dvvTriplet.ValidatorDstAddress),
			),
		)
	}
}

// ApplyAndReturnValidatorSetUpdates applies and return accumulated updates to the bonded validator set, including
// * Updates the active validator set as keyed by LastValidatorPowerKey.
// * Updates the total power as keyed by LastTotalPowerKey.
// * Updates validator status' according to updated powers.
// * Updates the fee pool bonded vs not-bonded tokens.
// * Updates relevant indices.
// Triggered upon every epoch.
// (adapted from https://github.com/cosmos/cosmos-sdk/blob/v0.45.5/x/staking/keeper/val_state_change.go#L18-L30)
func (k *Keeper) ApplyAndReturnValidatorSetUpdates(ctx sdk.Context) []abci.ValidatorUpdate {
	validatorUpdates, err := k.stk.ApplyAndReturnValidatorSetUpdates(ctx)
	if err != nil {
		panic(err)
	}

	return validatorUpdates
}

// UnbondAllMatureValidators unbonds all the mature unbonding validators that have finished their unbonding period.
// In addition, Babylon records the height of unbonding for each mature validator
// (adapted from https://github.com/cosmos/cosmos-sdk/blob/v0.45.5/x/staking/keeper/validator.go#L396-L447)
func (k Keeper) unbondAllMatureValidators(ctx sdk.Context) {
	store := ctx.KVStore(k.storeKey)

	blockTime := ctx.BlockTime()
	blockHeight := ctx.BlockHeight()

	// unbondingValIterator will contains all validator addresses indexed under
	// the ValidatorQueueKey prefix. Note, the entire index key is composed as
	// ValidatorQueueKey | timeBzLen (8-byte big endian) | timeBz | heightBz (8-byte big endian),
	// so it may be possible that certain validator addresses that are iterated
	// over are not ready to unbond, so an explicit check is required.
	unbondingValIterator := k.stk.ValidatorQueueIterator(ctx, blockTime, blockHeight)
	defer unbondingValIterator.Close()

	for ; unbondingValIterator.Valid(); unbondingValIterator.Next() {
		key := unbondingValIterator.Key()
		keyTime, keyHeight, err := stakingtypes.ParseValidatorQueueKey(key)
		if err != nil {
			panic(fmt.Errorf("failed to parse unbonding key: %w", err))
		}

		// All addresses for the given key have the same unbonding height and time.
		// We only unbond if the height and time are less than the current height
		// and time.
		if keyHeight <= blockHeight && (keyTime.Before(blockTime) || keyTime.Equal(blockTime)) {
			addrs := stakingtypes.ValAddresses{}
			k.cdc.MustUnmarshal(unbondingValIterator.Value(), &addrs)

			for _, valAddr := range addrs.Addresses {
				addr, err := sdk.ValAddressFromBech32(valAddr)
				if err != nil {
					panic(err)
				}
				val, found := k.stk.GetValidator(ctx, addr)
				if !found {
					panic("validator in the unbonding queue was not found")
				}

				if !val.IsUnbonding() {
					panic("unexpected validator in unbonding queue; status was not unbonding")
				}

				val = k.stk.UnbondingToUnbonded(ctx, val)
				if val.GetDelegatorShares().IsZero() {
					k.stk.RemoveValidator(ctx, val.GetOperator())
				}

				// Babylon modification: record the height when this validator becomes unbonded
				k.RecordNewValState(ctx, addr, types.BondState_UNBONDED)
			}

			store.Delete(key)
		}
	}
}
