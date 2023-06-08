package credential

import (
	"context"
	"sync"

	currencybase "github.com/ProtoconNet/mitum-currency/v3/base"
	types "github.com/ProtoconNet/mitum-currency/v3/operation/type"
	currency "github.com/ProtoconNet/mitum-currency/v3/state/currency"
	extensioncurrency "github.com/ProtoconNet/mitum-currency/v3/state/extension"
	"github.com/ProtoconNet/mitum2/base"
	"github.com/ProtoconNet/mitum2/util"
	"github.com/pkg/errors"
)

var createCredentialServiceProcessorPool = sync.Pool{
	New: func() interface{} {
		return new(CreateCredentialServiceProcessor)
	},
}

func (CreateCredentialService) Process(
	ctx context.Context, getStateFunc base.GetStateFunc,
) ([]base.StateMergeValue, base.OperationProcessReasonError, error) {
	return nil, nil, nil
}

type CreateCredentialServiceProcessor struct {
	*base.BaseOperationProcessor
}

func NewCreateCredentialServiceProcessor() types.GetNewProcessor {
	return func(
		height base.Height,
		getStateFunc base.GetStateFunc,
		newPreProcessConstraintFunc base.NewOperationProcessorProcessFunc,
		newProcessConstraintFunc base.NewOperationProcessorProcessFunc,
	) (base.OperationProcessor, error) {
		e := util.StringErrorFunc("failed to create new CreateCredentialServiceProcessor")

		nopp := createCredentialServiceProcessorPool.Get()
		opp, ok := nopp.(*CreateCredentialServiceProcessor)
		if !ok {
			return nil, errors.Errorf("expected CreateCredentialServiceProcessor, not %T", nopp)
		}

		b, err := base.NewBaseOperationProcessor(
			height, getStateFunc, newPreProcessConstraintFunc, newProcessConstraintFunc)
		if err != nil {
			return nil, e(err, "")
		}

		opp.BaseOperationProcessor = b

		return opp, nil
	}
}

func (opp *CreateCredentialServiceProcessor) PreProcess(
	ctx context.Context, op base.Operation, getStateFunc base.GetStateFunc,
) (context.Context, base.OperationProcessReasonError, error) {
	e := util.StringErrorFunc("failed to preprocess CreateCredentialService")

	fact, ok := op.Fact().(CreateCredentialServiceFact)
	if !ok {
		return ctx, nil, e(nil, "not CreateCredentialServiceFact, %T", op.Fact())
	}

	if err := fact.IsValid(nil); err != nil {
		return ctx, nil, e(err, "")
	}

	if err := checkExistsState(currency.StateKeyAccount(fact.Sender()), getStateFunc); err != nil {
		return nil, base.NewBaseOperationProcessReasonError("sender not found, %q: %w", fact.Sender(), err), nil
	}

	if err := checkNotExistsState(extensioncurrency.StateKeyContractAccount(fact.Sender()), getStateFunc); err != nil {
		return nil, base.NewBaseOperationProcessReasonError("contract account cannot create credential service, %q: %w", fact.Sender(), err), nil
	}

	st, err := existsState(extensioncurrency.StateKeyContractAccount(fact.Contract()), "key of contract account", getStateFunc)
	if err != nil {
		return nil, base.NewBaseOperationProcessReasonError("contract account not found, %q: %w", fact.Contract(), err), nil
	}

	ca, err := extensioncurrency.StateContractAccountValue(st)
	if err != nil {
		return nil, base.NewBaseOperationProcessReasonError("contract account value not found, %q: %w", fact.Contract(), err), nil
	}

	if !ca.Owner().Equal(fact.sender) {
		return nil, base.NewBaseOperationProcessReasonError("not contract account owner, %q", fact.sender), nil
	}

	if err := checkNotExistsState(StateKeyDesign(fact.Contract(), fact.CredentialServiceID()), getStateFunc); err != nil {
		return nil, base.NewBaseOperationProcessReasonError("credential service already exists, %s-%s: %w", fact.Contract(), fact.CredentialServiceID(), err), nil
	}

	if err := checkFactSignsByState(fact.Sender(), op.Signs(), getStateFunc); err != nil {
		return ctx, base.NewBaseOperationProcessReasonError("invalid signing: %w", err), nil
	}

	return ctx, nil, nil
}

func (opp *CreateCredentialServiceProcessor) Process(
	ctx context.Context, op base.Operation, getStateFunc base.GetStateFunc) (
	[]base.StateMergeValue, base.OperationProcessReasonError, error,
) {
	e := util.StringErrorFunc("failed to process CreateCredentialService")

	fact, ok := op.Fact().(CreateCredentialServiceFact)
	if !ok {
		return nil, nil, e(nil, "expected CreateCredentialServiceFact, not %T", op.Fact())
	}

	templates := []Uint256{}
	holders := []Holder{}

	policy := NewPolicy(templates, holders, 0)
	if err := policy.IsValid(nil); err != nil {
		return nil, base.NewBaseOperationProcessReasonError("invalid credential policy, %s-%s: %w", fact.Contract(), fact.CredentialServiceID(), err), nil
	}

	design := NewDesign(fact.CredentialServiceID(), policy)
	if err := design.IsValid(nil); err != nil {
		return nil, base.NewBaseOperationProcessReasonError("invalid credential design, %s-%s: %w", fact.Contract(), fact.CredentialServiceID(), err), nil
	}

	sts := make([]base.StateMergeValue, 2)

	sts[0] = NewStateMergeValue(
		StateKeyDesign(fact.Contract(), fact.CredentialServiceID()),
		NewDesignStateValue(design),
	)

	currencyPolicy, err := existsCurrencyPolicy(fact.Currency(), getStateFunc)
	if err != nil {
		return nil, base.NewBaseOperationProcessReasonError("currency not found, %q: %w", fact.Currency(), err), nil
	}

	fee, err := currencyPolicy.Feeer().Fee(currencybase.ZeroBig)
	if err != nil {
		return nil, base.NewBaseOperationProcessReasonError("failed to check fee of currency, %q: %w", fact.Currency(), err), nil
	}

	st, err := existsState(currency.StateKeyBalance(fact.Sender(), fact.Currency()), "key of sender balance", getStateFunc)
	if err != nil {
		return nil, base.NewBaseOperationProcessReasonError("sender balance not found, %q: %w", fact.Sender(), err), nil
	}
	sb := NewStateMergeValue(st.Key(), st.Value())

	switch b, err := currency.StateBalanceValue(st); {
	case err != nil:
		return nil, base.NewBaseOperationProcessReasonError("failed to get balance value, %q: %w", currency.StateKeyBalance(fact.Sender(), fact.Currency()), err), nil
	case b.Big().Compare(fee) < 0:
		return nil, base.NewBaseOperationProcessReasonError("not enough balance of sender, %q", fact.Sender()), nil
	}

	v, ok := sb.Value().(currency.BalanceStateValue)
	if !ok {
		return nil, base.NewBaseOperationProcessReasonError("expected BalanceStateValue, not %T", sb.Value()), nil
	}
	sts[1] = NewStateMergeValue(sb.Key(), currency.NewBalanceStateValue(v.Amount.WithBig(v.Amount.Big().Sub(fee))))

	return sts, nil, nil
}

func (opp *CreateCredentialServiceProcessor) Close() error {
	createCredentialServiceProcessorPool.Put(opp)

	return nil
}
