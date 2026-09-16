package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
)

// Wallet 钱包 = 一对密钥。
// 私钥保密（用来给交易签名），公钥公开（用来验签、当「地址」用）。
type Wallet struct {
	PrivateKey ed25519.PrivateKey // 私钥，只有本人知道
	PublicKey  ed25519.PublicKey  // 公钥，可以公开
}

// NewWallet 生成一个新钱包：随机生成一对密钥。
func NewWallet() *Wallet {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err) // 随机源出错，极罕见，直接崩
	}
	return &Wallet{PrivateKey: priv, PublicKey: pub}
}

// Address 用公钥的 hex 编码当「地址」，相当于银行卡号。
// 别人给你转账，只要知道这个地址就行，不需要你的公钥实际是什么。
func (w *Wallet) Address() string {
	return hex.EncodeToString(w.PublicKey)
}
