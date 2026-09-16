package main

import (
	"encoding/hex"
	"testing"
)

func TestWalletAddressIsHexOfPublicKey(t *testing.T) {
	w := NewWallet()
	if w.Address() != hex.EncodeToString(w.PublicKey) {
		t.Fatal("地址应等于公钥的 hex 编码")
	}
}
