package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := parseConfig()
	if cfg.Demo {
		runDemo(cfg.Difficulty)
		return
	}
	runNode(cfg)
}

// runDemo 内置两节点演示：先让两个节点分叉，再联网同步，展示「最长链胜出」。
func runDemo(difficulty int) {
	// 演示模式下把日志也写到标准输出，保证日志与结果按时间顺序交错输出
	log.SetOutput(os.Stdout)

	// 清掉旧数据，保证每次从创世块重新开始（分叉可控、可复现）
	os.RemoveAll("chain_8001.db")
	os.RemoveAll("chain_8002.db")

	bc1, err := NewBlockchain(difficulty, "chain_8001.db")
	if err != nil {
		fmt.Println("node1 建链失败:", err)
		return
	}
	defer bc1.Close()
	bc2, err := NewBlockchain(difficulty, "chain_8002.db")
	if err != nil {
		fmt.Println("node2 建链失败:", err)
		return
	}
	defer bc2.Close()

	// 两个节点一开始【互不相连】，才能各自挖出不同的分支（分叉）
	n1 := NewServer(bc1, ":8001", nil)
	n2 := NewServer(bc2, ":8002", nil)
	go func() {
		if err := n1.Listen(); err != nil {
			fmt.Println("node1 启动失败:", err)
		}
	}()
	go func() {
		if err := n2.Listen(); err != nil {
			fmt.Println("node2 启动失败:", err)
		}
	}()
	time.Sleep(800 * time.Millisecond) // 等节点起来

	mina := NewWallet() // node1 的矿工
	minb := NewWallet() // node2 的矿工

	// 分叉产生：两个节点各挖各的，互不广播
	mineAt("http://localhost:8001/mine?miner=" + mina.Address())
	mineAt("http://localhost:8001/mine?miner=" + mina.Address()) // node1 挖到高度 3
	mineAt("http://localhost:8002/mine?miner=" + minb.Address()) // node2 挖到高度 2

	fmt.Println("\n=== 分叉后：两条链已经分家 ===")
	fmt.Println("node1 链长:", chainLen("http://localhost:8001/chain"),
		" node2 链长:", chainLen("http://localhost:8002/chain"))
	fmt.Println("node1 尾块:", tailHash("http://localhost:8001/chain"))
	fmt.Println("node2 尾块:", tailHash("http://localhost:8002/chain"))

	// 联网 + 同步：node2 发现 node1 链更长，整链替换（reorg）
	n1.SetPeers([]string{"localhost:8002"})
	n2.SetPeers([]string{"localhost:8001"})
	n1.syncFromPeers()
	n2.syncFromPeers()

	fmt.Println("\n=== 共识后：最长链胜出 ===")
	fmt.Println("node1 链长:", chainLen("http://localhost:8001/chain"),
		" node2 链长:", chainLen("http://localhost:8002/chain"))
	fmt.Println("node1 尾块:", tailHash("http://localhost:8001/chain"))
	fmt.Println("node2 尾块:", tailHash("http://localhost:8002/chain"))

	fmt.Println("\n=== 余额账本（minb 的孤立块奖励已作废）===")
	printBalances("node1", "http://localhost:8001/balances")
	printBalances("node2", "http://localhost:8002/balances")
}

// runNode 单个可配置节点：起服务、等待 Ctrl+C、优雅退出。
func runNode(cfg Config) {
	bc, err := NewBlockchain(cfg.Difficulty, cfg.DataPath)
	if err != nil {
		log.Fatalf("建链失败: %v", err)
	}
	defer bc.Close()
	log.Printf("链已就绪，区块数: %d", len(bc.blocks))

	srv := NewServer(bc, cfg.Port, cfg.Peers)
	go func() {
		if err := srv.Listen(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("节点启动失败: %v", err)
		}
	}()

	// 阻塞等 Ctrl+C 或 kill 信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("收到退出信号，正在优雅关闭...")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("关闭失败: %v", err)
		return
	}
	log.Println("已优雅退出")
}

// chainLen 请求某节点的链，返回区块数。
func chainLen(url string) int {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("请求失败:", url, err)
		return -1
	}
	defer resp.Body.Close()
	var blocks []Block
	if err := json.NewDecoder(resp.Body).Decode(&blocks); err != nil {
		fmt.Println("解析失败:", url, err)
		return -1
	}
	return len(blocks)
}

// printBalances 请求某节点的余额账本并打印。地址太长，只显示前 8 位 + 后 6 位。
func printBalances(name, url string) {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println(name, "查询失败:", err)
		return
	}
	defer resp.Body.Close()
	var m map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		fmt.Println(name, "解析失败:", err)
		return
	}
	if len(m) == 0 {
		fmt.Printf("%s: 暂无余额\n", name)
		return
	}
	fmt.Printf("%s:\n", name)
	for addr, bal := range m {
		fmt.Printf("  %s…%s = %d\n", addr[:8], addr[len(addr)-6:], bal)
	}
}

// mineAt 请求某节点的 /mine 接口挖一块（矿工得铸币奖励）。
func mineAt(url string) {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("挖矿失败:", url, err)
		return
	}
	resp.Body.Close()
}

// tailHash 请求某节点的链，返回末尾区块哈希的短前缀（用于对比两条链是否分家）。
func tailHash(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return "请求失败"
	}
	defer resp.Body.Close()
	var blocks []Block
	if err := json.NewDecoder(resp.Body).Decode(&blocks); err != nil {
		return "解析失败"
	}
	if len(blocks) == 0 {
		return ""
	}
	h := blocks[len(blocks)-1].Hash
	if len(h) > 12 {
		return h[:12] + "…"
	}
	return h
}
