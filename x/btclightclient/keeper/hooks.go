package keeper

import (
	"github.com/babylonchain/babylon/x/btclightclient/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Implements BTCLightClientHooks interface
var _ types.BTCLightClientHooks = Keeper{}

// AfterBTCHeaderInserted - call hook if registered
func (k Keeper) AfterBTCHeaderInserted(ctx sdk.Context, headerInfo *types.BTCHeaderInfo) {
	if k.hooks != nil {
		k.hooks.AfterBTCHeaderInserted(ctx, headerInfo)
	}
}

// AfterBTCRollBack - call hook if registered
func (k Keeper) AfterBTCRollBack(ctx sdk.Context, headerInfo *types.BTCHeaderInfo) {
	if k.hooks != nil {
		k.hooks.AfterBTCRollBack(ctx, headerInfo)
	}
}

// AfterBTCRollForward - call hook if registered
func (k Keeper) AfterBTCRollForward(ctx sdk.Context, headerInfo *types.BTCHeaderInfo) {
	if k.hooks != nil {
		k.hooks.AfterBTCRollForward(ctx, headerInfo)
	}
}
