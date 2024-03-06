package xfunding

import (
	"context"
	"fmt"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

type FuturesTransfer interface {
	TransferFuturesAccountAsset(ctx context.Context, asset string, amount fixedpoint.Value, io types.TransferDirection) error
	QueryAccountBalances(ctx context.Context) (types.BalanceMap, error)
}

func (s *Strategy) resetTransfer(ctx context.Context, ex FuturesTransfer, asset string) error {
	balances, err := s.futuresSession.Exchange.QueryAccountBalances(ctx)
	if err != nil {
		return err
	}

	b, ok := balances[asset]
	if !ok {
		return nil
	}

	amount := b.MaxWithdrawAmount
	if amount.IsZero() {
		return nil
	}

	log.Infof("transfering out futures account asset %s %s", amount, asset)

	err = ex.TransferFuturesAccountAsset(ctx, asset, amount, types.TransferOut)
	if err != nil {
		return err
	}

	s.State.PendingBaseTransfer = fixedpoint.Zero
	s.State.TotalBaseTransfer = fixedpoint.Zero
	return nil
}

func (s *Strategy) transferOut(ctx context.Context, ex FuturesTransfer, asset string, quantity fixedpoint.Value) error {
	// if transfer done
	if s.State.TotalBaseTransfer.IsZero() {
		return nil
	}

	balances, err := s.futuresSession.Exchange.QueryAccountBalances(ctx)
	if err != nil {
		log.Infof("balance query error, adding to pending base transfer: %s %s + %s", quantity.String(), asset, s.State.PendingBaseTransfer.String())
		s.State.PendingBaseTransfer = s.State.PendingBaseTransfer.Add(quantity)
		return err
	}

	b, ok := balances[asset]
	if !ok {
		log.Infof("balance not found, adding to pending base transfer: %s %s + %s", quantity.String(), asset, s.State.PendingBaseTransfer.String())
		s.State.PendingBaseTransfer = s.State.PendingBaseTransfer.Add(quantity)
		return fmt.Errorf("%s balance not found", asset)
	}

	log.Infof("found futures balance: %+v", b)

	// add the previous pending base transfer and the current trade quantity
	amount := b.MaxWithdrawAmount
	if !quantity.IsZero() {
		amount = s.State.PendingBaseTransfer.Add(quantity)
	}

	// try to transfer more if we enough balance
	amount = fixedpoint.Min(amount, b.MaxWithdrawAmount)

	// we can only transfer the rest quota (total base transfer)
	amount = fixedpoint.Min(s.State.TotalBaseTransfer, amount)

	// TODO: according to the fee, we might not be able to get enough balance greater than the trade quantity, we can adjust the quantity here
	if amount.IsZero() {
		log.Infof("zero amount, adding to pending base transfer: %s %s + %s ", quantity.String(), asset, s.State.PendingBaseTransfer.String())
		s.State.PendingBaseTransfer = s.State.PendingBaseTransfer.Add(quantity)
		return nil
	}

	// de-leverage and get the collateral base quantity
	collateralBase := s.FuturesPosition.GetBase().Abs().Div(s.Leverage)
	_ = collateralBase

	// if s.State.TotalBaseTransfer.Compare(collateralBase)

	log.Infof("transfering out futures account asset %s %s", amount, asset)
	if err := ex.TransferFuturesAccountAsset(ctx, asset, amount, types.TransferOut); err != nil {
		return err
	}

	// reset pending transfer
	s.State.PendingBaseTransfer = fixedpoint.Zero

	// reduce the transfer in the total base transfer
	s.State.TotalBaseTransfer = s.State.TotalBaseTransfer.Sub(amount)
	return nil
}

// transferIn transfers the asset from the spot account to the futures account
func (s *Strategy) transferIn(ctx context.Context, ex FuturesTransfer, asset string, quantity fixedpoint.Value) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// query spot balances to validate the quantity
	balances, err := s.spotSession.Exchange.QueryAccountBalances(ctx)
	if err != nil {
		return err
	}

	b, ok := balances[asset]
	if !ok {
		return fmt.Errorf("%s balance not found", asset)
	}

	// if quantity = 0, we will transfer all available balance into the futures wallet
	if quantity.IsZero() {
		quantity = b.Available
	}

	// add the pending transfer and reset the pending transfer
	quantity = s.State.PendingBaseTransfer.Add(quantity)
	s.State.PendingBaseTransfer = fixedpoint.Zero

	// the available might not be "available" at the time point,
	// we add the quantity to the pending transfer amount for the next tick.
	if b.Available.Compare(quantity) < 0 {
		log.Infof("%s available balance is not enough for transfer (%f < %f)",
			asset,
			b.Available.Float64(),
			quantity.Float64())

		availableToTransfer := fixedpoint.Min(b.Available, quantity)
		pendingTransfer := quantity.Sub(availableToTransfer)
		log.Infof("adjusted transfer quantity from %f to %f", quantity.Float64(), availableToTransfer.Float64())
		quantity = availableToTransfer

		s.State.PendingBaseTransfer = pendingTransfer
	}

	if quantity.IsZero() {
		return fmt.Errorf("unable to transfer zero %s from spot wallet to futures wallet", asset)
	}

	log.Infof("transfering %f %s from the spot wallet into futures wallet...", quantity.Float64(), asset)
	if err := ex.TransferFuturesAccountAsset(ctx, asset, quantity, types.TransferIn); err != nil {
		s.State.PendingBaseTransfer = s.State.PendingBaseTransfer.Add(quantity)
		return err
	}

	// record the transfer in the total base transfer
	s.State.TotalBaseTransfer = s.State.TotalBaseTransfer.Add(quantity)
	return nil
}
