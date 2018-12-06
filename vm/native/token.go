package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/iost-official/go-iost/common"
	"github.com/iost-official/go-iost/core/contract"
	"github.com/iost-official/go-iost/vm/database"
	"github.com/iost-official/go-iost/vm/host"
)

var tokenABIs *abiSet

// const prefix
const (
	TokenInfoMapPrefix    = "TI"
	TokenBalanceMapPrefix = "TB"
	TokenFreezeMapPrefix  = "TF"
	IssuerMapField        = "issuer"
	SupplyMapField        = "supply"
	TotalSupplyMapField   = "totalSupply"
	CanTransferMapField   = "canTransfer"
	DefaultRateMapField   = "defaultRate"
	DecimalMapField       = "decimal"
)

func init() {
	tokenABIs = newAbiSet()
	tokenABIs.Register(initTokenABI, true)
	tokenABIs.Register(createTokenABI)
	tokenABIs.Register(issueTokenABI)
	tokenABIs.Register(transferTokenABI)
	tokenABIs.Register(transferFreezeTokenABI)
	tokenABIs.Register(balanceOfTokenABI)
	tokenABIs.Register(supplyTokenABI)
	tokenABIs.Register(totalSupplyTokenABI)
	tokenABIs.Register(destroyTokenABI)
}

func checkTokenExists(h *host.Host, tokenName string) (ok bool, cost contract.Cost) {
	exists, cost0 := h.MapHas(TokenInfoMapPrefix+tokenName, IssuerMapField)
	return exists, cost0
}

func setBalance(h *host.Host, tokenName string, from string, balance int64, ramPayer string) (cost contract.Cost) {
	ok, cost := h.MapHas(TokenBalanceMapPrefix+from, tokenName)
	if ok {
		cost0, _ := h.MapPut(TokenBalanceMapPrefix+from, tokenName, balance)
		cost.AddAssign(cost0)
	} else {
		cost0, _ := h.MapPut(TokenBalanceMapPrefix+from, tokenName, balance, ramPayer)
		cost.AddAssign(cost0)
	}
	return cost
}

func getBalance(h *host.Host, tokenName string, from string, ramPayer string) (balance int64, cost contract.Cost, err error) {
	balance = int64(0)
	cost = contract.Cost0()
	ok, cost0 := h.MapHas(TokenBalanceMapPrefix+from, tokenName)
	cost.AddAssign(cost0)
	if ok {
		tmp, cost0 := h.MapGet(TokenBalanceMapPrefix+from, tokenName)
		cost.AddAssign(cost0)
		balance = tmp.(int64)
	}

	ok, cost0 = h.MapHas(TokenFreezeMapPrefix+from, tokenName)
	cost.AddAssign(cost0)
	if !ok {
		return balance, cost, nil
	}

	ntime, cost0 := h.BlockTime()
	cost.AddAssign(cost0)

	freezeJSON, cost0 := h.MapGet(TokenFreezeMapPrefix+from, tokenName)
	cost.AddAssign(cost0)
	freezeList := []database.FreezeItem{}

	err = json.Unmarshal([]byte(freezeJSON.(database.SerializedJSON)), &freezeList)
	cost.AddAssign(host.CommonOpCost(1))
	if err != nil {
		return balance, cost, err
	}

	addBalance := int64(0)
	i := 0
	for i < len(freezeList) {
		if freezeList[i].Ftime > ntime {
			break
		}
		addBalance += freezeList[i].Amount
		i++
	}
	cost.AddAssign(host.CommonOpCost(i))

	if addBalance > 0 {
		balance += addBalance
		cost0 = setBalance(h, tokenName, from, balance, ramPayer)
		cost.AddAssign(cost0)
	}

	if i > 0 {
		freezeList = freezeList[i:]
		freezeJSON, err = json.Marshal(freezeList)
		cost.AddAssign(host.CommonOpCost(1))
		if err != nil {
			return balance, cost, err
		}
		cost0, err = h.MapPut(TokenFreezeMapPrefix+from, tokenName, database.SerializedJSON(freezeJSON.([]byte)))
		cost.AddAssign(cost0)
		if err != nil {
			return balance, cost, err
		}
	}

	return balance, cost, nil
}

func freezeBalance(h *host.Host, tokenName string, from string, balance int64, ftime int64, ramPayer string) (cost contract.Cost, err error) {
	ok, cost := h.MapHas(TokenFreezeMapPrefix+from, tokenName)
	freezeList := []database.FreezeItem{}
	if ok {
		freezeJSON, cost0 := h.MapGet(TokenFreezeMapPrefix+from, tokenName)
		cost.AddAssign(cost0)
		err = json.Unmarshal([]byte(freezeJSON.(database.SerializedJSON)), &freezeList)
		cost.AddAssign(host.CommonOpCost(1))
		if err != nil {
			return cost, err
		}
	}

	freezeList = append(freezeList, database.FreezeItem{Amount: balance, Ftime: ftime})
	sort.Slice(freezeList, func(i, j int) bool {
		return freezeList[i].Ftime < freezeList[j].Ftime ||
			freezeList[i].Ftime == freezeList[j].Ftime && freezeList[i].Amount < freezeList[j].Amount
	})
	cost.AddAssign(host.CommonOpCost(len(freezeList)))

	freezeJSON, err := json.Marshal(freezeList)
	cost.AddAssign(host.CommonOpCost(1))
	if err != nil {
		return cost, nil
	}
	cost0, err := h.MapPut(TokenFreezeMapPrefix+from, tokenName, database.SerializedJSON(freezeJSON), ramPayer)
	cost.AddAssign(cost0)
	if err != nil {
		return cost, err
	}

	return cost, nil
}

func parseAmount(h *host.Host, tokenName string, amountStr string) (amount int64, cost contract.Cost, err error) {
	decimal, cost := h.MapGet(TokenInfoMapPrefix+tokenName, DecimalMapField)
	amountNumber, err := common.NewFixed(amountStr, int(decimal.(int64)))

	cost.AddAssign(host.CommonOpCost(3))
	if err != nil {
		return 0, cost, fmt.Errorf("invalid amount %v %v", amountStr, err)
	}
	return amountNumber.Value, cost, err
}

func genAmount(h *host.Host, tokenName string, amount int64) (amountStr string, cost contract.Cost) {
	decimal, cost := h.MapGet(TokenInfoMapPrefix+tokenName, DecimalMapField)
	amountNumber := common.Fixed{Value: amount, Decimal: int(decimal.(int64))}
	cost.AddAssign(host.CommonOpCost(1))
	return amountNumber.ToString(), cost
}

func checkTokenNameValid(name string) error {
	if len(name) <= 0 || len(name) > 32 {
		return fmt.Errorf("token name invalid. token name length should be between 1,32  got %v", name)
	}
	for _, ch := range name {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_') {
			return fmt.Errorf("token name invalid. token name contains invalid character %v", ch)
		}
	}
	return nil
}

var (
	initTokenABI = &abi{
		name: "init",
		args: []string{},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			return []interface{}{}, host.CommonErrorCost(1), nil
		},
	}

	createTokenABI = &abi{
		name: "create",
		args: []string{"string", "string", "number", "json"},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			cost = contract.Cost0()
			cost.AddAssign(host.CommonOpCost(1))
			tokenName := args[0].(string)
			issuer := args[1].(string)
			totalSupply := args[2].(int64)
			configJSON := args[3].([]byte)

			cost.AddAssign(host.CommonOpCost(1))
			err = checkTokenNameValid(tokenName)
			if err != nil {
				return nil, cost, err
			}

			// config
			config := make(map[string]interface{})
			err = json.Unmarshal(configJSON, &config)
			cost.AddAssign(host.CommonOpCost(2))
			if err != nil {
				return nil, cost, err
			}
			decimal := 8
			canTransfer := true
			defaultRate := "1.0"
			cost.AddAssign(host.CommonOpCost(3))
			if tmp, ok := config[DecimalMapField]; ok {
				if _, ok = tmp.(float64); !ok {
					return nil, cost, errors.New("decimal in config should be number")
				}
				decimal = int(tmp.(float64))
			}
			if tmp, ok := config[CanTransferMapField]; ok {
				canTransfer, ok = tmp.(bool)
				if !ok {
					return nil, cost, errors.New("canTransfer in config should be bool")
				}
			}
			if tmp, ok := config[DefaultRateMapField]; ok {
				defaultRate, ok = tmp.(string)
				if !ok {
					return nil, cost, errors.New("defaultRate in config should be string")
				}
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// check auth
			ok, cost0 := h.RequireAuth(issuer, "token.iost")
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrPermissionLost
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// check exists
			ok, cost0 = checkTokenExists(h, tokenName)
			cost.AddAssign(cost0)
			if ok {
				return nil, cost, host.ErrTokenExists
			}

			// check valid
			if decimal < 0 || decimal >= 19 {
				return nil, cost, errors.New("invalid decimal")
			}
			if totalSupply > math.MaxInt64/int64(math.Pow10(decimal)) {
				return nil, cost, errors.New("invalid totalSupply")
			}
			totalSupply *= int64(math.Pow10(decimal))

			// put info
			cost0, _ = h.MapPut(TokenInfoMapPrefix+tokenName, IssuerMapField, issuer, issuer)
			cost.AddAssign(cost0)
			cost0, _ = h.MapPut(TokenInfoMapPrefix+tokenName, TotalSupplyMapField, totalSupply, issuer)
			cost.AddAssign(cost0)
			cost0, _ = h.MapPut(TokenInfoMapPrefix+tokenName, SupplyMapField, int64(0), issuer)
			cost.AddAssign(cost0)
			cost0, _ = h.MapPut(TokenInfoMapPrefix+tokenName, CanTransferMapField, canTransfer, issuer)
			cost.AddAssign(cost0)
			cost0, _ = h.MapPut(TokenInfoMapPrefix+tokenName, DefaultRateMapField, defaultRate, issuer)
			cost.AddAssign(cost0)
			cost0, _ = h.MapPut(TokenInfoMapPrefix+tokenName, DecimalMapField, int64(decimal), issuer)
			cost.AddAssign(cost0)

			return []interface{}{}, cost, nil
		},
	}

	issueTokenABI = &abi{
		name: "issue",
		args: []string{"string", "string", "string"},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			cost = contract.Cost0()
			cost.AddAssign(host.CommonOpCost(1))
			tokenName := args[0].(string)
			to := args[1].(string)
			amountStr := args[2].(string)

			// get token info
			ok, cost0 := checkTokenExists(h, tokenName)
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrTokenNotExists
			}
			issuer, cost0 := h.MapGet(TokenInfoMapPrefix+tokenName, IssuerMapField)
			cost.AddAssign(cost0)
			supply, cost0 := h.MapGet(TokenInfoMapPrefix+tokenName, SupplyMapField)
			cost.AddAssign(cost0)
			totalSupply, cost0 := h.MapGet(TokenInfoMapPrefix+tokenName, TotalSupplyMapField)
			cost.AddAssign(cost0)
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// check auth
			ok, cost0 = h.RequireAuth(issuer.(string), "token.iost")
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrPermissionLost
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// get amount by fixed point number
			amount, cost0, err := parseAmount(h, tokenName, amountStr)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			if amount <= 0 {
				return nil, cost, host.ErrInvalidAmount
			}

			// check supply
			if totalSupply.(int64)-supply.(int64) < amount {
				return nil, cost, errors.New("supply too much")
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// set supply, set balance
			cost0, err = h.MapPut(TokenInfoMapPrefix+tokenName, SupplyMapField, supply.(int64)+amount)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}

			balance, cost0, err := getBalance(h, tokenName, to, issuer.(string))
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			cost.AddAssign(cost0)

			balance += amount
			cost0 = setBalance(h, tokenName, to, balance, issuer.(string))
			cost.AddAssign(cost0)

			message, err := json.Marshal(args)
			cost.AddAssign(host.CommonOpCost(1))
			if err != nil {
				return nil, cost, err
			}
			cost0 = h.Receipt(string(message))
			cost.AddAssign(cost0)
			return []interface{}{}, cost, nil
		},
	}

	transferTokenABI = &abi{
		name: "transfer",
		args: []string{"string", "string", "string", "string", "string"},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			cost = contract.Cost0()
			cost.AddAssign(host.CommonOpCost(1))
			tokenName := args[0].(string)
			from := args[1].(string)
			to := args[2].(string)
			amountStr := args[3].(string)
			memo := args[4].(string) // memo
			if len(memo) > 512 {
				return nil, cost, host.ErrMemoTooLarge
			}
			if !h.IsValidAccount(from) {
				return nil, cost, fmt.Errorf("invalid account %v", from)
			}
			if !h.IsValidAccount(to) {
				return nil, cost, fmt.Errorf("invalid account %v", to)
			}

			//fmt.Printf("token transfer %v %v %v %v\n", tokenName, from, to, amountStr)

			if from == to {
				return []interface{}{}, cost, nil
			}

			// get token info
			ok, cost0 := checkTokenExists(h, tokenName)
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrTokenNotExists
			}
			canTransfer, cost0 := h.MapGet(TokenInfoMapPrefix+tokenName, CanTransferMapField)
			cost.AddAssign(cost0)
			if !(canTransfer.(bool)) {
				return nil, cost, host.ErrTokenNoTransfer
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// check auth
			ok, cost0 = h.RequireAuth(from, "transfer")
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrPermissionLost
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// get amount by fixed point number
			amount, cost0, err := parseAmount(h, tokenName, amountStr)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			if amount <= 0 {
				return nil, cost, host.ErrInvalidAmount
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// set balance
			fbalance, cost0, err := getBalance(h, tokenName, from, from)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			tbalance, cost0, err := getBalance(h, tokenName, to, from)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			if fbalance < amount {
				return nil, cost, fmt.Errorf("balance not enough %v < %v", fbalance, amount)
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			fbalance -= amount
			tbalance += amount

			cost0 = setBalance(h, tokenName, to, tbalance, from)
			//fmt.Printf("transfer set %v %v %v\n", tokenName, to, tbalance)
			cost.AddAssign(cost0)
			cost0 = setBalance(h, tokenName, from, fbalance, from)
			cost.AddAssign(cost0)

			message, err := json.Marshal(args)
			cost.AddAssign(host.CommonOpCost(1))
			if err != nil {
				return nil, cost, err
			}
			cost0 = h.Receipt(string(message))
			cost.AddAssign(cost0)
			return []interface{}{}, cost, nil
		},
	}

	transferFreezeTokenABI = &abi{
		name: "transferFreeze",
		args: []string{"string", "string", "string", "string", "number", "string"},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			cost = contract.Cost0()
			cost.AddAssign(host.CommonOpCost(1))
			tokenName := args[0].(string)
			from := args[1].(string)
			to := args[2].(string)
			amountStr := args[3].(string)
			ftime := args[4].(int64) // time.Now().UnixNano()
			memo := args[5].(string) // memo
			if len(memo) > 512 {
				return nil, cost, host.ErrMemoTooLarge
			}

			// get token info
			ok, cost0 := checkTokenExists(h, tokenName)
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrTokenNotExists
			}
			canTransfer, cost0 := h.MapGet(TokenInfoMapPrefix+tokenName, CanTransferMapField)
			cost.AddAssign(cost0)
			if !(canTransfer.(bool)) {
				return nil, cost, host.ErrTokenNoTransfer
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// check auth
			ok, cost0 = h.RequireAuth(from, "transfer")
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrPermissionLost
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// get amount by fixed point number
			amount, cost0, err := parseAmount(h, tokenName, amountStr)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			if amount <= 0 {
				return nil, cost, host.ErrInvalidAmount
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// sub balance of from
			fbalance, cost0, err := getBalance(h, tokenName, from, from)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			if fbalance < amount {
				return nil, cost, fmt.Errorf("balance not enough %v < %v", fbalance, amount)
			}

			fbalance -= amount
			cost0 = setBalance(h, tokenName, from, fbalance, from)
			cost.AddAssign(cost0)
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// freeze token of to
			cost0, err = freezeBalance(h, tokenName, to, amount, ftime, from)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}

			message, err := json.Marshal(args)
			cost.AddAssign(host.CommonOpCost(1))
			if err != nil {
				return nil, cost, err
			}
			cost0 = h.Receipt(string(message))
			cost.AddAssign(cost0)
			return []interface{}{}, cost, nil
		},
	}

	destroyTokenABI = &abi{
		name: "destroy",
		args: []string{"string", "string", "string"},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			cost = contract.Cost0()
			cost.AddAssign(host.CommonOpCost(1))
			tokenName := args[0].(string)
			from := args[1].(string)
			amountStr := args[2].(string)

			// get token info
			ok, cost0 := checkTokenExists(h, tokenName)
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrTokenNotExists
			}

			// check auth
			ok, cost0 = h.RequireAuth(from, "transfer")
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrPermissionLost
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// get amount by fixed point number
			amount, cost0, err := parseAmount(h, tokenName, amountStr)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			if amount <= 0 {
				return nil, cost, host.ErrInvalidAmount
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// set balance
			fbalance, cost0, err := getBalance(h, tokenName, from, from)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			if fbalance < amount {
				return nil, cost, fmt.Errorf("balance not enough %v < %v", fbalance, amount)
			}
			fbalance -= amount
			cost0 = setBalance(h, tokenName, from, fbalance, from)
			cost.AddAssign(cost0)
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			// set supply
			tmp, cost0 := h.MapGet(TokenInfoMapPrefix+tokenName, SupplyMapField)
			supply := tmp.(int64)
			cost.AddAssign(cost0)

			supply -= amount
			cost0, err = h.MapPut(TokenInfoMapPrefix+tokenName, SupplyMapField, supply)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}

			return []interface{}{}, cost, nil
		},
	}

	balanceOfTokenABI = &abi{
		name: "balanceOf",
		args: []string{"string", "string"},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			cost = contract.Cost0()
			cost.AddAssign(host.CommonOpCost(1))
			tokenName := args[0].(string)
			to := args[1].(string)

			// check token info
			ok, cost0 := checkTokenExists(h, tokenName)
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrTokenNotExists
			}

			balance, cost0, err := getBalance(h, tokenName, to, to)
			cost.AddAssign(cost0)
			if err != nil {
				return nil, cost, err
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			balanceStr, cost0 := genAmount(h, tokenName, balance)

			cost.AddAssign(cost0)

			return []interface{}{balanceStr}, cost, nil
		},
	}

	supplyTokenABI = &abi{
		name: "supply",
		args: []string{"string"},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			cost = contract.Cost0()
			cost.AddAssign(host.CommonOpCost(1))
			tokenName := args[0].(string)

			// check token info
			ok, cost0 := checkTokenExists(h, tokenName)
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrTokenNotExists
			}
			if !CheckCost(h, cost) {
				return nil, cost, host.ErrGasLimitExceeded
			}

			supply, cost0 := h.MapGet(TokenInfoMapPrefix+tokenName, SupplyMapField)
			cost.AddAssign(cost0)
			supplyStr, cost0 := genAmount(h, tokenName, supply.(int64))
			cost.AddAssign(cost0)

			return []interface{}{supplyStr}, cost, nil
		},
	}

	totalSupplyTokenABI = &abi{
		name: "totalSupply",
		args: []string{"string"},
		do: func(h *host.Host, args ...interface{}) (rtn []interface{}, cost contract.Cost, err error) {
			cost = contract.Cost0()
			cost.AddAssign(host.CommonOpCost(1))
			tokenName := args[0].(string)

			// check token info
			ok, cost0 := checkTokenExists(h, tokenName)
			cost.AddAssign(cost0)
			if !ok {
				return nil, cost, host.ErrTokenNotExists
			}

			totalSupply, cost0 := h.MapGet(TokenInfoMapPrefix+tokenName, TotalSupplyMapField)
			cost.AddAssign(cost0)
			totalSupplyStr, cost0 := genAmount(h, tokenName, totalSupply.(int64))
			cost.AddAssign(cost0)

			return []interface{}{totalSupplyStr}, cost, nil
		},
	}
)
