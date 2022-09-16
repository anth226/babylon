package types

import sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

// x/checkpointing module sentinel errors
var (
	ErrCkptDoesNotExist       = sdkerrors.Register(ModuleName, 1201, "raw checkpoint does not exist")
	ErrCkptAlreadyExist       = sdkerrors.Register(ModuleName, 1202, "raw checkpoint already exists")
	ErrCkptHashNotEqual       = sdkerrors.Register(ModuleName, 1203, "hash does not equal to raw checkpoint")
	ErrCkptNotAccumulating    = sdkerrors.Register(ModuleName, 1204, "raw checkpoint is no longer accumulating BLS sigs")
	ErrCkptAlreadyVoted       = sdkerrors.Register(ModuleName, 1205, "raw checkpoint already accumulated the validator")
	ErrInvalidRawCheckpoint   = sdkerrors.Register(ModuleName, 1206, "raw checkpoint is invalid")
	ErrInvalidCkptStatus      = sdkerrors.Register(ModuleName, 1207, "raw checkpoint's status is invalid")
	ErrBlsKeyDoesNotExist     = sdkerrors.Register(ModuleName, 1208, "BLS public key does not exist")
	ErrBlsKeyAlreadyExist     = sdkerrors.Register(ModuleName, 1209, "BLS public key already exists")
	ErrBlsPrivKeyDoesNotExist = sdkerrors.Register(ModuleName, 1210, "BLS private key does not exist")
)
