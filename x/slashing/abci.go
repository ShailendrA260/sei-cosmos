package slashing

import (
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

type SlashingWriteInfo struct {
	ConsAddr    sdk.ConsAddress
	MissedInfo  types.ValidatorMissedBlockArray
	SigningInfo types.ValidatorSigningInfo
	ShouldSlash bool
	SlashInfo   keeper.SlashInfo
}

// BeginBlocker check for infraction evidence or downtime of validators
// on every begin block
func BeginBlocker(ctx sdk.Context, req abci.RequestBeginBlock, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	var wg sync.WaitGroup
	// Iterate over all the validators which *should* have signed this block
	// store whether or not they have actually signed it and slash/unbond any
	// which have missed too many blocks in a row (downtime slashing)

	// this allows us to preserve the original ordering for writing purposes
	slashingWriteInfo := make([]*SlashingWriteInfo, len(req.LastCommitInfo.GetVotes()))

	for i, voteInfo := range req.LastCommitInfo.GetVotes() {
		wg.Add(1)
		go func(valIndex int, vInfo abci.VoteInfo) {
			defer wg.Done()
			consAddr, missedInfo, signInfo, shouldSlash, slashInfo := k.HandleValidatorSignatureConcurrent(ctx, vInfo.Validator.Address, vInfo.Validator.Power, vInfo.SignedLastBlock)
			slashingWriteInfo[valIndex] = &SlashingWriteInfo{
				ConsAddr:    consAddr,
				MissedInfo:  missedInfo,
				SigningInfo: signInfo,
				ShouldSlash: shouldSlash,
				SlashInfo:   slashInfo,
			}
		}(i, voteInfo)
	}
	wg.Wait()

	for _, writeInfo := range slashingWriteInfo {
		if writeInfo == nil {
			panic("Expected slashing write info to be non-nil")
		}
		// Update the validator missed block bit array by index if different from last value at the index
		if writeInfo.ShouldSlash {
			k.ClearValidatorMissedBlockBitArray(ctx, writeInfo.ConsAddr)
		} else {
			k.SetValidatorMissedBlocks(ctx, writeInfo.ConsAddr, writeInfo.MissedInfo)
		}
		if writeInfo.ShouldSlash {
			writeInfo.SigningInfo = k.SlashJailAndUpdateSigningInfo(ctx, writeInfo.ConsAddr, writeInfo.SlashInfo, writeInfo.SigningInfo)
		}
		k.SetValidatorSigningInfo(ctx, writeInfo.ConsAddr, writeInfo.SigningInfo)
	}
}
