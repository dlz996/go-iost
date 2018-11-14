package itest

import (
	"encoding/json"
	"sync"

	"github.com/iost-official/go-iost/account"
	"github.com/iost-official/go-iost/common"
	"github.com/iost-official/go-iost/core/tx"
	"github.com/iost-official/go-iost/crypto"
)

// Account is account of user
type Account struct {
	ID      string
	balance string
	rw      sync.RWMutex
	key     *Key
}

// AccountJSON is the json serialization of account
type AccountJSON struct {
	ID        string `json:"id"`
	Seckey    string `json:"seckey"`
	Algorithm string `json:"algorithm"`
}

// Sign will sign the transaction by current account
func (a *Account) Sign(t *Transaction) (*Transaction, error) {
	st, err := tx.SignTx(t.Tx, a.ID, []*account.KeyPair{a.key.KeyPair})
	if err != nil {
		return nil, err
	}

	transaction := &Transaction{
		Tx: st,
	}
	return transaction, nil
}

// Balance will return the balance of this account
func (a *Account) Balance() string {
	a.rw.RLock()
	defer a.rw.RUnlock()
	return a.balance
}

// SetBalance will set the balance of this account
func (a *Account) SetBalance(b string) {
	a.rw.Lock()
	defer a.rw.Unlock()
	a.balance = b
}

// UnmarshalJSON will unmarshal account from json
func (a *Account) UnmarshalJSON(b []byte) error {
	aux := &AccountJSON{}
	err := json.Unmarshal(b, aux)
	if err != nil {
		return err
	}

	a.ID = aux.ID
	a.key = NewKey(
		common.Base58Decode(aux.Seckey),
		crypto.NewAlgorithm(aux.Algorithm),
	)
	return nil
}

// MarshalJSON will marshal account to json
func (a *Account) MarshalJSON() ([]byte, error) {
	aux := &AccountJSON{
		ID:        a.ID,
		Seckey:    common.Base58Encode(a.key.Seckey),
		Algorithm: a.key.Algorithm.String(),
	}
	return json.Marshal(aux)
}
