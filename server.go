package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Server 一个「带 HTTP 接口 + P2P 广播」的区块链节点。
type Server struct {
	chain      *Blockchain
	mempool    []*Transaction // 交易池
	peers      []string       // 其他节点的地址列表（点对点网络的关键）
	httpAddr   string
	httpServer *http.Server // 存起来，方便优雅关闭
	mu         sync.Mutex   // 保护链和交易池：HTTP 每个请求都开独立 goroutine，必须加锁防数据竞争
}

// NewServer 构造一个节点。
func NewServer(chain *Blockchain, addr string, peers []string) *Server {
	return &Server{chain: chain, httpAddr: addr, peers: peers}
}

// Listen 启动 HTTP 服务，注册四个路由。
func (s *Server) Listen() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/chain", s.handleChain)               // 查链
	mux.HandleFunc("/transactions", s.handleTransactions) // 发交易
	mux.HandleFunc("/mine", s.handleMine)                 // 挖矿
	mux.HandleFunc("/receive", s.handleReceive)           // 接收别的节点广播来的区块
	mux.HandleFunc("/balances", s.handleBalances)         // 查余额
	s.httpServer = &http.Server{Addr: s.httpAddr, Handler: mux}
	go s.syncLoop() // 后台周期性向 peer 拉链，认最长链
	log.Printf("节点监听 %s", s.httpAddr)
	return s.httpServer.ListenAndServe()
}

// Shutdown 优雅关闭：停止接收新请求，等处理中的请求完成（最长等 ctx 超时）。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

// SetPeers 更新邻居节点列表（演示分叉后重新联网用）。
func (s *Server) SetPeers(peers []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers = peers
}

// syncLoop 周期性向所有 peer 拉取链并采纳最长链，保证全网最终收敛到同一条链。
func (s *Server) syncLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.syncFromPeers()
	}
}

// syncFromPeers 向每个 peer 拉整条链，若对方链更长且合法就整链替换（reorg）。
func (s *Server) syncFromPeers() {
	s.mu.Lock()
	peers := append([]string(nil), s.peers...)
	s.mu.Unlock()

	for _, peer := range peers {
		resp, err := http.Get("http://" + peer + "/chain")
		if err != nil {
			continue // 某个 peer 没起来，跳过，下轮再试
		}
		var blocks []*Block
		if err := json.NewDecoder(resp.Body).Decode(&blocks); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		s.mu.Lock()
		replaced, err := s.chain.ReplaceChain(blocks)
		length := s.chain.Len()
		s.mu.Unlock()
		if err != nil {
			log.Printf("对 %s 的链同步被拒: %v", peer, err)
			continue
		}
		if replaced {
			log.Printf("REORG：采用 %s 的更长的链，现链长 %d", peer, length)
		}
	}
}

// handleChain GET /chain：返回整条链。
func (s *Server) handleChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.chain.blocks)
}

// handleBalances GET /balances：查看每个地址的当前余额。
func (s *Server) handleBalances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.chain.Balances())
}

// handleTransactions POST /transactions：接收签名交易，验签后进交易池。
func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var tx Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, "无效的请求体", http.StatusBadRequest)
		return
	}
	if !tx.Verify() {
		http.Error(w, "交易签名无效", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.mempool = append(s.mempool, &tx)
	pool := len(s.mempool)
	s.mu.Unlock()
	log.Printf("交易验签通过并入池，当前池大小 %d", pool)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "交易已进入交易池，等待打包"})
}

// handleMine GET /mine：打包交易池挖矿，并把新区块广播给所有 peer。
func (s *Server) handleMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	miner := r.URL.Query().Get("miner")
	if miner == "" {
		http.Error(w, "需要矿工地址（/mine?miner=地址）", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	// 铸币交易放第一笔：挖出块，矿工拿奖励（钱从这里来）
	coinbase := &Transaction{To: miner, Amount: MiningReward}
	// 在试算账本上逐笔筛交易池：付得起的才打包（顺带防同块内双花）
	snapshot := s.chain.cloneBalances()
	_ = s.chain.applyTx(snapshot, coinbase) // 铸币永远成功
	var valid []*Transaction
	for _, tx := range s.mempool {
		if err := s.chain.applyTx(snapshot, tx); err != nil {
			log.Printf("丢弃非法交易: %v", err)
			continue
		}
		valid = append(valid, tx)
	}
	skipped := len(s.mempool) - len(valid)
	s.mempool = nil
	txs := append([]*Transaction{coinbase}, valid...)
	block, err := s.chain.AddBlock(txs)
	s.mu.Unlock()
	if err != nil {
		http.Error(w, "打包失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("挖出区块 #%d：1 笔铸币 + %d 笔转账（丢弃 %d 笔非法）", block.Index, len(valid), skipped)
	s.broadcast(block) // 挖出新块，广播出去
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(block)
}

// handleReceive POST /receive：接收别的节点广播来的区块。
func (s *Server) handleReceive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var block Block
	if err := json.NewDecoder(r.Body).Decode(&block); err != nil {
		http.Error(w, "无效的区块", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	ok, err := s.chain.AddExternalBlock(&block)
	s.mu.Unlock()
	if err != nil {
		http.Error(w, "落盘失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if ok {
		log.Printf("接收并挂链区块 #%d", block.Index)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "区块已接收并挂链"})
		return
	}
	http.Error(w, "区块无效，拒绝", http.StatusBadRequest)
}

// broadcast 把新区块并发广播给所有 peer（goroutine 并发发 + channel 收结果）。
func (s *Server) broadcast(block *Block) {
	data, _ := json.Marshal(block)
	results := make(chan string, len(s.peers)) // 带缓冲 channel，收集每个 peer 的结果
	for _, peer := range s.peers {
		go func(peer string) { // 每个 peer 一条 goroutine，并发发送
			resp, err := http.Post("http://"+peer+"/receive", "application/json", bytes.NewReader(data))
			if err != nil {
				results <- "广播到 " + peer + " 失败: " + err.Error()
				return
			}
			resp.Body.Close()
			results <- "广播到 " + peer + " 成功 (" + resp.Status + ")"
		}(peer)
	}
	for range s.peers {
		fmt.Println("  ", <-results) // 从 channel 收结果，收满 peers 个
	}
}
