package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"
)

// LoadDotenvResult 描述查找 .env 的全过程，方便启动时打日志诊断。
type LoadDotenvResult struct {
	Loaded     bool     // 是否成功加载
	Path       string   // 实际加载的 .env 路径（未加载时为空）
	CWD        string   // 进程启动时的工作目录
	SourceFile string   // 本源码文件路径（兜底锚点）
	Candidates []string // 依次检查过的 .env 路径
	Err        error    // 找到文件但 parse 失败
}

// LoadDotenv 自动加载 .env。优先级：
//  1. $CWD/.env
//  2. 向上递归最多 6 层
//  3. 本源码文件向上找（兜底，go run / IDE run CWD 异常时救命）
//
// 行为：用 godotenv.Overload —— .env 总是覆盖 shell 里残留的旧 env。
// 这是 dev 友好的取舍：prod 部署不会带 .env 文件，所以无副作用；
// dev 环境如果 shell 里残留上一次会话的 export，.env 会强制接管。
func LoadDotenv() LoadDotenvResult {
	res := LoadDotenvResult{}

	if cwd, err := os.Getwd(); err == nil {
		res.CWD = cwd
	}

	if _, src, _, ok := runtime.Caller(0); ok {
		res.SourceFile = src
	}

	// 生成候选路径列表
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			res.Candidates = append(res.Candidates, p)
		}
	}

	// 1. CWD 向上
	if res.CWD != "" {
		dir := res.CWD
		for i := 0; i < 7; i++ {
			add(filepath.Join(dir, ".env"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// 2. 源码位置向上（兜底）
	if res.SourceFile != "" {
		dir := filepath.Dir(res.SourceFile)
		for i := 0; i < 7; i++ {
			add(filepath.Join(dir, ".env"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// 3. 依次尝试加载（Overload：.env 覆盖 shell 残留 env）
	for _, p := range res.Candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := godotenv.Overload(p); err != nil {
			res.Err = fmt.Errorf("parse %s: %w", p, err)
			return res
		}
		res.Loaded = true
		res.Path = p
		return res
	}

	return res
}
